# EPP Application Layer (`src/internal/application`)

This package implements the core use-case orchestrators and decision policies for request routing (`PassThroughSelector` and `ActiveSelector`) and OSCAR autoscaling.

## Files

| File                                                                                                                       | Type      | Description                                                                                                                                                            |
| :------------------------------------------------------------------------------------------------------------------------- | :-------- | :--------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| [`picker_service.go`](file:///home/marc/Projects/oscar-inferencemesh/src/internal/application/picker_service.go)           | Go Source | Implements `EndpointPickerService`, `PassThroughSelector`, and `ActiveSelector` (`composite_score`, `least_kv_cache`, `least_queue_depth` with round-robin fallback).  |
| [`autoscaler_service.go`](file:///home/marc/Projects/oscar-inferencemesh/src/internal/application/autoscaler_service.go)   | Go Source | Implements `AutoscalerOrchestrator` and `NoOpAutoscalerPolicy` (aggregates pool KV-cache and queue depth and verifies OSCAR service registration without scaling yet). |
| [`picker_service_test.go`](file:///home/marc/Projects/oscar-inferencemesh/src/internal/application/picker_service_test.go) | Go Test   | Unit tests verifying pass-through and active endpoint selection, telemetry freshness/validity, deterministic ties, discovery error propagation, and autoscaler no-op.  |
