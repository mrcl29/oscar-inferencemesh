package k8s

import (
	"context"
	"strings"
	"testing"
)

func TestDeployGateway_IdempotentSkip(t *testing.T) {
	executedCommands := []string{}
	deployer := &DefaultKubeDeployer{
		CommandRunner: func(ctx context.Context, stdin []byte, name string, args ...string) ([]byte, error) {
			cmdStr := name + " " + strings.Join(args, " ")
			executedCommands = append(executedCommands, cmdStr)

			if strings.Contains(cmdStr, "wait --for=condition=Available") {
				return []byte("deployment.apps/envoy-gateway condition met"), nil
			}
			if strings.Contains(cmdStr, "get configmap envoy-gateway-config") {
				return []byte("enableBackend: true\nInferencePool\nai-gateway-controller"), nil
			}
			return []byte("ok"), nil
		},
		EnvoyGatewayVersion: "1.9.3",
		AIGatewayVersion:    "1.2.0",
	}

	err := deployer.DeployGateway(context.Background(), false)
	if err != nil {
		t.Fatalf("expected nil error on idempotent skip, got: %v", err)
	}

	for _, cmd := range executedCommands {
		if strings.HasPrefix(cmd, "helm") {
			t.Fatalf("expected helm not to be called on idempotent skip, but was called: %s", cmd)
		}
	}
}

func TestDeployEPP_IdempotentSkip(t *testing.T) {
	appliedStdin := []byte(nil)
	deployer := &DefaultKubeDeployer{
		CommandRunner: func(ctx context.Context, stdin []byte, name string, args ...string) ([]byte, error) {
			cmdStr := name + " " + strings.Join(args, " ")
			if strings.Contains(cmdStr, "get deployment oscar-inferencemesh-epp") {
				return []byte(""), nil
			}
			if strings.Contains(cmdStr, "wait --for=condition=Available") {
				return []byte("deployment.apps/oscar-inferencemesh-epp condition met"), nil
			}
			if strings.Contains(cmdStr, "apply -f -") {
				appliedStdin = stdin
			}
			return []byte("ok"), nil
		},
	}

	opts := EPPDeployOptions{
		Namespace: "oscar-svc",
		Force:     false,
	}

	err := deployer.DeployEPP(context.Background(), opts)
	if err != nil {
		t.Fatalf("expected nil error on idempotent skip, got: %v", err)
	}

	if appliedStdin != nil {
		t.Fatal("expected manifest not to be applied when already Available and Force=false")
	}
}

func TestDeployEPP_AvailableDeploymentWithChangedWorkerWaitTimeoutRollsOut(t *testing.T) {
	var applied bool
	var rolledOut bool
	deployer := &DefaultKubeDeployer{
		CommandRunner: func(ctx context.Context, stdin []byte, name string, args ...string) ([]byte, error) {
			cmd := name + " " + strings.Join(args, " ")
			switch {
			case strings.Contains(cmd, "wait --for=condition=Available"):
				return []byte("deployment available"), nil
			case strings.Contains(cmd, "get deployment oscar-inferencemesh-epp"):
				return []byte("30"), nil
			case strings.Contains(cmd, "apply -f -"):
				applied = true
			case strings.Contains(cmd, "rollout status"):
				rolledOut = true
			}
			return []byte("ok"), nil
		},
	}

	err := deployer.DeployEPP(context.Background(), EPPDeployOptions{
		Namespace: "oscar-svc", WorkerWaitTimeout: "45", Force: false,
	})
	if err != nil {
		t.Fatalf("expected successful rollout, got: %v", err)
	}
	if !applied || !rolledOut {
		t.Fatalf("expected changed timeout to apply and roll out; applied=%v rolledOut=%v", applied, rolledOut)
	}
}

func TestDeployEPP_ApplyEmbeddedManifest(t *testing.T) {
	var appliedStdin []byte
	rolloutChecked := false

	deployer := &DefaultKubeDeployer{
		CommandRunner: func(ctx context.Context, stdin []byte, name string, args ...string) ([]byte, error) {
			cmdStr := name + " " + strings.Join(args, " ")
			if strings.Contains(cmdStr, "wait --for=condition=Available") {
				return nil, context.DeadlineExceeded // simulate not ready
			}
			if strings.Contains(cmdStr, "apply -f -") {
				appliedStdin = stdin
				return []byte("deployment.apps/oscar-inferencemesh-epp created"), nil
			}
			if strings.Contains(cmdStr, "rollout status") {
				rolloutChecked = true
				return []byte("deployment successfully rolled out"), nil
			}
			return []byte("ok"), nil
		},
	}

	opts := EPPDeployOptions{
		Namespace:        "test-namespace",
		ModelName:        "test-model",
		EPPImage:         "custom-epp:v1",
		OscarEndpoint:    "https://test.direct",
		OscarClusterID:   "test-cluster",
		OscarVerifySSL:   "false",
		Force:            false,
		DiskManifestPath: "",
	}

	err := deployer.DeployEPP(context.Background(), opts)
	if err != nil {
		t.Fatalf("expected successful deploy, got error: %v", err)
	}

	if len(appliedStdin) == 0 {
		t.Fatal("expected manifest content to be piped to stdin, got empty")
	}

	manifestStr := string(appliedStdin)
	if !strings.Contains(manifestStr, "namespace: test-namespace") {
		t.Fatalf("expected rendered namespace 'test-namespace', got:\n%s", manifestStr[:200])
	}
	if !strings.Contains(manifestStr, "image: custom-epp:v1") {
		t.Fatalf("expected rendered image 'custom-epp:v1', got:\n%s", manifestStr[:200])
	}
	if !rolloutChecked {
		t.Fatal("expected rollout status check to be performed")
	}
}

