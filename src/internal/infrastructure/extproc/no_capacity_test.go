package extproc

import (
	"context"
	"testing"
	"time"

	extprocv3 "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	"github.com/grycap/oscar-inferencemesh/src/internal/application"
	"github.com/grycap/oscar-inferencemesh/src/internal/config"
	"github.com/grycap/oscar-inferencemesh/src/internal/domain"
)

type noWorkers struct{}

func (noWorkers) ListEndpoints(context.Context, string) ([]domain.WorkerEndpoint, error) {
	return nil, nil
}

type noMetrics struct{}

func (noMetrics) CollectAll(context.Context, []domain.WorkerEndpoint) map[string]domain.EndpointMetrics {
	return nil
}

func TestNoReadyWorkersProduceExtProc503(t *testing.T) {
	picker := application.NewEndpointPickerServiceWithWaitTimeout(noWorkers{}, noMetrics{}, application.NewPassThroughSelector(), time.Millisecond)
	s := NewGrpcExtProcServer(config.Settings{ModelName: "model"}, picker)
	resp := s.handleRequestHeaders(context.Background(), &extprocv3.HttpHeaders{})
	if _, ok := resp.Response.(*extprocv3.ProcessingResponse_ImmediateResponse); !ok {
		t.Fatalf("expected immediate HTTP 503 response, got %T", resp.Response)
	}
	if resp.GetImmediateResponse().GetStatus().GetCode() != 503 {
		t.Fatalf("status = %v, want 503", resp.GetImmediateResponse().GetStatus().GetCode())
	}
}

func TestCanceledRequestDoesNotProduceExtProc503(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	picker := application.NewEndpointPickerServiceWithWaitTimeout(noWorkers{}, noMetrics{}, application.NewPassThroughSelector(), time.Second)
	server := NewGrpcExtProcServer(config.Settings{}, picker)
	response := server.handleRequestHeaders(ctx, nil)
	if response != nil {
		t.Fatalf("canceled request fabricated an ext_proc response: %+v", response)
	}
}
