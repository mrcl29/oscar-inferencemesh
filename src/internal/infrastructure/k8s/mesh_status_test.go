package k8s

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/grycap/oscar-inferencemesh/src/internal/domain"
)

type stubOscarClient struct {
	cluster domain.OscarClusterStatus
	svc     *domain.OscarServiceSummary
}

func (s stubOscarClient) GetClusterInfo(context.Context) domain.OscarClusterStatus {
	return s.cluster
}

func (s stubOscarClient) GetService(context.Context, string) (*domain.OscarServiceSummary, error) {
	return s.svc, nil
}

func (s stubOscarClient) ApplyScalingDecision(context.Context, domain.ScalingDecision) error {
	return nil
}

type stubDiscovery struct {
	endpoints []domain.WorkerEndpoint
	err       error
}

func (s stubDiscovery) ListEndpoints(context.Context, string) ([]domain.WorkerEndpoint, error) {
	return s.endpoints, s.err
}

func TestMeshInspectorReportsMissingGatewayAndDetectsReadyWorker(t *testing.T) {
	inspector := &MeshInspector{
		RunCommand: func(_ context.Context, name string, args ...string) ([]byte, error) {
			joined := name + " " + strings.Join(args, " ")
			switch {
			case strings.Contains(joined, "config current-context"):
				return []byte("kind-oscar-test-n2r\n"), nil
			case strings.Contains(joined, "cluster-info"):
				return []byte("Kubernetes control plane is running at https://127.0.0.1:41207\n"), nil
			case strings.Contains(joined, "get llminferenceservice"):
				return []byte(`{"spec":{"replicas":1}}`), nil
			case strings.Contains(joined, "get pods -n oscar-svc -l app.kubernetes.io/name=qwen2-5-05b-instruct"):
				return []byte(`{"items":[{"metadata":{"name":"qwen2-5-05b-instruct-kserve-5bfdcbdfc-mfmqh","labels":{"app.kubernetes.io/component":"llminferenceservice-workload"}},"spec":{"nodeName":"oscar-test-n2r-control-plane"},"status":{"phase":"Running","podIP":"10.244.0.62","containerStatuses":[{"name":"main","ready":true},{"name":"modelcar","ready":true}]}}]}`), nil
			case strings.Contains(joined, "get endpointslices"):
				return []byte(`{"items":[{"endpoints":[{"addresses":["10.244.0.62"],"conditions":{"ready":true}}]}]}`), nil
			case strings.Contains(joined, "get deployment envoy-gateway -n envoy-gateway-system"):
				return []byte(`{"spec":{"replicas":1},"status":{"readyReplicas":1}}`), nil
			case strings.Contains(joined, "get deployment ai-gateway-controller -n envoy-ai-gateway-system"):
				return []byte(`{"spec":{"replicas":1},"status":{"readyReplicas":1}}`), nil
			case strings.Contains(joined, "get gateways -A"):
				return []byte(`{"items":[{"metadata":{"name":"traefik-gateway","namespace":"traefik"},"spec":{"gatewayClassName":"traefik"},"status":{"conditions":[{"type":"Programmed","status":"True"}]}}]}`), nil
			case strings.Contains(joined, "get svc -A"):
				return []byte(`{"items":[{"metadata":{"name":"traefik","namespace":"traefik"},"spec":{"ports":[{"port":80,"nodePort":30080}]}}]}`), nil
			case strings.Contains(joined, "get aigatewayroutes"):
				return []byte(`{"items":[]}`), nil
			case strings.Contains(joined, "get envoyextensionpolicy"):
				return []byte(`{"spec":{"targetRefs":[{"kind":"Gateway","name":"envoy-ai-gateway"}],"extProc":[{"failOpen":true,"backendRefs":[{"name":"oscar-inferencemesh-epp","port":9002}]}]}}`), nil
			case strings.Contains(joined, "get deployment oscar-inferencemesh-epp"):
				return []byte(`{"spec":{"replicas":1},"status":{"readyReplicas":1}}`), nil
			case strings.Contains(joined, "get pods -n oscar-svc -l app.kubernetes.io/name=oscar-inferencemesh-epp"):
				return []byte(`{"items":[{"metadata":{"name":"oscar-inferencemesh-epp-abc"},"status":{"phase":"Running","podIP":"10.244.0.34"}}]}`), nil
			default:
				return nil, fmt.Errorf("unexpected command: %s", joined)
			}
		},
		OscarClient: stubOscarClient{
			cluster: domain.OscarClusterStatus{
				Connected:       true,
				ClusterID:       "local",
				Endpoint:        "https://localhost.direct",
				AdapterMode:     "oscar-cli",
				RawServiceTable: []string{"NAME IMAGE CPU MEMORY", "qwen2-5-05b-instruct 0.2 256Mi"},
			},
			svc: &domain.OscarServiceSummary{Name: "qwen2-5-05b-instruct"},
		},
		EndpointDiscovery: stubDiscovery{
			endpoints: []domain.WorkerEndpoint{{Name: "qwen2-5-05b-instruct-kserve-5bfdcbdfc-mfmqh", Address: "10.244.0.62", Port: 8000, Ready: true}},
		},
		FetchEPPVerify: func(context.Context, string, string, int, bool) ([]byte, string, error) {
			return []byte(`{"status":"ok","mode":"composite_score","picker":{"total_requests_processed":0},"autoscaler":{"oscar_cluster":{"connected":false,"adapter_mode":"stub-no-cli","error":"oscar-cli binary not found in PATH"},"last_scaling_decision":{"action":"no_op","reason":"observed across 1 endpoint(s)"}}}`), "cluster svc/oscar-inferencemesh-epp", nil
		},
		CheckLocalTCP: func(string) bool { return true },
	}

	report := inspector.Inspect(context.Background(), StatusOptions{})

	if !report.KubeConnected {
		t.Fatalf("expected KubeConnected=true")
	}
	if !report.Oscar.TargetRegistered || len(report.Oscar.RegisteredServices) != 1 {
		t.Fatalf("unexpected OSCAR state: %+v", report.Oscar)
	}
	if report.Workload.ReadyWorkerCount != 1 || len(report.Workload.DiscoveredEndpoints) != 1 {
		t.Fatalf("expected 1 ready worker and 1 discovered endpoint, got ready=%d endpoints=%d",
			report.Workload.ReadyWorkerCount, len(report.Workload.DiscoveredEndpoints))
	}
	if report.Gateway.TargetGatewayExists {
		t.Fatalf("expected TargetGatewayExists=false when only traefik-gateway exists")
	}
	if !report.EPP.VerifyReachable || report.EPP.Verify == nil {
		t.Fatalf("expected EPP verify snapshot to be parsed")
	}

	failedComponents := map[string]bool{}
	for _, f := range report.Findings {
		if f.Severity == SeverityFail {
			failedComponents[f.Component] = true
		}
	}
	for _, want := range []string{"Gateway", "IngressPort", "AIGatewayRoute", "ExtensionPolicy"} {
		if !failedComponents[want] {
			t.Errorf("expected FAIL finding for component %q, findings=%+v", want, report.Findings)
		}
	}
}

