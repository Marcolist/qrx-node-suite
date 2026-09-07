package components

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
)

// binaryName returns the platform-appropriate executable name for the
// Agent's own binary within a release directory.
func binaryName() string {
	if runtime.GOOS == "windows" {
		return "agentd.exe"
	}
	return "agentd"
}

// extractBinary writes artifact as dir/<binaryName>, executable. Shared by
// Agent and Adapter controllers: in this project's single-binary
// architecture (ADR-001, agent/adapters/README.md), an adapter code change
// ships as a full Agent binary rebuild, so "extracting" an adapter update
// and "extracting" an agent update are the same operation on the same
// binary -- what differs is what gets health-checked afterward (see
// SelfBinary in selfupdate.go and Adapter's HealthCheck).
func extractBinary(ctx context.Context, artifact io.Reader, dir string) error {
	path := filepath.Join(dir, binaryName())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("extract binary: mkdir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return fmt.Errorf("extract binary: create: %w", err)
	}
	defer f.Close()
	if _, err := io.Copy(f, artifact); err != nil {
		return fmt.Errorf("extract binary: write: %w", err)
	}
	return nil
}
