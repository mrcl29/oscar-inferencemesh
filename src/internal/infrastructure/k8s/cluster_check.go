// Package k8s implements Kubernetes endpoint discovery and cluster pre-flight health checks.
package k8s

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// ClusterStatus represents the outcome of a Kubernetes connectivity check.
type ClusterStatus struct {
	Ready        bool
	EndpointInfo string
	Attempts     int
	Error        error
}

// ClusterChecker defines the contract for checking Kubernetes cluster reachability.
type ClusterChecker interface {
	CheckReadiness(ctx context.Context, maxRetries int, retryInterval time.Duration, onRetry func(attempt, max int, wait time.Duration)) ClusterStatus
}

// DefaultClusterChecker executes kubectl cluster-info to verify the Kubernetes API server.
type DefaultClusterChecker struct {
	CommandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)
}

// NewDefaultClusterChecker creates a DefaultClusterChecker using exec.CommandContext.
func NewDefaultClusterChecker() *DefaultClusterChecker {
	return &DefaultClusterChecker{
		CommandRunner: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			cmd := exec.CommandContext(ctx, name, args...)
			return cmd.CombinedOutput()
		},
	}
}

// CheckReadiness polls the Kubernetes API server until it succeeds or exhausts retries.
func (c *DefaultClusterChecker) CheckReadiness(
	ctx context.Context,
	maxRetries int,
	retryInterval time.Duration,
	onRetry func(attempt, max int, wait time.Duration),
) ClusterStatus {
	if maxRetries <= 0 {
		maxRetries = 1
	}

	var lastErr error
	for attempt := 1; attempt <= maxRetries; attempt++ {
		cmdCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
		out, err := c.CommandRunner(cmdCtx, "kubectl", "cluster-info", "--request-timeout=3s")
		cancel()

		if err == nil {
			lines := strings.Split(string(out), "\n")
			endpoint := ""
			if len(lines) > 0 && strings.TrimSpace(lines[0]) != "" {
				endpoint = strings.TrimSpace(lines[0])
			}
			return ClusterStatus{
				Ready:        true,
				EndpointInfo: endpoint,
				Attempts:     attempt,
			}
		}

		lastErr = fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))

		if attempt < maxRetries {
			if onRetry != nil {
				onRetry(attempt, maxRetries, retryInterval)
			}
			select {
			case <-ctx.Done():
				return ClusterStatus{
					Ready:    false,
					Attempts: attempt,
					Error:    ctx.Err(),
				}
			case <-time.After(retryInterval):
			}
		}
	}

	return ClusterStatus{
		Ready:    false,
		Attempts: maxRetries,
		Error: fmt.Errorf("kubernetes cluster is unreachable after %d attempts: %w\nHint: Verify that your cluster is running ('kind get clusters' or 'docker ps') and that your current kubeconfig context is valid",
			maxRetries, lastErr),
	}
}
