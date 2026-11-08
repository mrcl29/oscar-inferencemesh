# Deployment & Infrastructure Manifests (`deploy`)

This directory contains production and operational Kubernetes manifests for deploying the core **OSCAR InferenceMesh** components into target clusters.

Unlike [`examples/`](file:///home/marc/Projects/oscar-inferencemesh/examples) (which contains developer walkthroughs, sample client requests, and reference model configurations), this directory holds the actual deployment definitions required to run the runtime components in production.

## Subdirectories

| Directory                                                                     | Description                                                                                                 |
| :---------------------------------------------------------------------------- | :---------------------------------------------------------------------------------------------------------- |
| [`epp/`](file:///home/marc/Projects/oscar-inferencemesh/deploy/epp)           | Deployment, Service, and RBAC manifests for the **Endpoint Picker (EPP)** daemon.                           |
| [`gateways/`](file:///home/marc/Projects/oscar-inferencemesh/deploy/gateways) | Operational Gateway policies, including the `EnvoyExtensionPolicy` (`ext_proc` gRPC filter on port `9002`). |

## Automation

These manifests are dynamically parameterized and applied automatically by the [`inferencemesh init`](file:///home/marc/Projects/oscar-inferencemesh/src/cmd/inferencemesh) CLI or the modular scripts in [`scripts/production/`](file:///home/marc/Projects/oscar-inferencemesh/scripts/production).
