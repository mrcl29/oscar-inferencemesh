// Package application contains the use-case orchestrators and policies for the
// OSCAR InferenceMesh Endpoint Picker (EPP).
package application

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/grycap/oscar-inferencemesh/src/internal/domain"
)

var (
	// ErrNoReadyWorkers indicates discovery ran without returning any ready worker before the wait deadline.
	ErrNoReadyWorkers = errors.New("no ready workers before wait deadline")
	// ErrDiscoveryFailed indicates endpoint discovery returned errors throughout the wait window.
	ErrDiscoveryFailed = errors.New("endpoint discovery failed")
)

// WaitForReadyEndpoints polls discovery until at least one ready endpoint appears or the wait expires.
func WaitForReadyEndpoints(ctx context.Context, timeout, interval time.Duration, discover func(context.Context) ([]domain.WorkerEndpoint, error)) ([]domain.WorkerEndpoint, error) {
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var sawSuccess bool
	var lastErr error

	deadlineOutcome := func() error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !sawSuccess && lastErr != nil {
			return fmt.Errorf("%w: %v", ErrDiscoveryFailed, lastErr)
		}
		return ErrNoReadyWorkers
	}

	for {
		if waitCtx.Err() != nil {
			return nil, deadlineOutcome()
		}
		endpoints, err := discover(waitCtx)
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if err == nil {
			sawSuccess = true
			lastErr = nil
		} else if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			lastErr = err
		}
		if waitCtx.Err() != nil {
			return nil, deadlineOutcome()
		}
		if err == nil {
			ready := filterReadyEndpoints(endpoints)
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
			return nil, deadlineOutcome()
		case <-ticker.C:
		}
	}
}

