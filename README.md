# OSCAR InferenceMesh

**OSCAR InferenceMesh** is an intelligent, low-latency AI Gateway and Endpoint Picker (EPP) architecture designed to integrate **OSCAR** serverless AI computing with **Envoy Gateway** and **Envoy AI Gateway**.

## Architecture Overview

```mermaid
flowchart TD
    Client["Client / Application"] -->|"HTTP /v1/chat/completions"| Envoy["Envoy AI Gateway (:8080)"]
    Envoy <-->|"gRPC ext_proc (:9002)"| EPP["Endpoint Picker (EPP)"]
    EPP -.->|"Scrape /metrics"| Replicas["vLLM Model Replicas (:8000)"]
    EPP -.->|"Reconcile Lifecycle"| OSCAR["OSCAR Control Plane"]
    Envoy -->|"Dynamic Routing"| Replicas
```

- **Envoy AI Gateway**: Ingress proxy handling model routing, API schema validation, token tracking, and rate limiting.
- **EnvoyExtensionPolicy (`ext_proc`)**: Intercepts requests/responses via gRPC to route them dynamically to the best backend instance.
- **EPP (Endpoint Picker)**: Microservice that observes queue depths, model concurrency (via `/metrics`), and health status, scaling and selecting the optimal replica.
- **OSCAR**: Serverless computing control plane managing asynchronous workloads, warm pool provisioning, and autoscaling.

## Repository Layout

| Directory                                                              | Purpose                                                                                                                     |
| :--------------------------------------------------------------------- | :-------------------------------------------------------------------------------------------------------------------------- |
| [`deploy/`](file:///home/marc/Projects/oscar-inferencemesh/deploy)     | Container build definitions (`Dockerfile.epp`) and packaging artifacts.                                                     |
| [`scripts/`](file:///home/marc/Projects/oscar-inferencemesh/scripts)   | Automation scripts for production deployment (`scripts/production`) and local development (`scripts/development`).          |
| [`src/`](file:///home/marc/Projects/oscar-inferencemesh/src)           | Go source code implementing `oscar-im` CLI, EPP gRPC daemon, Clean Architecture domain models, and infrastructure adapters. |
| [`examples/`](file:///home/marc/Projects/oscar-inferencemesh/examples) | Sample scripts and curl requests to test inference.                                                                         |

## Quick Start

### 1. Install Host Dependencies

```bash
./scripts/production/install-dependencies.sh
```

### 2. Build the CLI Binary

Compile the standalone `oscar-im` binary from source:

```bash
go build -C src -o bin/oscar-im ./cmd/oscar-im
# Or navigate to src and build directly:
# cd src && go build -o bin/oscar-im ./cmd/oscar-im
```

### 3. Initialize the Mesh

```bash
# Using the CLI
./src/bin/oscar-im init --docker
```

### 4. Verify Inference

```bash
./examples/requests/curl-inference.sh "¿Qué es la arquitectura hexagonal?"
```

### 5. Restart or Tear Down

```bash
# Restart the mesh (remove followed by init)
./src/bin/oscar-im restart --docker --keep-gateway

# Remove all deployed components
./src/bin/oscar-im remove
```
