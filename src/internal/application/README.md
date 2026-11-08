# EPP Application Layer (`src/internal/application`)

This package implements the core use-case orchestrators and initial pass-through (no-op) decision policies for request routing and OSCAR autoscaling.

## Files

| File                                                                                                                       | Type      | Description                                                                                                                                                            |
| :------------------------------------------------------------------------------------------------------------------------- | :-------- | :--------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| [`picker_service.go`](file:///home/marc/Projects/oscar-inferencemesh/src/internal/application/picker_service.go)           | Go Source | Implements `EndpointPickerService` and `PassThroughSelector` (observes candidates and vLLM metrics without overriding routing, injecting `x-oscar-epp-*` headers).     |
| [`autoscaler_service.go`](file:///home/marc/Projects/oscar-inferencemesh/src/internal/application/autoscaler_service.go)   | Go Source | Implements `AutoscalerOrchestrator` and `NoOpAutoscalerPolicy` (aggregates pool KV-cache and queue depth and verifies OSCAR service registration without scaling yet). |
| [`picker_service_test.go`](file:///home/marc/Projects/oscar-inferencemesh/src/internal/application/picker_service_test.go) | Go Test   | Unit tests verifying the pass-through selector headers and no-op autoscaler reconciliation loop.                                                                       |
