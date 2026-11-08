// Package domain contains pure domain entities and value objects for the
// OSCAR InferenceMesh Endpoint Picker (EPP). It has zero external dependencies.
package domain

import "time"

// RoutingMode defines the strategy used by the EPP to select a target replica.
type RoutingMode string

const (
	// RoutingModePassThrough observes endpoints and metrics without overriding routing.
	RoutingModePassThrough RoutingMode = "pass_through"
	// RoutingModeLeastKVCache selects the replica with the lowest KV-cache saturation.
	RoutingModeLeastKVCache RoutingMode = "least_kv_cache"
	// RoutingModeLeastQueueDepth selects the replica with the shortest waiting queue.
	RoutingModeLeastQueueDepth RoutingMode = "least_queue_depth"
	// RoutingModeCompositeScore combines KV-cache and queue metrics into a weighted score.
	RoutingModeCompositeScore RoutingMode = "composite_score"
)

// ScalingAction represents the lifecycle operation to execute on the OSCAR control plane.
type ScalingAction string

const (
	// ScalingActionNoOp indicates no replica count mutation is required.
	ScalingActionNoOp ScalingAction = "no_op"
	// ScalingActionScaleUp provisions additional elastic worker replicas via OSCAR.
	ScalingActionScaleUp ScalingAction = "scale_up"
	// ScalingActionScaleDown decommissions idle worker replicas via OSCAR.
	ScalingActionScaleDown ScalingAction = "scale_down"
)

// WorkerEndpoint represents a discoverable vLLM worker replica in the inference pool.
type WorkerEndpoint struct {
	Name      string            `json:"name"`
	Address   string            `json:"address"`
	Port      int               `json:"port"`
	Namespace string            `json:"namespace"`
	ModelName string            `json:"model_name"`
	Ready     bool              `json:"ready"`
	Labels    map[string]string `json:"labels,omitempty"`
}

// SocketAddress returns the host:port target expected by Envoy routing headers.
func (w WorkerEndpoint) SocketAddress() string {
	return w.Address + ":" + itoa(w.Port)
}

// EndpointMetrics holds real-time saturation and queue telemetry scraped from a vLLM worker.
type EndpointMetrics struct {
	EndpointName      string    `json:"endpoint_name"`
	KVCacheUsagePerc  float64   `json:"kv_cache_usage_perc"`
	WaitingRequests   int       `json:"waiting_requests"`
	RunningRequests   int       `json:"running_requests"`
	Reachable         bool      `json:"reachable"`
	ScrapedAt         time.Time `json:"scraped_at"`
}

// InferenceRequestContext holds normalized metadata extracted from an incoming request.
type InferenceRequestContext struct {
	RequestID     string            `json:"request_id"`
	Model         string            `json:"model"`
	PromptPreview string            `json:"prompt_preview,omitempty"`
	Headers       map[string]string `json:"headers,omitempty"`
	ReceivedAt    time.Time         `json:"received_at"`
}

// RoutingDecision represents the result of an endpoint picking evaluation.
type RoutingDecision struct {
	RequestID        string            `json:"request_id"`
	Model            string            `json:"model"`
	Mode             RoutingMode       `json:"mode"`
	SelectedEndpoint string            `json:"selected_endpoint,omitempty"`
	Reason           string            `json:"reason"`
	CandidateCount   int               `json:"candidate_count"`
	ResponseHeaders  map[string]string `json:"response_headers"`
	DecidedAt        time.Time         `json:"decided_at"`
}

// ScalingDecision represents the result of evaluating pool saturation against OSCAR thresholds.
type ScalingDecision struct {
	ServiceName          string        `json:"service_name"`
	Action               ScalingAction `json:"action"`
	CurrentReplicas      int           `json:"current_replicas"`
	DesiredReplicas      int           `json:"desired_replicas"`
	AvgKVCachePerc       float64       `json:"avg_kv_cache_perc"`
	TotalWaitingRequests int           `json:"total_waiting_requests"`
	Reason               string        `json:"reason"`
	EvaluatedAt          time.Time     `json:"evaluated_at"`
}

// OscarClusterStatus summarizes connectivity with the OSCAR control plane.
type OscarClusterStatus struct {
	Connected       bool     `json:"connected"`
	ClusterID       string   `json:"cluster_id"`
	Endpoint        string   `json:"endpoint"`
	AdapterMode     string   `json:"adapter_mode"`
	RawServiceTable []string `json:"raw_service_table,omitempty"`
	Error           string   `json:"error,omitempty"`
}

// OscarServiceSummary represents a registered OSCAR service entry.
type OscarServiceSummary struct {
	Name      string `json:"name"`
	Image     string `json:"image"`
	CPU       string `json:"cpu"`
	Memory    string `json:"memory"`
	ClusterID string `json:"cluster_id"`
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := false
	if v < 0 {
		neg = true
		v = -v
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
