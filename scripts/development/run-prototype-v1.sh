#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd "$SCRIPT_DIR/../.." && pwd)

# shellcheck source=utils/logger.sh
source "$SCRIPT_DIR/utils/logger.sh"

# CLI Options & Flag Parsing
CLEAN_MODE=false
CLEAN_GATEWAY=false
POSITIONAL_ARGS=()

while [[ $# -gt 0 ]]; do
    case "$1" in
        --clean|-c)
            CLEAN_MODE=true
            shift
            ;;
        --clean-all)
            CLEAN_MODE=true
            CLEAN_GATEWAY=true
            shift
            ;;
        --help|-h)
            echo "Usage: $0 [OPTIONS] [MODEL_YAML] [SERVICE_YAML] [GATEWAY_YAML] [ROUTE_YAML]"
            echo ""
            echo "Options:"
            echo "  --clean, -c      Tear down and clean resources for the model (releases RAM & pods)"
            echo "  --clean-all      Tear down all resources including the Envoy AI Gateway"
            echo "  --help, -h       Show this help message"
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

# 0. Configurable manifest inputs (independent, generic templates)
MODEL_YAML="${1:-$REPO_ROOT/examples/inference-models/qwen.yml}"
SERVICE_YAML="${2:-$REPO_ROOT/examples/inference-services/llminferenceservice.yaml}"
GATEWAY_YAML="${3:-$REPO_ROOT/examples/gateways/envoy-ai-gateway.yaml}"
ROUTE_YAML="${4:-$REPO_ROOT/examples/gateways/aigateway-route.yaml}"

log_step "Configuration Summary"
log_info "Mode:             $([ "$CLEAN_MODE" = true ] && echo "TEARDOWN (--clean)" || echo "DEPLOY & TEST")"
log_info "Model Manifest:   $MODEL_YAML"
log_info "Service Manifest: $SERVICE_YAML"
log_info "Gateway Manifest: $GATEWAY_YAML"
log_info "Route Manifest:   $ROUTE_YAML"

for f in "$MODEL_YAML" "$SERVICE_YAML" "$GATEWAY_YAML" "$ROUTE_YAML"; do
    if [ ! -f "$f" ]; then
        log_err "Required manifest file not found: $f"
        exit 1
    fi
done

# Check required CLI tools
if ! command -v envsubst &>/dev/null; then
    log_err "envsubst not found. Please install gettext-base / gettext."
    exit 1
fi
if ! command -v kubectl &>/dev/null; then
    log_err "kubectl not found in PATH."
    exit 1
fi

# 1. Dynamically parse model metadata from the Model Manifest
log_step "1. Dynamically extracting model metadata from $MODEL_YAML"
EXTRACTED_VARS=$(python3 "$SCRIPT_DIR/utils/parse_model_manifest.py" "$MODEL_YAML")
eval "$EXTRACTED_VARS"

# 2. Export environment variables for dynamic manifest orchestration
export NAMESPACE="${NAMESPACE:-oscar-svc}"
export MODEL_NAME="${MODEL_NAME:-$PARSED_MODEL_NAME}"
export SERVICE_NAME="${SERVICE_NAME:-$MODEL_NAME}"
export MODEL_STORAGE_URI="${MODEL_STORAGE_URI:-$PARSED_STORAGE_URI}"
export RUNTIME_IMAGE="${RUNTIME_IMAGE:-$PARSED_RUNTIME_IMAGE}"
export MAX_MODEL_LEN="${MAX_MODEL_LEN:-$PARSED_MAX_MODEL_LEN}"
export VLLM_CPU_KVCACHE_SPACE="${VLLM_CPU_KVCACHE_SPACE:-$PARSED_KVCACHE_SPACE}"
export REPLICAS="${REPLICAS:-1}"

# Sane worker limits tuned for 14GB RAM hosts
export WORKER_CPU_LIMIT="${WORKER_CPU_LIMIT:-2}"
export WORKER_MEMORY_LIMIT="${WORKER_MEMORY_LIMIT:-5.5Gi}"
export WORKER_CPU_REQUEST="${WORKER_CPU_REQUEST:-250m}"
export WORKER_MEMORY_REQUEST="${WORKER_MEMORY_REQUEST:-800Mi}"

