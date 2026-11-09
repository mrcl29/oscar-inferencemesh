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

CHECK_ONLY=false
WITH_DOCKER=false
WITH_KIND=false
KUBECTL_VERSION="${KUBECTL_VERSION:-v1.31.2}"
KIND_VERSION="${KIND_VERSION:-v0.27.0}"

while [[ $# -gt 0 ]]; do
    case "$1" in
        --check-only | --check)
            CHECK_ONLY=true
            shift
            ;;
        --with-docker)
            WITH_DOCKER=true
            shift
            ;;
        --with-kind)
            WITH_KIND=true
            shift
            ;;
        --help | -h)
            echo "Usage: $0 [OPTIONS]"
            echo ""
            echo "Installs system dependencies required to run the oscar-im binary and manage Envoy AI Gateway."
            echo ""
            echo "Core Tools Installed:"
            echo "  - kubectl         Kubernetes control plane CLI (default: $KUBECTL_VERSION)"
            echo "  - helm            Kubernetes package manager (v3+)"
            echo "  - envsubst        Environment variable substitution tool (gettext)"
            echo "  - curl            HTTP client for downloads and API verification"
            echo "  - ca-certificates SSL root certificates for secure OCI and HTTPS registries"
            echo ""
            echo "Options:"
            echo "  --check-only      Inspect installed dependencies and versions without modifying system"
            echo "  --with-docker     Also install and configure the Docker Engine daemon"
            echo "  --with-kind       Also install Kubernetes in Docker (kind) CLI"
            echo "  --help, -h        Display this help message"
            exit 0
            ;;
        *)
            log_err "Unknown option: $1"
            echo "Run '$0 --help' for usage."
            exit 1
            ;;
    esac
done

# Architecture detection
ARCH=$(uname -m)
case "$ARCH" in
    x86_64) BIN_ARCH="amd64" ;;
    aarch64) BIN_ARCH="arm64" ;;
    *)
        log_err "Unsupported architecture: $ARCH (requires x86_64 or aarch64)"
        exit 1
        ;;
esac

# Privilege helper (sudo if non-root)
SUDO=""
if [ "$EUID" -ne 0 ]; then
    if command -v sudo &> /dev/null; then
        SUDO="sudo"
    else
        log_err "This script requires root or sudo privileges to install binaries in /usr/local/bin and packages."
        exit 1
    fi
fi

ACTUAL_USER="${SUDO_USER:-${USER:-$(id -un 2> /dev/null || echo '')}}"

# Package manager detection
PKG_MANAGER=""
if command -v apt-get &> /dev/null; then
    PKG_MANAGER="apt"
elif command -v pacman &> /dev/null; then
    PKG_MANAGER="pacman"
elif command -v dnf &> /dev/null; then
    PKG_MANAGER="dnf"
elif command -v zypper &> /dev/null; then
    PKG_MANAGER="zypper"
fi

# Diagnostic mode
if [ "$CHECK_ONLY" = true ]; then
    log_step "Dependency Verification Report"
    printf "%-18s %-12s %-30s\n" "TOOL" "STATUS" "DETAILS"
    printf "%-18s %-12s %-30s\n" "------------------" "------------" "------------------------------"

    # kubectl
    if command -v kubectl &> /dev/null; then
        k_ver=$(kubectl version --client -o json 2> /dev/null | grep -o '"gitVersion": "[^"]*"' | cut -d'"' -f4 || kubectl version --client 2> /dev/null | head -n1)
        printf "%-18s \033[0;32m%-12s\033[0m %-30s\n" "kubectl" "FOUND" "$k_ver"
    else
        printf "%-18s \033[0;31m%-12s\033[0m %-30s\n" "kubectl" "MISSING" "Required for k8s management"
    fi

    # helm
    if command -v helm &> /dev/null; then
        h_ver=$(helm version --template '{{.Version}}' 2> /dev/null || echo "installed")
        printf "%-18s \033[0;32m%-12s\033[0m %-30s\n" "helm" "FOUND" "$h_ver"
    else
        printf "%-18s \033[0;31m%-12s\033[0m %-30s\n" "helm" "MISSING" "Required for Envoy Gateway"
    fi

    # envsubst
    if command -v envsubst &> /dev/null; then
        e_ver=$(envsubst --version 2> /dev/null | head -n1 || echo "installed")
        printf "%-18s \033[0;32m%-12s\033[0m %-30s\n" "envsubst" "FOUND" "$e_ver"
    else
        printf "%-18s \033[0;31m%-12s\033[0m %-30s\n" "envsubst" "MISSING" "Required for manifest templating"
    fi

    # curl
    if command -v curl &> /dev/null; then
        c_ver=$(curl --version 2> /dev/null | head -n1 | cut -d' ' -f1-2)
        printf "%-18s \033[0;32m%-12s\033[0m %-30s\n" "curl" "FOUND" "$c_ver"
    else
        printf "%-18s \033[0;31m%-12s\033[0m %-30s\n" "curl" "MISSING" "Required for HTTP downloads"
    fi

    # docker
    if command -v docker &> /dev/null; then
        d_ver=$(docker --version 2> /dev/null | cut -d',' -f1)
        printf "%-18s \033[0;32m%-12s\033[0m %-30s\n" "docker" "FOUND" "$d_ver"
    else
        printf "%-18s \033[1;33m%-12s\033[0m %-30s\n" "docker" "NOT FOUND" "Optional (needed for --docker build)"
    fi

    # kind
    if command -v kind &> /dev/null; then
        kind_ver=$(kind version 2> /dev/null || echo "installed")
        printf "%-18s \033[0;32m%-12s\033[0m %-30s\n" "kind" "FOUND" "$kind_ver"
    else
        printf "%-18s \033[1;33m%-12s\033[0m %-30s\n" "kind" "NOT FOUND" "Optional (for local development)"
    fi

    echo ""
    exit 0
