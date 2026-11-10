// Package oscar implements the OSCAR serverless platform control plane adapter.
package oscar

import (
	"context"
	"os/exec"
	"strings"
	"time"

	"github.com/grycap/oscar-inferencemesh/src/internal/config"
	"github.com/grycap/oscar-inferencemesh/src/internal/domain"
)

// ControlPlaneAdapter implements domain.OscarClientPort via oscar-cli and REST hooks.
type ControlPlaneAdapter struct {
	settings config.Settings
}

// NewControlPlaneAdapter creates a new OSCAR ControlPlaneAdapter.
func NewControlPlaneAdapter(settings config.Settings) *ControlPlaneAdapter {
	return &ControlPlaneAdapter{settings: settings}
}

// GetClusterInfo inspects OSCAR cluster connectivity and registered services.
func (a *ControlPlaneAdapter) GetClusterInfo(ctx context.Context) domain.OscarClusterStatus {
	cliPath, err := exec.LookPath("oscar-cli")
	if err != nil {
		return domain.OscarClusterStatus{
			Connected:   false,
			ClusterID:   a.settings.OscarClusterID,
			Endpoint:    a.settings.OscarEndpoint,
			AdapterMode: "stub-no-cli",
			Error:       "oscar-cli binary not found in PATH",
		}
	}

	cmdCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()

	// TODO(FG-06): Bind CLI execution explicitly to the configured cluster and verify the
	// reported identity; ambient CLI defaults need not match settings.OscarClusterID/Endpoint.
	cmd := exec.CommandContext(cmdCtx, cliPath, "service", "list")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return domain.OscarClusterStatus{
			Connected:   false,
			ClusterID:   a.settings.OscarClusterID,
			Endpoint:    a.settings.OscarEndpoint,
			AdapterMode: "oscar-cli-error",
			Error:       strings.TrimSpace(string(out)),
		}
	}

	var rows []string
	for _, line := range strings.Split(string(out), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			rows = append(rows, trimmed)
		}
	}

	return domain.OscarClusterStatus{
		Connected:       true,
		ClusterID:       a.settings.OscarClusterID,
		Endpoint:        a.settings.OscarEndpoint,
		AdapterMode:     "oscar-cli",
		RawServiceTable: rows,
	}
}

// GetService checks whether serviceName is registered in the OSCAR cluster.
func (a *ControlPlaneAdapter) GetService(
	ctx context.Context,
	serviceName string,
) (*domain.OscarServiceSummary, error) {
	info := a.GetClusterInfo(ctx)
	if !info.Connected {
		return nil, nil
	}

	for _, row := range info.RawServiceTable {
		if strings.HasPrefix(row, "NAME") || strings.HasPrefix(row, "---") {
			continue
		}
		cols := strings.Fields(row)
		if len(cols) > 0 && cols[0] == serviceName {
			summary := &domain.OscarServiceSummary{
				Name:      cols[0],
				Image:     "unknown",
				CPU:       "unknown",
				Memory:    "unknown",
				ClusterID: a.settings.OscarClusterID,
			}
			if len(cols) > 1 {
				summary.Image = cols[1]
			}
			if len(cols) > 2 {
				summary.CPU = cols[2]
			}
			if len(cols) > 3 {
				summary.Memory = cols[3]
			}
			return summary, nil
		}
	}

	return nil, nil
}

// ApplyScalingDecision executes or skips a scaling decision on the OSCAR control plane.
// In the pass-through skeleton phase, ScalingActionNoOp returns nil without mutating the cluster.
func (a *ControlPlaneAdapter) ApplyScalingDecision(
	_ context.Context,
	decision domain.ScalingDecision,
) error {
	if decision.Action == domain.ScalingActionNoOp {
		return nil
	}
	// TODO(FG-06): Apply non-no-op decisions through a verified OSCAR transport, with
	// idempotency, explicit cluster targeting and observable failures; returning nil here performs no action.
	// Choose the supported transport after validating its contract; both REST and CLI are not required.
	return nil
}
