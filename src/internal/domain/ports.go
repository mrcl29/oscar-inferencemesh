// Package domain defines the Hexagonal Architecture ports (interfaces) that
// decouple the core application orchestrators from external infrastructure adapters.
package domain

import "context"

// EndpointDiscoveryPort discovers active vLLM worker endpoints in the Kubernetes cluster.
// TODO(FG-08): Define cluster-scoped discovery results and partial-failure semantics
// before multi-cluster aggregation; preserve single-cluster adapter ownership.
type EndpointDiscoveryPort interface {
	ListEndpoints(ctx context.Context, modelName string) ([]WorkerEndpoint, error)
}

// MetricsCollectorPort scrapes real-time saturation metrics from vLLM worker replicas.
type MetricsCollectorPort interface {
	CollectAll(ctx context.Context, endpoints []WorkerEndpoint) map[string]EndpointMetrics
}

// EndpointSelectorPort selects an optimal worker replica for an incoming inference request.
type EndpointSelectorPort interface {
	SelectEndpoint(
		reqCtx InferenceRequestContext,
		endpoints []WorkerEndpoint,
		metrics map[string]EndpointMetrics,
	) RoutingDecision
}

// AutoscalerPolicyPort evaluates whether the OSCAR worker pool should scale up or down.
type AutoscalerPolicyPort interface {
	Evaluate(
		serviceName string,
		endpoints []WorkerEndpoint,
		metrics map[string]EndpointMetrics,
	) ScalingDecision
}

// OscarClientPort interacts with the OSCAR serverless platform control plane.
// TODO(FG-08): Define lifecycle observations needed to distinguish desired, pending,
// model-loading, ready and failed workers before adding cold-start orchestration.
type OscarClientPort interface {
	GetClusterInfo(ctx context.Context) OscarClusterStatus
	GetService(ctx context.Context, serviceName string) (*OscarServiceSummary, error)
	ApplyScalingDecision(ctx context.Context, decision ScalingDecision) error
}
