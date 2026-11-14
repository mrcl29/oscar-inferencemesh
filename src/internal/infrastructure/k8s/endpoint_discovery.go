// Package k8s implements the Kubernetes endpoint discovery adapter for the EPP.
package k8s

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/grycap/oscar-inferencemesh/src/internal/config"
	"github.com/grycap/oscar-inferencemesh/src/internal/domain"
)

const serviceAccountTokenPath = "/var/run/secrets/kubernetes.io/serviceaccount/token"

// EndpointDiscoveryAdapter discovers ready vLLM worker pods in the Kubernetes cluster.
type EndpointDiscoveryAdapter struct {
	settings       config.Settings
	readToken      func() ([]byte, error)
	inClusterFetch func(ctx context.Context, url, token string) ([]byte, error)
	kubectlFetch   func(ctx context.Context, namespace string) ([]byte, error)
}

// NewEndpointDiscoveryAdapter creates a new Kubernetes EndpointDiscoveryAdapter.
func NewEndpointDiscoveryAdapter(settings config.Settings) *EndpointDiscoveryAdapter {
	adapter := &EndpointDiscoveryAdapter{
		settings: settings,
		readToken: func() ([]byte, error) {
			return os.ReadFile(serviceAccountTokenPath)
		},
	}
	adapter.inClusterFetch = adapter.defaultInClusterFetch
	adapter.kubectlFetch = adapter.defaultKubectlFetch
	return adapter
}

type k8sPodList struct {
	Items []k8sPodItem `json:"items"`
}

type k8sPodItem struct {
	Metadata struct {
		Name              string            `json:"name"`
		DeletionTimestamp string            `json:"deletionTimestamp,omitempty"`
		Labels            map[string]string `json:"labels"`
	} `json:"metadata"`
	Status struct {
		Phase      string            `json:"phase"`
		PodIP      string            `json:"podIP"`
		Conditions []k8sPodCondition `json:"conditions"`
	} `json:"status"`
}

type k8sPodCondition struct {
	Type   string `json:"type"`
	Status string `json:"status"`
}

// ListEndpoints queries Kubernetes for ready vLLM worker pods matching the target model.
func (a *EndpointDiscoveryAdapter) ListEndpoints(
	ctx context.Context,
	modelName string,
) ([]domain.WorkerEndpoint, error) {
	targetModel := modelName
	if targetModel == "" {
		targetModel = a.settings.ModelName
	}

	// 1. If running inside a Kubernetes Pod, query the API server using the mounted ServiceAccount token.
	if tokenBytes, err := a.readToken(); err == nil {
		token := strings.TrimSpace(string(tokenBytes))
		if token != "" {
			url := fmt.Sprintf("https://kubernetes.default.svc/api/v1/namespaces/%s/pods", a.settings.Namespace)
			data, err := a.inClusterFetch(ctx, url, token)
			if err != nil {
				return nil, fmt.Errorf("in-cluster pod discovery failed: %w", err)
			}
			return a.parsePodsJSON(data, targetModel)
		}
	}

	// 2. Otherwise (host/developer machine), query via kubectl.
	data, err := a.kubectlFetch(ctx, a.settings.Namespace)
	if err != nil {
		return nil, fmt.Errorf("kubectl pod discovery failed: %w", err)
	}
	return a.parsePodsJSON(data, targetModel)
}

func (a *EndpointDiscoveryAdapter) defaultInClusterFetch(
	ctx context.Context,
	url, token string,
) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	tr := &http.Transport{
		// TODO(RB-01): Trust the mounted service-account CA and verify the API server certificate instead of bypassing TLS verification.
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // Cluster internal CA
	}
	client := &http.Client{Transport: tr, Timeout: 3 * time.Second}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("kubernetes API returned HTTP %d", resp.StatusCode)
	}

	return io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024))
}

func (a *EndpointDiscoveryAdapter) defaultKubectlFetch(
	ctx context.Context,
	namespace string,
) ([]byte, error) {
	cmdCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	cmd := exec.CommandContext(
		cmdCtx,
		"kubectl",
		"get",
		"pods",
		"-n",
		namespace,
		"-o",
		"json",
	)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (a *EndpointDiscoveryAdapter) parsePodsJSON(
	data []byte,
	modelName string,
) ([]domain.WorkerEndpoint, error) {
	var podList k8sPodList
	if err := json.Unmarshal(data, &podList); err != nil {
		return nil, fmt.Errorf("failed to unmarshal pod list: %w", err)
	}

	var endpoints []domain.WorkerEndpoint
	for _, item := range podList.Items {
		if item.Metadata.DeletionTimestamp != "" {
			continue
		}
		if !isMatchingWorkerPod(item.Metadata.Name, item.Metadata.Labels, modelName) {
			continue
		}
		if item.Status.Phase != "Running" || item.Status.PodIP == "" {
			continue
		}
		if !hasPodReadyCondition(item.Status.Conditions) {
			continue
		}

		endpoints = append(endpoints, domain.WorkerEndpoint{
			Name:      item.Metadata.Name,
			Address:   item.Status.PodIP,
			Port:      a.settings.DefaultBackendPort,
			Namespace: a.settings.Namespace,
			ModelName: modelName,
			Ready:     true,
			Labels:    item.Metadata.Labels,
		})
	}

	return endpoints, nil
}

func hasPodReadyCondition(conditions []k8sPodCondition) bool {
	for _, cond := range conditions {
		if cond.Type == "Ready" && cond.Status == "True" {
			return true
		}
	}
	return false
}

// isMatchingWorkerPod verifies via explicit workload and model labels that a pod is a
// worker serving modelName, rejecting non-worker control-plane pods and similarly named models.
func isMatchingWorkerPod(podName string, labels map[string]string, modelName string) bool {
	if len(labels) == 0 || modelName == "" {
		return false
	}

	// Exclude control-plane, router-scheduler, and EPP pods explicitly.
	appComp := labels["app.kubernetes.io/component"]
	kserveComp := labels["kserve.io/component"]
	if appComp == "llminferenceservice-router-scheduler" ||
		kserveComp == "router-scheduler" ||
		labels["app.kubernetes.io/name"] == "oscar-inferencemesh-epp" ||
		labels["app.kubernetes.io/part-of"] == "oscar-inferencemesh" ||
		strings.Contains(podName, "router-scheduler") {
		return false
	}

	// If a workload component label is present, require it to denote an inference worker.
	if kserveComp != "" && kserveComp != "workload" && kserveComp != "predictor" {
		return false
	}
	if appComp != "" &&
		appComp != "llminferenceservice-workload" &&
		appComp != "workload" &&
		appComp != "predictor" {
		return false
	}

	// Match model identity strictly by explicit label equality (never pod-name substrings).
	modelLabels := []string{
		"app.kubernetes.io/name",
		"serving.kserve.io/inferenceservice",
		"model",
		"oscar-app",
	}
	for _, key := range modelLabels {
		if labels[key] == modelName {
			return true
		}
	}
	return false
}
