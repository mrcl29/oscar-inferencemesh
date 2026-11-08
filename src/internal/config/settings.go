// Package config provides environment-backed runtime configuration for the EPP.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Settings holds immutable runtime configuration for the Endpoint Picker and OSCAR connector.
type Settings struct {
	HTTPHost                  string        `json:"http_host"`
	HTTPPort                  int           `json:"http_port"`
	GRPCExtProcPort           int           `json:"grpc_ext_proc_port"`
	ModelName                 string        `json:"model_name"`
	Namespace                 string        `json:"namespace"`
	DefaultBackendFQDN        string        `json:"default_backend_fqdn"`
	DefaultBackendPort        int           `json:"default_backend_port"`
	MetricsScrapeTimeout      time.Duration `json:"metrics_scrape_timeout"`
	OscarEndpoint             string        `json:"oscar_endpoint"`
	OscarClusterID            string        `json:"oscar_cluster_id"`
	OscarVerifySSL            bool          `json:"oscar_verify_ssl"`
	KVCacheUpscaleThreshold   float64       `json:"kv_cache_upscale_threshold"`
	WaitingQueueUpscaleThresh int           `json:"waiting_queue_upscale_threshold"`
	MinReplicas               int           `json:"min_replicas"`
	MaxReplicas               int           `json:"max_replicas"`
}

// FromEnv loads EPP configuration from environment variables with sensible local defaults.
func FromEnv() Settings {
	model := envOrDefault("EPP_MODEL_NAME", "qwen2-5-05b-instruct")
	namespace := envOrDefault("EPP_NAMESPACE", "oscar-svc")
	defaultFQDN := envOrDefault(
		"EPP_DEFAULT_BACKEND_FQDN",
		fmt.Sprintf("%s-kserve-workload-svc.%s.svc.cluster.local", model, namespace),
	)

	timeoutSec := envFloatOrDefault("EPP_METRICS_TIMEOUT_SEC", 1.5)

	return Settings{
		HTTPHost:                  envOrDefault("EPP_HTTP_HOST", "0.0.0.0"),
		HTTPPort:                  envIntOrDefault("EPP_HTTP_PORT", 9091),
		GRPCExtProcPort:           envIntOrDefault("EPP_GRPC_PORT", 9002),
		ModelName:                 model,
		Namespace:                 namespace,
		DefaultBackendFQDN:        defaultFQDN,
		DefaultBackendPort:        envIntOrDefault("EPP_DEFAULT_BACKEND_PORT", 8000),
		MetricsScrapeTimeout:      time.Duration(timeoutSec * float64(time.Second)),
		OscarEndpoint:             envOrDefault("OSCAR_ENDPOINT", "https://localhost.direct"),
		OscarClusterID:            envOrDefault("OSCAR_CLUSTER_ID", "local"),
		OscarVerifySSL:            envOrDefault("OSCAR_VERIFY_SSL", "false") == "true",
		KVCacheUpscaleThreshold:   envFloatOrDefault("EPP_KV_CACHE_THRESHOLD", 0.80),
		WaitingQueueUpscaleThresh: envIntOrDefault("EPP_QUEUE_THRESHOLD", 5),
		MinReplicas:               envIntOrDefault("EPP_MIN_REPLICAS", 1),
		MaxReplicas:               envIntOrDefault("EPP_MAX_REPLICAS", 4),
	}
}

func envOrDefault(key, fallback string) string {
	if val, ok := os.LookupEnv(key); ok && val != "" {
		return val
	}
	return fallback
}

func envIntOrDefault(key string, fallback int) int {
	val, ok := os.LookupEnv(key)
	if !ok || val == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(val)
	if err != nil {
		return fallback
	}
	return parsed
}

func envFloatOrDefault(key string, fallback float64) float64 {
	val, ok := os.LookupEnv(key)
	if !ok || val == "" {
		return fallback
	}
	parsed, err := strconv.ParseFloat(val, 64)
	if err != nil {
		return fallback
	}
	return parsed
}
