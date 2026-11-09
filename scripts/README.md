# Automation Scripts (`scripts/`)

This directory houses automation scripts for setting up, deploying, and maintaining **OSCAR InferenceMesh** environments.

## Directory Structure

| Subdirectory                                                                         | Focus                           | Description                                                                                                                                                                                                                            |
| :----------------------------------------------------------------------------------- | :------------------------------ | :------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| [`production/`](file:///home/marc/Projects/oscar-inferencemesh/scripts/production)   | Production & Gateway Deployment | Scripts for host dependency installation (`install-dependencies.sh`), Envoy AI Gateway provisioning (`deploy-gateway.sh`), EPP containerization (`deploy-epp.sh`), and EnvoyExtensionPolicy attachment (`deploy-extension-policy.sh`). |
| [`development/`](file:///home/marc/Projects/oscar-inferencemesh/scripts/development) | Local Development & Emulation   | Kind cluster provisioning (`init-oscar-local-development.sh`, `deploy-oscar-local.sh`), pre-requisite installers (`install-oscar-prereqs.sh`), CLI setup (`setup-oscar-cli.sh`), and prototype orchestrators.                          |

## Quick Start

### 1. Check or Install Host Dependencies

```bash
# Verify existing tools
./scripts/production/install-dependencies.sh --check-only

# Install missing dependencies (kubectl, helm, envsubst)
./scripts/production/install-dependencies.sh
```

### 2. Deploy Infrastructure

```bash
# Provision Envoy AI Gateway
./scripts/production/deploy-gateway.sh

# Deploy EPP in Kubernetes
./scripts/production/deploy-epp.sh
```