func TestDeployExtensionPolicy_IdempotentSkip(t *testing.T) {
	applied := false
	deployer := &DefaultKubeDeployer{
		CommandRunner: func(ctx context.Context, stdin []byte, name string, args ...string) ([]byte, error) {
			cmdStr := name + " " + strings.Join(args, " ")
			if strings.Contains(cmdStr, "jsonpath={.status.conditions") {
				return []byte("True"), nil
			}
			if strings.Contains(cmdStr, "apply -f -") {
				applied = true
			}
			return []byte("ok"), nil
		},
	}

	opts := ExtensionPolicyOptions{
		Namespace:           "oscar-svc",
		ExtensionPolicyName: "test-policy",
		Force:               false,
	}

	err := deployer.DeployExtensionPolicy(context.Background(), opts)
	if err != nil {
		t.Fatalf("expected nil error on idempotent skip, got: %v", err)
	}
	if applied {
		t.Fatal("expected policy not to be reapplied when status is Accepted=True and Force=false")
	}
}

func TestDeployExtensionPolicy_ApplyEmbeddedManifest(t *testing.T) {
	var appliedStdin []byte
	deployer := &DefaultKubeDeployer{
		CommandRunner: func(ctx context.Context, stdin []byte, name string, args ...string) ([]byte, error) {
			cmdStr := name + " " + strings.Join(args, " ")
			if strings.Contains(cmdStr, "jsonpath={.status.conditions") {
				return []byte("False"), nil
			}
			if strings.Contains(cmdStr, "apply -f -") {
				appliedStdin = stdin
				return []byte("envoyextensionpolicy created"), nil
			}
			return []byte("ok"), nil
		},
	}

	opts := ExtensionPolicyOptions{
		Namespace:           "custom-ns",
		GatewayName:         "custom-gateway",
		EPPServiceName:      "custom-epp-svc",
		EPPGRPCPort:         9005,
		ExtensionPolicyName: "custom-policy",
		Force:               false,
	}

	err := deployer.DeployExtensionPolicy(context.Background(), opts)
	if err != nil {
		t.Fatalf("expected successful deploy, got error: %v", err)
	}

	manifestStr := string(appliedStdin)
	if !strings.Contains(manifestStr, "name: custom-policy") {
		t.Fatalf("expected rendered policy name, got:\n%s", manifestStr)
	}
	if !strings.Contains(manifestStr, "port: 9005") {
		t.Fatalf("expected rendered port 9005, got:\n%s", manifestStr)
	}
}

func TestRemoveExtensionPolicy(t *testing.T) {
	executed := []string{}
	deployer := &DefaultKubeDeployer{
		CommandRunner: func(ctx context.Context, stdin []byte, name string, args ...string) ([]byte, error) {
			executed = append(executed, name+" "+strings.Join(args, " "))
			return []byte("deleted"), nil
		},
	}

	opts := ExtensionPolicyOptions{
		Namespace:           "test-ns",
		ExtensionPolicyName: "test-policy",
	}

	err := deployer.RemoveExtensionPolicy(context.Background(), opts)
	if err != nil {
		t.Fatalf("expected nil error on remove policy, got: %v", err)
	}

	expected := "kubectl delete envoyextensionpolicy test-policy -n test-ns --ignore-not-found=true"
	if len(executed) != 1 || executed[0] != expected {
		t.Fatalf("expected command %q, got: %v", expected, executed)
	}
}

