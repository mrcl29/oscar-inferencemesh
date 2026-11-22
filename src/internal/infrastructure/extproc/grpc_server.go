// Package extproc implements the gRPC ExternalProcessor server for Envoy.
package extproc

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"strings"
	"time"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	extprocv3 "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	typev3 "github.com/envoyproxy/go-control-plane/envoy/type/v3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/protobuf/types/known/structpb"

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

const (
	// DestinationEndpointHeader is the Envoy / Gateway API Inference Extension header
	// read by ORIGINAL_DST clusters (original_dst_lb_config.http_header_name) and EPP integrations.
	DestinationEndpointHeader = "x-gateway-destination-endpoint"
	envoyLBDynamicMetadataNS  = "envoy.lb"
)

// Process implements the bidirectional streaming RPC for Envoy ext_proc filter.
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

		var resp *extprocv3.ProcessingResponse
		switch v := req.Request.(type) {
		case *extprocv3.ProcessingRequest_RequestHeaders:
			resp = s.handleRequestHeaders(ctx, v.RequestHeaders)
			if ctx.Err() != nil {
				return ctx.Err()
			}

		case *extprocv3.ProcessingRequest_RequestBody:
			resp = &extprocv3.ProcessingResponse{
				Response: &extprocv3.ProcessingResponse_RequestBody{
					RequestBody: &extprocv3.BodyResponse{},
				},
			}

		case *extprocv3.ProcessingRequest_RequestTrailers:
			resp = &extprocv3.ProcessingResponse{
				Response: &extprocv3.ProcessingResponse_RequestTrailers{
					RequestTrailers: &extprocv3.TrailersResponse{},
				},
			}

		case *extprocv3.ProcessingRequest_ResponseHeaders:
			resp = &extprocv3.ProcessingResponse{
				Response: &extprocv3.ProcessingResponse_ResponseHeaders{
					ResponseHeaders: &extprocv3.HeadersResponse{},
				},
			}

		case *extprocv3.ProcessingRequest_ResponseBody:
			resp = &extprocv3.ProcessingResponse{
				Response: &extprocv3.ProcessingResponse_ResponseBody{
					ResponseBody: &extprocv3.BodyResponse{},
				},
			}

		case *extprocv3.ProcessingRequest_ResponseTrailers:
			resp = &extprocv3.ProcessingResponse{
				Response: &extprocv3.ProcessingResponse_ResponseTrailers{
					ResponseTrailers: &extprocv3.TrailersResponse{},
				},
			}

		default:
			return fmt.Errorf("unsupported ext_proc processing phase: %T", req.Request)
		}

		if resp == nil {
			return fmt.Errorf("nil ext_proc response generated for phase %T", req.Request)
		}
		if err := srv.Send(resp); err != nil {
			return fmt.Errorf("failed to send ext_proc response for phase %T: %w", req.Request, err)
		}
	}
}

func (s *GrpcExtProcServer) handleRequestHeaders(
	ctx context.Context,
	reqHeaders *extprocv3.HttpHeaders,
) *extprocv3.ProcessingResponse {
	headersMap := extractHeaders(reqHeaders)

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

	destinationEndpoint := resolveDestinationEndpoint(decision)

	var setHeaders []*corev3.HeaderValueOption
	for k, v := range decision.ResponseHeaders {
		setHeaders = append(setHeaders, &corev3.HeaderValueOption{
			Header: &corev3.HeaderValue{
				Key:   k,
				Value: v,
			},
			AppendAction: corev3.HeaderValueOption_OVERWRITE_IF_EXISTS_OR_ADD,
		})
	}

	var removeHeaders []string
	var dynMeta *structpb.Struct
	if destinationEndpoint != "" {
		setHeaders = append(setHeaders, &corev3.HeaderValueOption{
			Header: &corev3.HeaderValue{
				Key:   DestinationEndpointHeader,
				Value: destinationEndpoint,
			},
			AppendAction: corev3.HeaderValueOption_OVERWRITE_IF_EXISTS_OR_ADD,
		})
		if meta, err := structpb.NewStruct(map[string]any{
			envoyLBDynamicMetadataNS: map[string]any{
				DestinationEndpointHeader: destinationEndpoint,
			},
		}); err == nil {
			dynMeta = meta
		}
	} else {
		// Prevent caller-supplied destination headers from bypassing upstream selection.
		removeHeaders = append(removeHeaders, DestinationEndpointHeader)
	}

	return &extprocv3.ProcessingResponse{
		DynamicMetadata: dynMeta,
		Response: &extprocv3.ProcessingResponse_RequestHeaders{
			RequestHeaders: &extprocv3.HeadersResponse{
				Response: &extprocv3.CommonResponse{
					HeaderMutation: &extprocv3.HeaderMutation{
						SetHeaders:    setHeaders,
						RemoveHeaders: removeHeaders,
					},
				},
			},
		},
	}
}

