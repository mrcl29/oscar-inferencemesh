#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)

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

ENVOY_GATEWAY_VERSION="${ENVOY_GATEWAY_VERSION:-1.9.3}"
AI_GATEWAY_VERSION="${AI_GATEWAY_VERSION:-1.2.0}"

UNINSTALL=false
while [[ $# -gt 0 ]]; do
    case "$1" in
        --uninstall | --clean)
            UNINSTALL=true
            shift
            ;;
        --help | -h)
            echo "Usage: $0 [OPTIONS]"
            echo ""
            echo "Deploys Envoy Gateway and Envoy AI Gateway platform infrastructure via Helm."
            echo ""
            echo "Options:"
            echo "  --uninstall, --clean   Uninstall Envoy Gateway and Envoy AI Gateway releases"
            echo "  --help, -h             Display this help message"
            exit 0
            ;;
        *)
            log_err "Unknown option: $1"
            exit 1
            ;;
    esac
done

if ! command -v helm &> /dev/null; then
    log_err "helm not found in PATH. Please install Helm first."
    exit 1
fi
if ! command -v kubectl &> /dev/null; then
    log_err "kubectl not found in PATH."
    exit 1
fi

if [ "$UNINSTALL" = true ]; then
    log_step "Uninstalling Envoy AI Gateway and Envoy Gateway..."

    if helm status aieg -n envoy-ai-gateway-system &> /dev/null; then
        log_info "Uninstalling Envoy AI Gateway controller Helm release..."
        helm uninstall aieg -n envoy-ai-gateway-system || true
    fi

    if helm status aieg-crd -n envoy-ai-gateway-system &> /dev/null; then
        log_info "Uninstalling Envoy AI Gateway CRD Helm release..."
        helm uninstall aieg-crd -n envoy-ai-gateway-system || true
    fi

    if helm status eg -n envoy-gateway-system &> /dev/null; then
        log_info "Uninstalling Envoy Gateway Helm release..."
        helm uninstall eg -n envoy-gateway-system || true
    fi

    log_info "Cleaning up namespaces..."
    kubectl delete namespace envoy-ai-gateway-system --ignore-not-found=true
    kubectl delete namespace envoy-gateway-system --ignore-not-found=true

    log_info "Envoy Gateway and Envoy AI Gateway uninstalled successfully."
    exit 0
fi

# Pre-flight check: ensure Kubernetes API server is reachable with retries
MAX_RETRIES="${K8S_MAX_RETRIES:-15}"
RETRY_DELAY="${K8S_RETRY_DELAY:-2}"
k8s_ready=false

for ((i = 1; i <= MAX_RETRIES; i++)); do
    if kubectl cluster-info --request-timeout=3s &> /dev/null; then
        k8s_ready=true
        break
    fi
    log_warn "Kubernetes API server not ready yet. Waiting (attempt $i/$MAX_RETRIES)..."
    sleep "$RETRY_DELAY"
done

if [ "$k8s_ready" = false ]; then
    log_err "Kubernetes cluster is unreachable. Please verify that your cluster is running ('kind get clusters' or 'docker ps') and that your current kubeconfig context is valid."
    exit 1
fi

# Idempotency check: are both deployments already running and configured with Backend API?
eg_ready=false
aieg_ready=false
backend_enabled=false

if kubectl -n envoy-gateway-system wait --for=condition=Available deployment/envoy-gateway --timeout=1s &> /dev/null; then
    eg_ready=true
fi
if kubectl -n envoy-ai-gateway-system wait --for=condition=Available deployment/ai-gateway-controller --timeout=1s &> /dev/null; then
    aieg_ready=true
fi
if kubectl -n envoy-gateway-system get configmap envoy-gateway-config -o jsonpath='{.data.envoy-gateway\.yaml}' 2> /dev/null | grep -q "enableBackend: true"; then
    backend_enabled=true
fi

if [ "$eg_ready" = true ] && [ "$aieg_ready" = true ] && [ "$backend_enabled" = true ]; then
    log_info "Envoy Gateway and Envoy AI Gateway are already deployed and Available. Skipping installation."
    exit 0
fi

# 1. Deploy Envoy Gateway
log_step "1. Deploying Envoy Gateway (v${ENVOY_GATEWAY_VERSION})..."
helm upgrade -i eg oci://docker.io/envoyproxy/gateway-helm \
    --version "$ENVOY_GATEWAY_VERSION" \
    --namespace envoy-gateway-system \
    --create-namespace \
    --force-conflicts \
    --set config.envoyGateway.extensionApis.enableBackend=true \
    --set config.envoyGateway.extensionApis.enableEnvoyPatchPolicy=true

log_info "Waiting for Envoy Gateway controller rollout..."
kubectl -n envoy-gateway-system wait --for=condition=Available deployment/envoy-gateway --timeout=180s
log_info "Envoy Gateway is Available."

# 2. Deploy Envoy AI Gateway CRDs
log_step "2. Deploying Envoy AI Gateway CRDs (v${AI_GATEWAY_VERSION})..."
helm upgrade -i aieg-crd oci://docker.io/envoyproxy/ai-gateway-crds-helm \
    --version "$AI_GATEWAY_VERSION" \
    --namespace envoy-ai-gateway-system \
    --create-namespace \
    --force-conflicts

# 3. Deploy Envoy AI Gateway Controller
log_step "3. Deploying Envoy AI Gateway Controller (v${AI_GATEWAY_VERSION})..."
helm upgrade -i aieg oci://docker.io/envoyproxy/ai-gateway-helm \
    --version "$AI_GATEWAY_VERSION" \
    --namespace envoy-ai-gateway-system \
    --create-namespace \
    --force-conflicts

log_info "Waiting for Envoy AI Gateway controller rollout..."
kubectl -n envoy-ai-gateway-system wait --for=condition=Available deployment/ai-gateway-controller --timeout=180s
log_info "Envoy AI Gateway controller is Available."

log_info "Envoy Gateway and Envoy AI Gateway deployment completed successfully."
