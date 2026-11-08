# EPP Domain Layer (`src/internal/domain`)

This package contains the pure domain models and Hexagonal Architecture port interfaces for the Endpoint Picker (EPP) and OSCAR autoscaler. It has zero external dependencies.

## Files

| File                                                                                        | Type      | Description                                                                                                                                                          |
| :------------------------------------------------------------------------------------------ | :-------- | :------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| [`models.go`](file:///home/marc/Projects/oscar-inferencemesh/src/internal/domain/models.go) | Go Source | Core domain entities and value objects (`WorkerEndpoint`, `EndpointMetrics`, `InferenceRequestContext`, `RoutingDecision`, `ScalingDecision`, `OscarClusterStatus`). |
| [`ports.go`](file:///home/marc/Projects/oscar-inferencemesh/src/internal/domain/ports.go)   | Go Source | Hexagonal interface contracts (`EndpointDiscoveryPort`, `MetricsCollectorPort`, `EndpointSelectorPort`, `AutoscalerPolicyPort`, `OscarClientPort`).                  |
