#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd "$SCRIPT_DIR/../.." && pwd)

# shellcheck source=utils/logger.sh
source "$SCRIPT_DIR/utils/logger.sh"

# Ensure user-local binaries (e.g. oscar-cli installed by setup-oscar-cli.sh) are discoverable.
export PATH="$HOME/.local/bin:$PATH"

CLEAN_MODE=false
REPLICAS="${REPLICAS:-1}"
SYNC_EXISTING_RESOURCES="${SYNC_EXISTING_RESOURCES:-false}"
POSITIONAL_ARGS=()

while [[ $# -gt 0 ]]; do
    case "$1" in
        --clean | -c)
            CLEAN_MODE=true
            shift
            ;;
        --replicas | -r)
            REPLICAS="$2"
            shift 2
            ;;
        --help | -h)
            echo "Usage: $0 [OPTIONS] [MODEL_YAML] [SERVICE_YAML]"
            echo ""
            echo "Deploys an LLM model (default: qwen2-5-05b-instruct) in local OSCAR"
            echo "and KServe LLMInferenceService without deploying Envoy Gateway or routes."
            echo ""
            echo "Options:"
            echo "  --replicas, -r N   Number of vLLM worker pods (default: 1)"
            echo "  --clean, -c        Remove the LLMInferenceService and OSCAR service (releases RAM)"
            echo "  --help, -h         Show this help message"
            exit 0
            ;;
        *)
            POSITIONAL_ARGS+=("$1")
            shift
            ;;
    esac
done

