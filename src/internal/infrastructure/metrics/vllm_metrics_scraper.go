// Package metrics implements the vLLM Prometheus /metrics scraper adapter for the EPP.
package metrics

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/grycap/oscar-inferencemesh/src/internal/config"
	"github.com/grycap/oscar-inferencemesh/src/internal/domain"
)

// VllmPrometheusAdapter scrapes real-time KV-cache and queue gauges from vLLM workers.
type VllmPrometheusAdapter struct {
	settings   config.Settings
	httpClient *http.Client
}

// NewVllmPrometheusAdapter creates a new VllmPrometheusAdapter with the configured scrape timeout.
func NewVllmPrometheusAdapter(settings config.Settings) *VllmPrometheusAdapter {
	return &VllmPrometheusAdapter{
		settings: settings,
		httpClient: &http.Client{
			Timeout: settings.MetricsScrapeTimeout,
		},
	}
}

// CollectAll scrapes metrics for every candidate endpoint.
func (a *VllmPrometheusAdapter) CollectAll(
	ctx context.Context,
	endpoints []domain.WorkerEndpoint,
) map[string]domain.EndpointMetrics {
	results := make(map[string]domain.EndpointMetrics, len(endpoints))
	for _, ep := range endpoints {
		results[ep.Name] = a.scrapeEndpoint(ctx, ep)
	}
	return results
}

func (a *VllmPrometheusAdapter) scrapeEndpoint(
	ctx context.Context,
	ep domain.WorkerEndpoint,
) domain.EndpointMetrics {
	url := fmt.Sprintf("http://%s:%d/metrics", ep.Address, ep.Port)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return unreachableSnapshot(ep.Name)
	}

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return unreachableSnapshot(ep.Name)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return unreachableSnapshot(ep.Name)
	}

	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return unreachableSnapshot(ep.Name)
	}

	return ParsePrometheusText(ep.Name, string(bodyBytes))
}

func unreachableSnapshot(endpointName string) domain.EndpointMetrics {
	return domain.EndpointMetrics{
		EndpointName:     endpointName,
		KVCacheUsagePerc: 0.0,
		WaitingRequests:  0,
		RunningRequests:  0,
		Reachable:        false,
		ScrapedAt:        time.Now().UTC(),
	}
}

// ParsePrometheusText parses raw Prometheus exposition text into domain.EndpointMetrics.
func ParsePrometheusText(endpointName, rawText string) domain.EndpointMetrics {
	var kvCache float64
	var waiting int
	var running int

	scanner := bufio.NewScanner(strings.NewReader(rawText))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		name, val, ok := parseMetricLine(line)
		if !ok {
			continue
		}

		switch name {
		case "vllm:gpu_cache_usage_perc", "vllm:cpu_cache_usage_perc", "vllm:kv_cache_usage_perc":
			if val > kvCache {
				kvCache = val
			}
		case "vllm:num_requests_waiting":
			waiting = int(val)
		case "vllm:num_requests_running":
			running = int(val)
		}
	}

	return domain.EndpointMetrics{
		EndpointName:     endpointName,
		KVCacheUsagePerc: kvCache,
		WaitingRequests:  waiting,
		RunningRequests:  running,
		Reachable:        true,
		ScrapedAt:        time.Now().UTC(),
	}
}

func parseMetricLine(line string) (string, float64, bool) {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return "", 0, false
	}

	rawMetric := fields[0]
	if braceIdx := strings.IndexByte(rawMetric, '{'); braceIdx != -1 {
		rawMetric = rawMetric[:braceIdx]
	}

	val, err := strconv.ParseFloat(fields[len(fields)-1], 64)
	if err != nil {
		return "", 0, false
	}

	return rawMetric, val, true
}
