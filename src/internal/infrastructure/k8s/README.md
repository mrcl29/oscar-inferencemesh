# Kubernetes Infrastructure & Discovery Adapter (`src/internal/infrastructure/k8s`)

This package implements cluster health pre-flight verification, endpoint discovery (`domain.EndpointDiscoveryPort`), and declarative mesh deployment automation.

## Files

| File                                                                                                                            | Type       | Description                                                                                                                                                     |
| :------------------------------------------------------------------------------------------------------------------------------ | :--------- | :-------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| [`endpoint_discovery.go`](file:///home/marc/Projects/oscar-inferencemesh/src/internal/infrastructure/k8s/endpoint_discovery.go) | Go Source  | Queries Kubernetes for running vLLM worker pods matching the model name (excluding the router-scheduler pod) and falls back to the default KServe Service FQDN. |
| [`cluster_check.go`](file:///home/marc/Projects/oscar-inferencemesh/src/internal/infrastructure/k8s/cluster_check.go)           | Go Source  | Pre-flight connectivity checker for Kubernetes API server with configurable retries, backoff, and diagnostics.                                                  |
| [`cluster_check_test.go`](file:///home/marc/Projects/oscar-inferencemesh/src/internal/infrastructure/k8s/cluster_check_test.go) | Go Test    | Unit tests verifying immediate readiness, retry loops, failure after exhausted retries, and context cancellation.                                               |
| [`deployer.go`](file:///home/marc/Projects/oscar-inferencemesh/src/internal/infrastructure/k8s/deployer.go)                     | Go Source  | Implements `Deployer` for Envoy Gateway, Envoy AI Gateway, EPP, and EnvoyExtensionPolicy using embedded manifests and idempotency checks.                       |
| [`deployer_test.go`](file:///home/marc/Projects/oscar-inferencemesh/src/internal/infrastructure/k8s/deployer_test.go)           | Go Test    | Unit tests for `DefaultKubeDeployer` checking idempotent skipping, manifest rendering, and rollout monitoring.                                                  |
| [`manifests/`](file:///home/marc/Projects/oscar-inferencemesh/src/internal/infrastructure/k8s/manifests)                        | Subpackage | Embeds baseline Kubernetes YAML templates (`epp-deployment.yaml`, `envoy-extension-policy.yaml`) via `//go:embed`.                                              |
