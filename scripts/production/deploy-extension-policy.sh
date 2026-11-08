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
export GATEWAY_NAME="${GATEWAY_NAME:-envoy-ai-gateway}"
export EPP_SERVICE_NAME="${EPP_SERVICE_NAME:-oscar-inferencemesh-epp}"
export EPP_GRPC_PORT="${EPP_GRPC_PORT:-9002}"
export EXTENSION_POLICY_NAME="${EXTENSION_POLICY_NAME:-envoy-ai-gateway-epp-ext-proc}"

POLICY_YAML="$REPO_ROOT/deploy/gateways/envoy-extension-policy.yaml"

DELETE_MODE=false
while [[ $# -gt 0 ]]; do
    case "$1" in
        --delete | --clean)
            DELETE_MODE=true
            shift
            ;;
        --help | -h)
            echo "Usage: $0 [OPTIONS]"
            echo ""
            echo "Applies or removes the EnvoyExtensionPolicy (ext_proc gRPC filter - Patrón B)."
            echo ""
            echo "Options:"
            echo "  --delete, --clean   Delete the EnvoyExtensionPolicy from the cluster"
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
    log_step "Deleting EnvoyExtensionPolicy '$EXTENSION_POLICY_NAME' in namespace '$NAMESPACE'..."
    kubectl delete envoyextensionpolicy "$EXTENSION_POLICY_NAME" -n "$NAMESPACE" --ignore-not-found=true
    log_info "EnvoyExtensionPolicy deleted successfully."
    exit 0
fi

log_step "Applying EnvoyExtensionPolicy (Patrón B: ext_proc gRPC on port $EPP_GRPC_PORT)..."
if [ ! -f "$POLICY_YAML" ]; then
    log_err "Policy manifest not found: $POLICY_YAML"
    exit 1
fi

envsubst < "$POLICY_YAML" | kubectl apply -f -
log_info "EnvoyExtensionPolicy '$EXTENSION_POLICY_NAME' successfully configured."
kubectl get envoyextensionpolicy "$EXTENSION_POLICY_NAME" -n "$NAMESPACE"
