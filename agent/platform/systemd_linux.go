//go:build linux

package platform

import (
	"context"
	"os/exec"
	"strings"

	"qrx-node-suite/agent/models"
)

// Systemd controls services via systemctl. Requires the Agent to have
// permission to manage the unit -- typically a narrowly-scoped polkit rule
// or sudoers entry installed by installer/linux, never running the whole
// Agent as root just for this (docs/security.md).
type Systemd struct {
	// UseSudo prepends "sudo" to systemctl invocations, for an Agent
	// running as an unprivileged user with a sudoers rule scoped to
	// exactly "systemctl {start,stop,restart,status} <unit>".
	UseSudo bool
}

// New returns this platform's ServiceManager.
func New() ServiceManager { return &Systemd{} }

func (s *Systemd) run(ctx context.Context, args ...string) ([]byte, error) {
	name := "systemctl"
	if s.UseSudo {
		args = append([]string{"systemctl"}, args...)
		name = "sudo"
	}
	cmd := exec.CommandContext(ctx, name, args...)
	return cmd.CombinedOutput()
}

func (s *Systemd) Start(ctx context.Context, unit string) error {
	_, err := s.run(ctx, "start", unit)
	return err
}

func (s *Systemd) Stop(ctx context.Context, unit string) error {
	_, err := s.run(ctx, "stop", unit)
	return err
}

func (s *Systemd) Restart(ctx context.Context, unit string) error {
	_, err := s.run(ctx, "restart", unit)
	return err
}

// Status reports the unit's run state. systemctl is-active exits nonzero
// for inactive/failed units too, so the exit code alone doesn't distinguish
// them -- only the printed state text does, which this parses directly.
func (s *Systemd) Status(ctx context.Context, unit string) (models.ServiceState, error) {
	out, _ := s.run(ctx, "is-active", unit)
	switch strings.TrimSpace(string(out)) {
	case "active":
		return models.ServiceRunning, nil
	case "inactive":
		return models.ServiceStopped, nil
	case "failed":
		return models.ServiceFailed, nil
	default:
		return models.ServiceUnknown, nil
	}
}
