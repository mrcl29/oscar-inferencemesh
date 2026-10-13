#!/usr/bin/env bash
set -euo pipefail

# Disable post-quantum key agreement in Go
export GODEBUG="tlsmlkem=0"

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)

# 1. Install prerequisites (Docker, kubectl, Kind, Helm)
if [ "$EUID" -ne 0 ]; then
    sudo bash "$SCRIPT_DIR/install-oscar-prereqs.sh"
else
    bash "$SCRIPT_DIR/install-oscar-prereqs.sh"
fi

# 2. Deploy local OSCAR cluster
bash "$SCRIPT_DIR/deploy-oscar-local.sh"

# 3. Setup and configure oscar-cli
bash "$SCRIPT_DIR/setup-oscar-cli.sh"

echo "[INFO] All steps completed successfully!"
echo "[INFO] OSCAR local development environment is ready."
