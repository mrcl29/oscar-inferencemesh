// Package k8s implements Kubernetes inspection, deployment, and cluster checks for OSCAR InferenceMesh.
package k8s

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/grycap/oscar-inferencemesh/src/internal/domain"
)

// FindingSeverity indicates the health state of a checked mesh component.
type FindingSeverity string

const (
	SeverityOK   FindingSeverity = "OK"
	SeverityWarn FindingSeverity = "WARN"
	SeverityFail FindingSeverity = "FAIL"
)

// StatusOptions configures what mesh resources and endpoints are inspected.
type StatusOptions struct {
	Namespace           string `json:"namespace"`
	ModelName           string `json:"model_name"`
	GatewayName         string `json:"gateway_name"`
	ExtensionPolicyName string `json:"extension_policy_name"`
	EPPServiceName      string `json:"epp_service_name"`
	EPPHTTPPort         int    `json:"epp_http_port"`
	GatewayPort         int    `json:"gateway_port"`
	OscarEndpoint       string `json:"oscar_endpoint"`
	OscarClusterID      string `json:"oscar_cluster_id"`
}

// DiagnosticFinding summarizes a pass, warning, or failure detected during status inspection.
type DiagnosticFinding struct {
	Severity  FindingSeverity `json:"severity"`
	Component string          `json:"component"`
	Message   string          `json:"message"`
	Hint      string          `json:"hint,omitempty"`
}

// OscarStatusSection captures OSCAR control plane connectivity and registered services.
type OscarStatusSection struct {
	Connected          bool                        `json:"connected"`
	ClusterID          string                      `json:"cluster_id"`
	Endpoint           string                      `json:"endpoint"`
	AdapterMode        string                      `json:"adapter_mode"`
	Error              string                      `json:"error,omitempty"`
	RegisteredServices []string                    `json:"registered_services,omitempty"`
	TargetRegistered   bool                        `json:"target_registered"`
	TargetSummary      *domain.OscarServiceSummary `json:"target_summary,omitempty"`
}

// WorkerPodSummary describes an individual KServe/vLLM pod or router scheduler pod.
type WorkerPodSummary struct {
	Name          string `json:"name"`
	Role          string `json:"role"`
	Phase         string `json:"phase"`
	Ready         string `json:"ready"`
	AllReady      bool   `json:"all_ready"`
	Restarts      int32  `json:"restarts"`
	PodIP         string `json:"pod_ip,omitempty"`
	Node          string `json:"node,omitempty"`
	Issue         string `json:"issue,omitempty"`
	IsTerminating bool   `json:"is_terminating,omitempty"`
}

// ModelWorkloadStatus captures KServe LLMInferenceService, worker pods, and EPP discovery state.
type ModelWorkloadStatus struct {
	LLMServiceExists       bool                    `json:"llm_service_exists"`
	DesiredReplicas        int32                   `json:"desired_replicas"`
	WorkerPods             []WorkerPodSummary      `json:"worker_pods,omitempty"`
	ReadyWorkerCount       int                     `json:"ready_worker_count"`
	TotalWorkerCount       int                     `json:"total_worker_count"`
	RouterScheduler        *WorkerPodSummary       `json:"router_scheduler,omitempty"`
	EndpointSliceIPs       []string                `json:"endpoint_slice_ips,omitempty"`
	DiscoveredEndpoints    []domain.WorkerEndpoint `json:"discovered_endpoints,omitempty"`
	DiscoveryError         string                  `json:"discovery_error,omitempty"`
}

// GatewaySummary describes a Kubernetes Gateway resource in the cluster.
type GatewaySummary struct {
	Namespace  string   `json:"namespace"`
	Name       string   `json:"name"`
	ClassName  string   `json:"class_name,omitempty"`
	Programmed string   `json:"programmed,omitempty"`
	Addresses  []string `json:"addresses,omitempty"`
}

// RouteSummary describes an Envoy AI Gateway route resource.
type RouteSummary struct {
	Namespace   string   `json:"namespace"`
	Name        string   `json:"name"`
	Models      []string `json:"models,omitempty"`
	GatewayRefs []string `json:"gateway_refs,omitempty"`
	BackendRefs []string `json:"backend_refs,omitempty"`
	Accepted    string   `json:"accepted,omitempty"`
}

// ConnectedModelSummary describes a model exposed via AIGatewayRoute together with its live pods/endpoints.
type ConnectedModelSummary struct {
	ModelName           string                  `json:"model_name"`
	RouteName           string                  `json:"route_name"`
	RouteAccepted       string                  `json:"route_accepted,omitempty"`
	GatewayRefs         []string                `json:"gateway_refs,omitempty"`
	BackendTarget       string                  `json:"backend_target,omitempty"`
	RegisteredInOSCAR   bool                    `json:"registered_in_oscar"`
	DesiredReplicas     int32                   `json:"desired_replicas"`
	ReadyWorkers        int                     `json:"ready_workers"`
	TotalWorkers        int                     `json:"total_workers"`
	WorkerPods          []WorkerPodSummary      `json:"worker_pods,omitempty"`
	DiscoveredEndpoints []domain.WorkerEndpoint `json:"discovered_endpoints,omitempty"`
	IsEPPDefaultTarget  bool                    `json:"is_epp_default_target,omitempty"`
}

// ExtensionPolicySummary describes the EnvoyExtensionPolicy attaching ext_proc to a Gateway.
type ExtensionPolicySummary struct {
	Exists       bool   `json:"exists"`
	Name         string `json:"name"`
	TargetKind   string `json:"target_kind,omitempty"`
	TargetName   string `json:"target_name,omitempty"`
	TargetExists bool   `json:"target_exists"`
	BackendRef   string `json:"backend_ref,omitempty"`
	FailOpen     bool   `json:"fail_open"`
	Accepted     string `json:"accepted,omitempty"`
}

// GatewayStatusSection aggregates Envoy control plane, data plane, routes, and extension policy.
type GatewayStatusSection struct {
	EnvoyControllerStatus     string                 `json:"envoy_controller_status"`
	AIGatewayControllerStatus string                 `json:"ai_gateway_controller_status"`
	AIGatewayCRDsMissing      bool                   `json:"ai_gateway_crds_missing,omitempty"`
	AIGatewayCRDError         string                 `json:"ai_gateway_crd_error,omitempty"`
	DataPlaneStatus           string                 `json:"data_plane_status"`
	TargetGatewayExists       bool                   `json:"target_gateway_exists"`
	Gateways                  []GatewaySummary       `json:"gateways,omitempty"`
	GatewayNodePort30880      string                 `json:"gateway_nodeport_30880,omitempty"`
	LocalGatewayPortOpen      bool                   `json:"local_gateway_port_open"`
	AIGatewayRoutes           []RouteSummary         `json:"ai_gateway_routes,omitempty"`
	ExtensionPolicy           ExtensionPolicySummary `json:"extension_policy"`
}

