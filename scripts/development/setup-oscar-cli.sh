#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)

# shellcheck source=utils/logger.sh
source "$SCRIPT_DIR/utils/logger.sh"

# 1. Architecture detection
ARCH=$(uname -m)
case "$ARCH" in
    x86_64)  CLI_BIN="oscar-cli" ;;
    aarch64) CLI_BIN="oscar-cli-arm64" ;;
    arm64)   CLI_BIN="oscar-cli-arm64" ;;
    *)
        log_err "Unsupported architecture for oscar-cli: $ARCH"
        exit 1
        ;;
esac

# 2. Check or install oscar-cli binary
if command -v oscar-cli &>/dev/null && oscar-cli version &>/dev/null; then
    log_info "oscar-cli is already functioning: $(oscar-cli version 2>/dev/null | tr '\n' ' ')"
else
    log_info "Downloading latest oscar-cli ($ARCH)..."
    DOWNLOAD_URL="https://github.com/grycap/oscar-cli/releases/latest/download/${CLI_BIN}"
    
    # Target directory: ~/.local/bin (user space, does not require sudo)
    TARGET_DIR="$HOME/.local/bin"
    mkdir -p "$TARGET_DIR"
    
    curl -fsSL -o "$TARGET_DIR/oscar-cli" "$DOWNLOAD_URL"
    chmod +x "$TARGET_DIR/oscar-cli"
    
    # Ensure target directory is in current session PATH
    export PATH="$TARGET_DIR:$PATH"
    log_info "oscar-cli installed successfully in $TARGET_DIR/oscar-cli."
fi

# 3. Detect active OSCAR deployment credentials via kubectl
log_info "Detecting OSCAR cluster credentials via kubectl..."
if ! command -v kubectl &>/dev/null; then
    log_err "kubectl is required to automatically retrieve OSCAR cluster credentials."
    exit 1
fi

OSCAR_USER=$(kubectl -n oscar get deployment oscar -o jsonpath='{.spec.template.spec.containers[0].env[?(@.name=="OSCAR_USERNAME")].value}' 2>/dev/null || echo "oscar")
OSCAR_PASS=$(kubectl -n oscar get deployment oscar -o jsonpath='{.spec.template.spec.containers[0].env[?(@.name=="OSCAR_PASSWORD")].value}' 2>/dev/null || true)

if [ -z "$OSCAR_PASS" ]; then
    log_err "Could not automatically retrieve OSCAR_PASSWORD from deployment in namespace 'oscar'."
    log_err "Make sure your current kubectl context points to the active OSCAR cluster."
    exit 1
fi

OSCAR_ENDPOINT="http://localhost.direct"

# 4. Configure oscar-cli with the local cluster
log_info "Configuring cluster 'local' ($OSCAR_ENDPOINT)..."
oscar-cli cluster remove local &>/dev/null || true
oscar-cli cluster add local "$OSCAR_ENDPOINT" "$OSCAR_USER" "$OSCAR_PASS"
oscar-cli cluster default -s local

# 5. Check cluster health with retries
log_info "Testing cluster health (waiting for ingress route)..."

if ! getent hosts localhost.direct &>/dev/null; then
    log_warn "localhost.direct cannot be resolved via DNS. Consider adding '127.0.0.1 localhost.direct' to /etc/hosts."
fi

max_retries=30
counter=1
until oscar-cli health &>/dev/null; do
    if [ "$counter" -ge "$max_retries" ]; then
        echo ""
        log_err "Cluster health check failed after $max_retries attempts."
        oscar-cli health
        exit 1
    fi
    echo -n "."
    sleep 2
    counter=$((counter + 1))
done
echo ""
oscar-cli health

MINIO_PASS=$(kubectl -n minio get secret minio -o jsonpath='{.data.rootPassword}' 2>/dev/null | base64 -d 2>/dev/null || echo "")

# 6. Display credentials summary
echo ""
echo "============================================================"
echo "          OSCAR LOCAL CLUSTER READY                         "
echo "============================================================"
echo "OSCAR Endpoint:  $OSCAR_ENDPOINT"
echo "OSCAR Username:  $OSCAR_USER"
echo "OSCAR Password:  $OSCAR_PASS"
echo "------------------------------------------------------------"
if [ -n "$MINIO_PASS" ]; then
echo "MinIO Console:   http://localhost:30301"
echo "MinIO Username:  minio"
echo "MinIO Password:  $MINIO_PASS"
echo "------------------------------------------------------------"
fi
echo "Traefik UI:      http://localhost:9080/dashboard/"
echo "============================================================"
