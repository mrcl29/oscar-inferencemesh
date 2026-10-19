#!/usr/bin/env python3
"""
Utility script to parse OSCAR FDL model definitions (e.g., qwen.yml)
and output shell-compatible variable assignments for dynamic orchestration.
"""

import sys
import yaml

def main():
    if len(sys.argv) < 2:
        print("Usage: parse_model_manifest.py <path_to_model_yaml>", file=sys.stderr)
        sys.exit(1)

    filepath = sys.argv[1]
    name = "qwen2-5-05b-instruct"
    uri = "oci://ghcr.io/grycap/kserve-qwen2-5-05b-instruct:latest"
    runtime = "ghcr.io/grycap/kserve-vllm-openai-cpu:v0.22.1"
    max_len = "2048"
    kv_space = "1"

    try:
        with open(filepath, "r") as f:
            doc = yaml.safe_load(f)

        if doc and "functions" in doc and "oscar" in doc["functions"]:
            funcs = doc["functions"]["oscar"]
            if isinstance(funcs, list) and len(funcs) > 0:
                local_cfg = funcs[0].get("local", {})
                name = local_cfg.get("name", name)
                ks = local_cfg.get("kserve", {})
                uri = ks.get("storage_uri", uri)
                llm = ks.get("llm_inference", {})
                runtime = llm.get("runtime_image", runtime)

                for arg in ks.get("args", []):
                    if "--max-model-len" in str(arg):
                        max_len = str(arg).split("=")[-1].strip()

                if "env" in ks and "VLLM_CPU_KVCACHE_SPACE" in ks["env"]:
                    kv_space = str(ks["env"]["VLLM_CPU_KVCACHE_SPACE"])
    except Exception as e:
        print(f"# Warning: Failed to parse {filepath}: {e}. Falling back to default values.", file=sys.stderr)

    print(f'PARSED_MODEL_NAME="{name}"')
    print(f'PARSED_STORAGE_URI="{uri}"')
    print(f'PARSED_RUNTIME_IMAGE="{runtime}"')
    print(f'PARSED_MAX_MODEL_LEN="{max_len}"')
    print(f'PARSED_KVCACHE_SPACE="{kv_space}"')

if __name__ == "__main__":
    main()
