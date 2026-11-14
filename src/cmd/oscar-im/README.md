# OSCAR-IM CLI (`src/cmd/oscar-im`)

This package provides the central command-line interface tool (`oscar-im`) for orchestrating the **OSCAR InferenceMesh** runtime, gateways, and custom Endpoint Picker (EPP).

## Files

| File                                                                                 | Type      | Description                                                                                                                                                                                                         |
| :----------------------------------------------------------------------------------- | :-------- | :------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| [`main.go`](file:///home/marc/Projects/oscar-inferencemesh/src/cmd/oscar-im/main.go) | Go Source | CLI entrypoint implementing `init`, `model` (`add`/`remove`/`list`), `status` (`info`/`check`), `remove`, `restart`, and `config` commands, supporting standalone embedded execution and modular script delegation. |

## Commands

### `init`

Initializes and configures the Envoy AI Gateway (Helm controllers + `Gateway` data plane on `:8080`), the Endpoint Picker, and the `EnvoyExtensionPolicy` (`ext_proc` gRPC filter). Automatically pulls defaults and active cluster from persistent configuration (`~/.config/oscar-im/config.yaml`).

```bash
# Direct host mode (default): prepares the gateway and runs EPP on your machine
oscar-im init

# Container mode: builds Docker image, deploys to Kubernetes, and wires EnvoyExtensionPolicy
oscar-im init --docker

# Idempotent execution (skips rebuilding if already Available):
oscar-im init

# Force rebuild and redeployment if desired:
oscar-im init --docker --force

# Retain gateway / skip gateway deployment:
oscar-im init --docker --skip-gateway
```

### `model` (`add` / `remove` / `list`)

Manually connects or disconnects a model deployed in OSCAR/KServe (`Backend`, `AIServiceBackend`, and `AIGatewayRoute`) to the Envoy Gateway.

```bash
# Connect a deployed model (exposes POST /v1/chat/completions with header x-ai-eg-model)
oscar-im model add qwen2-5-05b-instruct

# List connected and unconnected OSCAR/KServe models
oscar-im model list

# Disconnect a model route and backend resources
oscar-im model remove qwen2-5-05b-instruct
```

### `status` (aliases: `info`, `check`)

Inspects the deployed mesh stack across Kubernetes, OSCAR control plane, KServe `LLMInferenceService`, worker pods & EPP endpoint discovery, Envoy Gateway / `AIGatewayRoute` / `EnvoyExtensionPolicy`, and EPP `/verify`, flagging missing components or failing links.

```bash
# Human-readable diagnostic report
oscar-im status

# Aliases and JSON output
oscar-im info
oscar-im check --json
```

### `remove`

Stops and removes deployed mesh components (`EnvoyExtensionPolicy`, `EPP` Deployment/Service/RBAC, and `Envoy Gateway`/`Envoy AI Gateway` releases).

```bash
# Remove everything (EPP, EnvoyExtensionPolicy, and Envoy AI Gateway Helm releases)
oscar-im remove

# Keep Envoy Gateway intact, only removing EPP and EnvoyExtensionPolicy
oscar-im remove --keep-gateway
```

### `restart`

Performs a full restart of the mesh infrastructure by running `remove` followed immediately by `init`.

```bash
# Full restart (re-deploys everything from scratch)
oscar-im restart --docker

# Fast restart (preserves Envoy Gateway, only bouncing EPP and EnvoyExtensionPolicy)
oscar-im restart --docker --keep-gateway
```

### `config`

Manages persistent OSCAR clusters, authentication profiles, and runtime defaults stored at `~/.config/oscar-im/config.yaml`.

```bash
# 1. Launch interactive console menu (view/edit clusters, defaults, or import from ~/.oscar-cli)
go -C src run ./cmd/oscar-im config

# 2. View current configuration
go -C src run ./cmd/oscar-im config view

# 3. Switch active cluster
go -C src run ./cmd/oscar-im config use my-production-cluster

# 4. Import clusters from oscar-cli
go -C src run ./cmd/oscar-im config import

# 5. Set default property
go -C src run ./cmd/oscar-im config set model qwen2-5-05b-instruct
```
