package application_test

import (
	"context"
	"testing"
	"time"

	"github.com/grycap/oscar-inferencemesh/src/internal/application"
	"github.com/grycap/oscar-inferencemesh/src/internal/domain"
)

type stubDiscovery struct {
	endpoints []domain.WorkerEndpoint
}

func (s *stubDiscovery) ListEndpoints(_ context.Context, _ string) ([]domain.WorkerEndpoint, error) {
	return s.endpoints, nil
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
