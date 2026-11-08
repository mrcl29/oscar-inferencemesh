# EPP Configuration (`src/internal/config`)

This package loads environment-backed runtime configuration for the Endpoint Picker (EPP) and OSCAR control plane connector.

## Files

| File                                                                                            | Type      | Description                                                                                                                                                     |
| :---------------------------------------------------------------------------------------------- | :-------- | :-------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| [`settings.go`](file:///home/marc/Projects/oscar-inferencemesh/src/internal/config/settings.go) | Go Source | Defines `Settings` and `FromEnv()` for HTTP/gRPC ports, target model name, Kubernetes namespace, vLLM scrape timeouts, OSCAR endpoints, and scaling thresholds. |