func TestMeshInspectorReportsHealthyFullStack(t *testing.T) {
	inspector := &MeshInspector{
		RunCommand: func(_ context.Context, name string, args ...string) ([]byte, error) {
			joined := name + " " + strings.Join(args, " ")
			switch {
			case strings.Contains(joined, "config current-context"):
				return []byte("kind-oscar-test-n2r\n"), nil
			case strings.Contains(joined, "cluster-info"):
				return []byte("Kubernetes control plane is running\n"), nil
			case strings.Contains(joined, "get llminferenceservice"):
				return []byte(`{"spec":{"replicas":1}}`), nil
			case strings.Contains(joined, "get pods -n oscar-svc -l app.kubernetes.io/name=qwen2-5-05b-instruct"):
				return []byte(`{"items":[{"metadata":{"name":"qwen-pod-1","labels":{"app.kubernetes.io/component":"llminferenceservice-workload"}},"status":{"phase":"Running","podIP":"10.244.0.62","containerStatuses":[{"name":"main","ready":true}]}}]}`), nil
			case strings.Contains(joined, "get endpointslices"):
				return []byte(`{"items":[{"endpoints":[{"addresses":["10.244.0.62"],"conditions":{"ready":true}}]}]}`), nil
			case strings.Contains(joined, "get deployment envoy-gateway"), strings.Contains(joined, "get deployment ai-gateway-controller"), strings.Contains(joined, "get deployment oscar-inferencemesh-epp"):
				return []byte(`{"spec":{"replicas":1},"status":{"readyReplicas":1}}`), nil
			case strings.Contains(joined, "get gateways -A"):
				return []byte(`{"items":[{"metadata":{"name":"envoy-ai-gateway","namespace":"oscar-svc"},"spec":{"gatewayClassName":"eg"},"status":{"conditions":[{"type":"Programmed","status":"True"}]}}]}`), nil
			case strings.Contains(joined, "get svc -A"):
				return []byte(`{"items":[{"metadata":{"name":"envoy-oscar-svc-envoy-ai-gateway","namespace":"envoy-gateway-system"},"spec":{"ports":[{"port":8080,"nodePort":30880}]}}]}`), nil
			case strings.Contains(joined, "get aigatewayroutes"):
				return []byte(`{"items":[{"metadata":{"name":"qwen-route","namespace":"oscar-svc"},"spec":{"parentRefs":[{"name":"envoy-ai-gateway"}],"rules":[{"matches":[{"headers":[{"name":"x-ai-eg-model","value":"qwen2-5-05b-instruct"}]}],"backendRefs":[{"name":"qwen2-5-05b-instruct-backend"}]}]},"status":{"conditions":[{"type":"Accepted","status":"True"}]}}]}`), nil
			case strings.Contains(joined, "get backends "):
				return []byte(`{"items":[{"metadata":{"name":"qwen2-5-05b-instruct-eg-backend"},"spec":{"endpoints":[{"fqdn":{"hostname":"qwen2-5-05b-instruct-kserve-workload-svc.oscar-svc.svc.cluster.local","port":8000}}]}}]}`), nil
			case strings.Contains(joined, "get aiservicebackends "):
				return []byte(`{"items":[{"metadata":{"name":"qwen2-5-05b-instruct-backend"},"spec":{"backendRef":{"name":"qwen2-5-05b-instruct-eg-backend"}}}]}`), nil
			case strings.Contains(joined, "get deployment -n envoy-gateway-system"):
				return []byte(`{"items":[{"spec":{"replicas":1},"status":{"readyReplicas":1}}]}`), nil
			case strings.Contains(joined, "get llminferenceservices "):
				return []byte(`{"items":[{"metadata":{"name":"qwen2-5-05b-instruct"}}]}`), nil
			case strings.Contains(joined, "get envoyextensionpolicy"):
				return []byte(`{"spec":{"targetRefs":[{"kind":"Gateway","name":"envoy-ai-gateway"}],"extProc":[{"failOpen":true,"backendRefs":[{"name":"oscar-inferencemesh-epp","port":9002}]}]},"status":{"ancestors":[{"conditions":[{"type":"Accepted","status":"True"}]}]}}`), nil
			case strings.Contains(joined, "get pods -n oscar-svc -l app.kubernetes.io/name=oscar-inferencemesh-epp"):
				return []byte(`{"items":[{"metadata":{"name":"epp-pod"},"status":{"phase":"Running","podIP":"10.244.0.34"}}]}`), nil
			default:
				return []byte(`{}`), nil
			}
		},
		OscarClient: stubOscarClient{
			cluster: domain.OscarClusterStatus{
				Connected:       true,
				ClusterID:       "local",
				AdapterMode:     "oscar-cli",
				RawServiceTable: []string{"qwen2-5-05b-instruct 0.2 256Mi"},
			},
			svc: &domain.OscarServiceSummary{Name: "qwen2-5-05b-instruct"},
		},
		EndpointDiscovery: stubDiscovery{
			endpoints: []domain.WorkerEndpoint{{Name: "qwen-pod-1", Address: "10.244.0.62", Port: 8000, Ready: true}},
		},
		FetchEPPVerify: func(context.Context, string, string, int, bool) ([]byte, string, error) {
			return []byte(`{"status":"ok","mode":"composite_score","picker":{"total_requests_processed":2},"autoscaler":{"oscar_cluster":{"connected":true,"adapter_mode":"oscar-cli"},"last_scaling_decision":{"action":"no_op","reason":"ok"}}}`), "local (127.0.0.1:9091)", nil
		},
		CheckLocalTCP: func(string) bool { return true },
	}

	report := inspector.Inspect(context.Background(), StatusOptions{})
	if len(report.ConnectedModels) != 1 {
		t.Fatalf("expected 1 connected model, got %d", len(report.ConnectedModels))
	}
	cm := report.ConnectedModels[0]
	if cm.ModelName != "qwen2-5-05b-instruct" || cm.ReadyWorkers != 1 || cm.BackendTarget != "qwen2-5-05b-instruct-kserve-workload-svc.oscar-svc.svc.cluster.local:8000" {
		t.Fatalf("unexpected connected model summary: %+v", cm)
	}
	for _, f := range report.Findings {
		if f.Severity == SeverityFail || f.Severity == SeverityWarn {
			t.Errorf("expected no FAIL/WARN findings for healthy stack, got %+v", f)
		}
	}
}

