package application_test

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/grycap/oscar-inferencemesh/src/internal/application"
	"github.com/grycap/oscar-inferencemesh/src/internal/domain"
)

type stubDiscovery struct {
	endpoints []domain.WorkerEndpoint
	err       error
}

func (s *stubDiscovery) ListEndpoints(_ context.Context, _ string) ([]domain.WorkerEndpoint, error) {
	return s.endpoints, s.err
}

type stubMetrics struct {
	metrics map[string]domain.EndpointMetrics
}

func (s *stubMetrics) CollectAll(_ context.Context, _ []domain.WorkerEndpoint) map[string]domain.EndpointMetrics {
	return s.metrics
}

type stubOscar struct{}

func (s *stubOscar) GetClusterInfo(_ context.Context) domain.OscarClusterStatus {
	return domain.OscarClusterStatus{
		Connected:   true,
		ClusterID:   "local",
		Endpoint:    "https://localhost.direct",
		AdapterMode: "test-stub",
	}
}

func (s *stubOscar) GetService(_ context.Context, name string) (*domain.OscarServiceSummary, error) {
	return &domain.OscarServiceSummary{
		Name:      name,
		Image:     "ghcr.io/grycap/vllm:cpu",
		CPU:       "2.0",
		Memory:    "5.5Gi",
		ClusterID: "local",
	}, nil
}

func (s *stubOscar) ApplyScalingDecision(_ context.Context, _ domain.ScalingDecision) error {
	return nil
}

// TODO(FG-09): Retain this baseline and add active-policy tests for readiness, missing/stale
// metrics, discovery/apply errors, threshold boundaries, cooldown and cold-start transitions;
// verify effective selected-worker routing in an integration test, not only diagnostic headers.
func TestPassThroughPickerAndAutoscaler(t *testing.T) {
	endpoints := []domain.WorkerEndpoint{
		{
			Name:      "qwen-worker-0",
			Address:   "10.244.0.15",
			Port:      8000,
			Namespace: "oscar-svc",
			ModelName: "qwen2-5-05b-instruct",
			Ready:     true,
		},
	}
	metricsMap := map[string]domain.EndpointMetrics{
		"qwen-worker-0": {
			EndpointName:     "qwen-worker-0",
			KVCacheUsagePerc: 0.42,
			WaitingRequests:  2,
			RunningRequests:  1,
			Reachable:        true,
			ScrapedAt:        time.Now().UTC(),
		},
	}

	discovery := &stubDiscovery{endpoints: endpoints}
	collector := &stubMetrics{metrics: metricsMap}

	picker := application.NewEndpointPickerService(
		discovery,
		collector,
		application.NewPassThroughSelector(),
	)

	decision := picker.ProcessRequest(context.Background(), domain.InferenceRequestContext{
		RequestID:     "req-test-1",
		Model:         "qwen2-5-05b-instruct",
		PromptPreview: "Hello world",
	})

	if decision.Mode != domain.RoutingModePassThrough {
		t.Fatalf("expected mode %q, got %q", domain.RoutingModePassThrough, decision.Mode)
	}
	if decision.SelectedEndpoint != "" {
		t.Fatalf("expected empty SelectedEndpoint in pass-through mode, got %q", decision.SelectedEndpoint)
	}
	if decision.ResponseHeaders["x-oscar-epp-verified"] != "true" {
		t.Fatalf("expected verification header x-oscar-epp-verified=true")
	}

	autoscaler := application.NewAutoscalerOrchestrator(
		"qwen2-5-05b-instruct",
		discovery,
		collector,
		application.NewNoOpAutoscalerPolicy(),
		&stubOscar{},
	)

	scalingDecision := autoscaler.ReconcileOnce(context.Background())
	if scalingDecision.Action != domain.ScalingActionNoOp {
		t.Fatalf("expected scaling action %q, got %q", domain.ScalingActionNoOp, scalingDecision.Action)
	}
	if scalingDecision.CurrentReplicas != 1 || scalingDecision.DesiredReplicas != 1 {
		t.Fatalf("expected replicas 1->1, got %d->%d", scalingDecision.CurrentReplicas, scalingDecision.DesiredReplicas)
	}
}

