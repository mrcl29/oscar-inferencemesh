# InferenceMesh CLI (`src/cmd/inferencemesh`)

This package provides the central command-line interface tool (`inferencemesh`) for orchestrating the **OSCAR InferenceMesh** runtime, gateways, and custom Endpoint Picker (EPP).

## Files

| File                                                                                      | Type      | Description                                                                                                                                                       |
| :---------------------------------------------------------------------------------------- | :-------- | :---------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| [`main.go`](file:///home/marc/Projects/oscar-inferencemesh/src/cmd/inferencemesh/main.go) | Go Source | CLI entrypoint implementing the `init` command (with host direct process vs `--docker` container deployment modes), health checks, and modular script delegation. |

## Commands

### `init`

Initializes and configures the Envoy AI Gateway, the Endpoint Picker, and the Patrón B `EnvoyExtensionPolicy` (`ext_proc` gRPC filter).

```bash
# Direct host mode (default): prepares the gateway and runs EPP on your machine
go -C src run ./cmd/inferencemesh init

# Container mode: builds Docker image, deploys to Kubernetes, and wires EnvoyExtensionPolicy
go -C src run ./cmd/inferencemesh init --docker

# Custom OSCAR cluster and target model
go -C src run ./cmd/inferencemesh init --oscar-endpoint="https://my-oscar.example.org" --model="my-llm"
```
