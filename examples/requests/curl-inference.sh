#!/usr/bin/env bash
set -euo pipefail

# Endpoint & model configuration
GATEWAY_URL="${GATEWAY_URL:-"http://localhost:8080"}"
MODEL_NAME="${MODEL_NAME:-"qwen2-5-05b-instruct"}"
PROMPT="${1:-"Write a short poem about clouds"}"

# Execute inference request
# TODO(FG-09): Keep this smoke request; add a separate streaming TTFT/direct-worker
# comparison harness with controlled workload and reproducible results (see benchmarks/TODO.md).
curl --connect-timeout 5 -sS -X POST "${GATEWAY_URL}/v1/chat/completions" \
    -H "Content-Type: application/json" \
    -H "x-ai-eg-model: ${MODEL_NAME}" \
    -d '{
        "model": "'"${MODEL_NAME}"'",
        "messages": [
            {"role": "user", "content": "'"${PROMPT}"'"}
        ],
        "max_tokens": 50
    }'
echo ""
