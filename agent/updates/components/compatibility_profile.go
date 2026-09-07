package components

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"qrx-node-suite/agent/updates/store"
	"qrx-node-suite/agent/version"
)

// CompatibilityProfile is the Controller for a "compatibility_profile_<qrx
// version>" component: a small, independently-updateable JSON document
// (agent/version.CompatibilityProfile), not a binary -- see
// docs/qrx-compatibility.md. No process restart is needed to pick up a new
// one: Reload (called by the OTA Manager after Promote, in place of
// Stop/Start) re-parses the newly current file and swaps it into whatever
// in-memory holder the rest of the Agent reads from.
type CompatibilityProfile struct {
	Store  *store.Store
	Reload func(p *version.CompatibilityProfile) // installs p as the live profile
}

func (c *CompatibilityProfile) RequiresRestart() bool          { return false }
func (c *CompatibilityProfile) Stop(ctx context.Context) error { return nil }

func (c *CompatibilityProfile) Extract(ctx context.Context, artifact io.Reader, dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("compatibility profile extract: mkdir: %w", err)
	}
	path := filepath.Join(dir, "profile.json")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("compatibility profile extract: create: %w", err)
	}
	defer f.Close()
	if _, err := io.Copy(f, artifact); err != nil {
		return fmt.Errorf("compatibility profile extract: write: %w", err)
	}
	return nil
}

func (c *CompatibilityProfile) Start(ctx context.Context) error {
	dir, err := c.Store.CurrentDir()
	if err != nil {
		return err
	}
	profile, err := version.LoadCompatibilityProfileFile(filepath.Join(dir, "profile.json"))
	if err != nil {
		return fmt.Errorf("compatibility profile start: %w", err)
	}
	if c.Reload != nil {
		c.Reload(profile)
	}
	return nil
}

func (c *CompatibilityProfile) HealthCheck(ctx context.Context) error {
	dir, err := c.Store.CurrentDir()
	if err != nil {
		return err
	}
	_, err = version.LoadCompatibilityProfileFile(filepath.Join(dir, "profile.json"))
	if err != nil {
		return fmt.Errorf("compatibility profile health check: %w", err)
	}
	return nil
}
