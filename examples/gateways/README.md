# Gateway Manifests (`examples/gateways`)

This directory contains declarative manifests for configuring the ingress networking and routing layer using Kubernetes Gateway API and Envoy AI Gateway.

## Files

| File | Type | Description |
| :--- | :--- | :--- |
| [`envoy-ai-gateway.yaml`](file:///home/marc/Projects/oscar-inferencemesh/examples/gateways/envoy-ai-gateway.yaml) | Kubernetes Manifest | Deploys the Envoy Gateway infrastructure (`GatewayClass` `eg` and `Gateway`). It defines the public-facing entry point (HTTP listener on port `${GATEWAY_PORT}`, defaulting to `8080`) and permits routes from all namespaces. |
| [`aigateway-route.yaml`](file:///home/marc/Projects/oscar-inferencemesh/examples/gateways/aigateway-route.yaml) | Kubernetes Manifest | Configures semantic AI routing rules (`AIGatewayRoute`, `AIServiceBackend`, and `Backend`). Matches OpenAI-compatible requests by header (`x-ai-eg-model`) or model payload and dynamically forwards traffic to the target inference service FQDN. |

## Usage

These manifests are parameterized templates intended to be dynamically rendered using `envsubst` during orchestration:

```bash
# Render and apply Gateway infrastructure
export GATEWAY_NAME="envoy-ai-gateway"
export GATEWAY_NAMESPACE="oscar-svc"
export GATEWAY_PORT="8080"
envsubst < envoy-ai-gateway.yaml | kubectl apply -f -

# Render and apply AI Gateway routing for a specific backend
export ROUTE_NAME="qwen2-5-05b-instruct-ai-route"
export NAMESPACE="oscar-svc"
export GATEWAY_NAME="envoy-ai-gateway"
export GATEWAY_NAMESPACE="oscar-svc"
export ROUTE_RULE_NAME="qwen2-5-05b-instruct-inference-rule"
export MODEL_NAME="qwen2-5-05b-instruct"
export BACKEND_NAME="qwen2-5-05b-instruct-eg-backend"
export AI_SERVICE_BACKEND_NAME="qwen2-5-05b-instruct-backend"
export TARGET_BACKEND_HOST="qwen2-5-05b-instruct-kserve-workload-svc.oscar-svc.svc.cluster.local"
export TARGET_BACKEND_PORT="8000"
envsubst < aigateway-route.yaml | kubectl apply -f -
```

Typically, both manifests are managed automatically by [`scripts/development/run-prototype-v1.sh`](file:///home/marc/Projects/oscar-inferencemesh/scripts/development/run-prototype-v1.sh).
