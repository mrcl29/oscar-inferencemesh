// Package k8s provides deployment automation and resource orchestration for Envoy AI Gateway and EPP.
package k8s

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/grycap/oscar-inferencemesh/src/internal/infrastructure/k8s/manifests"
)

// EPPDeployOptions contains configuration settings for deploying the Endpoint Picker.
type EPPDeployOptions struct {
	Namespace         string
	ModelName         string
	EPPImage          string
	OscarEndpoint     string
	OscarClusterID    string
	OscarVerifySSL    string
	WorkerWaitTimeout string
	DiskManifestPath  string
	Force             bool
}

// ExtensionPolicyOptions contains configuration for configuring the EnvoyExtensionPolicy ext_proc filter.
type ExtensionPolicyOptions struct {
	Namespace           string
	GatewayName         string
	EPPServiceName      string
	EPPGRPCPort         int
	ExtensionPolicyName string
	DiskManifestPath    string
	Force               bool
}

// GatewayDataPlaneOptions configures the EnvoyProxy, GatewayClass, and Gateway data plane.
type GatewayDataPlaneOptions struct {
	GatewayName        string
	GatewayNamespace   string
	GatewayPort        int
	GatewayServiceType string
	DiskManifestPath   string
	Force              bool
}

// ModelRouteOptions configures connecting a model backend to the Envoy AI Gateway.
type ModelRouteOptions struct {
	Namespace         string
	ModelName         string
	GatewayName       string
	GatewayNamespace  string
	TargetBackendHost string
	TargetBackendPort int
	DiskManifestPath  string
	Force             bool
}

// Deployer defines the contract for deploying, updating, and removing mesh components.
type Deployer interface {
	DeployGateway(ctx context.Context, force bool) error
	DeployGatewayDataPlane(ctx context.Context, opts GatewayDataPlaneOptions) error
	DeployEPP(ctx context.Context, opts EPPDeployOptions) error
	DeployExtensionPolicy(ctx context.Context, opts ExtensionPolicyOptions) error
	ConnectModelRoute(ctx context.Context, opts ModelRouteOptions) error
	DisconnectModelRoute(ctx context.Context, opts ModelRouteOptions) error
	RemoveGatewayDataPlane(ctx context.Context, opts GatewayDataPlaneOptions) error
	RemoveGateway(ctx context.Context) error
	RemoveEPP(ctx context.Context, opts EPPDeployOptions) error
	RemoveExtensionPolicy(ctx context.Context, opts ExtensionPolicyOptions) error
}

// CommandWithStdinRunner executes an external CLI command with optional standard input.
type CommandWithStdinRunner func(ctx context.Context, stdin []byte, name string, args ...string) ([]byte, error)

// DefaultKubeDeployer implements Deployer using kubectl and helm with embedded manifests.
type DefaultKubeDeployer struct {
	CommandRunner       CommandWithStdinRunner
	EnvoyGatewayVersion string
	AIGatewayVersion    string
}

// NewDefaultKubeDeployer initializes a DefaultKubeDeployer.
func NewDefaultKubeDeployer() *DefaultKubeDeployer {
	return &DefaultKubeDeployer{
		CommandRunner: func(ctx context.Context, stdin []byte, name string, args ...string) ([]byte, error) {
			cmd := exec.CommandContext(ctx, name, args...)
			if len(stdin) > 0 {
				cmd.Stdin = strings.NewReader(string(stdin))
			}
			return cmd.CombinedOutput()
		},
		EnvoyGatewayVersion: "1.9.3",
		AIGatewayVersion:    "1.2.0",
	}
}

var requiredAIGatewayCRDs = []string{
	"aigatewayroutes.aigateway.envoyproxy.io",
	"aiservicebackends.aigateway.envoyproxy.io",
}