// EPPVerifySnapshot mirrors the relevant diagnostic fields from EPP GET /verify.
type EPPVerifySnapshot struct {
	Status string `json:"status"`
	Mode   string `json:"mode"`
	Config struct {
		ModelName          string `json:"model_name"`
		Namespace          string `json:"namespace"`
		DefaultBackendFQDN string `json:"default_backend_fqdn"`
		RoutingMode        string `json:"routing_mode"`
	} `json:"config"`
	Picker struct {
		TotalRequestsProcessed  uint64 `json:"total_requests_processed"`
		RecentDecisionsBuffered int    `json:"recent_decisions_buffered"`
	} `json:"picker"`
	Autoscaler struct {
		ServiceName      string `json:"service_name"`
		TotalEvaluations uint64 `json:"total_evaluations"`
		OscarCluster     struct {
			Connected   bool   `json:"connected"`
			AdapterMode string `json:"adapter_mode"`
			Error       string `json:"error"`
		} `json:"oscar_cluster"`
		LastScalingDecision struct {
			Action          string `json:"action"`
			CurrentReplicas int    `json:"current_replicas"`
			DesiredReplicas int    `json:"desired_replicas"`
			Reason          string `json:"reason"`
		} `json:"last_scaling_decision"`
	} `json:"autoscaler"`
}

// EPPStatusSection captures the EPP Kubernetes deployment and /verify snapshot.
type EPPStatusSection struct {
	DeploymentStatus string             `json:"deployment_status"`
	ReadyReplicas    int32              `json:"ready_replicas"`
	DesiredReplicas  int32              `json:"desired_replicas"`
	PodName          string             `json:"pod_name,omitempty"`
	PodPhase         string             `json:"pod_phase,omitempty"`
	PodIP            string             `json:"pod_ip,omitempty"`
	VerifyReachable  bool               `json:"verify_reachable"`
	VerifySource     string             `json:"verify_source,omitempty"`
	VerifyError      string             `json:"verify_error,omitempty"`
	Verify           *EPPVerifySnapshot `json:"verify,omitempty"`
}

// MeshStatusReport is the full diagnostic report returned by MeshInspector.
type MeshStatusReport struct {
	Timestamp         time.Time               `json:"timestamp"`
	KubeConnected     bool                    `json:"kube_connected"`
	KubeContext       string                  `json:"kube_context,omitempty"`
	KubeEndpoint      string                  `json:"kube_endpoint,omitempty"`
	KubeError         string                  `json:"kube_error,omitempty"`
	Namespace         string                  `json:"namespace"`
	ModelName         string                  `json:"model_name"`
	GatewayName       string                  `json:"gateway_name"`
	Oscar             OscarStatusSection      `json:"oscar"`
	Workload          ModelWorkloadStatus     `json:"workload"`
	ConnectedModels   []ConnectedModelSummary `json:"connected_models,omitempty"`
	UnconnectedModels []string                `json:"unconnected_models,omitempty"`
	Gateway           GatewayStatusSection    `json:"gateway"`
	EPP               EPPStatusSection        `json:"epp"`
	Findings          []DiagnosticFinding     `json:"findings"`
}

// CommandRunner executes CLI commands with arguments and returns combined output.
type CommandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

// MeshInspector gathers live diagnostics across OSCAR, KServe, Gateway, and EPP.
type MeshInspector struct {
	RunCommand        CommandRunner
	OscarClient       domain.OscarClientPort
	EndpointDiscovery domain.EndpointDiscoveryPort
	FetchEPPVerify    func(ctx context.Context, namespace, svcName string, httpPort int, allowPortForward bool) ([]byte, string, error)
	CheckLocalTCP     func(addr string) bool
}

// NewDefaultMeshInspector creates a MeshInspector wired with kubectl, HTTP client, and domain adapters.
func NewDefaultMeshInspector(
	oscarClient domain.OscarClientPort,
	endpointDiscovery domain.EndpointDiscoveryPort,
) *MeshInspector {
	inspector := &MeshInspector{
		RunCommand: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return exec.CommandContext(ctx, name, args...).CombinedOutput()
		},
		OscarClient:       oscarClient,
		EndpointDiscovery: endpointDiscovery,
		CheckLocalTCP: func(addr string) bool {
			// Probe HTTP instead of raw TCP dial so Kind docker-proxy sockets without
			// an underlying Envoy listener (which immediately RST on HTTP) report false.
			client := &http.Client{Timeout: time.Second}
			resp, err := client.Get("http://" + addr + "/")
			if err != nil {
				return false
			}
			_ = resp.Body.Close()
			return true
		},
	}
	inspector.FetchEPPVerify = inspector.defaultFetchEPPVerify
	return inspector
}

// Inspect collects the end-to-end mesh status and builds actionable diagnostic findings.
func (m *MeshInspector) Inspect(ctx context.Context, opts StatusOptions) MeshStatusReport {
	opts = applyDefaultStatusOptions(opts)
	report := MeshStatusReport{
		Timestamp:   time.Now().UTC(),
		Namespace:   opts.Namespace,
		ModelName:   opts.ModelName,
		GatewayName: opts.GatewayName,
	}

	m.inspectKubernetes(ctx, &report)
	m.inspectOscar(ctx, opts, &report)

	if report.KubeConnected {
		m.inspectWorkload(ctx, opts, &report)
		m.inspectGateway(ctx, opts, &report)
		m.inspectEPP(ctx, opts, &report)
		m.inspectConnectedModels(ctx, opts, &report)
	} else {
		// Allow local EPP /verify check even when Kubernetes API is offline (host mode).
		m.inspectEPPViaHTTP(ctx, opts, &report, false)
	}

	report.Findings = buildDiagnosticFindings(opts, &report)
	return report
}

func applyDefaultStatusOptions(opts StatusOptions) StatusOptions {
	if opts.Namespace == "" {
		opts.Namespace = "oscar-svc"
	}
	if opts.ModelName == "" {
		opts.ModelName = "qwen2-5-05b-instruct"
	}
	if opts.GatewayName == "" {
		opts.GatewayName = "envoy-ai-gateway"
	}
	if opts.ExtensionPolicyName == "" {
		opts.ExtensionPolicyName = "envoy-ai-gateway-epp-ext-proc"
	}
	if opts.EPPServiceName == "" {
		opts.EPPServiceName = "oscar-inferencemesh-epp"
	}
	if opts.EPPHTTPPort <= 0 {
		opts.EPPHTTPPort = 9091
	}
	if opts.GatewayPort <= 0 {
		opts.GatewayPort = 8080
	}
	return opts
}

func (m *MeshInspector) inspectKubernetes(ctx context.Context, report *MeshStatusReport) {
	if out, err := m.RunCommand(ctx, "kubectl", "config", "current-context"); err == nil {
		report.KubeContext = strings.TrimSpace(string(out))
	}
	out, err := m.RunCommand(ctx, "kubectl", "cluster-info", "--request-timeout=3s")
	if err != nil {
		report.KubeConnected = false
		report.KubeError = strings.TrimSpace(string(out))
		if report.KubeError == "" {
			report.KubeError = err.Error()
		}
		return
	}
	report.KubeConnected = true
	lines := strings.Split(string(out), "\n")
	if len(lines) > 0 {
		report.KubeEndpoint = strings.TrimSpace(lines[0])
	}
}

