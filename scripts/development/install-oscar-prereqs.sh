#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)

# shellcheck source=utils/logger.sh
source "$SCRIPT_DIR/utils/logger.sh"

# Require root/sudo privileges
if [ "$EUID" -ne 0 ]; then
    log_err "Please run this script with sudo: sudo $0"
    exit 1
fi

# Fallback safely when neither SUDO_USER nor USER is set (e.g. minimal container environments)
ACTUAL_USER="${SUDO_USER:-${USER:-$(id -un 2>/dev/null || echo '')}}"

# 1. Architecture detection (amd64 / arm64)
ARCH=$(uname -m)
case "$ARCH" in
    x86_64)  BIN_ARCH="amd64" ;;
    aarch64) BIN_ARCH="arm64" ;;
    *)
        log_err "Unsupported architecture: $ARCH"
        exit 1
        ;;
esac
log_info "Detected architecture: $ARCH ($BIN_ARCH)"

# 2. Install Docker via detected package manager (capability detection)
if command -v docker &>/dev/null; then
    log_info "Docker is already installed."
else
    log_info "Installing Docker via package manager..."
    if command -v pacman &>/dev/null; then
        pacman -Sy --noconfirm curl ca-certificates docker
    elif command -v apt-get &>/dev/null; then
        apt-get update -y && apt-get install -y ca-certificates curl docker.io
    elif command -v dnf &>/dev/null; then
        dnf install -y curl ca-certificates moby-engine
    else
        log_err "No supported package manager found (pacman, apt-get, or dnf)."
        exit 1
    fi

    # Enable systemd service if running on a booted systemd host (skipped in standard containers)
    if [ -d /run/systemd/system ] && command -v systemctl &>/dev/null; then
        systemctl enable --now docker || true
    fi

    if [ -n "$ACTUAL_USER" ] && [ "$ACTUAL_USER" != "root" ]; then
        usermod -aG docker "$ACTUAL_USER" || true
        log_warn "Added user '$ACTUAL_USER' to docker group. Run 'newgrp docker' or re-login to apply changes."
    fi
fi

# Fix internal Docker DNS in daemon.json if it points to an unresponsive bridge IP
if [ -f /etc/docker/daemon.json ] && grep -q "172.17.0.1" /etc/docker/daemon.json 2>/dev/null; then
    log_warn "Fixing unreachable DNS in /etc/docker/daemon.json to [1.1.1.1, 8.8.8.8]..."
    sed -i 's/"dns":\s*\["172.17.0.1"\]/"dns": ["1.1.1.1", "8.8.8.8"]/' /etc/docker/daemon.json
    systemctl restart docker 2>/dev/null || true
fi

# Ensure curl is available for standalone binary downloads
if ! command -v curl &>/dev/null; then
    log_err "curl is required to download binaries but is not installed."
    exit 1
fi

# 3. Install kubectl matching OSCAR kindest/node version (v1.33.1)
TARGET_KUBECTL_VERSION="v1.33.1"
need_kubectl=false
if command -v kubectl &>/dev/null; then
    installed_ver=$(kubectl version --client 2>/dev/null | grep -oE 'v1\.[0-9]+' | head -n 1 || echo "")
    if [ "$installed_ver" != "v1.33" ]; then
        log_warn "Current kubectl version ($installed_ver) differs from OSCAR cluster ($TARGET_KUBECTL_VERSION). Updating..."
        need_kubectl=true
    else
        log_info "kubectl $TARGET_KUBECTL_VERSION is already installed."
    fi
else
    need_kubectl=true
fi

if [ "$need_kubectl" = true ]; then
    log_info "Installing kubectl $TARGET_KUBECTL_VERSION ($BIN_ARCH)..."
    curl -fsSL -o /tmp/kubectl "https://dl.k8s.io/release/${TARGET_KUBECTL_VERSION}/bin/linux/${BIN_ARCH}/kubectl"
    install -o root -g root -m 0755 /tmp/kubectl /usr/local/bin/kubectl
    rm -f /tmp/kubectl
    log_info "kubectl installed to /usr/local/bin/kubectl."
fi

# 4. Install Kind (official release binary)
if command -v kind &>/dev/null; then
    log_info "Kind is already installed."
else
    log_info "Installing Kind ($BIN_ARCH)..."
    KIND_VERSION="v0.27.0"
    curl -fsSL -o /tmp/kind "https://kind.sigs.k8s.io/dl/${KIND_VERSION}/kind-linux-${BIN_ARCH}"
    install -o root -g root -m 0755 /tmp/kind /usr/local/bin/kind
    rm -f /tmp/kind
    log_info "Kind installed."
fi

# 5. Install Helm (official installer script)
if command -v helm &>/dev/null; then
    log_info "Helm is already installed."
else
    log_info "Installing Helm..."
    curl -fsSL https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-3 | bash
    log_info "Helm installed."
fi

# 6. Ensure localhost.direct resolves locally without relying on external DNS
if ! grep -qE '\blocalhost\.direct\b' /etc/hosts; then
    log_info "Adding localhost.direct mapping to /etc/hosts..."
    echo "127.0.0.1 localhost.direct" >> /etc/hosts
fi

log_info "All tools are verified and ready to use."
