# Example Requests (`examples/requests`)

This directory contains client request examples for interacting with deployed LLM services through the Envoy AI Gateway.

## Files

| File                                                                                                      | Type         | Description                                                                               |
| :-------------------------------------------------------------------------------------------------------- | :----------- | :---------------------------------------------------------------------------------------- |
| [`curl-inference.sh`](file:///home/marc/Projects/oscar-inferencemesh/examples/requests/curl-inference.sh) | Shell Script | Simple `curl` script to send an OpenAI-compatible chat completion request to the gateway. |

## Usage

```bash
# Run with default endpoint (http://localhost:8080) and default prompt
./examples/requests/curl-inference.sh

# Pass a custom question
./examples/requests/curl-inference.sh "What is an API Gateway?"

# Override endpoint or model manually via environment variables
GATEWAY_URL="http://localhost:8080" ./examples/requests/curl-inference.sh "Hello world"
```
