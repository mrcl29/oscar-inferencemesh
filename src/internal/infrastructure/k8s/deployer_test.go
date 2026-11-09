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
				return []byte("enableBackend: true"), nil
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
	for _, cmd := range executed {
		if strings.Contains(cmd, "delete namespace envoy-gateway-system") {
			hasNamespaceDelete = true
		}
	}
	if !hasNamespaceDelete {
		t.Fatal("expected namespace deletion for envoy-gateway-system")
	}
}
