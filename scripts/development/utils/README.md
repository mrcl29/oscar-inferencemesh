# Development Utilities (`scripts/development/utils`)

This directory contains reusable helper scripts and utility modules used across the automation and deployment workflows in `scripts/development/`.

## Files

| File                                                                                                                          | Language | Description                                                                                                                                                                                                                                                             |
| :---------------------------------------------------------------------------------------------------------------------------- | :------- | :---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| [`logger.sh`](file:///home/marc/Projects/oscar-inferencemesh/scripts/development/utils/logger.sh)                             | Bash     | Provides uniform ANSI color definitions and standardized logging helper functions: `log_info`, `log_warn`, `log_err`, and `log_step`. Also configures `GODEBUG="tlsmlkem=0"` to prevent TLS handshake connection resets with Kind and Docker proxies.                   |
| [`parse_model_manifest.py`](file:///home/marc/Projects/oscar-inferencemesh/scripts/development/utils/parse_model_manifest.py) | Python 3 | Parses an OSCAR FDL model definition (e.g., `qwen.yml`) using PyYAML and extracts model attributes (`PARSED_MODEL_NAME`, `PARSED_STORAGE_URI`, `PARSED_RUNTIME_IMAGE`, `PARSED_MAX_MODEL_LEN`, `PARSED_KVCACHE_SPACE`) as shell variable export statements.             |
| [`patch_kind_deploy.py`](file:///home/marc/Projects/oscar-inferencemesh/scripts/development/utils/patch_kind_deploy.py)       | Python 3 | Modifies OSCAR's upstream `kind-deploy.sh` script to enable non-interactive deployment, inject an internal DNS fix (`1.1.1.1` / `8.8.8.8` into `/etc/resolv.conf`), add resilient API server health check retry loops, and ensure proper exit codes on Traefik timeout. |

## Usage

### In Bash Scripts

```bash
# Source the logger
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
source "$SCRIPT_DIR/utils/logger.sh"

log_info "This is an informational message."
log_step "Step Title"
```

### Parsing a Model Manifest

```bash
# Extract model metadata into current shell environment
eval "$(python3 "$SCRIPT_DIR/utils/parse_model_manifest.py" "examples/inference-models/qwen.yml")"
echo "Model Name: $PARSED_MODEL_NAME"
```

### Patching OSCAR Kind Deployment Script

```bash
python3 "$SCRIPT_DIR/utils/patch_kind_deploy.py" "/tmp/oscar/deploy/kind-deploy.sh"
```