func filterReadyEndpoints(endpoints []domain.WorkerEndpoint) []domain.WorkerEndpoint {
	ready := make([]domain.WorkerEndpoint, 0, len(endpoints))
	for _, endpoint := range endpoints {
		if endpoint.Ready {
			ready = append(ready, endpoint)
		}
	}
	return ready
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

const defaultMaxMetricsAge = 10 * time.Second

// ActiveSelector implements domain.EndpointSelectorPort by ranking ready workers
// using fresh telemetry (composite_score, least_kv_cache, least_queue_depth),
// with deterministic tie-breaking and round-robin fallback when telemetry is missing or stale.
type ActiveSelector struct {
	mode          domain.RoutingMode
	maxMetricsAge time.Duration
	now           func() time.Time
	rrCounter     uint64
}

// NewActiveSelector creates an ActiveSelector configured for the given routing mode and telemetry freshness window.
func NewActiveSelector(mode domain.RoutingMode, maxMetricsAge time.Duration) *ActiveSelector {
	switch mode {
	case domain.RoutingModeCompositeScore, domain.RoutingModeLeastKVCache, domain.RoutingModeLeastQueueDepth:
	default:
		mode = domain.RoutingModeCompositeScore
	}
	if maxMetricsAge <= 0 {
		maxMetricsAge = defaultMaxMetricsAge
	}
	return &ActiveSelector{
		mode:          mode,
		maxMetricsAge: maxMetricsAge,
		now:           func() time.Time { return time.Now().UTC() },
	}
}

type scoredEndpoint struct {
	endpoint domain.WorkerEndpoint
	metrics  domain.EndpointMetrics
	score    float64
}

// SelectEndpoint chooses a ready endpoint according to the active routing policy.
func (s *ActiveSelector) SelectEndpoint(
	reqCtx domain.InferenceRequestContext,
	endpoints []domain.WorkerEndpoint,
	metrics map[string]domain.EndpointMetrics,
) domain.RoutingDecision {
	now := s.now().UTC()
	ready := filterReadyEndpoints(endpoints)
	sort.Slice(ready, func(i, j int) bool {
		if ready[i].Name != ready[j].Name {
			return ready[i].Name < ready[j].Name
		}
		return ready[i].SocketAddress() < ready[j].SocketAddress()
	})

	if len(ready) == 0 {
		return domain.RoutingDecision{
			RequestID:        reqCtx.RequestID,
			Model:            reqCtx.Model,
			Mode:             s.mode,
			SelectedEndpoint: "",
			Reason:           "no_ready_workers",
			CandidateCount:   0,
			ResponseHeaders: map[string]string{
				"x-oscar-epp-mode":       string(s.mode),
				"x-oscar-epp-verified":   "false",
				"x-oscar-epp-request-id": reqCtx.RequestID,
				"x-oscar-epp-candidates": "0",
			},
			DecidedAt: now,
		}
	}

	scored := make([]scoredEndpoint, 0, len(ready))
	for _, ep := range ready {
		m, ok := metrics[ep.Name]
		if !ok || !isMetricValidAndFresh(m, now, s.maxMetricsAge) {
			continue
		}
		scored = append(scored, scoredEndpoint{
			endpoint: ep,
			metrics:  m,
			score:    scoreEndpoint(s.mode, m),
		})
	}

	var selected domain.WorkerEndpoint
	var reason string
	telemetryState := "fresh"

	if len(scored) > 0 {
		bestScore := scored[0].score
		for _, item := range scored[1:] {
			if item.score < bestScore {
				bestScore = item.score
			}
		}
		const scoreEpsilon = 1e-9
		tied := make([]scoredEndpoint, 0, len(scored))
		for _, item := range scored {
			if math.Abs(item.score-bestScore) <= scoreEpsilon {
				tied = append(tied, item)
			}
		}
		idx := int(atomic.AddUint64(&s.rrCounter, 1)-1) % len(tied)
		chosen := tied[idx]
		selected = chosen.endpoint
		reason = fmt.Sprintf(
			"Selected %s (%s) via %s: score=%.3f (kv=%.3f, waiting=%d, running=%d) across %d/%d telemetry-ready candidates",
			selected.Name,
			selected.SocketAddress(),
			s.mode,
			chosen.score,
			chosen.metrics.KVCacheUsagePerc,
			chosen.metrics.WaitingRequests,
			chosen.metrics.RunningRequests,
			len(scored),
			len(ready),
		)
	} else {
		telemetryState = "fallback_rr"
		idx := int(atomic.AddUint64(&s.rrCounter, 1)-1) % len(ready)
		selected = ready[idx]
		reason = fmt.Sprintf(
			"Telemetry unavailable or stale for %d ready candidate(s); fallback round-robin selected %s (%s)",
			len(ready),
			selected.Name,
			selected.SocketAddress(),
		)
	}

	headers := map[string]string{
		"x-oscar-epp-mode":              string(s.mode),
		"x-oscar-epp-verified":          "true",
		"x-oscar-epp-request-id":        reqCtx.RequestID,
		"x-oscar-epp-candidates":        strconv.Itoa(len(ready)),
		"x-oscar-epp-selected-worker":   selected.Name,
		"x-oscar-epp-selected-endpoint": selected.SocketAddress(),
		"x-oscar-epp-observed-target":   selected.SocketAddress(),
		"x-oscar-epp-telemetry":         telemetryState,
	}

	return domain.RoutingDecision{
		RequestID:        reqCtx.RequestID,
		Model:            reqCtx.Model,
		Mode:             s.mode,
		SelectedEndpoint: selected.SocketAddress(),
		Reason:           reason,
		CandidateCount:   len(ready),
		ResponseHeaders:  headers,
		DecidedAt:        now,
	}
}

func isMetricValidAndFresh(m domain.EndpointMetrics, now time.Time, maxAge time.Duration) bool {
	if !m.Reachable || m.ScrapedAt.IsZero() {
		return false
	}
	age := now.Sub(m.ScrapedAt)
	if age < -5*time.Second || age > maxAge {
		return false
	}
	if math.IsNaN(m.KVCacheUsagePerc) || math.IsInf(m.KVCacheUsagePerc, 0) || m.KVCacheUsagePerc < 0 {
		return false
	}
	if m.WaitingRequests < 0 || m.RunningRequests < 0 {
		return false
	}
	return true
}

func scoreEndpoint(mode domain.RoutingMode, m domain.EndpointMetrics) float64 {
	switch mode {
	case domain.RoutingModeLeastKVCache:
		return m.KVCacheUsagePerc
	case domain.RoutingModeLeastQueueDepth:
		return float64(m.WaitingRequests)
	default:
		return m.KVCacheUsagePerc*1.0 + float64(m.WaitingRequests)*0.5 + float64(m.RunningRequests)*0.1
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
			if errors.Is(err, ErrDiscoveryFailed) {
				return domain.RoutingDecision{
					RequestID: reqCtx.RequestID,
					Model:     reqCtx.Model,
					Reason:    fmt.Sprintf("discovery_error: %v", err),
					ResponseHeaders: map[string]string{
						"x-oscar-epp-verified":   "false",
						"x-oscar-epp-request-id": reqCtx.RequestID,
					},
					DecidedAt: time.Now().UTC(),
				}
			}
			return domain.RoutingDecision{
				RequestID: reqCtx.RequestID,
				Model:     reqCtx.Model,
				Reason:    "no_ready_workers",
				ResponseHeaders: map[string]string{
					"x-oscar-epp-verified":   "false",
					"x-oscar-epp-request-id": reqCtx.RequestID,
				},
				DecidedAt: time.Now().UTC(),
			}
		}
	} else {
		discovered, err := s.discovery.ListEndpoints(ctx, reqCtx.Model)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return domain.RoutingDecision{RequestID: reqCtx.RequestID, Model: reqCtx.Model, Reason: err.Error(), DecidedAt: time.Now().UTC()}
			}
			return domain.RoutingDecision{
				RequestID: reqCtx.RequestID,
				Model:     reqCtx.Model,
				Reason:    fmt.Sprintf("discovery_error: %v", err),
				ResponseHeaders: map[string]string{
					"x-oscar-epp-verified":   "false",
					"x-oscar-epp-request-id": reqCtx.RequestID,
				},
				DecidedAt: time.Now().UTC(),
			}
		}
		endpoints = filterReadyEndpoints(discovered)
		if len(endpoints) == 0 {
			return domain.RoutingDecision{
				RequestID: reqCtx.RequestID,
				Model:     reqCtx.Model,
				Reason:    "no_ready_workers",
				ResponseHeaders: map[string]string{
					"x-oscar-epp-verified":   "false",
					"x-oscar-epp-request-id": reqCtx.RequestID,
				},
				DecidedAt: time.Now().UTC(),
			}
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
