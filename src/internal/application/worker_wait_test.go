package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/grycap/oscar-inferencemesh/src/internal/domain"
)

func TestWaitForReadyEndpointsRetriesUntilWorkerAppears(t *testing.T) {
	attempts := 0
	got, err := WaitForReadyEndpoints(context.Background(), 100*time.Millisecond, time.Millisecond, func(context.Context) ([]domain.WorkerEndpoint, error) {
		attempts++
		if attempts < 3 {
			return nil, nil
		}
		return []domain.WorkerEndpoint{{Name: "worker", Ready: true}}, nil
	})
	if err != nil || len(got) != 1 || attempts != 3 {
		t.Fatalf("got endpoints=%v attempts=%d err=%v", got, attempts, err)
	}
}

func TestWaitForReadyEndpointsTimesOut(t *testing.T) {
	started := time.Now()
	_, err := WaitForReadyEndpoints(context.Background(), 5*time.Millisecond, time.Millisecond, func(context.Context) ([]domain.WorkerEndpoint, error) { return nil, nil })
	if !errors.Is(err, ErrNoReadyWorkers) || time.Since(started) > 100*time.Millisecond {
		t.Fatalf("expected bounded no-ready timeout, got %v", err)
	}
}

func TestWaitForReadyEndpointsRejectsWorkerAfterDeadline(t *testing.T) {
	_, err := WaitForReadyEndpoints(context.Background(), 2*time.Millisecond, time.Millisecond, func(context.Context) ([]domain.WorkerEndpoint, error) {
		time.Sleep(5 * time.Millisecond)
		return []domain.WorkerEndpoint{{Name: "late", Ready: true}}, nil
	})
	if !errors.Is(err, ErrNoReadyWorkers) {
		t.Fatalf("expected late worker to miss deadline, got %v", err)
	}
}

func TestWaitForReadyEndpointsStopsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := WaitForReadyEndpoints(ctx, time.Second, time.Millisecond, func(context.Context) ([]domain.WorkerEndpoint, error) { return nil, nil })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
}

func TestWaitForReadyEndpointsDoesNotAcceptWorkerAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	_, err := WaitForReadyEndpoints(ctx, time.Second, time.Millisecond, func(context.Context) ([]domain.WorkerEndpoint, error) {
		cancel()
		return []domain.WorkerEndpoint{{Name: "worker", Ready: true}}, nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled wait, got %v", err)
	}
}

func TestWaitForReadyEndpointsBoundsContextAwareDiscovery(t *testing.T) {
	started := time.Now()
	_, err := WaitForReadyEndpoints(context.Background(), 10*time.Millisecond, time.Millisecond, func(ctx context.Context) ([]domain.WorkerEndpoint, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})
	if !errors.Is(err, ErrNoReadyWorkers) {
		t.Fatalf("expected wait deadline, got %v", err)
	}
	if time.Since(started) > 100*time.Millisecond {
		t.Fatalf("discovery exceeded wait bound: %v", time.Since(started))
	}
}

func TestPickerReturnsExplicitNoCapacityOutcome(t *testing.T) {
	picker := NewEndpointPickerServiceWithWaitTimeout(&emptyDiscovery{}, &emptyMetrics{}, NewPassThroughSelector(), 2*time.Millisecond)
	decision := picker.ProcessRequest(context.Background(), domain.InferenceRequestContext{RequestID: "no-capacity", Model: "model"})
	if decision.Reason != "no_ready_workers" || decision.ResponseHeaders["x-oscar-epp-verified"] == "true" {
		t.Fatalf("expected explicit no-capacity outcome without verification headers, got %+v", decision)
	}
}

func TestProcessRequestDoesNotClassifyCallerCancellationAsNoCapacity(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	picker := NewEndpointPickerServiceWithWaitTimeout(&emptyDiscovery{}, &emptyMetrics{}, NewPassThroughSelector(), time.Second)
	decision := picker.ProcessRequest(ctx, domain.InferenceRequestContext{RequestID: "canceled", Model: "model"})
	if decision.Reason == "no_ready_workers" {
		t.Fatalf("caller cancellation was classified as no capacity: %+v", decision)
	}
}

func TestProcessRequestPreservesCallerDeadlineOutcome(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	picker := NewEndpointPickerServiceWithWaitTimeout(&emptyDiscovery{}, &emptyMetrics{}, NewPassThroughSelector(), time.Second)
	decision := picker.ProcessRequest(ctx, domain.InferenceRequestContext{RequestID: "deadline", Model: "model"})
	if decision.Reason != context.DeadlineExceeded.Error() {
		t.Fatalf("caller deadline outcome = %q, want %q", decision.Reason, context.DeadlineExceeded.Error())
	}
}

type emptyDiscovery struct{}

func (*emptyDiscovery) ListEndpoints(context.Context, string) ([]domain.WorkerEndpoint, error) {
	return nil, nil
}

type emptyMetrics struct{}

func (*emptyMetrics) CollectAll(context.Context, []domain.WorkerEndpoint) map[string]domain.EndpointMetrics {
	return nil
}
