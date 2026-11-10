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

// FromEnv loads EPP configuration with full hierarchical precedence:
// Environment Variables > Persistent File (~/.config/inferencemesh/config.yaml) > Built-in Defaults.
func FromEnv() Settings {
	// TODO(RB-03): Validate final effective settings before startup: ports in 1..65535,
	// positive finite scrape timeout, finite normalized KV threshold, nonnegative queue
	// threshold and 0 <= MinReplicas <= MaxReplicas; numeric parsing alone allows invalid values.
	// 1. Start with built-in defaults
	model := "qwen2-5-05b-instruct"
	namespace := "oscar-svc"
	httpPort := 9091
	grpcPort := 9002
	oscarEndpoint := "https://localhost.direct"
	oscarClusterID := "local"
	oscarVerifySSL := false

	// 2. Overlay persistent file config if present
	if fileCfg, err := LoadOrCreateFileConfig(""); err == nil && fileCfg != nil {
		if fileCfg.Defaults.Model != "" {
			model = fileCfg.Defaults.Model
		}
		if fileCfg.Defaults.Namespace != "" {
			namespace = fileCfg.Defaults.Namespace
		}
		if fileCfg.Defaults.HTTPPort > 0 {
			httpPort = fileCfg.Defaults.HTTPPort
		}
		if fileCfg.Defaults.GRPCPort > 0 {
			grpcPort = fileCfg.Defaults.GRPCPort
		}
		if name, cluster, ok := fileCfg.GetActiveCluster(); ok {
			oscarClusterID = name
			if cluster.Endpoint != "" {
				oscarEndpoint = cluster.Endpoint
			}
			oscarVerifySSL = cluster.SSLVerify
		}
	}

	// 3. Overlay Environment Variables (highest non-flag precedence)
	model = envOrDefault("EPP_MODEL_NAME", model)
	namespace = envOrDefault("EPP_NAMESPACE", namespace)
	defaultFQDN := envOrDefault(
		"EPP_DEFAULT_BACKEND_FQDN",
		fmt.Sprintf("%s-kserve-workload-svc.%s.svc.cluster.local", model, namespace),
	)

	timeoutSec := envFloatOrDefault("EPP_METRICS_TIMEOUT_SEC", 1.5)

	return Settings{
		HTTPHost:                  envOrDefault("EPP_HTTP_HOST", "0.0.0.0"),
		HTTPPort:                  envIntOrDefault("EPP_HTTP_PORT", httpPort),
		GRPCExtProcPort:           envIntOrDefault("EPP_GRPC_PORT", grpcPort),
		ModelName:                 model,
		Namespace:                 namespace,
		DefaultBackendFQDN:        defaultFQDN,
		DefaultBackendPort:        envIntOrDefault("EPP_DEFAULT_BACKEND_PORT", 8000),
		MetricsScrapeTimeout:      time.Duration(timeoutSec * float64(time.Second)),
		OscarEndpoint:             envOrDefault("OSCAR_ENDPOINT", oscarEndpoint),
		OscarClusterID:            envOrDefault("OSCAR_CLUSTER_ID", oscarClusterID),
		OscarVerifySSL:            envBoolOrDefault("OSCAR_VERIFY_SSL", oscarVerifySSL),
		KVCacheUpscaleThreshold:   envFloatOrDefault("EPP_KV_CACHE_THRESHOLD", 0.80),
		WaitingQueueUpscaleThresh: envIntOrDefault("EPP_QUEUE_THRESHOLD", 5),
		MinReplicas:               envIntOrDefault("EPP_MIN_REPLICAS", 1),
		MaxReplicas:               envIntOrDefault("EPP_MAX_REPLICAS", 4),
	}
}

func envBoolOrDefault(key string, fallback bool) bool {
	val, ok := os.LookupEnv(key)
	if !ok || val == "" {
		return fallback
	}
	return val == "true" || val == "1"
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
