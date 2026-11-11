package main

import (
	"path/filepath"
	"testing"

	"github.com/grycap/oscar-inferencemesh/src/internal/config"
)

func TestPersistWorkerWaitTimeout(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	t.Setenv("OSCAR_IM_CONFIG", path)
	cfg := config.DefaultFileConfig()
	if err := persistWorkerWaitTimeout(cfg, 12); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.LoadOrCreateFileConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Defaults.WorkerWaitTimeout != 12 {
		t.Fatalf("persisted timeout = %d, want 12", loaded.Defaults.WorkerWaitTimeout)
	}
}

func TestPersistWorkerWaitTimeoutRejectsNonPositive(t *testing.T) {
	if err := persistWorkerWaitTimeout(config.DefaultFileConfig(), 0); err == nil {
		t.Fatal("expected non-positive timeout to be rejected")
	}
}
