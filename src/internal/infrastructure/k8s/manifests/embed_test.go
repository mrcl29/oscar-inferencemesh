package manifests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEmbeddedManifestsExist(t *testing.T) {
	eppData, err := GetEPPDeploymentManifest("")
	if err != nil {
		t.Fatalf("expected embedded EPP deployment manifest, got error: %v", err)
	}
	if len(eppData) == 0 {
		t.Fatal("embedded EPP deployment manifest is empty")
	}
	if !strings.Contains(string(eppData), "oscar-inferencemesh-epp") {
		t.Fatalf("expected manifest to contain 'oscar-inferencemesh-epp', got: %s", string(eppData[:100]))
	}

	policyData, err := GetEnvoyExtensionPolicyManifest("")
	if err != nil {
		t.Fatalf("expected embedded EnvoyExtensionPolicy manifest, got error: %v", err)
	}
	if len(policyData) == 0 {
		t.Fatal("embedded EnvoyExtensionPolicy manifest is empty")
	}
	if !strings.Contains(string(policyData), "EnvoyExtensionPolicy") {
		t.Fatalf("expected manifest to contain 'EnvoyExtensionPolicy', got: %s", string(policyData[:100]))
	}

	gwData, err := GetEnvoyGatewayManifest("")
	if err != nil {
		t.Fatalf("expected embedded EnvoyGateway manifest, got error: %v", err)
	}
	if !strings.Contains(string(gwData), "kind: Gateway") {
		t.Fatalf("expected EnvoyGateway manifest to contain 'kind: Gateway'")
	}

	routeData, err := GetAIGatewayRouteManifest("")
	if err != nil {
		t.Fatalf("expected embedded AIGatewayRoute manifest, got error: %v", err)
	}
	if !strings.Contains(string(routeData), "kind: AIGatewayRoute") {
		t.Fatalf("expected AIGatewayRoute manifest to contain 'kind: AIGatewayRoute'")
	}
}

func TestRenderManifest(t *testing.T) {
	rawTemplate := []byte("namespace: ${NAMESPACE}\nmodel: ${MODEL_NAME}\nport: ${PORT}")
	vars := map[string]string{
		"NAMESPACE":  "custom-ns",
		"MODEL_NAME": "llama-3-8b",
		"PORT":       "9002",
	}

	rendered := RenderManifest(rawTemplate, vars)
	expected := "namespace: custom-ns\nmodel: llama-3-8b\nport: 9002"
	if rendered != expected {
		t.Fatalf("expected rendered string:\n%s\ngot:\n%s", expected, rendered)
	}
}

func TestDiskOverridePriority(t *testing.T) {
	tempDir := t.TempDir()
	overridePath := filepath.Join(tempDir, "custom-epp.yaml")
	customContent := "apiVersion: v1\nkind: CustomEPP"
	if err := os.WriteFile(overridePath, []byte(customContent), 0644); err != nil {
		t.Fatalf("failed to write override file: %v", err)
	}

	data, err := GetManifest(EPPDeploymentFilename, overridePath)
	if err != nil {
		t.Fatalf("unexpected error reading override manifest: %v", err)
	}
	if string(data) != customContent {
		t.Fatalf("expected override content '%s', got '%s'", customContent, string(data))
	}

	// Non-existing override path must fall back to embedded FS
	dataFallback, err := GetManifest(EPPDeploymentFilename, filepath.Join(tempDir, "non-existent.yaml"))
	if err != nil {
		t.Fatalf("unexpected error falling back to embedded manifest: %v", err)
	}
	if !strings.Contains(string(dataFallback), "oscar-inferencemesh-epp") {
		t.Fatalf("expected fallback to embedded manifest, got: %s", string(dataFallback[:100]))
	}
}
