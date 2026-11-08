# EPP Binary Entrypoint (`src/cmd/epp`)

This directory contains the `main` package and composition root for the OSCAR InferenceMesh Endpoint Picker (`epp`) binary.

## Files

| File                                                                            | Type      | Description                                                                                                                                                              |
| :------------------------------------------------------------------------------ | :-------- | :----------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| [`main.go`](file:///home/marc/Projects/oscar-inferencemesh/src/cmd/epp/main.go) | Go Source | Wires the Hexagonal ports and adapters (`k8s`, `metrics`, `oscar`, `application`, `extproc`) and supports `--self-check` one-shot verification or HTTP server execution. |

## Usage

```bash
# Run one-shot verification from the repository root
go -C src run ./cmd/epp --self-check

# Start the HTTP verification server on port 9091
go -C src run ./cmd/epp --port 9091
```
