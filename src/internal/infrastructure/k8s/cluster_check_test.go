package k8s_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/grycap/oscar-inferencemesh/src/internal/infrastructure/k8s"
)

func TestCheckReadinessImmediateSuccess(t *testing.T) {
	checker := &k8s.DefaultClusterChecker{
		CommandRunner: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return []byte("Kubernetes control plane is running at https://127.0.0.1:39987\nCoreDNS is running at ..."), nil
		},
	}

	retried := false
	status := checker.CheckReadiness(context.Background(), 3, 10*time.Millisecond, func(attempt, max int, wait time.Duration) {
		retried = true
	})

	if !status.Ready {
		t.Fatalf("expected Ready=true, got error: %v", status.Error)
	}
	if status.Attempts != 1 {
		t.Fatalf("expected 1 attempt, got %d", status.Attempts)
	}
	if status.EndpointInfo != "Kubernetes control plane is running at https://127.0.0.1:39987" {
		t.Fatalf("unexpected endpoint: %s", status.EndpointInfo)
	}
	if retried {
		t.Fatalf("expected no retries for immediate success")
	}
}

func TestCheckReadinessSuccessOnRetry(t *testing.T) {
	callCount := 0
	checker := &k8s.DefaultClusterChecker{
		CommandRunner: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			callCount++
			if callCount < 3 {
				return []byte("dial tcp 127.0.0.1:39987: connect: connection refused"), errors.New("exit status 1")
			}
			return []byte("Kubernetes control plane is running at https://127.0.0.1:39987"), nil
		},
	}

	retryCallbacks := 0
	status := checker.CheckReadiness(context.Background(), 5, 5*time.Millisecond, func(attempt, max int, wait time.Duration) {
		retryCallbacks++
	})

	if !status.Ready {
		t.Fatalf("expected Ready=true after retries, got error: %v", status.Error)
	}
	if status.Attempts != 3 {
		t.Fatalf("expected 3 attempts, got %d", status.Attempts)
	}
	if retryCallbacks != 2 {
		t.Fatalf("expected 2 retry callbacks, got %d", retryCallbacks)
	}
}

func TestCheckReadinessExhaustedRetries(t *testing.T) {
	checker := &k8s.DefaultClusterChecker{
		CommandRunner: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return []byte("connect: connection refused"), errors.New("exit status 1")
		},
	}

	status := checker.CheckReadiness(context.Background(), 3, 5*time.Millisecond, nil)

	if status.Ready {
		t.Fatalf("expected Ready=false")
	}
	if status.Attempts != 3 {
		t.Fatalf("expected 3 attempts, got %d", status.Attempts)
	}
	if status.Error == nil {
		t.Fatalf("expected error, got nil")
	}
}

func TestCheckReadinessContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	checker := &k8s.DefaultClusterChecker{
		CommandRunner: func(c context.Context, name string, args ...string) ([]byte, error) {
			cancel() // Cancel context on first failure
			return []byte("connect: connection refused"), errors.New("exit status 1")
		},
	}

	status := checker.CheckReadiness(ctx, 5, 50*time.Millisecond, nil)

	if status.Ready {
		t.Fatalf("expected Ready=false")
	}
	if !errors.Is(status.Error, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", status.Error)
	}
}
