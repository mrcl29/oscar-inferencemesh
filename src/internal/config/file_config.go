// Package config implements persistent file configuration for OSCAR InferenceMesh.
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// ClusterConfig holds connection and authentication details for an OSCAR cluster.
type ClusterConfig struct {
	Endpoint     string `yaml:"endpoint" json:"endpoint"`
	AuthUser     string `yaml:"auth_user,omitempty" json:"auth_user,omitempty"`
	AuthPassword string `yaml:"auth_password,omitempty" json:"auth_password,omitempty"`
	AuthToken    string `yaml:"auth_token,omitempty" json:"auth_token,omitempty"`
	SSLVerify    bool   `yaml:"ssl_verify" json:"ssl_verify"`
}

// DefaultsConfig defines default parameters for EPP runtime and CLI initialization.
type DefaultsConfig struct {
	Namespace string `yaml:"namespace" json:"namespace"`
	Model     string `yaml:"model" json:"model"`
	HTTPPort  int    `yaml:"http_port" json:"http_port"`
	GRPCPort  int    `yaml:"grpc_port" json:"grpc_port"`
	Docker    bool   `yaml:"docker" json:"docker"`
}

// FileConfig models the complete persistent configuration file (~/.config/inferencemesh/config.yaml).
type FileConfig struct {
	CurrentCluster string                   `yaml:"current_cluster" json:"current_cluster"`
	Clusters       map[string]ClusterConfig `yaml:"clusters" json:"clusters"`
	Defaults       DefaultsConfig           `yaml:"defaults" json:"defaults"`
}

// GetConfigFilePath returns the canonical XDG path to oscar-im configuration.
func GetConfigFilePath() string {
	if custom := os.Getenv("OSCAR_IM_CONFIG"); custom != "" {
		return custom
	}
	if custom := os.Getenv("INFERENCEMESH_CONFIG"); custom != "" {
		return custom
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "oscar-im", "config.yaml")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	newPath := filepath.Join(home, ".config", "oscar-im", "config.yaml")
	if _, err := os.Stat(newPath); err == nil {
		return newPath
	}
	legacyPath := filepath.Join(home, ".config", "inferencemesh", "config.yaml")
	if _, err := os.Stat(legacyPath); err == nil {
		return legacyPath
	}
	return newPath
}

// GetOscarCLIConfigPath returns the path to oscar-cli's configuration file.
func GetOscarCLIConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".oscar-cli", "config.yaml")
}

// DefaultFileConfig generates a baseline configuration.
func DefaultFileConfig() *FileConfig {
	return &FileConfig{
		CurrentCluster: "local",
		Clusters: map[string]ClusterConfig{
			"local": {
				Endpoint:  "https://localhost.direct",
				SSLVerify: false,
			},
		},
		Defaults: DefaultsConfig{
			Namespace: "oscar-svc",
			Model:     "qwen2-5-05b-instruct",
			HTTPPort:  9091,
			GRPCPort:  9002,
			Docker:    false,
		},
	}
}

// LoadOrCreateFileConfig reads the configuration file from disk. If missing, it creates
// the directory and file, attempting to pre-populate clusters from ~/.oscar-cli/config.yaml.
func LoadOrCreateFileConfig(path string) (*FileConfig, error) {
	if path == "" {
		path = GetConfigFilePath()
	}

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		cfg := DefaultFileConfig()

		// Attempt automatic seed import from ~/.oscar-cli/config.yaml
		_ = cfg.ImportFromOscarCLI(GetOscarCLIConfigPath())

		if err := SaveFileConfig(path, cfg); err != nil {
			return nil, fmt.Errorf("failed to create initial config file at %s: %w", path, err)
		}
		return cfg, nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to read config file at %s: %w", path, err)
	}

	var cfg FileConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("malformed configuration YAML at %s: %w", path, err)
	}

	if cfg.Clusters == nil {
		cfg.Clusters = make(map[string]ClusterConfig)
	}
	return &cfg, nil
}

// SaveFileConfig writes the configuration safely to disk with 0600 permissions.
func SaveFileConfig(path string, cfg *FileConfig) error {
	if path == "" {
		path = GetConfigFilePath()
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create config directory %s: %w", dir, err)
	}

	encoded, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("failed to serialize configuration to YAML: %w", err)
	}

	if err := os.WriteFile(path, encoded, 0600); err != nil {
		return fmt.Errorf("failed to write config file %s: %w", path, err)
	}
	return nil
}

type oscarCLIFile struct {
	Oscar   map[string]ClusterConfig `yaml:"oscar"`
	Default string                   `yaml:"default"`
}

// ImportFromOscarCLI imports cluster profiles from ~/.oscar-cli/config.yaml into FileConfig.
func (c *FileConfig) ImportFromOscarCLI(path string) error {
	if path == "" {
		path = GetOscarCLIConfigPath()
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	var raw oscarCLIFile
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return err
	}

	if len(raw.Oscar) == 0 {
		return fmt.Errorf("no clusters found in %s", path)
	}

	if c.Clusters == nil {
		c.Clusters = make(map[string]ClusterConfig)
	}

	for name, cluster := range raw.Oscar {
		c.Clusters[name] = cluster
	}

	if raw.Default != "" && c.Clusters[raw.Default].Endpoint != "" {
		c.CurrentCluster = raw.Default
	}
	return nil
}

// GetActiveCluster returns the currently active ClusterConfig.
func (c *FileConfig) GetActiveCluster() (string, ClusterConfig, bool) {
	if c.CurrentCluster == "" {
		for name, cl := range c.Clusters {
			return name, cl, true
		}
		return "", ClusterConfig{}, false
	}
	cl, ok := c.Clusters[c.CurrentCluster]
	return c.CurrentCluster, cl, ok
}
