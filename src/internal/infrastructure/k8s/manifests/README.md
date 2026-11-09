# Embedded Kubernetes Manifests (`src/internal/infrastructure/k8s/manifests`)

This package serves as the **Single Source of Truth (SSOT)** for all Kubernetes manifests in the repository, embedding them directly into the `oscar-im` Go binary using Go standard library's `//go:embed` while also providing the canonical manifests for deployment scripts.

## Purpose & Architecture

To make `oscar-im` a true **standalone binary** while avoiding configuration drift across the repository:

1. All baseline deployment templates are embedded at compile time via `//go:embed *.yaml`.
2. Template variables (`${NAMESPACE}`, `${MODEL_NAME}`, etc.) are rendered in-memory using `manifests.RenderManifest()`.
3. If an explicit disk path is passed and exists on disk, `GetManifest` reads from disk (allowing local overrides during development). Otherwise, it seamlessly falls back to the embedded templates.

## Files

| File                          | Purpose                                                                             |
| :---------------------------- | :---------------------------------------------------------------------------------- |
| `embed.go`                    | Go embed driver providing `ManifestsFS`, `GetManifest`, and `RenderManifest`.       |
| `embed_test.go`               | Unit tests for embedded manifest reading, templating, and disk fallback logic.      |
| `epp-deployment.yaml`         | Declarative manifest for EPP RBAC, Service, and Deployment.                         |
| `envoy-extension-policy.yaml` | Declarative manifest configuring the `EnvoyExtensionPolicy` `ext_proc` gRPC filter. |
