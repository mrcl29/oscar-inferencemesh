# Deployment & Packaging Artifacts (`deploy`)

This directory contains containerization and packaging definitions for the **OSCAR InferenceMesh** components.

## Artifacts

| File                                                                                     | Type       | Description                                                                                                                                                  |
| :--------------------------------------------------------------------------------------- | :--------- | :----------------------------------------------------------------------------------------------------------------------------------------------------------- |
| [`Dockerfile.epp`](file:///home/marc/Projects/oscar-inferencemesh/deploy/Dockerfile.epp) | Dockerfile | Multi-stage Go build that compiles the static `/bin/epp` binary and packages it into a minimal Alpine runner image exposing gRPC (`9002`) and HTTP (`9091`). |

## Kubernetes Manifests (Single Source of Truth)

Declarative Kubernetes manifests (`epp-deployment.yaml` and `envoy-extension-policy.yaml`) are located in [`src/internal/infrastructure/k8s/manifests/`](file:///home/marc/Projects/oscar-inferencemesh/src/internal/infrastructure/k8s/manifests):

- **Embedded in Binary**: Embedded directly into the `oscar-im` CLI binary via Go's `//go:embed` for standalone deployment.
- **Used by Production Scripts**: Automated scripts in [`scripts/production/`](file:///home/marc/Projects/oscar-inferencemesh/scripts/production) directly consume this canonical location, guaranteeing zero configuration drift.

## Build Usage

To build the EPP container image locally (build context must be repository root):

```bash
docker build -t oscar-inferencemesh-epp:latest -f deploy/Dockerfile.epp .
```
