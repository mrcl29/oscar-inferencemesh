// Package extproc implements the gRPC ExternalProcessor server for Envoy.
package extproc

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"time"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	extprocv3 "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	typev3 "github.com/envoyproxy/go-control-plane/envoy/type/v3"
	"google.golang.org/grpc"

	"github.com/grycap/oscar-inferencemesh/src/internal/application"
	"github.com/grycap/oscar-inferencemesh/src/internal/config"
	"github.com/grycap/oscar-inferencemesh/src/internal/domain"
)

// GrpcExtProcServer implements extprocv3.ExternalProcessorServer.
type GrpcExtProcServer struct {
	extprocv3.UnimplementedExternalProcessorServer
	settings config.Settings
	picker   *application.EndpointPickerService
}

// NewGrpcExtProcServer creates a new GrpcExtProcServer instance.
func NewGrpcExtProcServer(
	settings config.Settings,
	picker *application.EndpointPickerService,
) *GrpcExtProcServer {
	return &GrpcExtProcServer{
		settings: settings,
		picker:   picker,
	}
}

// Process implements the bidirectional streaming RPC for Envoy ext_proc filter.
// TODO(RB-02): Validate negotiated processing phases against the gateway policy;
// reply with the matching body/trailer response type rather than an empty envelope,
// and test stream cancellation and unexpected phases.
func (s *GrpcExtProcServer) Process(srv extprocv3.ExternalProcessor_ProcessServer) error {
	ctx := srv.Context()
	log.Printf("[EPP-gRPC] New ext_proc bidirectional stream connected from Envoy")

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		req, err := srv.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}

		switch v := req.Request.(type) {
		case *extprocv3.ProcessingRequest_RequestHeaders:
			resp := s.handleRequestHeaders(ctx, v.RequestHeaders)
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err := srv.Send(resp); err != nil {
				return fmt.Errorf("failed to send request headers response: %w", err)
			}

		case *extprocv3.ProcessingRequest_ResponseHeaders:
			resp := &extprocv3.ProcessingResponse{
				Response: &extprocv3.ProcessingResponse_ResponseHeaders{
					ResponseHeaders: &extprocv3.HeadersResponse{},
				},
			}
			if err := srv.Send(resp); err != nil {
				return fmt.Errorf("failed to send response headers response: %w", err)
			}

		default:
			// For body or trailers in pass-through mode, return an empty immediate response
			resp := &extprocv3.ProcessingResponse{}
			if err := srv.Send(resp); err != nil {
				return err
			}
		}
	}
}

func (s *GrpcExtProcServer) handleRequestHeaders(
	ctx context.Context,
	reqHeaders *extprocv3.HttpHeaders,
) *extprocv3.ProcessingResponse {
	headersMap := make(map[string]string)
	if reqHeaders != nil && reqHeaders.Headers != nil {
		for _, h := range reqHeaders.Headers.Headers {
			// TODO(RB-02): Handle negotiated raw header bytes as well as string values;
			// test request decoding and mutation encoding with the deployed Envoy version.
			headersMap[h.Key] = h.Value
		}
	}

	reqID := headersMap["x-request-id"]
	if reqID == "" {
		reqID = fmt.Sprintf("extproc-%d", time.Now().UnixNano()%1000000)
	}

	model := headersMap["x-ai-eg-model"]
	if model == "" {
		model = headersMap["x-model-name"]
	}
	if model == "" {
		model = s.settings.ModelName
	}

	log.Printf("[EPP-gRPC] Processing request headers for model '%s' (request_id=%s)", model, reqID)

	reqCtx := domain.InferenceRequestContext{
		RequestID:     reqID,
		Model:         model,
		PromptPreview: "Envoy ext_proc stream",
		Headers:       headersMap,
		ReceivedAt:    time.Now().UTC(),
	}

	decision := s.picker.ProcessRequest(ctx, reqCtx)
	if ctx.Err() != nil {
		return nil
	}
	if decision.Reason == "no_ready_workers" {
		return &extprocv3.ProcessingResponse{
			Response: &extprocv3.ProcessingResponse_ImmediateResponse{
				ImmediateResponse: &extprocv3.ImmediateResponse{
					Status: &typev3.HttpStatus{Code: typev3.StatusCode_ServiceUnavailable},
					Body:   []byte("no ready inference workers available"),
				},
			},
		}
	}
	if strings.HasPrefix(decision.Reason, "discovery_error:") {
		return &extprocv3.ProcessingResponse{
			Response: &extprocv3.ProcessingResponse_ImmediateResponse{
				ImmediateResponse: &extprocv3.ImmediateResponse{
					Status: &typev3.HttpStatus{Code: typev3.StatusCode_ServiceUnavailable},
					Body:   []byte("inference endpoint discovery failed"),
				},
			},
		}
	}
	// TODO(FG-02): Translate SelectedEndpoint into an effective, supported Envoy routing
	// mechanism; current mutations carry diagnostics only. Prove the selected worker
	// receives traffic end to end, without assuming an arbitrary header changes routing.

	var setHeaders []*corev3.HeaderValueOption
	for k, v := range decision.ResponseHeaders {
		setHeaders = append(setHeaders, &corev3.HeaderValueOption{
			Header: &corev3.HeaderValue{
				Key:   k,
				Value: v,
			},
		})
	}

	return &extprocv3.ProcessingResponse{
		Response: &extprocv3.ProcessingResponse_RequestHeaders{
			RequestHeaders: &extprocv3.HeadersResponse{
				Response: &extprocv3.CommonResponse{
					HeaderMutation: &extprocv3.HeaderMutation{
						SetHeaders: setHeaders,
					},
				},
			},
		},
	}
}

// StartGrpcServer starts the gRPC ext_proc listener on the configured port.
func StartGrpcServer(
	settings config.Settings,
	picker *application.EndpointPickerService,
) (*grpc.Server, net.Listener, error) {
	addr := fmt.Sprintf(":%d", settings.GRPCExtProcPort)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to listen on gRPC port %d: %w", settings.GRPCExtProcPort, err)
	}

	grpcServer := grpc.NewServer()
	srv := NewGrpcExtProcServer(settings, picker)
	extprocv3.RegisterExternalProcessorServer(grpcServer, srv)

	log.Printf("[EPP-gRPC] Envoy ext_proc server listening on port %d", settings.GRPCExtProcPort)
	return grpcServer, lis, nil
}
