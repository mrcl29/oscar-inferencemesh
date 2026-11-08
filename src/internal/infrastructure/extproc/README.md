# EPP Verification & Pass-Through Server (`src/internal/infrastructure/extproc`)

This package exposes the HTTP verification, health, and pass-through request interception simulation server for the Endpoint Picker (EPP).

## Files

| File                                                                                                        | Type      | Description                                                                                                                                           |
| :---------------------------------------------------------------------------------------------------------- | :-------- | :---------------------------------------------------------------------------------------------------------------------------------------------------- |
| [`server.go`](file:///home/marc/Projects/oscar-inferencemesh/src/internal/infrastructure/extproc/server.go) | Go Source | Implements `GET /healthz`, `GET /verify`, and `POST /v1/ext_proc/simulate` (or `POST /pick`) returning `x-oscar-epp-*` pass-through decision headers. |
