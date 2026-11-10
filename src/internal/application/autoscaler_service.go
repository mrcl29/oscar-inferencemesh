// Package application contains the OSCAR Elastic Autoscaler Orchestrator and
// no-op scaling policy skeleton.
package application

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/grycap/oscar-inferencemesh/src/internal/domain"
)

// NoOpAutoscalerPolicy aggregates vLLM saturation telemetry and returns a NO_OP decision.
type NoOpAutoscalerPolicy struct{}

// NewNoOpAutoscalerPolicy returns a new NoOpAutoscalerPolicy instance.
func NewNoOpAutoscalerPolicy() *NoOpAutoscalerPolicy {
	return &NoOpAutoscalerPolicy{}
}

// Evaluate inspects pool metrics and emits a verifiable NO_OP scaling decision.
// TODO(FG-05): Add a separate active policy using configured KV/queue thresholds and
// replica bounds, with hysteresis/cooldown and missing-metric tests; retain this no-op baseline.
func (p *NoOpAutoscalerPolicy) Evaluate(
	serviceName string,
	endpoints []domain.WorkerEndpoint,
	metrics map[string]domain.EndpointMetrics,
) domain.ScalingDecision {
	var sumKV float64
	var reachableCount int
	var totalWaiting int

	for _, m := range metrics {
		if m.Reachable {
			sumKV += m.KVCacheUsagePerc
			totalWaiting += m.WaitingRequests
			reachableCount++
		}
	}

	avgKV := 0.0
	if reachableCount > 0 {
		avgKV = sumKV / float64(reachableCount)
	}

	currentReplicas := len(endpoints)
	return domain.ScalingDecision{
		ServiceName:          serviceName,
		Action:               domain.ScalingActionNoOp,
		CurrentReplicas:      currentReplicas,
		DesiredReplicas:      currentReplicas,
		AvgKVCachePerc:       avgKV,
		TotalWaitingRequests: totalWaiting,
		Reason: fmt.Sprintf(
			"No-op autoscaler skeleton: observed avg_kv_cache=%.2f%%, waiting_queue=%d across %d endpoint(s). Active scaling disabled.",
			avgKV*100.0,
			totalWaiting,
			currentReplicas,
		),
		EvaluatedAt: time.Now().UTC(),
	}
}

// AutoscalerVerificationState exposes diagnostic telemetry for the OSCAR control loop.
type AutoscalerVerificationState struct {
	ServiceName            string                      `json:"service_name"`
	TotalEvaluations       int                         `json:"total_evaluations"`
	OscarCluster           domain.OscarClusterStatus   `json:"oscar_cluster"`
	OscarServiceRegistered bool                        `json:"oscar_service_registered"`
	OscarServiceSummary    *domain.OscarServiceSummary `json:"oscar_service_summary,omitempty"`
	LastScalingDecision    *domain.ScalingDecision     `json:"last_scaling_decision,omitempty"`
}

// AutoscalerOrchestrator connects vLLM saturation telemetry with the OSCAR control plane.
type AutoscalerOrchestrator struct {
	serviceName string
	discovery   domain.EndpointDiscoveryPort
	metrics     domain.MetricsCollectorPort
	policy      domain.AutoscalerPolicyPort
	oscarClient domain.OscarClientPort
	maxHistory  int

	mu               sync.RWMutex
	totalEvaluations int
	history          []domain.ScalingDecision
}

// NewAutoscalerOrchestrator wires the AutoscalerOrchestrator with its ports.
func NewAutoscalerOrchestrator(
	serviceName string,
	discovery domain.EndpointDiscoveryPort,
	metrics domain.MetricsCollectorPort,
	policy domain.AutoscalerPolicyPort,
	oscarClient domain.OscarClientPort,
) *AutoscalerOrchestrator {
	return &AutoscalerOrchestrator{
		serviceName: serviceName,
		discovery:   discovery,
		metrics:     metrics,
		policy:      policy,
		oscarClient: oscarClient,
		maxHistory:  50,
		history:     make([]domain.ScalingDecision, 0, 50),
	}
}

// ReconcileOnce runs a single autoscaling evaluation and OSCAR synchronization cycle.
func (o *AutoscalerOrchestrator) ReconcileOnce(ctx context.Context) domain.ScalingDecision {
	// TODO(FG-05): Surface discovery and apply failures rather than recording successful-looking decisions;
	// incorporate lifecycle/cold-start observations before retrying provisioning (see TODO.md).
	endpoints, _ := o.discovery.ListEndpoints(ctx, o.serviceName)
	metricsMap := o.metrics.CollectAll(ctx, endpoints)
	decision := o.policy.Evaluate(o.serviceName, endpoints, metricsMap)

	_ = o.oscarClient.ApplyScalingDecision(ctx, decision)

	o.mu.Lock()
	o.totalEvaluations++
	if len(o.history) >= o.maxHistory {
		o.history = o.history[1:]
	}
	o.history = append(o.history, decision)
	o.mu.Unlock()

	return decision
}

// GetVerificationState returns diagnostic evidence for the autoscaler loop and OSCAR connection.
func (o *AutoscalerOrchestrator) GetVerificationState(ctx context.Context) AutoscalerVerificationState {
	clusterInfo := o.oscarClient.GetClusterInfo(ctx)
	svcSummary, _ := o.oscarClient.GetService(ctx, o.serviceName)

	o.mu.RLock()
	defer o.mu.RUnlock()

	var last *domain.ScalingDecision
	if len(o.history) > 0 {
		copyLast := o.history[len(o.history)-1]
		last = &copyLast
	}

	return AutoscalerVerificationState{
		ServiceName:            o.serviceName,
		TotalEvaluations:       o.totalEvaluations,
		OscarCluster:           clusterInfo,
		OscarServiceRegistered: svcSummary != nil,
		OscarServiceSummary:    svcSummary,
		LastScalingDecision:    last,
	}
}