if [[ ${#POSITIONAL_ARGS[@]} -gt 0 ]]; then
    set -- "${POSITIONAL_ARGS[@]}"
else
    set --
fi

MODEL_YAML="${1:-$REPO_ROOT/examples/inference-models/qwen.yml}"
SERVICE_YAML="${2:-$REPO_ROOT/examples/inference-services/llminferenceservice.yaml}"

for f in "$MODEL_YAML" "$SERVICE_YAML"; do
    if [ ! -f "$f" ]; then
        log_err "Required manifest file not found: $f"
        exit 1
    fi
done

for bin in envsubst kubectl python3 awk; do
    if ! command -v "$bin" &> /dev/null; then
        log_err "Required tool '$bin' not found in PATH."
        exit 1
    fi
done

if ! kubectl cluster-info --request-timeout=15s &> /dev/null; then
    log_err "Kubernetes cluster is unreachable. Verify Kind/kubectl context first."
    exit 1
fi

# 1. Dynamically parse model metadata from the Model Manifest
EXTRACTED_VARS=$(python3 "$SCRIPT_DIR/utils/parse_model_manifest.py" "$MODEL_YAML")
eval "$EXTRACTED_VARS"

export NAMESPACE="${NAMESPACE:-oscar-svc}"
export MODEL_NAME="${MODEL_NAME:-$PARSED_MODEL_NAME}"
export SERVICE_NAME="${SERVICE_NAME:-$MODEL_NAME}"
export MODEL_STORAGE_URI="${MODEL_STORAGE_URI:-$PARSED_STORAGE_URI}"
export RUNTIME_IMAGE="${RUNTIME_IMAGE:-$PARSED_RUNTIME_IMAGE}"
export MAX_MODEL_LEN="${MAX_MODEL_LEN:-$PARSED_MAX_MODEL_LEN}"
export VLLM_CPU_KVCACHE_SPACE="${VLLM_CPU_KVCACHE_SPACE:-$PARSED_KVCACHE_SPACE}"

# vLLM CPU (Qwen2.5-0.5B + 1Gi KV cache) consumes ~4.5 GiB anon RSS once loaded:
# - Defaults mirror qwen.yml (5.5Gi limit) so the worker pod never hits OOMKilled (exit 137)
#   and fits comfortably on a 15 GiB host with 1 replica.
# - For a single replica, allow up to 4 CPU cores by default to speed up vLLM CPU warmup
#   while keeping requests equal to the manifest baseline.
DEFAULT_CPU_LIMIT="$PARSED_CPU"
if [ "$REPLICAS" -eq 1 ] 2> /dev/null; then
    DEFAULT_CPU_LIMIT="4"
fi
export WORKER_CPU_LIMIT="${WORKER_CPU_LIMIT:-$DEFAULT_CPU_LIMIT}"
export WORKER_MEMORY_LIMIT="${WORKER_MEMORY_LIMIT:-$PARSED_MEMORY}"
export WORKER_CPU_REQUEST="${WORKER_CPU_REQUEST:-$PARSED_CPU}"
export WORKER_MEMORY_REQUEST="${WORKER_MEMORY_REQUEST:-$PARSED_MEMORY}"

# ==================== TEARDOWN MODE (--clean) ====================
if [ "$CLEAN_MODE" = true ]; then
    log_step "Cleaning up model '${MODEL_NAME}' from Kubernetes (${NAMESPACE}) and local OSCAR"
    kubectl delete llminferenceservice "$SERVICE_NAME" -n "$NAMESPACE" --ignore-not-found=true
    kubectl delete deployment "${SERVICE_NAME}-kserve" "${SERVICE_NAME}-kserve-router-scheduler" -n "$NAMESPACE" --ignore-not-found=true
    if command -v oscar-cli &> /dev/null; then
        oscar-cli service remove "$MODEL_NAME" 2> /dev/null || true
    fi
    log_info "Model resources, pods, and memory have been released."
    exit 0
fi

# Ensure KServe LLMInferenceService webhook is ready before applying
if kubectl -n kserve get deployment llmisvc-controller-manager &> /dev/null; then
    kubectl -n kserve wait --for=condition=Available deployment/llmisvc-controller-manager --timeout=60s
fi

MEM_AVAIL_MIB=$(awk '/MemAvailable:/ {printf "%d", $2/1024}' /proc/meminfo 2> /dev/null || echo "0")
log_step "Model Deployment Configuration"
log_info "Model Name:        $MODEL_NAME"
log_info "Namespace:         $NAMESPACE"
log_info "Replicas Target:   $REPLICAS"
log_info "Worker Requests:   cpu=$WORKER_CPU_REQUEST, memory=$WORKER_MEMORY_REQUEST"
log_info "Worker Limits:     cpu=$WORKER_CPU_LIMIT, memory=$WORKER_MEMORY_LIMIT (KV cache=${VLLM_CPU_KVCACHE_SPACE}Gi)"
log_info "Host MemAvailable: ${MEM_AVAIL_MIB} MiB"

# 2. Register Model via OSCAR CLI (same mechanism as run-prototype-v1.sh)
OSCAR_NEWLY_APPLIED=false
log_step "Registering model '${MODEL_NAME}' in local OSCAR"
if command -v oscar-cli &> /dev/null; then
    if oscar-cli service list 2> /dev/null | grep -q "$MODEL_NAME"; then
        log_info "OSCAR service '$MODEL_NAME' already registered."
    else
        log_info "Registering '$MODEL_NAME' with oscar-cli..."
        oscar-cli apply "$MODEL_YAML" || log_warn "oscar-cli apply returned non-zero, proceeding..."
        OSCAR_NEWLY_APPLIED=true
    fi
else
    log_warn "oscar-cli not found, skipping OSCAR registration (Kubernetes direct mode)."
fi

if [ "$SYNC_EXISTING_RESOURCES" = "true" ] && kubectl get llminferenceservice "$SERVICE_NAME" -n "$NAMESPACE" &> /dev/null; then
    EXISTING_CPU_LIMIT=$(kubectl get llminferenceservice "$SERVICE_NAME" -n "$NAMESPACE" -o jsonpath='{.spec.template.containers[0].resources.limits.cpu}' 2> /dev/null || echo "")
    EXISTING_MEM_LIMIT=$(kubectl get llminferenceservice "$SERVICE_NAME" -n "$NAMESPACE" -o jsonpath='{.spec.template.containers[0].resources.limits.memory}' 2> /dev/null || echo "")
    EXISTING_CPU_REQ=$(kubectl get llminferenceservice "$SERVICE_NAME" -n "$NAMESPACE" -o jsonpath='{.spec.template.containers[0].resources.requests.cpu}' 2> /dev/null || echo "")
    EXISTING_MEM_REQ=$(kubectl get llminferenceservice "$SERVICE_NAME" -n "$NAMESPACE" -o jsonpath='{.spec.template.containers[0].resources.requests.memory}' 2> /dev/null || echo "")

    [ -n "$EXISTING_CPU_LIMIT" ] && export WORKER_CPU_LIMIT="$EXISTING_CPU_LIMIT"
    [ -n "$EXISTING_MEM_LIMIT" ] && export WORKER_MEMORY_LIMIT="$EXISTING_MEM_LIMIT"
    [ -n "$EXISTING_CPU_REQ" ] && export WORKER_CPU_REQUEST="$EXISTING_CPU_REQ"
    [ -n "$EXISTING_MEM_REQ" ] && export WORKER_MEMORY_REQUEST="$EXISTING_MEM_REQ"
fi

# 3. Apply LLMInferenceService sequentially (1 replica first, then scale to target)
# so two heavy vLLM CPU warmup phases do not compete simultaneously for host RAM/CPU.
INITIAL_REPLICAS="$REPLICAS"
if [ "$REPLICAS" -gt 1 ]; then
    INITIAL_REPLICAS=1
fi

# OSCAR creates an initial LLMInferenceService without llminferenceservice.yaml's router/baseRefs,
# so applying llminferenceservice.yaml immediately after would trigger a 2nd ReplicaSet while the
# first pod is still warming up. Drain OSCAR's initial pod first via replicas=0 so only one
# vLLM pod ever boots.
if [ "$OSCAR_NEWLY_APPLIED" = true ]; then
    log_info "Syncing LLMInferenceService template at 0 replicas before starting worker..."
    REPLICAS=0 envsubst < "$SERVICE_YAML" | kubectl apply -f -
    TARGET_GEN=$(kubectl -n "$NAMESPACE" get llminferenceservice "$SERVICE_NAME" -o jsonpath='{.metadata.generation}' 2> /dev/null || echo "")
    for _ in $(seq 1 60); do
        OBS_GEN=$(kubectl -n "$NAMESPACE" get llminferenceservice "$SERVICE_NAME" -o jsonpath='{.status.observedGeneration}' 2> /dev/null || echo "0")
        DEPLOY_SPEC_REPLICAS=$(kubectl -n "$NAMESPACE" get deployment "${SERVICE_NAME}-kserve" -o jsonpath='{.spec.replicas}' 2> /dev/null || echo "")
        DEPLOY_STATUS_REPLICAS=$(kubectl -n "$NAMESPACE" get deployment "${SERVICE_NAME}-kserve" -o jsonpath='{.status.replicas}' 2> /dev/null || echo "0")
        POD_COUNT=$(kubectl -n "$NAMESPACE" get pods -l "app.kubernetes.io/name=${MODEL_NAME},app.kubernetes.io/component=llminferenceservice-workload" --no-headers 2> /dev/null | wc -l | tr -d ' ')
        if [ -n "$TARGET_GEN" ] && [ "${OBS_GEN:-0}" -ge "$TARGET_GEN" ] 2> /dev/null &&
            [ "${DEPLOY_SPEC_REPLICAS:-0}" -eq 0 ] 2> /dev/null &&
            [ "${DEPLOY_STATUS_REPLICAS:-0}" -eq 0 ] 2> /dev/null &&
            [ "${POD_COUNT:-0}" -eq 0 ]; then
            break
        fi
        sleep 1
    done
fi

log_step "Applying LLMInferenceService (${SERVICE_YAML}) with initial replicas=${INITIAL_REPLICAS}"
REPLICAS="$INITIAL_REPLICAS" envsubst < "$SERVICE_YAML" | kubectl apply -f -

log_info "Waiting for router scheduler rollout..."
while ! kubectl -n "$NAMESPACE" get deployment "${SERVICE_NAME}-kserve-router-scheduler" &> /dev/null; do
    sleep 2
done
kubectl -n "$NAMESPACE" rollout status "deployment/${SERVICE_NAME}-kserve-router-scheduler" --timeout=300s

log_info "Waiting for vLLM worker pod readiness..."
while ! kubectl -n "$NAMESPACE" get deployment "${SERVICE_NAME}-kserve" &> /dev/null; do
    sleep 2
done
kubectl -n "$NAMESPACE" rollout status "deployment/${SERVICE_NAME}-kserve" --timeout=600s

if [ "$REPLICAS" -gt 1 ]; then
    log_step "Scaling '${SERVICE_NAME}' to ${REPLICAS} replicas (sequentially after first pod is Ready)"
    REPLICAS="$REPLICAS" envsubst < "$SERVICE_YAML" | kubectl apply -f -
    kubectl -n "$NAMESPACE" rollout status "deployment/${SERVICE_NAME}-kserve" --timeout=600s
fi

log_step "Worker Pool Status & Memory Usage"
kubectl get pods -n "$NAMESPACE" -l "app.kubernetes.io/name=${MODEL_NAME}" -o wide
kubectl top pods -n "$NAMESPACE" -l "app.kubernetes.io/name=${MODEL_NAME}" 2> /dev/null || true
free -h
log_info "Model '${MODEL_NAME}' is Ready with ${REPLICAS} pod(s)."
