# EPP vLLM Metrics Adapter (`src/internal/infrastructure/metrics`)

This package implements `domain.MetricsCollectorPort` to scrape real-time Prometheus telemetry from vLLM worker replicas.

## Files

| File                                                                                                                                    | Type      | Description                                                                                                                                                                      |
| :-------------------------------------------------------------------------------------------------------------------------------------- | :-------- | :------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| [`vllm_metrics_scraper.go`](file:///home/marc/Projects/oscar-inferencemesh/src/internal/infrastructure/metrics/vllm_metrics_scraper.go) | Go Source | Scrapes `http://<pod-ip>:8000/metrics` and parses `vllm:gpu_cache_usage_perc`, `vllm:cpu_cache_usage_perc`, `vllm:num_requests_waiting`, and `vllm:num_requests_running` gauges. |