// DeployGateway provisions Envoy Gateway and Envoy AI Gateway via Helm charts with idempotency.
func (d *DefaultKubeDeployer) DeployGateway(ctx context.Context, force bool) error {
	// Verify helm exists
	if _, err := exec.LookPath("helm"); err != nil {
		return fmt.Errorf("helm not found in PATH: please install helm first")
	}

	// Idempotency check: are both deployments Available, Backend API enabled, and AI Gateway CRDs established?
	if !force {
		egReady := d.isDeploymentAvailable(ctx, "envoy-gateway-system", "envoy-gateway")
		aiegReady := d.isDeploymentAvailable(ctx, "envoy-ai-gateway-system", "ai-gateway-controller")
		backendEnabled := d.isBackendAPIEnabled(ctx)
		crdsReady := d.areAIGatewayCRDsEstablished(ctx)

		if egReady && aiegReady && backendEnabled && crdsReady {
			return nil
		}
	}

	// 1. Deploy Envoy Gateway
	out, err := d.CommandRunner(ctx, nil, "helm", "upgrade", "-i", "eg", "oci://docker.io/envoyproxy/gateway-helm",
		"--version", d.EnvoyGatewayVersion,
		"--namespace", "envoy-gateway-system",
		"--create-namespace",
		"--force-conflicts",
		"--set", "config.envoyGateway.extensionApis.enableBackend=true",
		"--set", "config.envoyGateway.extensionApis.enableEnvoyPatchPolicy=true")
	if err != nil {
		return fmt.Errorf("failed to deploy Envoy Gateway helm release: %w (%s)", err, string(out))
	}

	// Wait for Envoy Gateway rollout
	out, err = d.CommandRunner(ctx, nil, "kubectl", "-n", "envoy-gateway-system", "wait",
		"--for=condition=Available", "deployment/envoy-gateway", "--timeout=180s")
	if err != nil {
		return fmt.Errorf("timeout waiting for Envoy Gateway rollout: %w (%s)", err, string(out))
	}

	// 2. Deploy Envoy AI Gateway CRDs (wait for any terminating CRDs so Helm SSA never patches a deleting object)
	d.waitForTerminatingCRDs(ctx, requiredAIGatewayCRDs)

	out, err = d.CommandRunner(ctx, nil, "helm", "upgrade", "-i", "aieg-crd", "oci://docker.io/envoyproxy/ai-gateway-crds-helm",
		"--version", d.AIGatewayVersion,
		"--namespace", "envoy-ai-gateway-system",
		"--create-namespace",
		"--force-conflicts")
	if err != nil {
		return fmt.Errorf("failed to deploy Envoy AI Gateway CRDs: %w (%s)", err, string(out))
	}

	if err := d.waitForAIGatewayCRDsEstablished(ctx, "60s"); err != nil {
		return err
	}

	// 3. Deploy Envoy AI Gateway Controller
	out, err = d.CommandRunner(ctx, nil, "helm", "upgrade", "-i", "aieg", "oci://docker.io/envoyproxy/ai-gateway-helm",
		"--version", d.AIGatewayVersion,
		"--namespace", "envoy-ai-gateway-system",
		"--create-namespace",
		"--force-conflicts")
	if err != nil {
		return fmt.Errorf("failed to deploy Envoy AI Gateway Controller: %w (%s)", err, string(out))
	}

	// Wait for Envoy AI Gateway rollout
	out, err = d.CommandRunner(ctx, nil, "kubectl", "-n", "envoy-ai-gateway-system", "wait",
		"--for=condition=Available", "deployment/ai-gateway-controller", "--timeout=180s")
	if err != nil {
		return fmt.Errorf("timeout waiting for Envoy AI Gateway rollout: %w (%s)", err, string(out))
	}

	return nil
}

// DeployGatewayDataPlane deploys the GatewayClass, EnvoyProxy, and Gateway listener resources.
func (d *DefaultKubeDeployer) DeployGatewayDataPlane(ctx context.Context, opts GatewayDataPlaneOptions) error {
	gwName, gwNS, gwPort, svcType := normalizeGatewayDataPlaneOpts(opts)

	if !opts.Force {
		if _, gwErr := d.CommandRunner(ctx, nil, "kubectl", "get", "gateway", gwName, "-n", gwNS); gwErr == nil {
			svcOut, svcErr := d.CommandRunner(ctx, nil, "kubectl", "get", "svc", "-n", "envoy-gateway-system",
				"-l", fmt.Sprintf("gateway.envoyproxy.io/owning-gateway-name=%s", gwName),
				"-o", "jsonpath={.items[0].metadata.name}")
			if svcErr == nil && strings.TrimSpace(string(svcOut)) != "" {
				return nil
			}
		}
	}

	rawYAML, err := manifests.GetEnvoyGatewayManifest(opts.DiskManifestPath)
	if err != nil {
		return fmt.Errorf("failed to load EnvoyGateway manifest: %w", err)
	}

	rendered := manifests.RenderManifest(rawYAML, map[string]string{
		"GATEWAY_NAME":         gwName,
		"GATEWAY_NAMESPACE":    gwNS,
		"GATEWAY_PORT":         strconv.Itoa(gwPort),
		"GATEWAY_SERVICE_TYPE": svcType,
	})

	if out, err := d.CommandRunner(ctx, []byte(rendered), "kubectl", "apply", "-f", "-"); err != nil {
		return fmt.Errorf("failed to apply Gateway data-plane manifest: %w (%s)", err, string(out))
	}

	return d.waitForEnvoyDataPlane(ctx, gwName)
}

