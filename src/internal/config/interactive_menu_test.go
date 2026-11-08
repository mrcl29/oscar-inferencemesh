package config

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestRunConfigCommandSubcommands(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yaml")
	t.Setenv("OSCAR_IM_CONFIG", configPath)

	// Create initial file
	cfg := DefaultFileConfig()
	if err := SaveFileConfig(configPath, cfg); err != nil {
		t.Fatalf("failed to create config: %v", err)
	}

	// 1. Help
	var buf bytes.Buffer
	if err := RunConfigCommand([]string{"help"}, nil, &buf); err != nil {
		t.Fatalf("help failed: %v", err)
	}
	if !strings.Contains(buf.String(), "Usage: oscar-im config") {
		t.Fatalf("expected usage output, got: %s", buf.String())
	}

	// 2. Path
	buf.Reset()
	if err := RunConfigCommand([]string{"path"}, nil, &buf); err != nil {
		t.Fatalf("path failed: %v", err)
	}
	if !strings.Contains(buf.String(), configPath) {
		t.Fatalf("expected path %s, got: %s", configPath, buf.String())
	}

	// 3. View
	buf.Reset()
	if err := RunConfigCommand([]string{"view"}, nil, &buf); err != nil {
		t.Fatalf("view failed: %v", err)
	}
	if !strings.Contains(buf.String(), "current_cluster: local") {
		t.Fatalf("expected current_cluster in yaml, got: %s", buf.String())
	}

	// 4. Set model
	buf.Reset()
	if err := RunConfigCommand([]string{"set", "model", "llama-3-8b"}, nil, &buf); err != nil {
		t.Fatalf("set model failed: %v", err)
	}
	reloaded, _ := LoadOrCreateFileConfig(configPath)
	if reloaded.Defaults.Model != "llama-3-8b" {
		t.Fatalf("expected model llama-3-8b, got %s", reloaded.Defaults.Model)
	}

	// 5. Set docker mode
	buf.Reset()
	if err := RunConfigCommand([]string{"set", "docker", "true"}, nil, &buf); err != nil {
		t.Fatalf("set docker failed: %v", err)
	}
	reloaded, _ = LoadOrCreateFileConfig(configPath)
	if !reloaded.Defaults.Docker {
		t.Fatalf("expected docker true, got %t", reloaded.Defaults.Docker)
	}

	// 6. Use cluster
	buf.Reset()
	if err := RunConfigCommand([]string{"use", "local"}, nil, &buf); err != nil {
		t.Fatalf("use cluster failed: %v", err)
	}
}

func TestMenuModelNavigationAndActions(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yaml")
	cfg := DefaultFileConfig()
	cfg.Clusters["test-node"] = ClusterConfig{
		Endpoint: "https://test.oscar.es",
	}

	m := newMenuModel(cfg, configPath)

	// 1. Initial state
	if m.state != stateMainMenu {
		t.Fatalf("expected stateMainMenu, got %d", m.state)
	}
	if m.mainCursor != 0 {
		t.Fatalf("expected initial cursor 0, got %d", m.mainCursor)
	}

	// 2. Down navigation
	msgDown := tea.KeyMsg{Type: tea.KeyDown}
	res, _ := m.Update(msgDown)
	m = res.(*menuModel)
	if m.mainCursor != 1 {
		t.Fatalf("expected cursor 1 after Down, got %d", m.mainCursor)
	}

	// 3. Up navigation
	msgUp := tea.KeyMsg{Type: tea.KeyUp}
	res, _ = m.Update(msgUp)
	m = res.(*menuModel)
	if m.mainCursor != 0 {
		t.Fatalf("expected cursor 0 after Up, got %d", m.mainCursor)
	}

	// 4. Enter on Switch Active Cluster (cursor 0)
	msgEnter := tea.KeyMsg{Type: tea.KeyEnter}
	res, _ = m.Update(msgEnter)
	m = res.(*menuModel)
	if m.state != stateSwitchCluster {
		t.Fatalf("expected stateSwitchCluster, got %d", m.state)
	}

	// 5. Select first cluster with Enter
	res, _ = m.Update(msgEnter)
	m = res.(*menuModel)
	if m.state != stateMainMenu {
		t.Fatalf("expected return to stateMainMenu, got %d", m.state)
	}
	if !m.dirty {
		t.Fatalf("expected dirty=true after switching cluster")
	}

	// 6. Save with 's' key
	msgSave := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}}
	res, _ = m.Update(msgSave)
	m = res.(*menuModel)
	if m.dirty {
		t.Fatalf("expected dirty=false after save")
	}

	// 7. Verify file written
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("expected config file to be written: %v", err)
	}

	// 8. View rendering check
	viewStr := m.View()
	if !strings.Contains(viewStr, "OSCAR-IM Configuration Manager") {
		t.Fatalf("expected title in view output, got: %s", viewStr)
	}
	if !strings.Contains(viewStr, "Switch active OSCAR cluster") {
		t.Fatalf("expected menu item in view output, got: %s", viewStr)
	}
}