func extractHeaders(reqHeaders *extprocv3.HttpHeaders) map[string]string {
	headersMap := make(map[string]string)
	if reqHeaders == nil || reqHeaders.Headers == nil {
		return headersMap
	}
	for _, h := range reqHeaders.Headers.Headers {
		if h == nil || h.Key == "" {
			continue
		}
		val := h.Value
		if val == "" && len(h.RawValue) > 0 {
			val = string(h.RawValue)
		}
		headersMap[strings.ToLower(h.Key)] = val
	}
	return headersMap
}

func resolveDestinationEndpoint(decision domain.RoutingDecision) string {
	if decision.SelectedEndpoint != "" {
		return decision.SelectedEndpoint
	}
	if observed := decision.ResponseHeaders["x-oscar-epp-observed-target"]; strings.Contains(observed, ":") {
		return observed
	}
	return ""
}

func selfSignedGRPCTLSConfig(namespace string) (*tls.Config, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate ecdsa key: %w", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 62))
	if err != nil {
		return nil, fmt.Errorf("generate serial: %w", err)
	}
	ns := strings.TrimSpace(namespace)
	if ns == "" {
		ns = "oscar-svc"
	}
	now := time.Now().UTC()
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName: "oscar-inferencemesh-epp",
		},
		NotBefore:             now.Add(-1 * time.Hour),
		NotAfter:              now.Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames: []string{
			"oscar-inferencemesh-epp",
			fmt.Sprintf("oscar-inferencemesh-epp.%s", ns),
			fmt.Sprintf("oscar-inferencemesh-epp.%s.svc", ns),
			fmt.Sprintf("oscar-inferencemesh-epp.%s.svc.cluster.local", ns),
			"localhost",
		},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		return nil, fmt.Errorf("create certificate: %w", err)
	}
	cert := tls.Certificate{
		Certificate: [][]byte{der},
		PrivateKey:  priv,
	}
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
		NextProtos:   []string{"h2"},
	}, nil
}

// StartGrpcServer starts the TLS-enabled gRPC ext_proc listener on the configured port.
// Envoy Gateway's InferencePool reconciler always configures UpstreamTlsContext on the
// endpointpicker cluster, so serving TLS with HTTP/2 ALPN matches Envoy's data plane.
func StartGrpcServer(
	settings config.Settings,
	picker *application.EndpointPickerService,
) (*grpc.Server, net.Listener, error) {
	tlsCfg, err := selfSignedGRPCTLSConfig(settings.Namespace)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to build gRPC TLS config: %w", err)
	}

	addr := fmt.Sprintf(":%d", settings.GRPCExtProcPort)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to listen on gRPC port %d: %w", settings.GRPCExtProcPort, err)
	}

	grpcServer := grpc.NewServer(grpc.Creds(credentials.NewTLS(tlsCfg)))
	srv := NewGrpcExtProcServer(settings, picker)
	extprocv3.RegisterExternalProcessorServer(grpcServer, srv)

	log.Printf("[EPP-gRPC] Envoy ext_proc TLS server listening on port %d", settings.GRPCExtProcPort)
	return grpcServer, lis, nil
}