func TestActiveSelector_ModesAndReadinessFiltering(t *testing.T) {
	now := time.Now().UTC()
	endpoints := []domain.WorkerEndpoint{
		{Name: "worker-unready", Address: "10.0.0.1", Port: 8000, Ready: false},
		{Name: "worker-low-kv", Address: "10.0.0.2", Port: 8000, Ready: true},
		{Name: "worker-low-queue", Address: "10.0.0.3", Port: 8000, Ready: true},
	}
	metrics := map[string]domain.EndpointMetrics{
		"worker-unready":   {EndpointName: "worker-unready", KVCacheUsagePerc: 0.01, WaitingRequests: 0, RunningRequests: 0, Reachable: true, ScrapedAt: now},
		"worker-low-kv":    {EndpointName: "worker-low-kv", KVCacheUsagePerc: 0.10, WaitingRequests: 6, RunningRequests: 2, Reachable: true, ScrapedAt: now},
		"worker-low-queue": {EndpointName: "worker-low-queue", KVCacheUsagePerc: 0.80, WaitingRequests: 0, RunningRequests: 1, Reachable: true, ScrapedAt: now},
	}

	req := domain.InferenceRequestContext{RequestID: "req-1", Model: "qwen2-5-05b-instruct"}

	kvDecision := application.NewActiveSelector(domain.RoutingModeLeastKVCache, 10*time.Second).SelectEndpoint(req, endpoints, metrics)
	if kvDecision.SelectedEndpoint != "10.0.0.2:8000" {
		t.Fatalf("least_kv_cache expected 10.0.0.2:8000, got %s", kvDecision.SelectedEndpoint)
	}
	if kvDecision.CandidateCount != 2 || kvDecision.ResponseHeaders["x-oscar-epp-selected-worker"] != "worker-low-kv" {
		t.Fatalf("unexpected least_kv_cache metadata: %+v", kvDecision)
	}

	queueDecision := application.NewActiveSelector(domain.RoutingModeLeastQueueDepth, 10*time.Second).SelectEndpoint(req, endpoints, metrics)
	if queueDecision.SelectedEndpoint != "10.0.0.3:8000" {
		t.Fatalf("least_queue_depth expected 10.0.0.3:8000, got %s", queueDecision.SelectedEndpoint)
	}

	// composite_score:
	// worker-low-kv:    0.10*1.0 + 6*0.5 + 2*0.1 = 3.30
	// worker-low-queue: 0.80*1.0 + 0*0.5 + 1*0.1 = 0.90 -> wins
	compDecision := application.NewActiveSelector(domain.RoutingModeCompositeScore, 10*time.Second).SelectEndpoint(req, endpoints, metrics)
	if compDecision.SelectedEndpoint != "10.0.0.3:8000" {
		t.Fatalf("composite_score expected 10.0.0.3:8000, got %s", compDecision.SelectedEndpoint)
	}
	if compDecision.ResponseHeaders["x-oscar-epp-telemetry"] != "fresh" {
		t.Fatalf("expected fresh telemetry header, got %q", compDecision.ResponseHeaders["x-oscar-epp-telemetry"])
	}
}

func TestActiveSelector_RejectsStaleUnreachableInvalidAndFallsBackRR(t *testing.T) {
	now := time.Now().UTC()
	endpoints := []domain.WorkerEndpoint{
		{Name: "worker-b", Address: "10.0.0.2", Port: 8000, Ready: true},
		{Name: "worker-a", Address: "10.0.0.1", Port: 8000, Ready: true},
	}

	// Stale + unreachable + NaN metrics -> none valid -> deterministic sorted round-robin fallback.
	badMetrics := map[string]domain.EndpointMetrics{
		"worker-a": {EndpointName: "worker-a", KVCacheUsagePerc: 0.0, Reachable: true, ScrapedAt: now.Add(-30 * time.Second)},
		"worker-b": {EndpointName: "worker-b", KVCacheUsagePerc: math.NaN(), Reachable: false, ScrapedAt: now},
	}

	sel := application.NewActiveSelector(domain.RoutingModeCompositeScore, 10*time.Second)
	d1 := sel.SelectEndpoint(domain.InferenceRequestContext{RequestID: "r1"}, endpoints, badMetrics)
	d2 := sel.SelectEndpoint(domain.InferenceRequestContext{RequestID: "r2"}, endpoints, badMetrics)
	if d1.SelectedEndpoint != "10.0.0.1:8000" || d2.SelectedEndpoint != "10.0.0.2:8000" {
		t.Fatalf("expected sorted fallback RR (worker-a -> worker-b), got %s -> %s", d1.SelectedEndpoint, d2.SelectedEndpoint)
	}
	if d1.ResponseHeaders["x-oscar-epp-telemetry"] != "fallback_rr" {
		t.Fatalf("expected fallback_rr header, got %q", d1.ResponseHeaders["x-oscar-epp-telemetry"])
	}
}

