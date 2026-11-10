# Proposed inference experiment harness

TODO(FG-09): Add an experimental harness here; a future `streaming_ttft.py` (or
equivalent script) is provisional, not a required language or new runtime module.
Keep examples/requests/curl-inference.sh as the simple smoke request.

Ownership: benchmark tooling measures the existing inference path without changing
selection or control-loop behavior. Compare direct-worker, gateway pass-through,
and active-selection runs only when the latter is actually implemented.

Inputs/interfaces: explicit authorized endpoints, model, prompt/output sizes,
concurrency, repetitions, seed, warmup and timeout; streaming completion events
must distinguish first generated token from headers or metadata. Report TTFT,
end-to-end latency, throughput, errors and selected-worker evidence, with workload,
versions and raw samples. Separate warm steady-state from measured cold starts.

Dependencies: streaming-capable model API, reachable test workers/gateway, effective
routing validation and a controlled workload. Do not execute remote experiments
without separate authorization or assert performance improvements in advance.

Acceptance tests: deterministic local stream fixtures cover metadata-only events,
first token, failed/truncated streams and timeout; identical workload parameters
are used across comparison paths; failed requests are counted rather than dropped.
