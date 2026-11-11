// Package application contains the use-case orchestrators and policies for the
// OSCAR InferenceMesh Endpoint Picker (EPP).
package application

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/grycap/oscar-inferencemesh/src/internal/domain"
)

var ErrNoReadyWorkers = errors.New("no ready workers before wait deadline")

// WaitForReadyEndpoints polls discovery until at least one ready endpoint appears or the wait expires.
func WaitForReadyEndpoints(ctx context.Context, timeout, interval time.Duration, discover func(context.Context) ([]domain.WorkerEndpoint, error)) ([]domain.WorkerEndpoint, error) {
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for {
		if err := waitCtx.Err(); err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, ErrNoReadyWorkers
		}
		endpoints, err := discover(waitCtx)
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if waitCtx.Err() != nil {
			return nil, ErrNoReadyWorkers
		}
		if err == nil {
			ready := make([]domain.WorkerEndpoint, 0, len(endpoints))
			for _, endpoint := range endpoints {
				if endpoint.Ready {
					ready = append(ready, endpoint)
				}
			}
			if len(ready) > 0 {
				return ready, nil
			}
		}
		ticker := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			ticker.Stop()
			return nil, ctx.Err()
		case <-waitCtx.Done():
			ticker.Stop()
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, ErrNoReadyWorkers
		case <-ticker.C:
		}
	}
}

// PassThroughSelector implements domain.EndpointSelectorPort as a verifiable no-op.
// It inspects candidate endpoints and metrics snapshots, delegates actual routing
// to the default Kubernetes/Envoy service, and attaches x-oscar-epp-* headers.
type PassThroughSelector struct{}

// NewPassThroughSelector returns a new PassThroughSelector instance.
func NewPassThroughSelector() *PassThroughSelector {
	return &PassThroughSelector{}
}

// SelectEndpoint records candidate state and returns a pass-through routing decision.
// TODO(FG-01): Add active selection alongside this baseline using ready workers and valid,
// fresh telemetry, deterministic ties, and explicit empty/unhealthy-pool behavior.
func (s *PassThroughSelector) SelectEndpoint(
	reqCtx domain.InferenceRequestContext,
	endpoints []domain.WorkerEndpoint,
	metrics map[string]domain.EndpointMetrics,
) domain.RoutingDecision {
	observedTarget := "envoy-default"
	if len(endpoints) > 0 {
		observedTarget = endpoints[0].SocketAddress()
	}

	headers := map[string]string{
		"x-oscar-epp-mode":            string(domain.RoutingModePassThrough),
		"x-oscar-epp-verified":        "true",
		"x-oscar-epp-request-id":      reqCtx.RequestID,
		"x-oscar-epp-candidates":      strconv.Itoa(len(endpoints)),
		"x-oscar-epp-observed-target": observedTarget,
	}

	return domain.RoutingDecision{
		RequestID:        reqCtx.RequestID,
		Model:            reqCtx.Model,
		Mode:             domain.RoutingModePassThrough,
		SelectedEndpoint: "",
		Reason: fmt.Sprintf(
			"No-op pass-through decision: observed %d candidate(s) and %d metric snapshot(s); delegating routing to default upstream pool.",
			len(endpoints),
			len(metrics),
		),
		CandidateCount:  len(endpoints),
		ResponseHeaders: headers,
		DecidedAt:       time.Now().UTC(),
	}
}

// PickerVerificationState exposes diagnostic telemetry for the picker pipeline.
type PickerVerificationState struct {
	TotalRequestsProcessed  int                     `json:"total_requests_processed"`
	RecentDecisionsBuffered int                     `json:"recent_decisions_buffered"`
	LastDecision            *domain.RoutingDecision `json:"last_decision,omitempty"`
}

// EndpointPickerService orchestrates endpoint discovery, metrics scraping, and replica selection.
type EndpointPickerService struct {
	discovery   domain.EndpointDiscoveryPort
	metrics     domain.MetricsCollectorPort
	selector    domain.EndpointSelectorPort
	maxHistory  int
	waitTimeout time.Duration

	mu             sync.RWMutex
	totalProcessed int
	history        []domain.RoutingDecision
}

// NewEndpointPickerServiceWithWaitTimeout configures bounded waiting for ready workers.
func NewEndpointPickerServiceWithWaitTimeout(discovery domain.EndpointDiscoveryPort, metrics domain.MetricsCollectorPort, selector domain.EndpointSelectorPort, timeout time.Duration) *EndpointPickerService {
	s := NewEndpointPickerService(discovery, metrics, selector)
	s.waitTimeout = timeout
	return s
}

// NewEndpointPickerService wires the EndpointPickerService use case with its ports.
func NewEndpointPickerService(
	discovery domain.EndpointDiscoveryPort,
	metrics domain.MetricsCollectorPort,
	selector domain.EndpointSelectorPort,
) *EndpointPickerService {
	return &EndpointPickerService{
		discovery:  discovery,
		metrics:    metrics,
		selector:   selector,
		maxHistory: 50,
		history:    make([]domain.RoutingDecision, 0, 50),
	}
}

// ProcessRequest executes the full discovery -> metrics -> selection pipeline.
func (s *EndpointPickerService) ProcessRequest(
	ctx context.Context,
	reqCtx domain.InferenceRequestContext,
) domain.RoutingDecision {
	// TODO(FG-01): Preserve discovery failures in the routing outcome instead of silently treating them as an empty pool.
	var endpoints []domain.WorkerEndpoint
	if s.waitTimeout > 0 {
		var err error
		endpoints, err = WaitForReadyEndpoints(ctx, s.waitTimeout, 250*time.Millisecond, func(ctx context.Context) ([]domain.WorkerEndpoint, error) {
			return s.discovery.ListEndpoints(ctx, reqCtx.Model)
		})
			if err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					return domain.RoutingDecision{RequestID: reqCtx.RequestID, Model: reqCtx.Model, Reason: err.Error(), DecidedAt: time.Now().UTC()}
			}
			decision := domain.RoutingDecision{RequestID: reqCtx.RequestID, Model: reqCtx.Model, Reason: "no_ready_workers", DecidedAt: time.Now().UTC()}
			return decision
		}
	} else {
		var err error
		endpoints, err = s.discovery.ListEndpoints(ctx, reqCtx.Model)
		if err != nil || !hasReadyEndpoint(endpoints) {
			return domain.RoutingDecision{RequestID: reqCtx.RequestID, Model: reqCtx.Model, Reason: "no_ready_workers", DecidedAt: time.Now().UTC()}
		}
	}
	metricsMap := s.metrics.CollectAll(ctx, endpoints)
	decision := s.selector.SelectEndpoint(reqCtx, endpoints, metricsMap)

	s.mu.Lock()
	s.totalProcessed++
	if len(s.history) >= s.maxHistory {
		s.history = s.history[1:]
	}
	s.history = append(s.history, decision)
	s.mu.Unlock()

	return decision
}

func hasReadyEndpoint(endpoints []domain.WorkerEndpoint) bool {
	for _, endpoint := range endpoints {
		if endpoint.Ready {
			return true
		}
	}
	return false
}

// GetVerificationState returns diagnostic evidence that the picker pipeline is wired.
func (s *EndpointPickerService) GetVerificationState() PickerVerificationState {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var last *domain.RoutingDecision
	if len(s.history) > 0 {
		copyLast := s.history[len(s.history)-1]
		last = &copyLast
	}

	return PickerVerificationState{
		TotalRequestsProcessed:  s.totalProcessed,
		RecentDecisionsBuffered: len(s.history),
		LastDecision:            last,
	}
}
