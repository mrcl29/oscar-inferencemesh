#!/usr/bin/env bash
set -euo pipefail

# Disable post-quantum key agreement in Go to avoid connection resets with Kind/docker-proxy
export GODEBUG="tlsmlkem=0"

GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

log_info() { echo -e "${GREEN}[INFO]${NC} $1"; }
log_warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }
log_err() { echo -e "${RED}[ERROR]${NC} $1"; }

# 1. Idempotency Check: Is an OSCAR cluster already running and healthy?
existing_cluster=$(kind get clusters 2>/dev/null | grep -E '^oscar-test-' | head -n 1 || echo "")
if [ -n "$existing_cluster" ]; then
    ctx="kind-$existing_cluster"
    log_info "Found existing cluster '$existing_cluster'. Checking health..."
    if kubectl cluster-info --context "$ctx" &>/dev/null; then
        if kubectl --context "$ctx" -n oscar get deployment oscar &>/dev/null; then
            ready_replicas=$(kubectl --context "$ctx" -n oscar get deployment oscar -o jsonpath='{.status.readyReplicas}' 2>/dev/null || echo "0")
            if [ "${ready_replicas:-0}" -ge 1 ]; then
                log_info "OSCAR is already deployed and ready in '$existing_cluster'. Skipping re-creation."
                kubectl config use-context "$ctx" &>/dev/null || true
                exit 0
            fi
        fi
    fi
    log_warn "Cluster '$existing_cluster' is not fully ready or broken. Cleaning it up before redeploying..."
    kind delete cluster --name "$existing_cluster" || true
fi

# Clean up any leftover stopped/orphaned containers to avoid port collisions
for c in $(docker ps -a --filter "name=oscar-test-" -q 2>/dev/null); do
    docker rm -f "$c" &>/dev/null || true
done

# 2. Clone or update OSCAR repository in /tmp
OSCAR_DIR="/tmp/oscar"
trap 'rm -rf "$OSCAR_DIR"' EXIT

if [ -d "$OSCAR_DIR/.git" ]; then
    log_info "Reusing existing OSCAR repository in $OSCAR_DIR..."
    git -C "$OSCAR_DIR" pull --ff-only 2>/dev/null || true
elif [ -d "$OSCAR_DIR" ]; then
    rm -rf "$OSCAR_DIR"
    git clone --depth 1 https://github.com/grycap/oscar.git "$OSCAR_DIR"
else
    log_info "Cloning OSCAR repository into $OSCAR_DIR..."
    git clone --depth 1 https://github.com/grycap/oscar.git "$OSCAR_DIR"
fi

# 3. Patch kind-deploy.sh for non-interactive execution, resilient apiserver check, and DNS fix
sed -i 's/SKIP_PROMPTS="false"/SKIP_PROMPTS="true"/' "$OSCAR_DIR/deploy/kind-deploy.sh"

python3 -c '
with open("'$OSCAR_DIR'/deploy/kind-deploy.sh", "r") as f:
    content = f.read()

# Inject DNS fix and retry loop in createKindCluster
target1 = """    if ! kubectl cluster-info --context "$KIND_CONTEXT" &> /dev/null; then
        echo -e "$RED[*]$END_COLOR Kind cluster not found."
        echo "Stopping execution ...."
        if [ -f $CONFIG_FILEPATH ]; then 
            rm $CONFIG_FILEPATH
        fi
        exit
    fi"""

replacement1 = """    echo "[*] Ensuring DNS resolver inside Kind node..."
    docker exec "$CLUSTER_NAME-control-plane" bash -c "echo -e \"nameserver 1.1.1.1\\nnameserver 8.8.8.8\" > /etc/resolv.conf" 2>/dev/null || true
    echo "[*] Waiting for Kubernetes control plane to be responsive..."
    local ready=false
    for i in $(seq 1 30); do
        if kubectl cluster-info --context "$KIND_CONTEXT" &> /dev/null; then
            ready=true
            break
        fi
        sleep 2
    done
    if [ "$ready" != "true" ]; then
        echo -e "$RED[*]$END_COLOR Kind cluster apiserver not responding after 60 seconds."
        [ -f "$CONFIG_FILEPATH" ] && rm "$CONFIG_FILEPATH"
        exit 1
    fi"""

if target1 in content:
    content = content.replace(target1, replacement1)

# Ensure Traefik status timeout fails with exit 1 instead of continuing
target2 = """            echo -e "\\n$RED[!]$END_COLOR Error: Timeout: Traefik pod status: $traefik_status"
            exit"""

replacement2 = """            echo -e "\\n$RED[!]$END_COLOR Error: Timeout: Traefik pod status: $traefik_status"
            exit 1"""

if target2 in content:
    content = content.replace(target2, replacement2)

with open("'$OSCAR_DIR'/deploy/kind-deploy.sh", "w") as f:
    f.write(content)
'

# 4. Deploy OSCAR using Kind
log_info "Running kind-deploy.sh (Traefik + KServe)..."
bash "$OSCAR_DIR/deploy/kind-deploy.sh" --traefik --kserve

# 5. Wait for OSCAR deployment to be fully Available
log_info "Verifying that OSCAR deployment is Available..."
kubectl -n oscar wait --for=condition=Available deployment/oscar --timeout=180s
log_info "OSCAR deployment verified successfully."
