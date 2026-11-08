# EPP Kubernetes Discovery Adapter (`src/internal/infrastructure/k8s`)

This package implements `domain.EndpointDiscoveryPort` to discover active vLLM worker pods in the target Kubernetes namespace.

## Files

| File                                                                                                                            | Type      | Description                                                                                                                                                     |
| :------------------------------------------------------------------------------------------------------------------------------ | :-------- | :-------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| [`endpoint_discovery.go`](file:///home/marc/Projects/oscar-inferencemesh/src/internal/infrastructure/k8s/endpoint_discovery.go) | Go Source | Queries Kubernetes for running vLLM worker pods matching the model name (excluding the router-scheduler pod) and falls back to the default KServe Service FQDN. |
