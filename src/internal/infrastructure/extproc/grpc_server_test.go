package extproc

import (
	"context"
	"crypto/tls"
	"io"
	"testing"
	"time"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	extprocv3 "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	"github.com/grycap/oscar-inferencemesh/src/internal/application"
	"github.com/grycap/oscar-inferencemesh/src/internal/config"
	"github.com/grycap/oscar-inferencemesh/src/internal/domain"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
)

type staticWorkers []domain.WorkerEndpoint

func (s staticWorkers) ListEndpoints(_ context.Context, modelName string) ([]domain.WorkerEndpoint, error) {
	var out []domain.WorkerEndpoint
	for _, ep := range s {
		if ep.ModelName == "" || ep.ModelName == modelName {
			out = append(out, ep)
		}
	}
	return out, nil
}

type staticMetrics map[string]domain.EndpointMetrics

func (s staticMetrics) CollectAll(context.Context, []domain.WorkerEndpoint) map[string]domain.EndpointMetrics {
	return s
}

type mockExtProcStream struct {
	ctx  context.Context
	reqs []*extprocv3.ProcessingRequest
	idx  int
	sent []*extprocv3.ProcessingResponse
}

func (m *mockExtProcStream) Send(resp *extprocv3.ProcessingResponse) error {
	m.sent = append(m.sent, resp)
	return nil
}

func (m *mockExtProcStream) Recv() (*extprocv3.ProcessingRequest, error) {
	if m.idx >= len(m.reqs) {
		return nil, io.EOF
	}
	req := m.reqs[m.idx]
	m.idx++
	return req, nil
}

func (m *mockExtProcStream) SetHeader(metadata.MD) error  { return nil }
func (m *mockExtProcStream) SendHeader(metadata.MD) error { return nil }
func (m *mockExtProcStream) SetTrailer(metadata.MD)       {}
func (m *mockExtProcStream) Context() context.Context     { return m.ctx }
func (m *mockExtProcStream) SendMsg(any) error            { return nil }
func (m *mockExtProcStream) RecvMsg(any) error            { return nil }

func TestHandleRequestHeadersSetsDestinationEndpointAndReadsRawValue(t *testing.T) {
	workers := staticWorkers{
		{Name: "worker-busy", Address: "10.244.0.10", Port: 8000, ModelName: "qwen2-5-05b-instruct", Ready: true},
		{Name: "worker-idle", Address: "10.244.0.11", Port: 8000, ModelName: "qwen2-5-05b-instruct", Ready: true},
	}
	now := time.Now().UTC()
	metrics := staticMetrics{
		"worker-busy": {EndpointName: "worker-busy", KVCacheUsagePerc: 0.80, WaitingRequests: 4, RunningRequests: 2, Reachable: true, ScrapedAt: now},
		"worker-idle": {EndpointName: "worker-idle", KVCacheUsagePerc: 0.05, WaitingRequests: 0, RunningRequests: 0, Reachable: true, ScrapedAt: now},
	}
	selector := application.NewActiveSelector(domain.RoutingModeCompositeScore, 10*time.Second)
	picker := application.NewEndpointPickerServiceWithWaitTimeout(workers, metrics, selector, time.Second)
	srv := NewGrpcExtProcServer(config.Settings{ModelName: "fallback-model"}, picker)

	reqHeaders := &extprocv3.HttpHeaders{
		Headers: &corev3.HeaderMap{
			Headers: []*corev3.HeaderValue{
				{Key: "X-Ai-Eg-Model", RawValue: []byte("qwen2-5-05b-instruct")},
				{Key: "x-request-id", RawValue: []byte("req-raw-123")},
			},
		},
	}

	resp := srv.handleRequestHeaders(context.Background(), reqHeaders)
	rh := resp.GetRequestHeaders()
	if rh == nil || rh.GetResponse() == nil || rh.GetResponse().GetHeaderMutation() == nil {
		t.Fatalf("expected RequestHeaders mutation response, got %+v", resp)
	}

	foundHeader := map[string]corev3.HeaderValueOption_HeaderAppendAction{}
	headerValues := map[string]string{}
	for _, opt := range rh.GetResponse().GetHeaderMutation().GetSetHeaders() {
		foundHeader[opt.GetHeader().GetKey()] = opt.GetAppendAction()
		headerValues[opt.GetHeader().GetKey()] = opt.GetHeader().GetValue()
	}

	if got := headerValues[DestinationEndpointHeader]; got != "10.244.0.11:8000" {
		t.Fatalf("destination header = %q, want 10.244.0.11:8000", got)
	}
	if foundHeader[DestinationEndpointHeader] != corev3.HeaderValueOption_OVERWRITE_IF_EXISTS_OR_ADD {
		t.Fatalf("expected OVERWRITE_IF_EXISTS_OR_ADD on %s", DestinationEndpointHeader)
	}
	if got := headerValues["x-oscar-epp-request-id"]; got != "req-raw-123" {
		t.Fatalf("expected RawValue request-id req-raw-123, got %q", got)
	}

	lbMeta := resp.GetDynamicMetadata().GetFields()[envoyLBDynamicMetadataNS].GetStructValue()
	if lbMeta == nil || lbMeta.GetFields()[DestinationEndpointHeader].GetStringValue() != "10.244.0.11:8000" {
		t.Fatalf("expected dynamic_metadata envoy.lb.%s=10.244.0.11:8000, got %+v", DestinationEndpointHeader, resp.GetDynamicMetadata())
	}
}

