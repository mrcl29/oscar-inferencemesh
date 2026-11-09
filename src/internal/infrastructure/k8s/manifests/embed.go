// Package manifests embeds the declarative Kubernetes deployment manifests for OSCAR InferenceMesh.
package manifests

import (
	"embed"
	"fmt"
	"os"
)

//go:embed *.yaml
var ManifestsFS embed.FS

const (
	// EPPDeploymentFilename is the embedded filename for the EPP Kubernetes deployment.
	EPPDeploymentFilename = "epp-deployment.yaml"
	// EnvoyExtensionPolicyFilename is the embedded filename for the EnvoyExtensionPolicy resource.
	EnvoyExtensionPolicyFilename = "envoy-extension-policy.yaml"
)

// GetManifest retrieves the manifest by filename. If diskOverridePath is provided and
// exists on the local filesystem, it reads from disk; otherwise, it falls back to the embedded FS.
func GetManifest(filename string, diskOverridePath string) ([]byte, error) {
	if diskOverridePath != "" {
		if data, err := os.ReadFile(diskOverridePath); err == nil {
			return data, nil
		}
	}

	data, err := ManifestsFS.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("embedded manifest '%s' not found: %w", filename, err)
	}
	return data, nil
}

// GetEPPDeploymentManifest returns the EPP deployment manifest, prioritizing disk if present.
func GetEPPDeploymentManifest(diskOverridePath string) ([]byte, error) {
	return GetManifest(EPPDeploymentFilename, diskOverridePath)
}

// GetEnvoyExtensionPolicyManifest returns the EnvoyExtensionPolicy manifest, prioritizing disk if present.
func GetEnvoyExtensionPolicyManifest(diskOverridePath string) ([]byte, error) {
	return GetManifest(EnvoyExtensionPolicyFilename, diskOverridePath)
}

// RenderManifest substitutes ${VAR} or $VAR in the template bytes using the provided dictionary.
// If a variable is not found in the dictionary, it checks os.Getenv before falling back to empty string.
func RenderManifest(content []byte, vars map[string]string) string {
	return os.Expand(string(content), func(key string) string {
		if val, exists := vars[key]; exists {
			return val
		}
		return os.Getenv(key)
	})
}