func (m *MeshInspector) inspectOscar(ctx context.Context, opts StatusOptions, report *MeshStatusReport) {
	report.Oscar.ClusterID = opts.OscarClusterID
	report.Oscar.Endpoint = opts.OscarEndpoint
	if m.OscarClient == nil {
		report.Oscar.Error = "OSCAR client not configured"
		return
	}
	info := m.OscarClient.GetClusterInfo(ctx)
	report.Oscar.Connected = info.Connected
	if info.ClusterID != "" {
		report.Oscar.ClusterID = info.ClusterID
	}
	if info.Endpoint != "" {
		report.Oscar.Endpoint = info.Endpoint
	}
	report.Oscar.AdapterMode = info.AdapterMode
	report.Oscar.Error = info.Error

	for _, row := range info.RawServiceTable {
		if strings.HasPrefix(row, "NAME") || strings.HasPrefix(row, "---") {
			continue
		}
		cols := strings.Fields(row)
		if len(cols) == 0 {
			continue
		}
		report.Oscar.RegisteredServices = append(report.Oscar.RegisteredServices, cols[0])
		if cols[0] == opts.ModelName {
			report.Oscar.TargetRegistered = true
		}
	}
	if report.Oscar.Connected {
		if summary, _ := m.OscarClient.GetService(ctx, opts.ModelName); summary != nil {
			report.Oscar.TargetRegistered = true
			report.Oscar.TargetSummary = summary
		}
	}
}

type kubePodListJSON struct {
	Items []struct {
		Metadata struct {
			Name              string            `json:"name"`
			DeletionTimestamp string            `json:"deletionTimestamp"`
			Labels            map[string]string `json:"labels"`
		} `json:"metadata"`
		Spec struct {
			NodeName string `json:"nodeName"`
		} `json:"spec"`
		Status struct {
			Phase             string `json:"phase"`
			PodIP             string `json:"podIP"`
			ContainerStatuses []struct {
				Name         string `json:"name"`
				Ready        bool   `json:"ready"`
				RestartCount int32  `json:"restartCount"`
				State        struct {
					Waiting *struct {
						Reason string `json:"reason"`
					} `json:"waiting"`
				} `json:"state"`
				LastState struct {
					Terminated *struct {
						Reason   string `json:"reason"`
						ExitCode int32  `json:"exitCode"`
					} `json:"terminated"`
				} `json:"lastState"`
			} `json:"containerStatuses"`
		} `json:"status"`
	} `json:"items"`
}

func (m *MeshInspector) inspectWorkload(ctx context.Context, opts StatusOptions, report *MeshStatusReport) {
	report.Workload = m.inspectModelWorkload(ctx, opts.Namespace, opts.ModelName)
}

func (m *MeshInspector) inspectModelWorkload(ctx context.Context, namespace, modelName string) ModelWorkloadStatus {
	var wl ModelWorkloadStatus
	// Check KServe LLMInferenceService
	if out, err := m.RunCommand(ctx, "kubectl", "get", "llminferenceservice", modelName, "-n", namespace, "-o", "json"); err == nil {
		var svc struct {
			Spec struct {
				Replicas *int32 `json:"replicas"`
			} `json:"spec"`
		}
		if json.Unmarshal(out, &svc) == nil {
			wl.LLMServiceExists = true
			if svc.Spec.Replicas != nil {
				wl.DesiredReplicas = *svc.Spec.Replicas
			} else {
				wl.DesiredReplicas = 1
			}
		}
	}

	// Inspect model pods (workers + router scheduler)
	labelSel := fmt.Sprintf("app.kubernetes.io/name=%s", modelName)
	if out, err := m.RunCommand(ctx, "kubectl", "get", "pods", "-n", namespace, "-l", labelSel, "-o", "json"); err == nil {
		var podList kubePodListJSON
		if json.Unmarshal(out, &podList) == nil {
			for _, item := range podList.Items {
				summary := buildPodSummary(item.Metadata.Name, item.Metadata.DeletionTimestamp, item.Metadata.Labels, item.Spec.NodeName, item.Status.Phase, item.Status.PodIP, item.Status.ContainerStatuses)
				if summary.Role == "router-scheduler" {
					copySummary := summary
					wl.RouterScheduler = &copySummary
					continue
				}
				wl.WorkerPods = append(wl.WorkerPods, summary)
				wl.TotalWorkerCount++
				if summary.AllReady && !summary.IsTerminating {
					wl.ReadyWorkerCount++
				}
			}
		}
	}

	// Inspect EndpointSlice for <model>-kserve-workload-svc
	esSel := fmt.Sprintf("kubernetes.io/service-name=%s-kserve-workload-svc", modelName)
	if out, err := m.RunCommand(ctx, "kubectl", "get", "endpointslices", "-n", namespace, "-l", esSel, "-o", "json"); err == nil {
		var esList struct {
			Items []struct {
				Endpoints []struct {
					Addresses  []string `json:"addresses"`
					Conditions struct {
						Ready *bool `json:"ready"`
					} `json:"conditions"`
				} `json:"endpoints"`
			} `json:"items"`
		}
		if json.Unmarshal(out, &esList) == nil {
			for _, es := range esList.Items {
				for _, ep := range es.Endpoints {
					if ep.Conditions.Ready != nil && !*ep.Conditions.Ready {
						continue
					}
					wl.EndpointSliceIPs = append(wl.EndpointSliceIPs, ep.Addresses...)
				}
			}
		}
	}

	// Query EPP's Kubernetes EndpointDiscovery adapter directly to see what EPP sees
	if m.EndpointDiscovery != nil {
		endpoints, err := m.EndpointDiscovery.ListEndpoints(ctx, modelName)
		if err != nil {
			wl.DiscoveryError = err.Error()
		} else {
			wl.DiscoveredEndpoints = endpoints
		}
	}
	return wl
}

func buildPodSummary(
	name, deletionTS string,
	labels map[string]string,
	nodeName, phase, podIP string,
	containers []struct {
		Name         string `json:"name"`
		Ready        bool   `json:"ready"`
		RestartCount int32  `json:"restartCount"`
		State        struct {
			Waiting *struct {
				Reason string `json:"reason"`
			} `json:"waiting"`
		} `json:"state"`
		LastState struct {
			Terminated *struct {
				Reason   string `json:"reason"`
				ExitCode int32  `json:"exitCode"`
			} `json:"terminated"`
		} `json:"lastState"`
	},
) WorkerPodSummary {
	role := "worker"
	if labels["app.kubernetes.io/component"] == "llminferenceservice-router-scheduler" {
		role = "router-scheduler"
	}

	readyCount := 0
	totalCount := len(containers)
	var restarts int32
	var issues []string
	for _, c := range containers {
		if c.Ready {
			readyCount++
		}
		restarts += c.RestartCount
		if c.State.Waiting != nil && c.State.Waiting.Reason != "" {
			issues = append(issues, fmt.Sprintf("%s:%s", c.Name, c.State.Waiting.Reason))
		} else if !c.Ready && c.LastState.Terminated != nil && c.LastState.Terminated.Reason != "" {
			issues = append(issues, fmt.Sprintf("%s:last=%s(%d)", c.Name, c.LastState.Terminated.Reason, c.LastState.Terminated.ExitCode))
		}
	}

	isTerminating := deletionTS != ""
	if isTerminating {
		phase = "Terminating"
	}

	return WorkerPodSummary{
		Name:          name,
		Role:          role,
		Phase:         phase,
		Ready:         fmt.Sprintf("%d/%d", readyCount, totalCount),
		AllReady:      totalCount > 0 && readyCount == totalCount,
		Restarts:      restarts,
		PodIP:         podIP,
		Node:          nodeName,
		Issue:         strings.Join(issues, ", "),
		IsTerminating: isTerminating,
	}
}

