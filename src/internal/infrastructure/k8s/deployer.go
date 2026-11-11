// Package k8s provides deployment automation and resource orchestration for Envoy AI Gateway and EPP.
package k8s

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

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

// Deployer defines the contract for deploying, updating, and removing mesh components.
type Deployer interface {
	DeployGateway(ctx context.Context, force bool) error
	DeployEPP(ctx context.Context, opts EPPDeployOptions) error
	DeployExtensionPolicy(ctx context.Context, opts ExtensionPolicyOptions) error
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

// DeployGateway provisions Envoy Gateway and Envoy AI Gateway via Helm charts with idempotency.
func (d *DefaultKubeDeployer) DeployGateway(ctx context.Context, force bool) error {
	// Verify helm exists
	if _, err := exec.LookPath("helm"); err != nil {
		return fmt.Errorf("helm not found in PATH: please install helm first")
	}

	// Idempotency check: are both deployments already running and Available with Backend API?
	if !force {
		egReady := d.isDeploymentAvailable(ctx, "envoy-gateway-system", "envoy-gateway")
		aiegReady := d.isDeploymentAvailable(ctx, "envoy-ai-gateway-system", "ai-gateway-controller")
		backendEnabled := d.isBackendAPIEnabled(ctx)

		if egReady && aiegReady && backendEnabled {
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

	// 2. Deploy Envoy AI Gateway CRDs
	out, err = d.CommandRunner(ctx, nil, "helm", "upgrade", "-i", "aieg-crd", "oci://docker.io/envoyproxy/ai-gateway-crds-helm",
		"--version", d.AIGatewayVersion,
		"--namespace", "envoy-ai-gateway-system",
		"--create-namespace",
		"--force-conflicts")
	if err != nil {
		return fmt.Errorf("failed to deploy Envoy AI Gateway CRDs: %w (%s)", err, string(out))
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
				_, _ = d.CommandRunner(ctx, nil, "helm", "uninstall", rel.name, "-n", rel.namespace)
			}
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
