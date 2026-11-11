# Development Scripts (`scripts/development`)

This directory contains automation scripts for provisioning local development clusters, deploying the OSCAR serverless platform, configuring CLI tools, and orchestrating end-to-end inference workflows.

## Files & Modules

| Script                                                                                                                                                                                                                      | Purpose                                                                                                                                                                                                                                                                                                                                                                                       |
| :-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | :-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| [`run-prototype-v1.sh`](file:///home/marc/Projects/oscar-inferencemesh/scripts/development/run-prototype-v1.sh) / [`run-prototype.sh`](file:///home/marc/Projects/oscar-inferencemesh/scripts/development/run-prototype.sh) | Central dynamic orchestrator. Extracts model metadata, renders decoupled manifest templates via `envsubst`, deploys Envoy Gateway, registers the model in OSCAR, deploys the `LLMInferenceService` (vLLM + `llm-d` EPP router), configures `AIGatewayRoute`, waits for rollouts, issues an inference test request via curl, checks EPP decision logs, and supports `--clean` / `--clean-all`. |
| [`init-oscar-local-development.sh`](file:///home/marc/Projects/oscar-inferencemesh/scripts/development/init-oscar-local-development.sh)                                                                                     | Master local bootstrap script. Sequentially invokes `install-oscar-prereqs.sh`, `deploy-oscar-local.sh`, and `setup-oscar-cli.sh` to bring up a fully working local OSCAR development cluster from scratch.                                                                                                                                                                                   |
| [`deploy-oscar-local.sh`](file:///home/marc/Projects/oscar-inferencemesh/scripts/development/deploy-oscar-local.sh)                                                                                                         | Clones OSCAR repository, patches `kind-deploy.sh` via `utils/patch_kind_deploy.py`, spins up the Kind cluster with Traefik and KServe support, verifies readiness, and delegates Envoy AI Gateway platform provisioning to `deploy-envoy-ai-gateway.sh`.                                                                                                                                      |
| [`deploy-envoy-ai-gateway.sh`](file:///home/marc/Projects/oscar-inferencemesh/scripts/development/deploy-envoy-ai-gateway.sh)                                                                                               | Installs and manages the platform controllers and CRDs for Envoy Gateway and Envoy AI Gateway via Helm charts with Server-Side Apply conflict resolution. Also supports `--uninstall` / `--clean`.                                                                                                                                                                                            |
| [`install-oscar-prereqs.sh`](file:///home/marc/Projects/oscar-inferencemesh/scripts/development/install-oscar-prereqs.sh)                                                                                                   | Detects system architecture and package managers (pacman, apt, dnf) to install prerequisite dependencies: Docker, kubectl (`v1.33.1`), Kind (`v0.27.0`), Helm 3, and `/etc/hosts` resolution for `localhost.direct`. Requires sudo.                                                                                                                                                           |
| [`setup-oscar-cli.sh`](file:///home/marc/Projects/oscar-inferencemesh/scripts/development/setup-oscar-cli.sh)                                                                                                               | Downloads the latest `oscar-cli` binary, extracts cluster credentials dynamically from Kubernetes secrets/deployments, and registers the `local` OSCAR cluster.                                                                                                                                                                                                                               |
| [`install-oscar-im.sh`](./install-oscar-im.sh)                                                                                                                                                                               | Builds the current Go CLI and installs `oscar-im` to `~/.local/bin`, adding that directory to the selected shell startup file once.                                                                                                                                                                                                                                                        |
| [`utils/`](file:///home/marc/Projects/oscar-inferencemesh/scripts/development/utils)                                                                                                                                        | Shared utility modules for logging, FDL YAML manifest parsing, and Kind deployment patching.                                                                                                                                                                                                                                                                                                  |

## Workflow Examples

### Install the `oscar-im` CLI from the Current Checkout

```bash
./scripts/development/install-oscar-im.sh
# Source the printed profile, or open a new shell, then:
oscar-im --help
```

### 1. Initialize Complete Local Environment from Scratch

```bash
./scripts/development/init-oscar-local-development.sh
```

### 2. Deploy and Test the Inference Mesh Prototype

```bash
# Uses default qwen.yml model and decoupled manifests
./scripts/development/run-prototype-v1.sh

# Or supply a custom model definition
./scripts/development/run-prototype-v1.sh path/to/custom-model.yml
```

### 3. Clean up Deployed Model Resources (Free Host RAM)

```bash
# Deletes AI Gateway routes, KServe worker & scheduler pods, and OSCAR service
./scripts/development/run-prototype-v1.sh --clean

# Or clean all resources including the Envoy AI Gateway infrastructure
./scripts/development/run-prototype-v1.sh --clean-all
```