func TestRemoveEPP(t *testing.T) {
	executed := []string{}
	deployer := &DefaultKubeDeployer{
		CommandRunner: func(ctx context.Context, stdin []byte, name string, args ...string) ([]byte, error) {
			executed = append(executed, name+" "+strings.Join(args, " "))
			return []byte("deleted"), nil
		},
	}

	opts := EPPDeployOptions{
		Namespace: "test-ns",
	}

	err := deployer.RemoveEPP(context.Background(), opts)
	if err != nil {
		t.Fatalf("expected nil error on remove EPP, got: %v", err)
	}

	if len(executed) != 5 {
		t.Fatalf("expected 5 resource deletions, got: %d (%v)", len(executed), executed)
	}
	if !strings.Contains(executed[0], "delete deployment oscar-inferencemesh-epp -n test-ns") {
		t.Fatalf("expected deployment deletion first, got: %s", executed[0])
	}
}

func TestRemoveGateway(t *testing.T) {
	executed := []string{}
	deployer := &DefaultKubeDeployer{
		CommandRunner: func(ctx context.Context, stdin []byte, name string, args ...string) ([]byte, error) {
			cmdStr := name + " " + strings.Join(args, " ")
			executed = append(executed, cmdStr)
			if strings.HasPrefix(cmdStr, "helm status") {
				return []byte("STATUS: deployed"), nil
			}
			return []byte("uninstalled"), nil
		},
	}

	err := deployer.RemoveGateway(context.Background())
	if err != nil {
		t.Fatalf("expected nil error on remove gateway, got: %v", err)
	}

	hasNamespaceDelete := false
	hasWaitUninstall := false
	hasCRDDeleteWait := false
	for _, cmd := range executed {
		if strings.Contains(cmd, "delete namespace envoy-gateway-system") {
			hasNamespaceDelete = true
		}
		if strings.Contains(cmd, "helm uninstall aieg-crd") && strings.Contains(cmd, "--wait") {
			hasWaitUninstall = true
		}
		if strings.Contains(cmd, "wait --for=delete crd/aigatewayroutes.aigateway.envoyproxy.io") {
			hasCRDDeleteWait = true
		}
	}
	if !hasNamespaceDelete || !hasWaitUninstall || !hasCRDDeleteWait {
		t.Fatalf("expected namespace deletion, --wait uninstall, and CRD delete wait; got=%v", executed)
	}
}

func TestDeployGatewayDataPlane_AppliesAndWaits(t *testing.T) {
	var appliedYAML string
	var waitedDeployment bool
	deployer := &DefaultKubeDeployer{
		CommandRunner: func(ctx context.Context, stdin []byte, name string, args ...string) ([]byte, error) {
			cmd := name + " " + strings.Join(args, " ")
			switch {
			case strings.Contains(cmd, "get gateway "):
				return nil, context.DeadlineExceeded
			case strings.Contains(cmd, "apply -f -"):
				appliedYAML = string(stdin)
			case strings.Contains(cmd, "get deployment -n envoy-gateway-system"):
				return []byte("envoy-oscar-svc-envoy-ai-gateway-123"), nil
			case strings.Contains(cmd, "wait --for=condition=Available deployment/envoy-oscar-svc-envoy-ai-gateway-123"):
				waitedDeployment = true
			}
			return []byte("ok"), nil
		},
	}

	err := deployer.DeployGatewayDataPlane(context.Background(), GatewayDataPlaneOptions{
		GatewayNamespace: "oscar-svc",
		GatewayPort:      8080,
	})
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
	if !strings.Contains(appliedYAML, "name: envoy-ai-gateway") || !strings.Contains(appliedYAML, "port: 8080") {
		t.Fatalf("unexpected rendered Gateway YAML:\n%s", appliedYAML)
	}
	if !waitedDeployment {
		t.Fatal("expected wait for Envoy proxy deployment Available condition")
	}
}

func TestConnectAndDisconnectModelRoute(t *testing.T) {
	var appliedYAML string
	var deleted []string
	deployer := &DefaultKubeDeployer{
		CommandRunner: func(ctx context.Context, stdin []byte, name string, args ...string) ([]byte, error) {
			cmd := name + " " + strings.Join(args, " ")
			if strings.Contains(cmd, "apply -f -") {
				appliedYAML = string(stdin)
			}
			if strings.Contains(cmd, "delete ") {
				deleted = append(deleted, cmd)
			}
			return []byte("ok"), nil
		},
	}

	err := deployer.ConnectModelRoute(context.Background(), ModelRouteOptions{
		Namespace: "oscar-svc",
		ModelName: "qwen2-5-05b-instruct",
	})
	if err != nil {
		t.Fatalf("ConnectModelRoute failed: %v", err)
	}
	if !strings.Contains(appliedYAML, "kind: InferencePool") ||
		!strings.Contains(appliedYAML, "name: qwen2-5-05b-instruct-pool") ||
		!strings.Contains(appliedYAML, "name: oscar-inferencemesh-epp") ||
		!strings.Contains(appliedYAML, "value: qwen2-5-05b-instruct") {
		t.Fatalf("unexpected rendered route YAML:\n%s", appliedYAML)
	}

	if err := deployer.DisconnectModelRoute(context.Background(), ModelRouteOptions{
		Namespace: "oscar-svc",
		ModelName: "qwen2-5-05b-instruct",
	}); err != nil {
		t.Fatalf("DisconnectModelRoute failed: %v", err)
	}
	if len(deleted) != 4 {
		t.Fatalf("expected 4 deleted resources, got %d (%v)", len(deleted), deleted)
	}
}