func TestActiveSelector_DeterministicTieBreakingAndEmptyPool(t *testing.T) {
	now := time.Now().UTC()
	sel := application.NewActiveSelector(domain.RoutingModeCompositeScore, 10*time.Second)

	// Empty / all-unready pool
	emptyDecision := sel.SelectEndpoint(
		domain.InferenceRequestContext{RequestID: "r-empty", Model: "m"},
		[]domain.WorkerEndpoint{{Name: "w1", Address: "10.0.0.1", Port: 8000, Ready: false}},
		nil,
	)
	if emptyDecision.SelectedEndpoint != "" || emptyDecision.Reason != "no_ready_workers" || emptyDecision.ResponseHeaders["x-oscar-epp-verified"] != "false" {
		t.Fatalf("unexpected empty pool decision: %+v", emptyDecision)
	}

	// Tied fresh metrics with reversed input order should deterministically alternate worker-1 -> worker-2 -> worker-1.
	tiedEndpoints := []domain.WorkerEndpoint{
		{Name: "worker-2", Address: "10.0.0.2", Port: 8000, Ready: true},
		{Name: "worker-1", Address: "10.0.0.1", Port: 8000, Ready: true},
	}
	tiedMetrics := map[string]domain.EndpointMetrics{
		"worker-1": {EndpointName: "worker-1", KVCacheUsagePerc: 0.25, WaitingRequests: 1, RunningRequests: 1, Reachable: true, ScrapedAt: now},
		"worker-2": {EndpointName: "worker-2", KVCacheUsagePerc: 0.25, WaitingRequests: 1, RunningRequests: 1, Reachable: true, ScrapedAt: now},
	}
	t1 := sel.SelectEndpoint(domain.InferenceRequestContext{RequestID: "t1"}, tiedEndpoints, tiedMetrics)
	t2 := sel.SelectEndpoint(domain.InferenceRequestContext{RequestID: "t2"}, tiedEndpoints, tiedMetrics)
	t3 := sel.SelectEndpoint(domain.InferenceRequestContext{RequestID: "t3"}, tiedEndpoints, tiedMetrics)
	if t1.SelectedEndpoint != "10.0.0.1:8000" || t2.SelectedEndpoint != "10.0.0.2:8000" || t3.SelectedEndpoint != "10.0.0.1:8000" {
		t.Fatalf("unexpected tie-break sequence: %s, %s, %s", t1.SelectedEndpoint, t2.SelectedEndpoint, t3.SelectedEndpoint)
	}
}

func TestPickerService_PreservesDiscoveryError(t *testing.T) {
	discoveryErr := errors.New("k8s api 403 forbidden")
	pickerNoWait := application.NewEndpointPickerService(
		&stubDiscovery{err: discoveryErr},
		&stubMetrics{},
		application.NewActiveSelector(domain.RoutingModeCompositeScore, 10*time.Second),
	)
	d1 := pickerNoWait.ProcessRequest(context.Background(), domain.InferenceRequestContext{RequestID: "r1", Model: "m"})
	if !strings.HasPrefix(d1.Reason, "discovery_error:") || !strings.Contains(d1.Reason, "403 forbidden") {
		t.Fatalf("expected discovery_error without waitTimeout, got %q", d1.Reason)
	}

	pickerWithWait := application.NewEndpointPickerServiceWithWaitTimeout(
		&stubDiscovery{err: discoveryErr},
		&stubMetrics{},
		application.NewActiveSelector(domain.RoutingModeCompositeScore, 10*time.Second),
		60*time.Millisecond,
	)
	d2 := pickerWithWait.ProcessRequest(context.Background(), domain.InferenceRequestContext{RequestID: "r2", Model: "m"})
	if !strings.HasPrefix(d2.Reason, "discovery_error:") || !strings.Contains(d2.Reason, "403 forbidden") {
		t.Fatalf("expected discovery_error with waitTimeout, got %q", d2.Reason)
	}
}
