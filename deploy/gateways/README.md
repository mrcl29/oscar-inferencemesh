# Gateway Operational Policies (`deploy/gateways`)

This directory contains production and operational Gateway API extension policies for integrating the **Envoy AI Gateway** with the **Endpoint Picker (EPP)**.

## Files

| File                                                                                                                        | Type                | Description                                                                                                                                                                               |
| :-------------------------------------------------------------------------------------------------------------------------- | :------------------ | :---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| [`envoy-extension-policy.yaml`](file:///home/marc/Projects/oscar-inferencemesh/deploy/gateways/envoy-extension-policy.yaml) | Kubernetes Manifest | Configures Envoy Gateway `EnvoyExtensionPolicy` (`extProc`) targeting `envoy-ai-gateway` to stream request/response headers through the custom EPP gRPC filter on port `9002` (Patrón B). |

## Usage

```bash
export EXTENSION_POLICY_NAME="envoy-ai-gateway-epp-ext-proc"
export NAMESPACE="oscar-svc"
export GATEWAY_NAME="envoy-ai-gateway"
export EPP_SERVICE_NAME="oscar-inferencemesh-epp"
export EPP_GRPC_PORT="9002"

envsubst < deploy/gateways/envoy-extension-policy.yaml | kubectl apply -f -
```