fi

log_step "Installing OSCAR-IM System Dependencies ($ARCH)..."

# 1. Base tools: curl, ca-certificates, gettext (envsubst)
log_step "1. Verifying and installing system utilities (curl, gettext/envsubst)..."
if ! command -v envsubst &> /dev/null || ! command -v curl &> /dev/null; then
    if [ -z "$PKG_MANAGER" ]; then
        log_err "No supported package manager found (apt, pacman, dnf, zypper). Please install 'curl' and 'gettext' manually."
        exit 1
    fi

    log_info "Installing base packages via $PKG_MANAGER..."
    case "$PKG_MANAGER" in
        apt)
            $SUDO apt-get update -y
            $SUDO apt-get install -y --no-install-recommends curl ca-certificates gettext-base
            ;;
        pacman)
            $SUDO pacman -Sy --noconfirm curl ca-certificates gettext
            ;;
        dnf)
            $SUDO dnf install -y curl ca-certificates gettext
            ;;
        zypper)
            $SUDO zypper --non-interactive install curl ca-certificates gettext-runtime
            ;;
    esac
else
    log_info "System utilities (curl, envsubst) are already available."
fi

# 2. kubectl
log_step "2. Checking kubectl..."
if command -v kubectl &> /dev/null; then
    log_info "kubectl is already installed: $(kubectl version --client --short 2> /dev/null || kubectl version --client 2> /dev/null | head -n1)"
else
    log_info "Installing kubectl ($KUBECTL_VERSION) for linux-$BIN_ARCH..."
    TMP_KUBECTL=$(mktemp /tmp/kubectl.XXXXXX)
    curl -fsSL -o "$TMP_KUBECTL" "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/${BIN_ARCH}/kubectl"
    chmod +x "$TMP_KUBECTL"
    $SUDO install -o root -g root -m 0755 "$TMP_KUBECTL" /usr/local/bin/kubectl
    rm -f "$TMP_KUBECTL"
    log_info "kubectl successfully installed to /usr/local/bin/kubectl."
fi

# 3. Helm v3+
log_step "3. Checking Helm (v3+)..."
if command -v helm &> /dev/null; then
    log_info "Helm is already installed: $(helm version --short 2> /dev/null || helm version --template '{{.Version}}' 2> /dev/null)"
else
    log_info "Installing Helm v3 via official installer..."
    TMP_HELM=$(mktemp /tmp/get_helm.XXXXXX.sh)
    curl -fsSL -o "$TMP_HELM" https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-3
    chmod +x "$TMP_HELM"
    $SUDO "$TMP_HELM"
    rm -f "$TMP_HELM"
    log_info "Helm successfully installed."
fi

# 4. Optional: Docker
if [ "$WITH_DOCKER" = true ]; then
    log_step "4. Checking Docker Engine..."
    if command -v docker &> /dev/null; then
        log_info "Docker is already installed: $(docker --version)"
    else
        log_info "Installing Docker via $PKG_MANAGER..."
        case "$PKG_MANAGER" in
            apt)
                $SUDO apt-get update -y
                $SUDO apt-get install -y docker.io
                ;;
            pacman)
                $SUDO pacman -Sy --noconfirm docker
                ;;
            dnf)
                $SUDO dnf install -y moby-engine || $SUDO dnf install -y docker
                ;;
            zypper)
                $SUDO zypper --non-interactive install docker
                ;;
            *)
                log_warn "Could not auto-install Docker on this distribution. Please install Docker manually."
                ;;
        esac

        if [ -d /run/systemd/system ] && command -v systemctl &> /dev/null; then
            $SUDO systemctl enable --now docker || true
        fi

        if [ -n "$ACTUAL_USER" ] && [ "$ACTUAL_USER" != "root" ]; then
            $SUDO usermod -aG docker "$ACTUAL_USER" || true
            log_warn "User '$ACTUAL_USER' added to the 'docker' group. Log out and back in to apply group changes."
        fi
        log_info "Docker installed successfully."
    fi
fi

# 5. Optional: Kind
if [ "$WITH_KIND" = true ]; then
    log_step "5. Checking Kind (Kubernetes in Docker)..."
    if command -v kind &> /dev/null; then
        log_info "Kind is already installed: $(kind version)"
    else
        log_info "Installing Kind ($KIND_VERSION) for linux-$BIN_ARCH..."
        TMP_KIND=$(mktemp /tmp/kind.XXXXXX)
        curl -fsSL -o "$TMP_KIND" "https://kind.sigs.k8s.io/dl/${KIND_VERSION}/kind-linux-${BIN_ARCH}"
        chmod +x "$TMP_KIND"
        $SUDO install -o root -g root -m 0755 "$TMP_KIND" /usr/local/bin/kind
        rm -f "$TMP_KIND"
        log_info "Kind successfully installed to /usr/local/bin/kind."
    fi
fi

log_step "Dependencies Installation Complete"
log_info "All necessary dependencies for oscar-im are installed and ready."