func TestProcessHandlesAllNegotiatedPhases(t *testing.T) {
	workers := staticWorkers{
		{Name: "worker-1", Address: "10.244.0.12", Port: 8000, ModelName: "qwen2-5-05b-instruct", Ready: true},
	}
	selector := application.NewActiveSelector(domain.RoutingModeCompositeScore, 10*time.Second)
	picker := application.NewEndpointPickerServiceWithWaitTimeout(workers, noMetrics{}, selector, time.Second)
	srv := NewGrpcExtProcServer(config.Settings{ModelName: "qwen2-5-05b-instruct"}, picker)

	stream := &mockExtProcStream{
		ctx: context.Background(),
		reqs: []*extprocv3.ProcessingRequest{
			{Request: &extprocv3.ProcessingRequest_RequestHeaders{RequestHeaders: &extprocv3.HttpHeaders{}}},
			{Request: &extprocv3.ProcessingRequest_RequestBody{RequestBody: &extprocv3.HttpBody{Body: []byte(`{"model":"qwen"}`)}}},
			{Request: &extprocv3.ProcessingRequest_RequestTrailers{RequestTrailers: &extprocv3.HttpTrailers{}}},
			{Request: &extprocv3.ProcessingRequest_ResponseHeaders{ResponseHeaders: &extprocv3.HttpHeaders{}}},
			{Request: &extprocv3.ProcessingRequest_ResponseBody{ResponseBody: &extprocv3.HttpBody{}}},
			{Request: &extprocv3.ProcessingRequest_ResponseTrailers{ResponseTrailers: &extprocv3.HttpTrailers{}}},
		},
	}

	if err := srv.Process(stream); err != nil {
		t.Fatalf("Process returned error: %v", err)
	}
	if len(stream.sent) != 6 {
		t.Fatalf("expected 6 typed responses, got %d", len(stream.sent))
	}
	if _, ok := stream.sent[0].Response.(*extprocv3.ProcessingResponse_RequestHeaders); !ok {
		t.Errorf("resp[0] = %T, want RequestHeaders", stream.sent[0].Response)
	}
	if _, ok := stream.sent[1].Response.(*extprocv3.ProcessingResponse_RequestBody); !ok {
		t.Errorf("resp[1] = %T, want RequestBody", stream.sent[1].Response)
	}
	if _, ok := stream.sent[2].Response.(*extprocv3.ProcessingResponse_RequestTrailers); !ok {
		t.Errorf("resp[2] = %T, want RequestTrailers", stream.sent[2].Response)
	}
	if _, ok := stream.sent[3].Response.(*extprocv3.ProcessingResponse_ResponseHeaders); !ok {
		t.Errorf("resp[3] = %T, want ResponseHeaders", stream.sent[3].Response)
	}
	if _, ok := stream.sent[4].Response.(*extprocv3.ProcessingResponse_ResponseBody); !ok {
		t.Errorf("resp[4] = %T, want ResponseBody", stream.sent[4].Response)
	}
	if _, ok := stream.sent[5].Response.(*extprocv3.ProcessingResponse_ResponseTrailers); !ok {
		t.Errorf("resp[5] = %T, want ResponseTrailers", stream.sent[5].Response)
	}
}

func TestStartGrpcServerServesTLSForInferencePool(t *testing.T) {
	workers := staticWorkers{
		{Name: "worker-1", Address: "10.244.0.62", Port: 8000, ModelName: "qwen2-5-05b-instruct", Ready: true},
	}
	selector := application.NewActiveSelector(domain.RoutingModeCompositeScore, 10*time.Second)
	picker := application.NewEndpointPickerServiceWithWaitTimeout(workers, noMetrics{}, selector, time.Second)

	grpcSrv, lis, err := StartGrpcServer(config.Settings{
		Namespace:       "oscar-svc",
		ModelName:       "qwen2-5-05b-instruct",
		GRPCExtProcPort: 0,
	}, picker)
	if err != nil {
		t.Fatalf("StartGrpcServer failed: %v", err)
	}
	defer grpcSrv.Stop()
	go func() { _ = grpcSrv.Serve(lis) }()

	creds := credentials.NewTLS(&tls.Config{
		InsecureSkipVerify: true, // matches Envoy InferencePool UpstreamTlsContext
		NextProtos:         []string{"h2"},
	})
	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(creds))
	if err != nil {
		t.Fatalf("grpc.NewClient failed: %v", err)
	}
	defer conn.Close()

	client := extprocv3.NewExternalProcessorClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := client.Process(ctx)
	if err != nil {
		t.Fatalf("Process RPC failed: %v", err)
	}
	if err := stream.Send(&extprocv3.ProcessingRequest{
		Request: &extprocv3.ProcessingRequest_RequestHeaders{
			RequestHeaders: &extprocv3.HttpHeaders{
				Headers: &corev3.HeaderMap{
					Headers: []*corev3.HeaderValue{
						{Key: "x-ai-eg-model", RawValue: []byte("qwen2-5-05b-instruct")},
					},
				},
			},
		},
	}); err != nil {
		t.Fatalf("stream.Send failed: %v", err)
	}
	_ = stream.CloseSend()

	resp, err := stream.Recv()
	if err != nil {
		t.Fatalf("stream.Recv failed: %v", err)
	}
	got := ""
	for _, h := range resp.GetRequestHeaders().GetResponse().GetHeaderMutation().GetSetHeaders() {
		if h.GetHeader().GetKey() == DestinationEndpointHeader {
			got = h.GetHeader().GetValue()
		}
	}
	if got != "10.244.0.62:8000" {
		t.Fatalf("got %s=%q, want 10.244.0.62:8000", DestinationEndpointHeader, got)
	}
}
