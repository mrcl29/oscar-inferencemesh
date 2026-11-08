# EPP OSCAR Control Plane Adapter (`src/internal/infrastructure/oscar`)

This package implements `domain.OscarClientPort` to connect the EPP autoscaler with the OSCAR serverless platform.

## Files

| File                                                                                                                  | Type      | Description                                                                                                                                                      |
| :-------------------------------------------------------------------------------------------------------------------- | :-------- | :--------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| [`oscar_client.go`](file:///home/marc/Projects/oscar-inferencemesh/src/internal/infrastructure/oscar/oscar_client.go) | Go Source | Queries OSCAR cluster status and registered inference services via `oscar-cli` and provides the execution hook (`ApplyScalingDecision`) for elastic FDL scaling. |
