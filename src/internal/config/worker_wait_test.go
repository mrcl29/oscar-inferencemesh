package config

import (
	"path/filepath"
	"testing"
	"time"
)

func TestWorkerWaitTimeoutDefaultsAndPersists(t *testing.T) {
	if got := DefaultFileConfig().Defaults.WorkerWaitTimeout; got != 30 {
		t.Fatalf("default worker wait timeout = %d, want 30 seconds", got)
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := DefaultFileConfig()
	cfg.Defaults.WorkerWaitTimeout = 7
	if err := SaveFileConfig(path, cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadOrCreateFileConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Defaults.WorkerWaitTimeout != 7 {
		t.Fatalf("persisted timeout = %d, want 7", loaded.Defaults.WorkerWaitTimeout)
	}
}

func TestFromEnvWorkerWaitTimeoutOverridesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := DefaultFileConfig()
	cfg.Defaults.WorkerWaitTimeout = 7
	if err := SaveFileConfig(path, cfg); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OSCAR_IM_CONFIG", path)
	t.Setenv("EPP_WORKER_WAIT_TIMEOUT_SEC", "9")
	got := FromEnv().WorkerWaitTimeout
	if got != 9*time.Second {
		t.Fatalf("timeout = %v, want 9s", got)
	}
}
