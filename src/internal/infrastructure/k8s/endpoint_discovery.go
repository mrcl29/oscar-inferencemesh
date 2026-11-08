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

// EndpointDiscoveryAdapter discovers running vLLM worker pods in the Kubernetes cluster.
type EndpointDiscoveryAdapter struct {
	settings config.Settings
}

// NewEndpointDiscoveryAdapter creates a new Kubernetes EndpointDiscoveryAdapter.
func NewEndpointDiscoveryAdapter(settings config.Settings) *EndpointDiscoveryAdapter {
	return &EndpointDiscoveryAdapter{settings: settings}
}

type k8sPodList struct {
	Items []struct {
		Metadata struct {
			Name   string            `json:"name"`
			Labels map[string]string `json:"labels"`
		} `json:"metadata"`
		Status struct {
			Phase      string `json:"phase"`
			PodIP      string `json:"podIP"`
			Conditions []struct {
				Type   string `json:"type"`
				Status string `json:"status"`
			} `json:"conditions"`
		} `json:"status"`
	} `json:"items"`
}

// ListEndpoints queries Kubernetes for running vLLM worker pods, falling back to the default Service FQDN.
func (a *EndpointDiscoveryAdapter) ListEndpoints(
	ctx context.Context,
	modelName string,
) ([]domain.WorkerEndpoint, error) {
	targetModel := modelName
	if targetModel == "" {
		targetModel = a.settings.ModelName
	}

	// 1. Try discovering via in-cluster service account token if running inside a Pod
	discovered := a.discoverViaInCluster(ctx, targetModel)
	if len(discovered) > 0 {
		return discovered, nil
	}

	// 2. Try discovering via kubectl if running on host/developer machine
	discovered = a.discoverViaKubectl(ctx, targetModel)
	if len(discovered) > 0 {
		return discovered, nil
	}

	// 3. Fallback to default KServe Service FQDN
	return []domain.WorkerEndpoint{
		{
			Name:      targetModel + "-default-svc",
			Address:   a.settings.DefaultBackendFQDN,
			Port:      a.settings.DefaultBackendPort,
			Namespace: a.settings.Namespace,
			ModelName: targetModel,
			Ready:     true,
			Labels:    map[string]string{"source": "static-service-fallback"},
		},
	}, nil
}

func (a *EndpointDiscoveryAdapter) discoverViaInCluster(
	ctx context.Context,
	modelName string,
) []domain.WorkerEndpoint {
	tokenBytes, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/token")
	if err != nil || len(tokenBytes) == 0 {
		return nil
	}

	url := fmt.Sprintf("https://kubernetes.default.svc/api/v1/namespaces/%s/pods", a.settings.Namespace)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("Authorization", "Bearer "+string(tokenBytes))

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // Cluster internal CA
	}
	client := &http.Client{Transport: tr, Timeout: 3 * time.Second}

	resp, err := client.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		return nil
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024))
	if err != nil {
		return nil
	}

	return a.parsePodsJSON(bodyBytes, modelName)
}

func (a *EndpointDiscoveryAdapter) discoverViaKubectl(
	ctx context.Context,
	modelName string,
) []domain.WorkerEndpoint {
	cmdCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	cmd := exec.CommandContext(
		cmdCtx,
		"kubectl",
		"get",
		"pods",
		"-n",
		a.settings.Namespace,
		"-o",
		"json",
	)
	out, err := cmd.Output()
	if err != nil || len(out) == 0 {
		return nil
	}

	return a.parsePodsJSON(out, modelName)
}

func (a *EndpointDiscoveryAdapter) parsePodsJSON(
	data []byte,
	modelName string,
) []domain.WorkerEndpoint {
	var podList k8sPodList
	if err := json.Unmarshal(data, &podList); err != nil {
		return nil
	}

	var endpoints []domain.WorkerEndpoint
	for _, item := range podList.Items {
		podName := item.Metadata.Name
		if !strings.Contains(podName, modelName) || strings.Contains(podName, "router-scheduler") {
			continue
		}
		if item.Status.Phase != "Running" || item.Status.PodIP == "" {
			continue
		}

		isReady := false
		for _, cond := range item.Status.Conditions {
			if cond.Type == "Ready" && cond.Status == "True" {
				isReady = true
				break
			}
		}

		endpoints = append(endpoints, domain.WorkerEndpoint{
			Name:      podName,
			Address:   item.Status.PodIP,
			Port:      a.settings.DefaultBackendPort,
			Namespace: a.settings.Namespace,
			ModelName: modelName,
			Ready:     isReady,
			Labels:    item.Metadata.Labels,
		})
	}

	return endpoints
}
