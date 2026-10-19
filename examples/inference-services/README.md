# Inference Services (`examples/inference-services`)

This directory contains decoupled Kubernetes manifests for deploying scalable, intelligent LLM inference workloads using KServe v1alpha2 `LLMInferenceService` and `llm-d` components.

## Files

| File                                                                                                                              | Type                         | Description                                                                                                                                                                                                                                                                                                             |
| :-------------------------------------------------------------------------------------------------------------------------------- | :--------------------------- | :---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| [`llminferenceservice.yaml`](file:///home/marc/Projects/oscar-inferencemesh/examples/inference-services/llminferenceservice.yaml) | Kubernetes Manifest Template | Generic, decoupled template for a KServe `LLMInferenceService`. Uses the base preset `kserve-config-llm-scheduler` to inject the `llm-d` Endpoint Picker (EPP) router and provision the Gateway API `InferencePool`. Configures a pool of vLLM worker containers on CPU with customizable resource requests and limits. |

## Usage

This manifest is a parameterized template that can be dynamically rendered with `envsubst`:

```bash
export SERVICE_NAME="qwen2-5-05b-instruct"
export NAMESPACE="oscar-svc"
export MODEL_NAME="qwen2-5-05b-instruct"
export MODEL_STORAGE_URI="oci://ghcr.io/grycap/kserve-qwen2-5-05b-instruct:latest"
export RUNTIME_IMAGE="ghcr.io/grycap/kserve-vllm-openai-cpu:v0.22.1"
export REPLICAS="1"
export MAX_MODEL_LEN="2048"
export VLLM_CPU_KVCACHE_SPACE="1"
export WORKER_CPU_LIMIT="2"
export WORKER_MEMORY_LIMIT="5.5Gi"
export WORKER_CPU_REQUEST="250m"
export WORKER_MEMORY_REQUEST="800Mi"

envsubst < llminferenceservice.yaml | kubectl apply -f -
```

Applying this template creates:

1. `InferencePool`: The Gateway API inference pool representing the worker set.
2. `Deployment` (`<SERVICE_NAME>-kserve`): The vLLM worker pod(s) with an init container and modelcar sidecar mounting model weights.
3. `Deployment` (`<SERVICE_NAME>-kserve-router-scheduler`): The `llm-d` EPP router scheduler container running on gRPC port 9002.
4. `Service` (`<SERVICE_NAME>-kserve-workload-svc`): ClusterIP service routing traffic to the worker pods.