func (d *DefaultKubeDeployer) waitForEnvoyDataPlane(ctx context.Context, gwName string) error {
	label := fmt.Sprintf("gateway.envoyproxy.io/owning-gateway-name=%s", gwName)
	var deployName string
	for attempt := 0; attempt < 30; attempt++ {
		out, err := d.CommandRunner(ctx, nil, "kubectl", "get", "deployment", "-n", "envoy-gateway-system",
			"-l", label, "-o", "jsonpath={.items[0].metadata.name}")
		if err == nil && strings.TrimSpace(string(out)) != "" {
			deployName = strings.TrimSpace(string(out))
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	if deployName == "" {
		return fmt.Errorf("timed out waiting for Envoy proxy Deployment for gateway '%s'", gwName)
	}

	out, err := d.CommandRunner(ctx, nil, "kubectl", "-n", "envoy-gateway-system", "wait",
		"--for=condition=Available", "deployment/"+deployName, "--timeout=120s")
	if err != nil {
		return fmt.Errorf("timeout waiting for Envoy proxy deployment '%s': %w (%s)", deployName, err, string(out))
	}
	return nil
}

// RemoveGatewayDataPlane deletes the Gateway and EnvoyProxy resources for the configured Gateway.
func (d *DefaultKubeDeployer) RemoveGatewayDataPlane(ctx context.Context, opts GatewayDataPlaneOptions) error {
	gwName, gwNS, _, _ := normalizeGatewayDataPlaneOpts(opts)
	if out, err := d.CommandRunner(ctx, nil, "kubectl", "delete", "gateway", gwName, "-n", gwNS, "--ignore-not-found=true"); err != nil {
		return fmt.Errorf("failed to delete Gateway %s/%s: %w (%s)", gwNS, gwName, err, string(out))
	}
	if out, err := d.CommandRunner(ctx, nil, "kubectl", "delete", "envoyproxy", gwName+"-proxy-config", "-n", gwNS, "--ignore-not-found=true"); err != nil {
		return fmt.Errorf("failed to delete EnvoyProxy %s-proxy-config: %w (%s)", gwName, err, string(out))
	}
	return nil
}

// ConnectModelRoute creates or updates Backend, AIServiceBackend, and AIGatewayRoute for a model.
func (d *DefaultKubeDeployer) ConnectModelRoute(ctx context.Context, opts ModelRouteOptions) error {
	norm := normalizeModelRouteOpts(opts)
	if norm.ModelName == "" {
		return fmt.Errorf("model name is required")
	}

	rawYAML, err := manifests.GetAIGatewayRouteManifest(norm.DiskManifestPath)
	if err != nil {
		return fmt.Errorf("failed to load AIGatewayRoute manifest: %w", err)
	}

	rendered := manifests.RenderManifest(rawYAML, map[string]string{
		"NAMESPACE":               norm.Namespace,
		"MODEL_NAME":              norm.ModelName,
		"GATEWAY_NAME":            norm.GatewayName,
		"GATEWAY_NAMESPACE":       norm.GatewayNamespace,
		"BACKEND_NAME":            norm.ModelName + "-eg-backend",
		"AI_SERVICE_BACKEND_NAME": norm.ModelName + "-backend",
		"ROUTE_NAME":              norm.ModelName + "-ai-route",
		"ROUTE_RULE_NAME":         norm.ModelName + "-inference-rule",
		"TARGET_BACKEND_HOST":     norm.TargetBackendHost,
		"TARGET_BACKEND_PORT":     strconv.Itoa(norm.TargetBackendPort),
	})

	if out, err := d.CommandRunner(ctx, []byte(rendered), "kubectl", "apply", "-f", "-"); err != nil {
		outStr := string(out)
		if isMissingAIGatewayCRDError(outStr) {
			return fmt.Errorf("failed to apply AIGatewayRoute manifest for '%s': %w (%s) — Envoy AI Gateway CRDs are missing; run 'oscar-im init --force' (or './scripts/production/deploy-gateway.sh') to restore them", norm.ModelName, err, outStr)
		}
		return fmt.Errorf("failed to apply AIGatewayRoute manifest for '%s': %w (%s)", norm.ModelName, err, outStr)
	}
	return nil
}

// DisconnectModelRoute removes Backend, AIServiceBackend, and AIGatewayRoute for a model.
func (d *DefaultKubeDeployer) DisconnectModelRoute(ctx context.Context, opts ModelRouteOptions) error {
	norm := normalizeModelRouteOpts(opts)
	if norm.ModelName == "" {
		return fmt.Errorf("model name is required")
	}

	resources := []struct {
		kind string
		name string
	}{
		{"aigatewayroute", norm.ModelName + "-ai-route"},
		{"aiservicebackend", norm.ModelName + "-backend"},
		{"backend", norm.ModelName + "-eg-backend"},
	}
	for _, res := range resources {
		out, err := d.CommandRunner(ctx, nil, "kubectl", "delete", res.kind, res.name, "-n", norm.Namespace, "--ignore-not-found=true")
		if err != nil {
			return fmt.Errorf("failed to delete %s %s: %w (%s)", res.kind, res.name, err, string(out))
		}
	}
	return nil
}

func normalizeGatewayDataPlaneOpts(opts GatewayDataPlaneOptions) (name, ns string, port int, svcType string) {
	name = opts.GatewayName
	if name == "" {
		name = "envoy-ai-gateway"
	}
	ns = opts.GatewayNamespace
	if ns == "" {
		ns = "oscar-svc"
	}
	port = opts.GatewayPort
	if port <= 0 {
		port = 8080
	}
	svcType = opts.GatewayServiceType
	if svcType == "" {
		svcType = "NodePort"
	}
	return name, ns, port, svcType
}

func normalizeModelRouteOpts(opts ModelRouteOptions) ModelRouteOptions {
	if opts.Namespace == "" {
		opts.Namespace = "oscar-svc"
	}
	if opts.GatewayName == "" {
		opts.GatewayName = "envoy-ai-gateway"
	}
	if opts.GatewayNamespace == "" {
		opts.GatewayNamespace = opts.Namespace
	}
	if opts.TargetBackendHost == "" && opts.ModelName != "" {
		opts.TargetBackendHost = fmt.Sprintf("%s-kserve-workload-svc.%s.svc.cluster.local", opts.ModelName, opts.Namespace)
	}
	if opts.TargetBackendPort <= 0 {
		opts.TargetBackendPort = 8000
	}
	return opts
}

// DeployEPP deploys the Endpoint Picker Deployment, Service, and RBAC using embedded or overridden manifests.
func (d *DefaultKubeDeployer) DeployEPP(ctx context.Context, opts EPPDeployOptions) error {
	ns := opts.Namespace
	if ns == "" {
		ns = "oscar-svc"
	}

	// Idempotency check: is EPP already Available?
	if !opts.Force && d.isDeploymentAvailable(ctx, ns, "oscar-inferencemesh-epp") {
		out, err := d.CommandRunner(ctx, nil, "kubectl", "-n", ns, "get", "deployment", "oscar-inferencemesh-epp",
			"-o", "jsonpath={.spec.template.spec.containers[?(@.name=='epp')].env[?(@.name=='EPP_WORKER_WAIT_TIMEOUT_SEC')].value}")
		if err == nil && strings.TrimSpace(string(out)) == opts.WorkerWaitTimeout {
			return nil
		}
	}

	// Load manifest (disk override prioritizing, falling back to embedded FS)
	rawYAML, err := manifests.GetEPPDeploymentManifest(opts.DiskManifestPath)
	if err != nil {
		return fmt.Errorf("failed to load EPP deployment manifest: %w", err)
	}

	vars := map[string]string{
		"NAMESPACE":                   ns,
		"MODEL_NAME":                  opts.ModelName,
		"EPP_IMAGE":                   opts.EPPImage,
		"OSCAR_ENDPOINT":              opts.OscarEndpoint,
		"OSCAR_CLUSTER_ID":            opts.OscarClusterID,
		"OSCAR_VERIFY_SSL":            opts.OscarVerifySSL,
		"EPP_WORKER_WAIT_TIMEOUT_SEC": opts.WorkerWaitTimeout,
	}
	rendered := manifests.RenderManifest(rawYAML, vars)

	// Apply rendered YAML via kubectl apply -f -
	out, err := d.CommandRunner(ctx, []byte(rendered), "kubectl", "apply", "-f", "-")
	if err != nil {
		return fmt.Errorf("failed to apply EPP deployment manifest: %w (%s)", err, string(out))
	}

	// Wait for EPP deployment rollout
	out, err = d.CommandRunner(ctx, nil, "kubectl", "-n", ns, "rollout", "status",
		"deployment/oscar-inferencemesh-epp", "--timeout=120s")
	if err != nil {
		return fmt.Errorf("timeout waiting for EPP rollout: %w (%s)", err, string(out))
	}

	return nil
}

// DeployExtensionPolicy configures the EnvoyExtensionPolicy ext_proc filter using embedded or overridden manifests.
func (d *DefaultKubeDeployer) DeployExtensionPolicy(ctx context.Context, opts ExtensionPolicyOptions) error {
	ns := opts.Namespace
	if ns == "" {
		ns = "oscar-svc"
	}
	policyName := opts.ExtensionPolicyName
	if policyName == "" {
		policyName = "envoy-ai-gateway-epp-ext-proc"
	}
	gatewayName := opts.GatewayName
	if gatewayName == "" {
		gatewayName = "envoy-ai-gateway"
	}
	serviceName := opts.EPPServiceName
	if serviceName == "" {
		serviceName = "oscar-inferencemesh-epp"
	}
	grpcPort := opts.EPPGRPCPort
	if grpcPort <= 0 {
		grpcPort = 9002
	}

	// Idempotency check: is EnvoyExtensionPolicy already Accepted?
	if !opts.Force {
		out, err := d.CommandRunner(ctx, nil, "kubectl", "get", "envoyextensionpolicy", policyName,
			"-n", ns, "-o", "jsonpath={.status.conditions[?(@.type==\"Accepted\")].status}")
		if err == nil && strings.TrimSpace(string(out)) == "True" {
			return nil
		}
	}

	// Load manifest
	rawYAML, err := manifests.GetEnvoyExtensionPolicyManifest(opts.DiskManifestPath)
	if err != nil {
		return fmt.Errorf("failed to load EnvoyExtensionPolicy manifest: %w", err)
	}

	vars := map[string]string{
		"NAMESPACE":             ns,
		"GATEWAY_NAME":          gatewayName,
		"EPP_SERVICE_NAME":      serviceName,
		"EPP_GRPC_PORT":         strconv.Itoa(grpcPort),
		"EXTENSION_POLICY_NAME": policyName,
	}
	rendered := manifests.RenderManifest(rawYAML, vars)

	out, err := d.CommandRunner(ctx, []byte(rendered), "kubectl", "apply", "-f", "-")
	if err != nil {
		return fmt.Errorf("failed to apply EnvoyExtensionPolicy manifest: %w (%s)", err, string(out))
	}

	return nil
}

// RemoveExtensionPolicy deletes the EnvoyExtensionPolicy resource from Kubernetes.
func (d *DefaultKubeDeployer) RemoveExtensionPolicy(ctx context.Context, opts ExtensionPolicyOptions) error {
	ns := opts.Namespace
	if ns == "" {
		ns = "oscar-svc"
	}
	policyName := opts.ExtensionPolicyName
	if policyName == "" {
		policyName = "envoy-ai-gateway-epp-ext-proc"
	}

	out, err := d.CommandRunner(ctx, nil, "kubectl", "delete", "envoyextensionpolicy", policyName,
		"-n", ns, "--ignore-not-found=true")
	if err != nil {
		return fmt.Errorf("failed to delete EnvoyExtensionPolicy: %w (%s)", err, string(out))
	}
	return nil
}

// RemoveEPP deletes the EPP Deployment, Service, and RBAC resources from Kubernetes.
func (d *DefaultKubeDeployer) RemoveEPP(ctx context.Context, opts EPPDeployOptions) error {
	ns := opts.Namespace
	if ns == "" {
		ns = "oscar-svc"
	}

	resources := []struct {
		kind string
		name string
	}{
		{"deployment", "oscar-inferencemesh-epp"},
		{"service", "oscar-inferencemesh-epp"},
		{"rolebinding", "oscar-inferencemesh-epp-rolebinding"},
		{"role", "oscar-inferencemesh-epp-role"},
		{"serviceaccount", "oscar-inferencemesh-epp"},
	}

	for _, res := range resources {
		out, err := d.CommandRunner(ctx, nil, "kubectl", "delete", res.kind, res.name,
			"-n", ns, "--ignore-not-found=true")
		if err != nil {
			return fmt.Errorf("failed to delete %s %s: %w (%s)", res.kind, res.name, err, string(out))
		}
	}
	return nil
}

// RemoveGateway uninstalls Envoy Gateway and Envoy AI Gateway Helm releases and removes their namespaces.
func (d *DefaultKubeDeployer) RemoveGateway(ctx context.Context) error {
	if _, err := exec.LookPath("helm"); err == nil {
		releases := []struct {
			name      string
			namespace string
		}{
			{"aieg", "envoy-ai-gateway-system"},
			{"aieg-crd", "envoy-ai-gateway-system"},
			{"eg", "envoy-gateway-system"},
		}
		for _, rel := range releases {
			statusOut, statusErr := d.CommandRunner(ctx, nil, "helm", "status", rel.name, "-n", rel.namespace)
			if statusErr == nil && !strings.Contains(strings.ToLower(string(statusOut)), "not found") {
				_, _ = d.CommandRunner(ctx, nil, "helm", "uninstall", rel.name, "-n", rel.namespace, "--wait")
			}
		}
	}

	for _, crd := range requiredAIGatewayCRDs {
		if _, err := d.CommandRunner(ctx, nil, "kubectl", "get", "crd", crd); err == nil {
			_, _ = d.CommandRunner(ctx, nil, "kubectl", "wait", "--for=delete", "crd/"+crd, "--timeout=60s")
		}
	}

	_, _ = d.CommandRunner(ctx, nil, "kubectl", "delete", "namespace", "envoy-ai-gateway-system", "--ignore-not-found=true")
	_, _ = d.CommandRunner(ctx, nil, "kubectl", "delete", "namespace", "envoy-gateway-system", "--ignore-not-found=true")

	return nil
}

func (d *DefaultKubeDeployer) isDeploymentAvailable(ctx context.Context, ns, deployment string) bool {
	out, err := d.CommandRunner(ctx, nil, "kubectl", "-n", ns, "wait",
		"--for=condition=Available", fmt.Sprintf("deployment/%s", deployment), "--timeout=2s")
	return err == nil && !strings.Contains(strings.ToLower(string(out)), "error")
}

func (d *DefaultKubeDeployer) isBackendAPIEnabled(ctx context.Context) bool {
	out, err := d.CommandRunner(ctx, nil, "kubectl", "-n", "envoy-gateway-system", "get", "configmap",
		"envoy-gateway-config", "-o", "jsonpath={.data.envoy-gateway\\.yaml}")
	return err == nil && strings.Contains(string(out), "enableBackend: true")
}

func (d *DefaultKubeDeployer) areAIGatewayCRDsEstablished(ctx context.Context) bool {
	return d.waitForAIGatewayCRDsEstablished(ctx, "1s") == nil
}

func (d *DefaultKubeDeployer) waitForAIGatewayCRDsEstablished(ctx context.Context, timeout string) error {
	args := []string{"wait", "--for=condition=Established"}
	for _, crd := range requiredAIGatewayCRDs {
		args = append(args, "crd/"+crd)
	}
	args = append(args, "--timeout="+timeout)
	out, err := d.CommandRunner(ctx, nil, "kubectl", args...)
	if err != nil {
		return fmt.Errorf("Envoy AI Gateway CRDs are not Established: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (d *DefaultKubeDeployer) waitForTerminatingCRDs(ctx context.Context, crds []string) {
	for _, crd := range crds {
		out, err := d.CommandRunner(ctx, nil, "kubectl", "get", "crd", crd, "-o", "jsonpath={.metadata.deletionTimestamp}")
		if err == nil && strings.TrimSpace(string(out)) != "" {
			_, _ = d.CommandRunner(ctx, nil, "kubectl", "wait", "--for=delete", "crd/"+crd, "--timeout=60s")
		}
	}
}

func isMissingAIGatewayCRDError(out string) bool {
	lower := strings.ToLower(out)
	return strings.Contains(lower, "could not find the requested resource") &&
		(strings.Contains(lower, "aigatewayroutes") || strings.Contains(lower, "aiservicebackends"))
}
