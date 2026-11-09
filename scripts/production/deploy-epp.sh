#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd "$SCRIPT_DIR/../.." && pwd)

GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

log_info() { echo -e "${GREEN}[INFO]${NC} $1"; }
log_warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }
log_err() { echo -e "${RED}[ERROR]${NC} $1"; }
log_step() { echo -e "\n${CYAN}${BOLD}===> $1${NC}"; }

export NAMESPACE="${NAMESPACE:-oscar-svc}"
export MODEL_NAME="${MODEL_NAME:-qwen2-5-05b-instruct}"
export EPP_IMAGE="${EPP_IMAGE:-oscar-inferencemesh-epp:latest}"
export OSCAR_ENDPOINT="${OSCAR_ENDPOINT:-https://localhost.direct}"
export OSCAR_CLUSTER_ID="${OSCAR_CLUSTER_ID:-local}"
export OSCAR_VERIFY_SSL="${OSCAR_VERIFY_SSL:-false}"

EPP_YAML="$REPO_ROOT/src/internal/infrastructure/k8s/manifests/epp-deployment.yaml"
DOCKERFILE="$REPO_ROOT/deploy/Dockerfile.epp"

DELETE_MODE=false
NO_BUILD=false
FORCE="${FORCE:-false}"

while [[ $# -gt 0 ]]; do
    case "$1" in
        --delete | --clean)
            DELETE_MODE=true
            shift
            ;;
        --no-build)
            NO_BUILD=true
            shift
            ;;
        --force)
            FORCE=true
            shift
            ;;
        --help | -h)
            echo "Usage: $0 [OPTIONS]"
            echo ""
            echo "Builds and deploys the Endpoint Picker (EPP) container and Service in Kubernetes."
            echo ""
            echo "Options:"
            echo "  --force             Force rebuild and redeployment even if EPP is already running"
            echo "  --delete, --clean   Remove EPP Deployment and Service from the cluster"
            echo "  --no-build          Skip docker build (use existing image)"
            echo "  --help, -h          Display this help message"
            exit 0
            ;;
        *)
            log_err "Unknown option: $1"
            exit 1
            ;;
    esac
done

if ! command -v kubectl &> /dev/null; then
    log_err "kubectl not found in PATH."
    exit 1
fi

if [ "$DELETE_MODE" = true ]; then
    log_step "Deleting EPP Deployment and Service from namespace '$NAMESPACE'..."
    kubectl delete deployment oscar-inferencemesh-epp -n "$NAMESPACE" --ignore-not-found=true
    kubectl delete service oscar-inferencemesh-epp -n "$NAMESPACE" --ignore-not-found=true
    kubectl delete rolebinding oscar-inferencemesh-epp-rolebinding -n "$NAMESPACE" --ignore-not-found=true
    kubectl delete role oscar-inferencemesh-epp-role -n "$NAMESPACE" --ignore-not-found=true
    kubectl delete serviceaccount oscar-inferencemesh-epp -n "$NAMESPACE" --ignore-not-found=true
    log_info "EPP resources deleted successfully."
    exit 0
fi

# Idempotency check: is the EPP deployment already running and available?
if [ "$FORCE" = false ] && kubectl -n "$NAMESPACE" wait --for=condition=Available deployment/oscar-inferencemesh-epp --timeout=2s &> /dev/null; then
    log_info "EPP deployment is already running and Available in namespace '$NAMESPACE'. Skipping rebuild and redeployment."
    exit 0
fi

# 1. Build Docker image if not skipped
if [ "$NO_BUILD" = false ]; then
    if ! command -v docker &> /dev/null; then
        log_err "docker binary not found in PATH. Cannot build $EPP_IMAGE."
        exit 1
    fi
    log_step "1. Building EPP container image ($EPP_IMAGE)..."
    docker build -t "$EPP_IMAGE" -f "$DOCKERFILE" "$REPO_ROOT"
    log_info "Container image $EPP_IMAGE built successfully."

    # If running in a local Kind cluster, load the image directly into Kind nodes
    if command -v kind &> /dev/null; then
        KIND_CLUSTER=$(kind get clusters 2> /dev/null | grep -E '^oscar-test-' | head -n 1 || echo "")
        if [ -n "$KIND_CLUSTER" ]; then
            log_info "Kind cluster '$KIND_CLUSTER' detected. Loading $EPP_IMAGE into Kind..."
            kind load docker-image "$EPP_IMAGE" --name "$KIND_CLUSTER"
        fi
    fi
fi

# 2. Deploy EPP Deployment, Service, and RBAC
log_step "2. Applying EPP Deployment & Service manifests..."
if [ ! -f "$EPP_YAML" ]; then
    log_err "Manifest file not found: $EPP_YAML"
    exit 1
fi

envsubst < "$EPP_YAML" | kubectl apply -f -

log_info "Waiting for EPP rollout..."
kubectl -n "$NAMESPACE" rollout status deployment/oscar-inferencemesh-epp --timeout=120s
log_info "EPP is successfully deployed and running in namespace '$NAMESPACE'."
