# OSCAR-IM CLI (`src/cmd/oscar-im`)

This package provides the central command-line interface tool (`oscar-im`) for orchestrating the **OSCAR InferenceMesh** runtime, gateways, and custom Endpoint Picker (EPP).

## Files

| File                                                                                 | Type      | Description                                                                                                                                                                     |
| :----------------------------------------------------------------------------------- | :-------- | :------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| [`main.go`](file:///home/marc/Projects/oscar-inferencemesh/src/cmd/oscar-im/main.go) | Go Source | CLI entrypoint implementing the `init` and `config` commands (with host direct process vs `--docker` container deployment modes), health checks, and modular script delegation. |

## Commands

### `init`

Initializes and configures the Envoy AI Gateway, the Endpoint Picker, and the `EnvoyExtensionPolicy` (`ext_proc` gRPC filter). Automatically pulls defaults and active cluster from persistent configuration (`~/.config/oscar-im/config.yaml`).

```bash
# Direct host mode (default): prepares the gateway and runs EPP on your machine
go -C src run ./cmd/oscar-im init

# Container mode: builds Docker image, deploys to Kubernetes, and wires EnvoyExtensionPolicy
go -C src run ./cmd/oscar-im init --docker

# Idempotent execution (skips rebuilding if already Available):
go -C src run ./cmd/oscar-im init

# Force rebuild and redeployment if desired:
go -C src run ./cmd/oscar-im init --docker --force

# Override on the fly via flags
go -C src run ./cmd/oscar-im init --oscar-endpoint="https://my-oscar.example.org" --model="my-llm"
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
