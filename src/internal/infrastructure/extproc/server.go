// Package extproc provides the HTTP verification and pass-through simulation
// server for the OSCAR InferenceMesh Endpoint Picker (EPP).
package extproc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/grycap/oscar-inferencemesh/src/internal/application"
	"github.com/grycap/oscar-inferencemesh/src/internal/config"
	"github.com/grycap/oscar-inferencemesh/src/internal/domain"
)

// VerificationReport aggregates the full pass-through state of the EPP and OSCAR connector.
type VerificationReport struct {
	Component               string                                  `json:"component"`
	Status                  string                                  `json:"status"`
	Mode                    string                                  `json:"mode"`
	Config                  config.Settings                         `json:"config"`
	Picker                  application.PickerVerificationState     `json:"picker"`
	Autoscaler              application.AutoscalerVerificationState `json:"autoscaler"`
	LatestReconciliation    domain.ScalingDecision                  `json:"latest_reconciliation"`
	SimulatedRoutingOutcome *domain.RoutingDecision                 `json:"simulated_routing_decision,omitempty"`
}

// SimulateRequestPayload defines the JSON payload accepted by POST /v1/ext_proc/simulate.
type SimulateRequestPayload struct {
	RequestID string            `json:"request_id"`
	Model     string            `json:"model"`
	Prompt    string            `json:"prompt"`
	Headers   map[string]string `json:"headers"`
}

// Server exposes health, verification, and pass-through request simulation endpoints.
type Server struct {
	settings   config.Settings
	picker     *application.EndpointPickerService
	autoscaler *application.AutoscalerOrchestrator
}

// NewServer creates a new EPP verification and simulation Server.
func NewServer(
	settings config.Settings,
	picker *application.EndpointPickerService,
	autoscaler *application.AutoscalerOrchestrator,
) *Server {
	return &Server{
		settings:   settings,
		picker:     picker,
		autoscaler: autoscaler,
	}
}

// BuildVerificationReport executes a single autoscaler reconciliation and builds a diagnostic report.
func (s *Server) BuildVerificationReport(ctx context.Context) VerificationReport {
	reconciliation := s.autoscaler.ReconcileOnce(ctx)
	return VerificationReport{
		Component:            "oscar-inferencemesh-epp",
		Status:               "ok",
		Mode:                 "pass_through_skeleton",
		Config:               s.settings,
		Picker:               s.picker.GetVerificationState(),
		Autoscaler:           s.autoscaler.GetVerificationState(ctx),
		LatestReconciliation: reconciliation,
	}
}

// SimulateRequest runs an incoming or synthetic request through the pass-through EndpointPickerService.
func (s *Server) SimulateRequest(
	ctx context.Context,
	payload SimulateRequestPayload,
) domain.RoutingDecision {
	reqID := payload.RequestID
	if reqID == "" {
		reqID = fmt.Sprintf("sim-%d", time.Now().UnixNano()%1000000)
	}
	model := payload.Model
	if model == "" {
		model = s.settings.ModelName
	}
	prompt := payload.Prompt
	if prompt == "" {
		prompt = "Ping verification prompt"
	}
	if len(prompt) > 80 {
		prompt = prompt[:80]
	}

	reqCtx := domain.InferenceRequestContext{
		RequestID:     reqID,
		Model:         model,
		PromptPreview: prompt,
		Headers:       payload.Headers,
		ReceivedAt:    time.Now().UTC(),
	}
	return s.picker.ProcessRequest(ctx, reqCtx)
}

// Handler returns the http.Handler Multiplexer for the EPP verification server.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"}, nil)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{
			"status": "healthy",
			"mode":   "pass_through_skeleton",
			"model":  s.settings.ModelName,
		}, nil)
	})

	mux.HandleFunc("/verify", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"}, nil)
			return
		}
		report := s.BuildVerificationReport(r.Context())
		writeJSON(w, http.StatusOK, report, nil)
	})

	simulateHandler := func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"}, nil)
			return
		}
		var payload SimulateRequestPayload
		if r.Body != nil {
			defer r.Body.Close()
			bodyBytes, _ := io.ReadAll(io.LimitReader(r.Body, 64*1024))
			if len(bodyBytes) > 0 {
				_ = json.Unmarshal(bodyBytes, &payload)
			}
		}

		decision := s.SimulateRequest(r.Context(), payload)
		writeJSON(w, http.StatusOK, decision, decision.ResponseHeaders)
	}

	mux.HandleFunc("/v1/ext_proc/simulate", simulateHandler)
	mux.HandleFunc("/pick", simulateHandler)

	return mux
}

// ListenAndServe starts both the HTTP verification server and the gRPC ext_proc server.
func (s *Server) ListenAndServe() error {
	grpcServer, lis, err := StartGrpcServer(s.settings, s.picker)
	if err != nil {
		return fmt.Errorf("failed to initialize gRPC ext_proc server: %w", err)
	}
	defer grpcServer.GracefulStop()

	go func() {
		if err := grpcServer.Serve(lis); err != nil {
			log.Printf("[EPP-gRPC] Server stopped: %v", err)
		}
	}()

	addr := fmt.Sprintf("%s:%d", s.settings.HTTPHost, s.settings.HTTPPort)
	log.Printf(
		"[EPP-HTTP] Starting Golang verification server on http://%s (GET /healthz, GET /verify, POST /v1/ext_proc/simulate)",
		addr,
	)
	httpServer := &http.Server{
		Addr:              addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	return httpServer.ListenAndServe()
}

func writeJSON(
	w http.ResponseWriter,
	status int,
	payload any,
	extraHeaders map[string]string,
) {
	encoded, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	encoded = append(encoded, '\n')

	w.Header().Set("Content-Type", "application/json")
	for k, v := range extraHeaders {
		w.Header().Set(k, v)
	}
	w.WriteHeader(status)
	_, _ = w.Write(encoded)
}
