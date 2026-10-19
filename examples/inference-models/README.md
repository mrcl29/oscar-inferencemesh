# Inference Models (`examples/inference-models`)

This directory contains OSCAR Function Definition Language (FDL) manifests representing deployable LLM inference services in OSCAR.

## Files

| File | Type | Description |
| :--- | :--- | :--- |
| [`qwen.yml`](file:///home/marc/Projects/oscar-inferencemesh/examples/inference-models/qwen.yml) | OSCAR FDL Manifest | Declares the serverless function `qwen2-5-05b-instruct` using KServe's `llm_inference` runtime. Specifies the runtime image (`ghcr.io/grycap/kserve-vllm-openai-cpu:v0.22.1`), the model weights storage URI (`oci://ghcr.io/grycap/kserve-qwen2-5-05b-instruct:latest`), `--max-model-len=2048`, and memory/CPU limits. |
| [`script.sh`](file:///home/marc/Projects/oscar-inferencemesh/examples/inference-models/script.sh) | Shell Script | Default dummy script referenced by the OSCAR FDL `script: script.sh` entry. |

## Usage

Register the model directly into an active OSCAR cluster using `oscar-cli`:

```bash
# Register or update the function in OSCAR
oscar-cli apply qwen.yml

# Verify registered service
oscar-cli service list
oscar-cli service get qwen2-5-05b-instruct

# Remove the service from OSCAR
oscar-cli service remove qwen2-5-05b-instruct
```

When using [`run-prototype-v1.sh`](file:///home/marc/Projects/oscar-inferencemesh/scripts/development/run-prototype-v1.sh), the script automatically inspects this file, extracts its metadata via `parse_model_manifest.py`, registers the function in OSCAR, and dynamically feeds the configuration to the downstream KServe and Gateway API layers.