func (m *MeshInspector) inspectGateway(ctx context.Context, opts StatusOptions, report *MeshStatusReport) {
	report.Gateway.EnvoyControllerStatus = m.deploymentReadySummary(ctx, "envoy-gateway-system", "envoy-gateway")
	report.Gateway.AIGatewayControllerStatus = m.deploymentReadySummary(ctx, "envoy-ai-gateway-system", "ai-gateway-controller")
	report.Gateway.DataPlaneStatus = m.deploymentReadySummaryByLabel(ctx, "envoy-gateway-system", fmt.Sprintf("gateway.envoyproxy.io/owning-gateway-name=%s", opts.GatewayName))

	// Check Gateway resources across all namespaces
	if out, err := m.RunCommand(ctx, "kubectl", "get", "gateways", "-A", "-o", "json"); err == nil {
		var gwList struct {
			Items []struct {
				Metadata struct {
					Name      string `json:"name"`
					Namespace string `json:"namespace"`
				} `json:"metadata"`
				Spec struct {
					GatewayClassName string `json:"gatewayClassName"`
				} `json:"spec"`
				Status struct {
					Addresses []struct {
						Value string `json:"value"`
					} `json:"addresses"`
					Conditions []struct {
						Type   string `json:"type"`
						Status string `json:"status"`
					} `json:"conditions"`
				} `json:"status"`
			} `json:"items"`
		}
		if json.Unmarshal(out, &gwList) == nil {
			for _, gw := range gwList.Items {
				prog := conditionStatus(gw.Status.Conditions, "Programmed")
				var addrs []string
				for _, a := range gw.Status.Addresses {
					addrs = append(addrs, a.Value)
				}
				report.Gateway.Gateways = append(report.Gateway.Gateways, GatewaySummary{
					Namespace:  gw.Metadata.Namespace,
					Name:       gw.Metadata.Name,
					ClassName:  gw.Spec.GatewayClassName,
					Programmed: prog,
					Addresses:  addrs,
				})
				if gw.Metadata.Name == opts.GatewayName && gw.Metadata.Namespace == opts.Namespace {
					report.Gateway.TargetGatewayExists = true
				}
			}
		}
	}

	// Check whether any Kubernetes Service exposes NodePort 30880 (mapped to host 8080 in Kind)
	if out, err := m.RunCommand(ctx, "kubectl", "get", "svc", "-A", "-o", "json"); err == nil {
		var svcList struct {
			Items []struct {
				Metadata struct {
					Name      string `json:"name"`
					Namespace string `json:"namespace"`
				} `json:"metadata"`
				Spec struct {
					Ports []struct {
						NodePort int32 `json:"nodePort"`
						Port     int32 `json:"port"`
					} `json:"ports"`
				} `json:"spec"`
			} `json:"items"`
		}
		if json.Unmarshal(out, &svcList) == nil {
			for _, svc := range svcList.Items {
				for _, p := range svc.Spec.Ports {
					if p.NodePort == 30880 {
						report.Gateway.GatewayNodePort30880 = fmt.Sprintf("%s/%s (port %d -> nodePort 30880)", svc.Metadata.Namespace, svc.Metadata.Name, p.Port)
					}
				}
			}
		}
	}

	if m.CheckLocalTCP != nil {
		report.Gateway.LocalGatewayPortOpen = m.CheckLocalTCP(fmt.Sprintf("127.0.0.1:%d", opts.GatewayPort))
	}

	// Check AIGatewayRoute resources in namespace
	if out, err := m.RunCommand(ctx, "kubectl", "get", "aigatewayroutes", "-n", opts.Namespace, "-o", "json"); err != nil {
		outStr := strings.TrimSpace(string(out))
		if isMissingAIGatewayCRDError(outStr) {
			report.Gateway.AIGatewayCRDsMissing = true
			report.Gateway.AIGatewayCRDError = outStr
		}
	} else {
		var routeList struct {
			Items []struct {
				Metadata struct {
					Name      string            `json:"name"`
					Namespace string            `json:"namespace"`
					Labels    map[string]string `json:"labels"`
				} `json:"metadata"`
				Spec struct {
					ParentRefs []struct {
						Name      string `json:"name"`
						Namespace string `json:"namespace"`
					} `json:"parentRefs"`
					Rules []struct {
						Matches []struct {
							Headers []struct {
								Name  string `json:"name"`
								Value string `json:"value"`
							} `json:"headers"`
						} `json:"matches"`
						BackendRefs []struct {
							Name string `json:"name"`
						} `json:"backendRefs"`
					} `json:"rules"`
				} `json:"spec"`
				Status struct {
					Conditions []struct {
						Type   string `json:"type"`
						Status string `json:"status"`
					} `json:"conditions"`
				} `json:"status"`
			} `json:"items"`
		}
		if json.Unmarshal(out, &routeList) == nil {
			for _, r := range routeList.Items {
				var models, gwRefs, beRefs []string
				for _, p := range r.Spec.ParentRefs {
					if p.Name != "" {
						gwRefs = append(gwRefs, p.Name)
					}
				}
				for _, rule := range r.Spec.Rules {
					for _, match := range rule.Matches {
						for _, h := range match.Headers {
							if h.Value != "" {
								models = append(models, h.Value)
							}
						}
					}
					for _, br := range rule.BackendRefs {
						if br.Name != "" {
							beRefs = append(beRefs, br.Name)
						}
					}
				}
				if len(models) == 0 && r.Metadata.Labels["oscar-inferencemesh.io/model"] != "" {
					models = append(models, r.Metadata.Labels["oscar-inferencemesh.io/model"])
				}
				report.Gateway.AIGatewayRoutes = append(report.Gateway.AIGatewayRoutes, RouteSummary{
					Namespace:   r.Metadata.Namespace,
					Name:        r.Metadata.Name,
					Models:      models,
					GatewayRefs: gwRefs,
					BackendRefs: beRefs,
					Accepted:    conditionStatus(r.Status.Conditions, "Accepted"),
				})
			}
		}
	}

	// Check EnvoyExtensionPolicy in namespace
	ep := ExtensionPolicySummary{
		Name:         opts.ExtensionPolicyName,
		TargetExists: report.Gateway.TargetGatewayExists,
	}
	if out, err := m.RunCommand(ctx, "kubectl", "get", "envoyextensionpolicy", opts.ExtensionPolicyName, "-n", opts.Namespace, "-o", "json"); err == nil {
		var eep struct {
			Spec struct {
				TargetRefs []struct {
					Kind string `json:"kind"`
					Name string `json:"name"`
				} `json:"targetRefs"`
				ExtProc []struct {
					FailOpen    bool `json:"failOpen"`
					BackendRefs []struct {
						Name string `json:"name"`
						Port int32  `json:"port"`
					} `json:"backendRefs"`
				} `json:"extProc"`
			} `json:"spec"`
			Status struct {
				Ancestors []struct {
					Conditions []struct {
						Type   string `json:"type"`
						Status string `json:"status"`
					} `json:"conditions"`
				} `json:"ancestors"`
			} `json:"status"`
		}
		if json.Unmarshal(out, &eep) == nil {
			ep.Exists = true
			if len(eep.Spec.TargetRefs) > 0 {
				ep.TargetKind = eep.Spec.TargetRefs[0].Kind
				ep.TargetName = eep.Spec.TargetRefs[0].Name
				for _, gw := range report.Gateway.Gateways {
					if gw.Name == ep.TargetName && gw.Namespace == opts.Namespace {
						ep.TargetExists = true
						break
					}
				}
			}
			if len(eep.Spec.ExtProc) > 0 {
				ep.FailOpen = eep.Spec.ExtProc[0].FailOpen
				if len(eep.Spec.ExtProc[0].BackendRefs) > 0 {
					b := eep.Spec.ExtProc[0].BackendRefs[0]
					ep.BackendRef = fmt.Sprintf("%s:%d", b.Name, b.Port)
				}
			}
			if len(eep.Status.Ancestors) > 0 {
				ep.Accepted = conditionStatus(eep.Status.Ancestors[0].Conditions, "Accepted")
			}
		}
	}
	report.Gateway.ExtensionPolicy = ep
}