# Dynamic Gateway & Route wiring
export GATEWAY_NAME="${GATEWAY_NAME:-envoy-ai-gateway}"
export GATEWAY_NAMESPACE="${GATEWAY_NAMESPACE:-oscar-svc}"
export GATEWAY_PORT="${GATEWAY_PORT:-8080}"

export TARGET_BACKEND_HOST="${SERVICE_NAME}-kserve-workload-svc.${NAMESPACE}.svc.cluster.local"
export TARGET_BACKEND_PORT="${TARGET_BACKEND_PORT:-8000}"
export BACKEND_NAME="${SERVICE_NAME}-eg-backend"
export AI_SERVICE_BACKEND_NAME="${SERVICE_NAME}-backend"
export ROUTE_NAME="${SERVICE_NAME}-ai-route"
export ROUTE_RULE_NAME="${SERVICE_NAME}-inference-rule"

log_info "Dynamic Context Resolved:"
log_info "  - Model Name:       $MODEL_NAME"
log_info "  - Namespace:        $NAMESPACE"
log_info "  - Target FQDN:      $TARGET_BACKEND_HOST:$TARGET_BACKEND_PORT"
log_info "  - Gateway:          $GATEWAY_NAME.$GATEWAY_NAMESPACE:$GATEWAY_PORT"
log_info "  - Route Name:       $ROUTE_NAME"
log_info "  - Storage URI:      $MODEL_STORAGE_URI"

# ==================== TEARDOWN MODE (--clean) ====================
if [ "$CLEAN_MODE" = true ]; then
    log_step "Teardown Mode (--clean): Cleaning up resources for model '$MODEL_NAME'"

    # 1. Delete AI Gateway Route and Backends (stops incoming traffic)
    log_info "1. Deleting AI Gateway Route and Backends..."
    kubectl delete aigatewayroute "$ROUTE_NAME" -n "$NAMESPACE" --ignore-not-found=true
    kubectl delete aiservicebackend "$AI_SERVICE_BACKEND_NAME" -n "$NAMESPACE" --ignore-not-found=true
    kubectl delete backend "$BACKEND_NAME" -n "$NAMESPACE" --ignore-not-found=true

    # 2. Delete LLMInferenceService (terminates vLLM worker pods & EPP scheduler)
    log_info "2. Deleting LLMInferenceService '$SERVICE_NAME'..."
    kubectl delete llminferenceservice "$SERVICE_NAME" -n "$NAMESPACE" --ignore-not-found=true

    # 3. Unregister from OSCAR service catalog
    if command -v oscar-cli &>/dev/null; then
        log_info "3. Removing service '$MODEL_NAME' from OSCAR..."
        oscar-cli service remove "$MODEL_NAME" 2>/dev/null || true
    fi

    # 4. Optional: Clean Gateway infrastructure if requested with --clean-all
    if [ "$CLEAN_GATEWAY" = true ]; then
        log_info "4. Deleting Envoy AI Gateway infrastructure..."
        kubectl delete gateway "$GATEWAY_NAME" -n "$GATEWAY_NAMESPACE" --ignore-not-found=true
    else
        log_info "4. Keeping shared Envoy AI Gateway infrastructure intact (use --clean-all to delete)."
    fi

    echo ""
    log_info "Cleanup completed successfully. Model resources, pods, and memory have been released."
    exit 0
fi

# ==================== DEPLOYMENT & VERIFICATION MODE ====================

# 3. Deploy Gateway Infrastructure (Independent Gateway layer)
log_step "2. Dynamically deploying Envoy Gateway infrastructure"
envsubst < "$GATEWAY_YAML" | kubectl apply -f -

# 4. Deploy Model via OSCAR CLI (Independent Model layer)
log_step "3. Registering model in OSCAR"
if command -v oscar-cli &>/dev/null; then
    if oscar-cli service list 2>/dev/null | grep -q "$MODEL_NAME"; then
        log_info "OSCAR service '$MODEL_NAME' already registered."
    else
        log_info "Registering '$MODEL_NAME' with oscar-cli..."
        oscar-cli apply "$MODEL_YAML" || log_warn "oscar-cli apply returned non-zero, proceeding..."
    fi
else
    log_warn "oscar-cli not found, skipping OSCAR registration (Kubernetes direct mode)."
fi

