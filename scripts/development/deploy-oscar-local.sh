#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)

# shellcheck source=utils/logger.sh
source "$SCRIPT_DIR/utils/logger.sh"

# 1. Idempotency Check: Is an OSCAR cluster already running and healthy?
existing_cluster=$(kind get clusters 2> /dev/null | grep -E '^oscar-test-' | head -n 1 || echo "")
if [ -n "$existing_cluster" ]; then
    ctx="kind-$existing_cluster"
    log_info "Found existing cluster '$existing_cluster'. Checking health..."
    if kubectl cluster-info --context "$ctx" &> /dev/null; then
        if kubectl --context "$ctx" -n oscar get deployment oscar &> /dev/null; then
            ready_replicas=$(kubectl --context "$ctx" -n oscar get deployment oscar -o jsonpath='{.status.readyReplicas}' 2> /dev/null || echo "0")
            if [ "${ready_replicas:-0}" -ge 1 ]; then
                log_info "OSCAR is already deployed and ready in '$existing_cluster'. Skipping re-creation."
                kubectl config use-context "$ctx" &> /dev/null || true
                bash "$SCRIPT_DIR/deploy-envoy-ai-gateway.sh"
                exit 0
            fi
        fi
    fi
    log_warn "Cluster '$existing_cluster' is not fully ready or broken. Cleaning it up before redeploying..."
    kind delete cluster --name "$existing_cluster" || true
fi

# Clean up any leftover stopped/orphaned containers to avoid port collisions
for c in $(docker ps -a --filter "name=oscar-test-" -q 2> /dev/null); do
    docker rm -f "$c" &> /dev/null || true
done

# 2. Clone or update OSCAR repository in /tmp
OSCAR_DIR="/tmp/oscar"
trap 'rm -rf "$OSCAR_DIR"' EXIT

if [ -d "$OSCAR_DIR/.git" ]; then
    log_info "Reusing existing OSCAR repository in $OSCAR_DIR..."
    git -C "$OSCAR_DIR" pull --ff-only 2> /dev/null || true
elif [ -d "$OSCAR_DIR" ]; then
    rm -rf "$OSCAR_DIR"
    git clone --depth 1 https://github.com/grycap/oscar.git "$OSCAR_DIR"
else
    log_info "Cloning OSCAR repository into $OSCAR_DIR..."
    git clone --depth 1 https://github.com/grycap/oscar.git "$OSCAR_DIR"
fi

# 3. Patch kind-deploy.sh for non-interactive execution, resilient apiserver check, and DNS fix
sed -i 's/SKIP_PROMPTS="false"/SKIP_PROMPTS="true"/' "$OSCAR_DIR/deploy/kind-deploy.sh"

python3 "$SCRIPT_DIR/utils/patch_kind_deploy.py" "$OSCAR_DIR/deploy/kind-deploy.sh"

# 4. Deploy OSCAR using Kind
log_info "Running kind-deploy.sh (Traefik + KServe)..."
bash "$OSCAR_DIR/deploy/kind-deploy.sh" --traefik --kserve

# 5. Wait for OSCAR and Traefik deployments to be fully Available
log_info "Verifying that OSCAR and Traefik deployments are Available..."
kubectl -n oscar wait --for=condition=Available deployment/oscar --timeout=180s
kubectl -n traefik wait --for=condition=Available deployment/traefik --timeout=180s 2> /dev/null || true
log_info "OSCAR and Traefik deployments verified successfully."

# 6. Deploy Envoy Gateway and Envoy AI Gateway platform infrastructure
bash "$SCRIPT_DIR/deploy-envoy-ai-gateway.sh"