func (m *MeshInspector) inspectEPP(ctx context.Context, opts StatusOptions, report *MeshStatusReport) {
	if out, err := m.RunCommand(ctx, "kubectl", "get", "deployment", opts.EPPServiceName, "-n", opts.Namespace, "-o", "json"); err == nil {
		var dep struct {
			Spec struct {
				Replicas *int32 `json:"replicas"`
			} `json:"spec"`
			Status struct {
				ReadyReplicas int32 `json:"readyReplicas"`
			} `json:"status"`
		}
		if json.Unmarshal(out, &dep) == nil {
			desired := int32(1)
			if dep.Spec.Replicas != nil {
				desired = *dep.Spec.Replicas
			}
			report.EPP.DesiredReplicas = desired
			report.EPP.ReadyReplicas = dep.Status.ReadyReplicas
			report.EPP.DeploymentStatus = fmt.Sprintf("%d/%d Ready", dep.Status.ReadyReplicas, desired)
		}
	} else {
		report.EPP.DeploymentStatus = "Not deployed"
	}

	if out, err := m.RunCommand(ctx, "kubectl", "get", "pods", "-n", opts.Namespace, "-l", fmt.Sprintf("app.kubernetes.io/name=%s", opts.EPPServiceName), "-o", "json"); err == nil {
		var podList kubePodListJSON
		if json.Unmarshal(out, &podList) == nil && len(podList.Items) > 0 {
			p := podList.Items[0]
			report.EPP.PodName = p.Metadata.Name
			report.EPP.PodPhase = p.Status.Phase
			report.EPP.PodIP = p.Status.PodIP
		}
	}

	m.inspectEPPViaHTTP(ctx, opts, report, report.EPP.ReadyReplicas > 0)
}

func (m *MeshInspector) inspectEPPViaHTTP(ctx context.Context, opts StatusOptions, report *MeshStatusReport, allowPortForward bool) {
	if m.FetchEPPVerify == nil {
		return
	}
	raw, source, err := m.FetchEPPVerify(ctx, opts.Namespace, opts.EPPServiceName, opts.EPPHTTPPort, allowPortForward)
	if err != nil {
		report.EPP.VerifyReachable = false
		report.EPP.VerifyError = err.Error()
		return
	}
	var snap EPPVerifySnapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		report.EPP.VerifyReachable = false
		report.EPP.VerifyError = fmt.Sprintf("invalid /verify JSON: %v", err)
		return
	}
	report.EPP.VerifyReachable = true
	report.EPP.VerifySource = source
	report.EPP.Verify = &snap
}

func (m *MeshInspector) defaultFetchEPPVerify(ctx context.Context, namespace, svcName string, httpPort int, allowPortForward bool) ([]byte, string, error) {
	// 1. Try local endpoint first (works for host mode or active user port-forward).
	localURL := fmt.Sprintf("http://127.0.0.1:%d/verify", httpPort)
	if body, err := httpGetWithTimeout(ctx, localURL, 1200*time.Millisecond); err == nil {
		return body, fmt.Sprintf("local (127.0.0.1:%d)", httpPort), nil
	}

	if !allowPortForward {
		return nil, "", fmt.Errorf("local EPP endpoint %s unreachable and cluster port-forward disabled", localURL)
	}

	// 2. Open an ephemeral kubectl port-forward to svc/<svcName> so scratch/distroless containers also work.
	pfCtx, cancelPF := context.WithTimeout(ctx, 8*time.Second)
	defer cancelPF()

	cmd := exec.CommandContext(pfCtx, "kubectl", "port-forward", "-n", namespace, fmt.Sprintf("svc/%s", svcName), fmt.Sprintf("0:%d", httpPort))
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, "", fmt.Errorf("failed to create port-forward stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, "", fmt.Errorf("failed to start kubectl port-forward: %w", err)
	}
	defer func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	}()

	portRe := regexp.MustCompile(`127\.0\.0\.1:(\d+)`)
	localPortCh := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			if matches := portRe.FindStringSubmatch(scanner.Text()); len(matches) == 2 {
				localPortCh <- matches[1]
				return
			}
		}
		close(localPortCh)
	}()

	select {
	case port, ok := <-localPortCh:
		if !ok || port == "" {
			return nil, "", fmt.Errorf("kubectl port-forward closed before binding local port")
		}
		pfURL := fmt.Sprintf("http://127.0.0.1:%s/verify", port)
		body, err := httpGetWithTimeout(pfCtx, pfURL, 3*time.Second)
		if err != nil {
			return nil, "", fmt.Errorf("GET %s failed: %w", pfURL, err)
		}
		return body, fmt.Sprintf("cluster svc/%s (ephemeral port-forward)", svcName), nil
	case <-pfCtx.Done():
		return nil, "", fmt.Errorf("timed out waiting for kubectl port-forward to svc/%s", svcName)
	}
}

