# Endpoint Picker Deployment Manifests (`deploy/epp`)

This directory contains the production and cluster deployment manifests for the **Endpoint Picker (EPP)** and OSCAR elastic autoscaler.

## Files

| File                                                                                                   | Type                | Description                                                                                                                                                                        |
| :----------------------------------------------------------------------------------------------------- | :------------------ | :--------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| [`epp-deployment.yaml`](file:///home/marc/Projects/oscar-inferencemesh/deploy/epp/epp-deployment.yaml) | Kubernetes Manifest | Provisions the `ServiceAccount`, `Role`, `RoleBinding`, `Service` (`oscar-inferencemesh-epp`), and `Deployment` for the EPP exposing gRPC (`9002`) and HTTP verification (`9091`). |

## Usage

```bash
export NAMESPACE="oscar-svc"
export MODEL_NAME="qwen2-5-05b-instruct"
export EPP_IMAGE="oscar-inferencemesh-epp:latest"
export OSCAR_ENDPOINT="https://localhost.direct"
export OSCAR_CLUSTER_ID="local"
export OSCAR_VERIFY_SSL="false"

envsubst < deploy/epp/epp-deployment.yaml | kubectl apply -f -
```