# 5. Deploy LLMInferenceService & Router Scheduler (Independent Service layer)
log_step "4. Dynamically applying LLMInferenceService ($SERVICE_YAML)"
envsubst < "$SERVICE_YAML" | kubectl apply -f -

# 6. Relate Gateway and LLM Service via AI Gateway Route (Independent Route layer)
log_step "5. Dynamically wiring Envoy AI Gateway Route ($ROUTE_YAML)"
envsubst < "$ROUTE_YAML" | kubectl apply -f -

# 7. Wait for Router Scheduler (EPP) and vLLM Worker Pool Readiness
log_step "6. Waiting for llm-d Router Scheduler and vLLM workers readiness"
log_info "Waiting for router scheduler rollout..."
while ! kubectl -n "$NAMESPACE" get deployment "${SERVICE_NAME}-kserve-router-scheduler" &>/dev/null; do
    sleep 2
done
kubectl -n "$NAMESPACE" rollout status "deployment/${SERVICE_NAME}-kserve-router-scheduler" --timeout=120s

log_info "Waiting for vLLM inference worker pool rollout..."
while ! kubectl -n "$NAMESPACE" get deployment "${SERVICE_NAME}-kserve" &>/dev/null; do
    sleep 2
done
kubectl -n "$NAMESPACE" rollout status "deployment/${SERVICE_NAME}-kserve" --timeout=600s

log_info "Checking InferencePool and Route status..."
kubectl -n "$NAMESPACE" get inferencepool "${SERVICE_NAME}-inference-pool" || true
kubectl -n "$NAMESPACE" get aigatewayroute "$ROUTE_NAME" || true

# 8. Detect Envoy AI Gateway Endpoint
log_step "7. Resolving Envoy AI Gateway endpoint"
GATEWAY_SVC=$(kubectl get svc -n envoy-gateway-system -l "gateway.envoyproxy.io/owning-gateway-name=$GATEWAY_NAME" -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || echo "")

if [ -z "$GATEWAY_SVC" ]; then
    GATEWAY_SVC=$(kubectl get svc -n envoy-gateway-system -o jsonpath='{.items[?(@.spec.ports[0].port==8080)].metadata.name}' 2>/dev/null || echo "")
fi

if [ -z "$GATEWAY_SVC" ]; then
    log_err "Could not resolve Envoy AI Gateway service in namespace 'envoy-gateway-system'."
    exit 1
fi

GATEWAY_IP=$(kubectl get svc -n envoy-gateway-system "$GATEWAY_SVC" -o jsonpath='{.spec.clusterIP}')
GATEWAY_PORT=$(kubectl get svc -n envoy-gateway-system "$GATEWAY_SVC" -o jsonpath='{.spec.ports[0].port}')
log_info "Envoy AI Gateway is active at http://$GATEWAY_IP:$GATEWAY_PORT"

# 9. Execute Test Inference Request via curl
log_step "8. Sending test inference request for model: '$MODEL_NAME'"
PROMPT_PAYLOAD='{
  "model": "'"$MODEL_NAME"'",
  "messages": [
    {"role": "user", "content": "Write a short poem about clouds"}
  ],
  "max_tokens": 50
}'

log_info "Issuing HTTP POST to http://$GATEWAY_IP:$GATEWAY_PORT/v1/chat/completions..."
INFERENCE_RESPONSE=$(docker exec oscar-test-nzh-control-plane curl -s -X POST "http://$GATEWAY_IP:$GATEWAY_PORT/v1/chat/completions" \
  -H "Content-Type: application/json" \
  -H "x-ai-eg-model: $MODEL_NAME" \
  -d "$PROMPT_PAYLOAD" 2>/dev/null || echo "")

echo ""
echo "==================== GATEWAY INFERENCE RESPONSE ===================="
echo "$INFERENCE_RESPONSE"
echo "===================================================================="

# 10. Inspect llm-d Endpoint Picker (EPP) Decision Logs
log_step "9. Inspecting llm-d Endpoint Picker (EPP) router scheduler logs"
echo "--- EPP Router Scheduler Status & Recent Logs ---"
kubectl logs -n "$NAMESPACE" "deployment/${SERVICE_NAME}-kserve-router-scheduler" -c main --tail=20 || true
echo "--------------------------------------------------"

echo ""
log_info "Prototype execution completed successfully."
