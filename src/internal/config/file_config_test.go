package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/grycap/oscar-inferencemesh/src/internal/config"
)

func TestFileConfigCreateAndLoad(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "inferencemesh", "config.yaml")

	// 1. Initial creation when missing
	cfg, err := config.LoadOrCreateFileConfig(configPath)
	if err != nil {
		t.Fatalf("failed to create initial config: %v", err)
	}

	if cfg.CurrentCluster != "local" {
		t.Fatalf("expected default cluster 'local', got '%s'", cfg.CurrentCluster)
	}
	if cfg.Defaults.Model != "qwen2-5-05b-instruct" {
		t.Fatalf("expected default model 'qwen2-5-05b-instruct', got '%s'", cfg.Defaults.Model)
	}

	// 2. Modify and save
	cfg.Clusters["prod"] = config.ClusterConfig{
		Endpoint:  "https://oscar.prod.org",
		AuthUser:  "admin",
		SSLVerify: true,
	}
	cfg.CurrentCluster = "prod"
	cfg.Defaults.Namespace = "custom-ns"

	if err := config.SaveFileConfig(configPath, cfg); err != nil {
		t.Fatalf("failed to save config: %v", err)
	}

	// 3. Reload from disk
	reloaded, err := config.LoadOrCreateFileConfig(configPath)
	if err != nil {
		t.Fatalf("failed to reload config: %v", err)
	}

	if reloaded.CurrentCluster != "prod" {
		t.Fatalf("expected current cluster 'prod', got '%s'", reloaded.CurrentCluster)
	}
	if reloaded.Clusters["prod"].Endpoint != "https://oscar.prod.org" {
		t.Fatalf("expected endpoint 'https://oscar.prod.org', got '%s'", reloaded.Clusters["prod"].Endpoint)
	}
	if reloaded.Defaults.Namespace != "custom-ns" {
		t.Fatalf("expected namespace 'custom-ns', got '%s'", reloaded.Defaults.Namespace)
	}
}

func TestImportFromOscarCLI(t *testing.T) {
	tempDir := t.TempDir()
	oscarPath := filepath.Join(tempDir, "oscar-config.yaml")

	rawOscarYAML := `
oscar:
  cluster-a:
    endpoint: https://cluster-a.oscar.es
    auth_user: user-a
    auth_password: secret-password
    ssl_verify: true
  cluster-b:
    endpoint: http://cluster-b.oscar.es
    ssl_verify: false
default: cluster-a
`
	if err := os.WriteFile(oscarPath, []byte(rawOscarYAML), 0600); err != nil {
		t.Fatalf("failed to write mock oscar config: %v", err)
	}

	cfg := config.DefaultFileConfig()
	if err := cfg.ImportFromOscarCLI(oscarPath); err != nil {
		t.Fatalf("failed to import oscar-cli config: %v", err)
	}

	if cfg.CurrentCluster != "cluster-a" {
		t.Fatalf("expected current cluster 'cluster-a', got '%s'", cfg.CurrentCluster)
	}
	if cfg.Clusters["cluster-a"].Endpoint != "https://cluster-a.oscar.es" {
		t.Fatalf("unexpected cluster-a endpoint: %s", cfg.Clusters["cluster-a"].Endpoint)
	}
	if cfg.Clusters["cluster-b"].Endpoint != "http://cluster-b.oscar.es" {
		t.Fatalf("unexpected cluster-b endpoint: %s", cfg.Clusters["cluster-b"].Endpoint)
	}
}