func TestMeshInspectorReportsMissingAIGatewayCRDs(t *testing.T) {
	inspector := &MeshInspector{
		RunCommand: func(_ context.Context, name string, args ...string) ([]byte, error) {
			joined := name + " " + strings.Join(args, " ")
			switch {
			case strings.Contains(joined, "config current-context"):
				return []byte("kind-oscar-test-n2r\n"), nil
			case strings.Contains(joined, "cluster-info"):
				return []byte("Kubernetes control plane is running\n"), nil
			case strings.Contains(joined, "get aigatewayroutes"):
				return []byte(`Error from server (NotFound): the server could not find the requested resource (get aigatewayroutes.aigateway.envoyproxy.io)`), fmt.Errorf("exit status 1")
			case strings.Contains(joined, "get aiservicebackends "):
				return []byte(`Error from server (NotFound): the server could not find the requested resource (get aiservicebackends.aigateway.envoyproxy.io)`), fmt.Errorf("exit status 1")
			case strings.Contains(joined, "get deployment envoy-gateway"), strings.Contains(joined, "get deployment ai-gateway-controller"), strings.Contains(joined, "get deployment oscar-inferencemesh-epp"):
				return []byte(`{"spec":{"replicas":1},"status":{"readyReplicas":1}}`), nil
			case strings.Contains(joined, "get gateways -A"):
				return []byte(`{"items":[{"metadata":{"name":"envoy-ai-gateway","namespace":"oscar-svc"}}]}`), nil
			default:
				return []byte(`{}`), nil
			}
		},
		CheckLocalTCP: func(string) bool { return true },
	}

	report := inspector.Inspect(context.Background(), StatusOptions{})
	if !report.Gateway.AIGatewayCRDsMissing {
		t.Fatal("expected AIGatewayCRDsMissing=true")
	}

	var foundCRDFAIL bool
	for _, f := range report.Findings {
		if f.Component == "AIGatewayCRDs" && f.Severity == SeverityFail {
			foundCRDFAIL = true
		}
	}
	if !foundCRDFAIL {
		t.Fatalf("expected FAIL finding for AIGatewayCRDs, got %+v", report.Findings)
	}
}