func httpGetWithTimeout(ctx context.Context, url string, timeout time.Duration) ([]byte, error) {
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := (&http.Client{Timeout: timeout}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

func (m *MeshInspector) deploymentReadySummary(ctx context.Context, namespace, name string) string {
	out, err := m.RunCommand(ctx, "kubectl", "get", "deployment", name, "-n", namespace, "-o", "json")
	if err != nil {
		return "Not found"
	}
	var dep struct {
		Spec struct {
			Replicas *int32 `json:"replicas"`
		} `json:"spec"`
		Status struct {
			ReadyReplicas int32 `json:"readyReplicas"`
		} `json:"status"`
	}
	if json.Unmarshal(out, &dep) != nil {
		return "Unknown"
	}
	desired := int32(1)
	if dep.Spec.Replicas != nil {
		desired = *dep.Spec.Replicas
	}
	if dep.Status.ReadyReplicas == desired && desired > 0 {
		return fmt.Sprintf("Available (%d/%d)", dep.Status.ReadyReplicas, desired)
	}
	return fmt.Sprintf("Unavailable (%d/%d)", dep.Status.ReadyReplicas, desired)
}

func (m *MeshInspector) deploymentReadySummaryByLabel(ctx context.Context, namespace, labelSelector string) string {
	out, err := m.RunCommand(ctx, "kubectl", "get", "deployment", "-n", namespace, "-l", labelSelector, "-o", "json")
	if err != nil {
		return "Not provisioned"
	}
	var list struct {
		Items []struct {
			Spec struct {
				Replicas *int32 `json:"replicas"`
			} `json:"spec"`
			Status struct {
				ReadyReplicas int32 `json:"readyReplicas"`
			} `json:"status"`
		} `json:"items"`
	}
	if json.Unmarshal(out, &list) != nil || len(list.Items) == 0 {
		return "Not provisioned"
	}
	dep := list.Items[0]
	desired := int32(1)
	if dep.Spec.Replicas != nil {
		desired = *dep.Spec.Replicas
	}
	if dep.Status.ReadyReplicas == desired && desired > 0 {
		return fmt.Sprintf("Available (%d/%d)", dep.Status.ReadyReplicas, desired)
	}
	return fmt.Sprintf("Unavailable (%d/%d)", dep.Status.ReadyReplicas, desired)
}

func (m *MeshInspector) inspectConnectedModels(ctx context.Context, opts StatusOptions, report *MeshStatusReport) {
	backendTargets := m.resolveBackendTargets(ctx, opts.Namespace, &report.Gateway)

	eppTargetModel := opts.ModelName
	if report.EPP.Verify != nil && report.EPP.Verify.Config.ModelName != "" {
		eppTargetModel = report.EPP.Verify.Config.ModelName
	}

	oscarSet := map[string]bool{}
	for _, svc := range report.Oscar.RegisteredServices {
		oscarSet[svc] = true
	}

	connectedSet := map[string]bool{}
	for _, rt := range report.Gateway.AIGatewayRoutes {
		backendTarget := ""
		for _, br := range rt.BackendRefs {
			if target, ok := backendTargets[br]; ok {
				backendTarget = target
				break
			}
		}

		models := rt.Models
		if len(models) == 0 {
			models = []string{rt.Name}
		}
		for _, modelName := range models {
			connectedSet[modelName] = true
			var wl ModelWorkloadStatus
			if modelName == opts.ModelName {
				wl = report.Workload
			} else {
				wl = m.inspectModelWorkload(ctx, opts.Namespace, modelName)
			}
			report.ConnectedModels = append(report.ConnectedModels, ConnectedModelSummary{
				ModelName:           modelName,
				RouteName:           rt.Name,
				RouteAccepted:       rt.Accepted,
				GatewayRefs:         rt.GatewayRefs,
				BackendTarget:       backendTarget,
				RegisteredInOSCAR:   oscarSet[modelName],
				DesiredReplicas:     wl.DesiredReplicas,
				ReadyWorkers:        wl.ReadyWorkerCount,
				TotalWorkers:        wl.TotalWorkerCount,
				WorkerPods:          wl.WorkerPods,
				DiscoveredEndpoints: wl.DiscoveredEndpoints,
				IsEPPDefaultTarget:  modelName == eppTargetModel,
			})
		}
	}

	// Track deployed/registered models not yet connected via AIGatewayRoute.
	seenUnconnected := map[string]bool{}
	for _, svc := range report.Oscar.RegisteredServices {
		if !connectedSet[svc] && !seenUnconnected[svc] {
			seenUnconnected[svc] = true
			report.UnconnectedModels = append(report.UnconnectedModels, svc)
		}
	}
	if out, err := m.RunCommand(ctx, "kubectl", "get", "llminferenceservices", "-n", opts.Namespace, "-o", "json"); err == nil {
		var list struct {
			Items []struct {
				Metadata struct {
					Name string `json:"name"`
				} `json:"metadata"`
			} `json:"items"`
		}
		if json.Unmarshal(out, &list) == nil {
			for _, item := range list.Items {
				if item.Metadata.Name != "" && !connectedSet[item.Metadata.Name] && !seenUnconnected[item.Metadata.Name] {
					seenUnconnected[item.Metadata.Name] = true
					report.UnconnectedModels = append(report.UnconnectedModels, item.Metadata.Name)
				}
			}
		}
	}
}

func (m *MeshInspector) resolveBackendTargets(ctx context.Context, namespace string, gw *GatewayStatusSection) map[string]string {
	backendFQDNs := map[string]string{}
	if out, err := m.RunCommand(ctx, "kubectl", "get", "backends", "-n", namespace, "-o", "json"); err == nil {
		var beList struct {
			Items []struct {
				Metadata struct {
					Name string `json:"name"`
				} `json:"metadata"`
				Spec struct {
					Endpoints []struct {
						FQDN *struct {
							Hostname string `json:"hostname"`
							Port     int    `json:"port"`
						} `json:"fqdn"`
					} `json:"endpoints"`
				} `json:"spec"`
			} `json:"items"`
		}
		if json.Unmarshal(out, &beList) == nil {
			for _, be := range beList.Items {
				if len(be.Spec.Endpoints) > 0 && be.Spec.Endpoints[0].FQDN != nil {
					fqdn := be.Spec.Endpoints[0].FQDN
					backendFQDNs[be.Metadata.Name] = fmt.Sprintf("%s:%d", fqdn.Hostname, fqdn.Port)
				}
			}
		}
	}

	aiTargets := map[string]string{}
	if out, err := m.RunCommand(ctx, "kubectl", "get", "aiservicebackends", "-n", namespace, "-o", "json"); err != nil {
		outStr := strings.TrimSpace(string(out))
		if gw != nil && isMissingAIGatewayCRDError(outStr) {
			gw.AIGatewayCRDsMissing = true
			if gw.AIGatewayCRDError == "" {
				gw.AIGatewayCRDError = outStr
			}
		}
	} else {
		var aisList struct {
			Items []struct {
				Metadata struct {
					Name string `json:"name"`
				} `json:"metadata"`
				Spec struct {
					BackendRef struct {
						Name string `json:"name"`
					} `json:"backendRef"`
				} `json:"spec"`
			} `json:"items"`
		}
		if json.Unmarshal(out, &aisList) == nil {
			for _, ais := range aisList.Items {
				if target, ok := backendFQDNs[ais.Spec.BackendRef.Name]; ok {
					aiTargets[ais.Metadata.Name] = target
				} else if ais.Spec.BackendRef.Name != "" {
					aiTargets[ais.Metadata.Name] = ais.Spec.BackendRef.Name
				}
			}
		}
	}

	if out, err := m.RunCommand(ctx, "kubectl", "get", "inferencepools", "-n", namespace, "-o", "json"); err == nil {
		var poolList struct {
			Items []struct {
				Metadata struct {
					Name string `json:"name"`
				} `json:"metadata"`
				Spec struct {
					TargetPorts []struct {
						Number int `json:"number"`
					} `json:"targetPorts"`
					EndpointPickerRef *struct {
						Name string `json:"name"`
						Port *struct {
							Number int `json:"number"`
						} `json:"port"`
					} `json:"endpointPickerRef"`
				} `json:"spec"`
			} `json:"items"`
		}
		if json.Unmarshal(out, &poolList) == nil {
			for _, pool := range poolList.Items {
				if pool.Metadata.Name == "" {
					continue
				}
				port := 8000
				if len(pool.Spec.TargetPorts) > 0 && pool.Spec.TargetPorts[0].Number > 0 {
					port = pool.Spec.TargetPorts[0].Number
				}
				eppTarget := "epp"
				if pool.Spec.EndpointPickerRef != nil && pool.Spec.EndpointPickerRef.Name != "" {
					eppPort := 9002
					if pool.Spec.EndpointPickerRef.Port != nil && pool.Spec.EndpointPickerRef.Port.Number > 0 {
						eppPort = pool.Spec.EndpointPickerRef.Port.Number
					}
					eppTarget = fmt.Sprintf("%s:%d", pool.Spec.EndpointPickerRef.Name, eppPort)
				}
				aiTargets[pool.Metadata.Name] = fmt.Sprintf("InferencePool/%s (port=%d, epp=%s)", pool.Metadata.Name, port, eppTarget)
			}
		}
	}
	return aiTargets
}

func conditionStatus(conditions []struct {
	Type   string `json:"type"`
	Status string `json:"status"`
}, targetType string) string {
	for _, c := range conditions {
		if c.Type == targetType {
			return c.Status
		}
	}
	return "Unknown"
}

func buildDiagnosticFindings(opts StatusOptions, report *MeshStatusReport) []DiagnosticFinding {
	var findings []DiagnosticFinding

	if !report.KubeConnected {
		findings = append(findings, DiagnosticFinding{
			Severity:  SeverityFail,
			Component: "Kubernetes",
			Message:   "Kubernetes API is unreachable.",
			Hint:      "Verify Kind/kubectl context is running ('kind get clusters' / 'kubectl cluster-info').",
		})
		return findings
	}
	findings = append(findings, DiagnosticFinding{
		Severity:  SeverityOK,
		Component: "Kubernetes",
		Message:   fmt.Sprintf("Cluster reachable (context: %s).", emptyFallback(report.KubeContext, "current")),
	})

	// OSCAR checks
	if !report.Oscar.Connected {
		findings = append(findings, DiagnosticFinding{
			Severity:  SeverityWarn,
			Component: "OSCAR",
			Message:   fmt.Sprintf("Host OSCAR CLI check failed (%s).", emptyFallback(report.Oscar.Error, report.Oscar.AdapterMode)),
			Hint:      "Ensure oscar-cli is installed and active cluster is configured.",
		})
	} else if !report.Oscar.TargetRegistered {
		findings = append(findings, DiagnosticFinding{
			Severity:  SeverityWarn,
			Component: "OSCAR",
			Message:   fmt.Sprintf("Connected to OSCAR '%s' (%d service(s)), but '%s' is not registered.", report.Oscar.ClusterID, len(report.Oscar.RegisteredServices), opts.ModelName),
			Hint:      "Deploy the model in OSCAR with ./scripts/development/deploy-qwen-two-pods.sh.",
		})
	} else {
		findings = append(findings, DiagnosticFinding{
			Severity:  SeverityOK,
			Component: "OSCAR",
			Message:   fmt.Sprintf("Service '%s' is registered in OSCAR cluster '%s' (%d total service(s)).", opts.ModelName, report.Oscar.ClusterID, len(report.Oscar.RegisteredServices)),
		})
	}

	// Model workload & endpoint discovery checks
	if report.Workload.TotalWorkerCount == 0 {
		findings = append(findings, DiagnosticFinding{
			Severity:  SeverityFail,
			Component: "Workers",
			Message:   fmt.Sprintf("No worker pods found for model '%s' in namespace '%s'.", opts.ModelName, opts.Namespace),
			Hint:      "Run ./scripts/development/deploy-qwen-two-pods.sh to deploy a model worker pod.",
		})
	} else if report.Workload.ReadyWorkerCount == 0 {
		findings = append(findings, DiagnosticFinding{
			Severity:  SeverityFail,
			Component: "Workers",
			Message:   fmt.Sprintf("%d worker pod(s) exist for '%s', but 0 are Ready.", report.Workload.TotalWorkerCount, opts.ModelName),
			Hint:      "Inspect pod logs/events (vLLM CPU warmup takes ~2-3 min, or check OOMKilled).",
		})
	} else {
		findings = append(findings, DiagnosticFinding{
			Severity:  SeverityOK,
			Component: "Workers",
			Message:   fmt.Sprintf("%d/%d worker pod(s) Ready; EndpointDiscovery detected %d ready endpoint(s).", report.Workload.ReadyWorkerCount, report.Workload.TotalWorkerCount, len(report.Workload.DiscoveredEndpoints)),
		})
	}

	if report.Workload.DiscoveryError != "" {
		findings = append(findings, DiagnosticFinding{
			Severity:  SeverityFail,
			Component: "EndpointDiscovery",
			Message:   fmt.Sprintf("EPP discovery query failed: %s", report.Workload.DiscoveryError),
		})
	} else if report.Workload.ReadyWorkerCount > 0 && len(report.Workload.DiscoveredEndpoints) == 0 {
		findings = append(findings, DiagnosticFinding{
			Severity:  SeverityFail,
			Component: "EndpointDiscovery",
			Message:   fmt.Sprintf("Worker pods exist (%d Ready) but EPP discovery returned 0 endpoints for '%s'.", report.Workload.ReadyWorkerCount, opts.ModelName),
			Hint:      "Check pod labels ('app.kubernetes.io/name', 'kserve.io/component=workload') in endpoint_discovery.go.",
		})
	}

	// Gateway & Data Plane checks
	if !report.Gateway.TargetGatewayExists {
		findings = append(findings, DiagnosticFinding{
			Severity:  SeverityFail,
			Component: "Gateway",
			Message:   fmt.Sprintf("Envoy Gateway resource '%s/%s' does not exist (only controllers installed).", opts.Namespace, opts.GatewayName),
			Hint:      "Run 'oscar-im init --docker' to deploy the Envoy Gateway data plane and bind port 8080/30880.",
		})
	} else {
		findings = append(findings, DiagnosticFinding{
			Severity:  SeverityOK,
			Component: "Gateway",
			Message:   fmt.Sprintf("Gateway '%s/%s' is present (data plane: %s).", opts.Namespace, opts.GatewayName, emptyFallback(report.Gateway.DataPlaneStatus, "Unknown")),
		})
	}

	if report.Gateway.GatewayNodePort30880 == "" {
		findings = append(findings, DiagnosticFinding{
			Severity:  SeverityFail,
			Component: "IngressPort",
			Message:   fmt.Sprintf("No Kubernetes Service binds NodePort 30880 (localhost:%d will reset connections).", opts.GatewayPort),
			Hint:      "Run 'oscar-im init --docker' to provision EnvoyProxy with NodePort 30880.",
		})
	}

	if report.Gateway.AIGatewayCRDsMissing {
		findings = append(findings, DiagnosticFinding{
			Severity:  SeverityFail,
			Component: "AIGatewayCRDs",
			Message:   "Envoy AI Gateway CRDs (aigatewayroutes / aiservicebackends) are missing from the Kubernetes API.",
			Hint:      "Run 'oscar-im init --force' (or './scripts/production/deploy-gateway.sh') to reinstall the missing CRDs.",
		})
	} else if len(report.ConnectedModels) == 0 {
		findings = append(findings, DiagnosticFinding{
			Severity:  SeverityFail,
			Component: "AIGatewayRoute",
			Message:   fmt.Sprintf("No models are connected via AIGatewayRoute in namespace '%s'.", opts.Namespace),
			Hint:      fmt.Sprintf("Run 'oscar-im model add %s' to connect the model to Envoy Gateway.", opts.ModelName),
		})
	} else {
		findings = append(findings, DiagnosticFinding{
			Severity:  SeverityOK,
			Component: "AIGatewayRoute",
			Message:   fmt.Sprintf("%d connected model route(s) found in namespace '%s'.", len(report.ConnectedModels), opts.Namespace),
		})
	}

	if len(report.UnconnectedModels) > 0 {
		findings = append(findings, DiagnosticFinding{
			Severity:  SeverityWarn,
			Component: "ConnectedModels",
			Message:   fmt.Sprintf("Deployed OSCAR/KServe model(s) not yet connected to the Gateway: %s.", strings.Join(report.UnconnectedModels, ", ")),
			Hint:      fmt.Sprintf("Run 'oscar-im model add %s' to expose it via Envoy Gateway.", report.UnconnectedModels[0]),
		})
	}

	// EnvoyExtensionPolicy checks (optional/legacy when InferencePool routes manage endpointPickerRef natively)
	hasInferencePool := false
	for _, cm := range report.ConnectedModels {
		if strings.HasPrefix(cm.BackendTarget, "InferencePool/") {
			hasInferencePool = true
			break
		}
	}
	ep := report.Gateway.ExtensionPolicy
	if !ep.Exists {
		if hasInferencePool || len(report.ConnectedModels) == 0 {
			findings = append(findings, DiagnosticFinding{
				Severity:  SeverityOK,
				Component: "ExtensionPolicy",
				Message:   fmt.Sprintf("No gateway-wide EnvoyExtensionPolicy '%s'; InferencePool endpointPickerRef binds EPP natively per model route.", opts.ExtensionPolicyName),
			})
		} else {
			findings = append(findings, DiagnosticFinding{
				Severity:  SeverityFail,
				Component: "ExtensionPolicy",
				Message:   fmt.Sprintf("EnvoyExtensionPolicy '%s' not found in namespace '%s'.", opts.ExtensionPolicyName, opts.Namespace),
				Hint:      "Connect the model via InferencePool ('oscar-im model add') or apply EnvoyExtensionPolicy.",
			})
		}
	} else if !ep.TargetExists {
		findings = append(findings, DiagnosticFinding{
			Severity:  SeverityFail,
			Component: "ExtensionPolicy",
			Message:   fmt.Sprintf("EnvoyExtensionPolicy '%s' targets %s '%s', which does not exist.", ep.Name, emptyFallback(ep.TargetKind, "Gateway"), ep.TargetName),
			Hint:      "Create the target Gateway resource before Envoy ext_proc traffic can invoke EPP.",
		})
	} else if hasInferencePool {
		findings = append(findings, DiagnosticFinding{
			Severity:  SeverityWarn,
			Component: "ExtensionPolicy",
			Message:   fmt.Sprintf("EnvoyExtensionPolicy '%s' is present alongside InferencePool routes and may cause duplicate/plaintext ext_proc calls.", ep.Name),
			Hint:      fmt.Sprintf("Remove it with 'kubectl delete envoyextensionpolicy %s -n %s'.", ep.Name, opts.Namespace),
		})
	} else if ep.Accepted != "True" {
		findings = append(findings, DiagnosticFinding{
			Severity:  SeverityWarn,
			Component: "ExtensionPolicy",
			Message:   fmt.Sprintf("EnvoyExtensionPolicy '%s' target exists, but Accepted=%s.", ep.Name, emptyFallback(ep.Accepted, "Unknown")),
		})
	} else {
		findings = append(findings, DiagnosticFinding{
			Severity:  SeverityOK,
			Component: "ExtensionPolicy",
			Message:   fmt.Sprintf("EnvoyExtensionPolicy '%s' is Accepted -> %s.", ep.Name, ep.BackendRef),
		})
	}

	// EPP runtime checks
	if report.EPP.VerifyReachable && report.EPP.Verify != nil {
		v := report.EPP.Verify
		findings = append(findings, DiagnosticFinding{
			Severity:  SeverityOK,
			Component: "EPP",
			Message:   fmt.Sprintf("EPP /verify reachable (%s): mode=%s, processed=%d req(s), %s", report.EPP.VerifySource, v.Mode, v.Picker.TotalRequestsProcessed, v.Autoscaler.LastScalingDecision.Reason),
		})
		if !v.Autoscaler.OscarCluster.Connected {
			findings = append(findings, DiagnosticFinding{
				Severity:  SeverityWarn,
				Component: "EPP->OSCAR",
				Message:   fmt.Sprintf("Inside EPP, OSCAR adapter is '%s' (%s) — pod discovery still works via Kubernetes API.", v.Autoscaler.OscarCluster.AdapterMode, v.Autoscaler.OscarCluster.Error),
			})
		}
	} else if report.EPP.ReadyReplicas == 0 {
		findings = append(findings, DiagnosticFinding{
			Severity:  SeverityFail,
			Component: "EPP",
			Message:   fmt.Sprintf("EPP deployment '%s' is not ready (%s).", opts.EPPServiceName, report.EPP.DeploymentStatus),
			Hint:      "Run 'oscar-im init --docker' or start host EPP via 'go -C src run ./cmd/epp'.",
		})
	} else {
		findings = append(findings, DiagnosticFinding{
			Severity:  SeverityWarn,
			Component: "EPP",
			Message:   fmt.Sprintf("EPP pod is Ready, but /verify could not be queried (%s).", report.EPP.VerifyError),
		})
	}

	return findings
}

func emptyFallback(val, fallback string) string {
	if strings.TrimSpace(val) == "" {
		return fallback
	}
	return val
}