func TestDeployGateway_UpgradesWhenInferencePoolExtensionMissing(t *testing.T) {
	var egUpgradeStdin string
	deployer := &DefaultKubeDeployer{
		CommandRunner: func(ctx context.Context, stdin []byte, name string, args ...string) ([]byte, error) {
			cmdStr := name + " " + strings.Join(args, " ")
			switch {
			case strings.Contains(cmdStr, "wait --for=condition=Available"):
				return []byte("condition met"), nil
			case strings.Contains(cmdStr, "get configmap envoy-gateway-config"):
				return []byte("enableBackend: true"), nil
			case strings.Contains(cmdStr, "helm upgrade -i eg "):
				egUpgradeStdin = string(stdin)
				return []byte("upgraded"), nil
			default:
				return []byte("ok"), nil
			}
		},
		EnvoyGatewayVersion: "1.9.3",
		AIGatewayVersion:    "1.2.0",
	}

	if err := deployer.DeployGateway(context.Background(), false); err != nil {
		t.Fatalf("DeployGateway failed: %v", err)
	}
	if !strings.Contains(egUpgradeStdin, "extensionManager:") || !strings.Contains(egUpgradeStdin, "InferencePool") {
		t.Fatalf("expected eg helm upgrade stdin with extensionManager + InferencePool, got:\n%s", egUpgradeStdin)
	}
}

func TestDeployGateway_RepairsMissingAIGatewayCRDs(t *testing.T) {
	var commands []string
	deployer := &DefaultKubeDeployer{
		CommandRunner: func(ctx context.Context, stdin []byte, name string, args ...string) ([]byte, error) {
			cmdStr := name + " " + strings.Join(args, " ")
			commands = append(commands, cmdStr)
			switch {
			case strings.Contains(cmdStr, "wait --for=condition=Available"):
				return []byte("condition met"), nil
			case strings.Contains(cmdStr, "get configmap envoy-gateway-config"):
				return []byte("enableBackend: true\nInferencePool\nai-gateway-controller"), nil
			case strings.Contains(cmdStr, "wait --for=condition=Established") && strings.Contains(cmdStr, "--timeout=1s"):
				return []byte("not found"), context.DeadlineExceeded
			case strings.Contains(cmdStr, "get crd ") && strings.Contains(cmdStr, "deletionTimestamp"):
				return []byte("2026-11-14T20:27:00Z"), nil
			default:
				return []byte("ok"), nil
			}
		},
		EnvoyGatewayVersion: "1.9.3",
		AIGatewayVersion:    "1.2.0",
	}

	if err := deployer.DeployGateway(context.Background(), false); err != nil {
		t.Fatalf("expected DeployGateway to repair missing CRDs cleanly, got: %v", err)
	}

	var waitedDelete, ranCRDHelm, waitedEstablished bool
	for _, cmd := range commands {
		if strings.Contains(cmd, "wait --for=delete crd/aigatewayroutes.aigateway.envoyproxy.io") {
			waitedDelete = true
		}
		if strings.Contains(cmd, "helm upgrade -i aieg-crd") {
			ranCRDHelm = true
		}
		if strings.Contains(cmd, "wait --for=condition=Established") && strings.Contains(cmd, "--timeout=60s") {
			waitedEstablished = true
		}
	}
	if !waitedDelete || !ranCRDHelm || !waitedEstablished {
		t.Fatalf("expected terminating CRD wait, aieg-crd helm install, and 60s Established wait; cmds=%v", commands)
	}
}

func TestConnectModelRoute_MissingCRDHint(t *testing.T) {
	deployer := &DefaultKubeDeployer{
		CommandRunner: func(ctx context.Context, stdin []byte, name string, args ...string) ([]byte, error) {
			return []byte(`Error from server (NotFound): error when creating "STDIN": the server could not find the requested resource (post aigatewayroutes.aigateway.envoyproxy.io)`), context.DeadlineExceeded
		},
	}

	err := deployer.ConnectModelRoute(context.Background(), ModelRouteOptions{
		Namespace: "oscar-svc",
		ModelName: "qwen2-5-05b-instruct",
	})
	if err == nil {
		t.Fatal("expected error when CRDs are missing")
	}
	if !strings.Contains(err.Error(), "oscar-im init --force") {
		t.Fatalf("expected actionable recovery hint in error, got: %v", err)
	}
}
