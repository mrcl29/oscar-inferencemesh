package k8s

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/grycap/oscar-inferencemesh/src/internal/config"
)

func TestParsePodsJSON_FiltersLabelsReadinessAndTerminating(t *testing.T) {
	adapter := NewEndpointDiscoveryAdapter(config.Settings{
		Namespace:          "oscar-svc",
		ModelName:          "qwen2-5-05b-instruct",
		DefaultBackendPort: 8000,
	})

	payload := `{
		"items": [
			{
				"metadata": {
					"name": "qwen2-5-05b-instruct-kserve-worker-abc",
					"labels": {
						"app.kubernetes.io/component": "llminferenceservice-workload",
						"app.kubernetes.io/name": "qwen2-5-05b-instruct",
						"kserve.io/component": "workload"
					}
				},
				"status": {
					"phase": "Running",
					"podIP": "10.42.0.11",
					"conditions": [{"type": "Ready", "status": "True"}]
				}
			},
			{
				"metadata": {
					"name": "qwen2-5-05b-instruct-kserve-worker-crashing",
					"labels": {
						"app.kubernetes.io/component": "llminferenceservice-workload",
						"app.kubernetes.io/name": "qwen2-5-05b-instruct",
						"kserve.io/component": "workload"
					}
				},
				"status": {
					"phase": "Running",
					"podIP": "10.42.0.12",
					"conditions": [{"type": "Ready", "status": "False"}]
				}
			},
			{
				"metadata": {
					"name": "qwen2-5-05b-instruct-kserve-worker-terminating",
					"deletionTimestamp": "2026-11-14T10:00:00Z",
					"labels": {
						"app.kubernetes.io/component": "llminferenceservice-workload",
						"app.kubernetes.io/name": "qwen2-5-05b-instruct",
						"kserve.io/component": "workload"
					}
				},
				"status": {
					"phase": "Running",
					"podIP": "10.42.0.13",
					"conditions": [{"type": "Ready", "status": "True"}]
				}
			},
			{
				"metadata": {
					"name": "qwen2-5-05b-instruct-kserve-router-scheduler-xyz",
					"labels": {
						"app.kubernetes.io/component": "llminferenceservice-router-scheduler",
						"app.kubernetes.io/name": "qwen2-5-05b-instruct"
					}
				},
				"status": {
					"phase": "Running",
					"podIP": "10.42.0.14",
					"conditions": [{"type": "Ready", "status": "True"}]
				}
			},
			{
				"metadata": {
					"name": "qwen2-5-05b-instruct-impostor-by-name-only",
					"labels": {
						"app.kubernetes.io/component": "llminferenceservice-workload",
						"app.kubernetes.io/name": "qwen2-5-05b-instruct-v2"
					}
				},
				"status": {
					"phase": "Running",
					"podIP": "10.42.0.15",
					"conditions": [{"type": "Ready", "status": "True"}]
				}
			},
			{
				"metadata": {
					"name": "oscar-inferencemesh-epp-7f8d9",
					"labels": {
						"app.kubernetes.io/name": "oscar-inferencemesh-epp",
						"app.kubernetes.io/part-of": "oscar-inferencemesh",
						"model": "qwen2-5-05b-instruct"
					}
				},
				"status": {
					"phase": "Running",
					"podIP": "10.42.0.16",
					"conditions": [{"type": "Ready", "status": "True"}]
				}
			},
			{
				"metadata": {
					"name": "worker-via-oscar-app-label",
					"labels": {
						"oscar-app": "qwen2-5-05b-instruct"
					}
				},
				"status": {
					"phase": "Running",
					"podIP": "10.42.0.17",
					"conditions": [{"type": "Ready", "status": "True"}]
				}
			}
		]
	}`

	endpoints, err := adapter.parsePodsJSON([]byte(payload), "qwen2-5-05b-instruct")
	if err != nil {
		t.Fatalf("unexpected error parsing pods: %v", err)
	}
	if len(endpoints) != 2 {
		t.Fatalf("expected exactly 2 ready matching workers, got %d: %+v", len(endpoints), endpoints)
	}
	if endpoints[0].Name != "qwen2-5-05b-instruct-kserve-worker-abc" || endpoints[0].Address != "10.42.0.11" || endpoints[0].Port != 8000 {
		t.Fatalf("unexpected first endpoint: %+v", endpoints[0])
	}
	if endpoints[1].Name != "worker-via-oscar-app-label" || endpoints[1].Address != "10.42.0.17" {
		t.Fatalf("unexpected second endpoint: %+v", endpoints[1])
	}
}

func TestListEndpoints_InClusterPropagatesErrorWithoutKubectlFallback(t *testing.T) {
	adapter := NewEndpointDiscoveryAdapter(config.Settings{
		Namespace:          "oscar-svc",
		ModelName:          "qwen2-5-05b-instruct",
		DefaultBackendPort: 8000,
	})

	kubectlCalled := false
	adapter.readToken = func() ([]byte, error) {
		return []byte("sa-token\n"), nil
	}
	adapter.inClusterFetch = func(_ context.Context, url, token string) ([]byte, error) {
		if token != "sa-token" {
			t.Fatalf("expected trimmed token, got %q", token)
		}
		if !strings.Contains(url, "/namespaces/oscar-svc/pods") {
			t.Fatalf("unexpected API url: %s", url)
		}
		return nil, errors.New("kubernetes API returned HTTP 403")
	}
	adapter.kubectlFetch = func(_ context.Context, _ string) ([]byte, error) {
		kubectlCalled = true
		return nil, nil
	}

	endpoints, err := adapter.ListEndpoints(context.Background(), "")
	if err == nil {
		t.Fatal("expected error when in-cluster discovery fails")
	}
	if len(endpoints) != 0 {
		t.Fatalf("expected no endpoints on failure, got %+v", endpoints)
	}
	if kubectlCalled {
		t.Fatal("expected in-cluster failure not to silently fall back to kubectl")
	}
}

func TestListEndpoints_KubectlFallbackWhenNoTokenAndPropagatesError(t *testing.T) {
	adapter := NewEndpointDiscoveryAdapter(config.Settings{
		Namespace:          "oscar-svc",
		ModelName:          "qwen2-5-05b-instruct",
		DefaultBackendPort: 8000,
	})

	adapter.readToken = func() ([]byte, error) {
		return nil, errors.New("no token")
	}
	adapter.kubectlFetch = func(_ context.Context, ns string) ([]byte, error) {
		if ns != "oscar-svc" {
			t.Fatalf("expected namespace oscar-svc, got %s", ns)
		}
		return nil, errors.New("kubectl exec failed")
	}

	if _, err := adapter.ListEndpoints(context.Background(), "qwen2-5-05b-instruct"); err == nil {
		t.Fatal("expected error when kubectl discovery fails")
	}
}

func TestListEndpoints_InvalidJSONReturnsError(t *testing.T) {
	adapter := NewEndpointDiscoveryAdapter(config.Settings{
		Namespace: "oscar-svc",
		ModelName: "qwen2-5-05b-instruct",
	})
	adapter.readToken = func() ([]byte, error) {
		return nil, errors.New("no token")
	}
	adapter.kubectlFetch = func(_ context.Context, _ string) ([]byte, error) {
		return []byte("{not-json"), nil
	}

	if _, err := adapter.ListEndpoints(context.Background(), ""); err == nil {
		t.Fatal("expected JSON unmarshal error")
	}
}
