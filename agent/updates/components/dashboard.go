package components

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"qrx-node-suite/agent/updates/store"
)

// Dashboard extracts a dashboard release (a .tar.gz of static assets) and
// health-checks it by confirming the activated build actually has an
// index.html. Per docs/updates.md#dashboard-ota-updates, activating a new
// dashboard build never requires a QRX Core or even Agent restart: the
// Agent serves whichever directory store.Store.CurrentDir() currently
// resolves to on each request, so RequiresRestart is false and
// Stop/Start are no-ops.
type Dashboard struct {
	Store *store.Store
}

func (d *Dashboard) RequiresRestart() bool           { return false }
func (d *Dashboard) Stop(ctx context.Context) error  { return nil }
func (d *Dashboard) Start(ctx context.Context) error { return nil }

func (d *Dashboard) HealthCheck(ctx context.Context) error {
	dir, err := d.Store.CurrentDir()
	if err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(dir, "index.html")); err != nil {
		return fmt.Errorf("dashboard health check: %w", err)
	}
	return nil
}

// Extract untars a gzip-compressed tar archive of static assets into dir,
// rejecting any entry that would escape dir (zip-slip / path traversal).
func (d *Dashboard) Extract(ctx context.Context, artifact io.Reader, dir string) error {
	gz, err := gzip.NewReader(artifact)
	if err != nil {
		return fmt.Errorf("dashboard extract: not a gzip stream: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("dashboard extract: %w", err)
		}
		target, err := safeJoin(dir, hdr.Name)
		if err != nil {
			return err
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("dashboard extract: mkdir %s: %w", hdr.Name, err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("dashboard extract: mkdir for %s: %w", hdr.Name, err)
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode)&0o777|0o600)
			if err != nil {
				return fmt.Errorf("dashboard extract: create %s: %w", hdr.Name, err)
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return fmt.Errorf("dashboard extract: write %s: %w", hdr.Name, err)
			}
			f.Close()
		default:
			// Symlinks, devices, etc. in a static-asset bundle are refused
			// rather than silently skipped or followed.
			return fmt.Errorf("dashboard extract: unsupported tar entry type %v for %s", hdr.Typeflag, hdr.Name)
		}
	}
}

// safeJoin joins dir and name, refusing any result that escapes dir --
// blocks "../../etc/passwd"-style archive entries.
func safeJoin(dir, name string) (string, error) {
	cleaned := filepath.Clean(filepath.Join(dir, name))
	dirWithSep := filepath.Clean(dir) + string(filepath.Separator)
	if cleaned != filepath.Clean(dir) && !strings.HasPrefix(cleaned, dirWithSep) {
		return "", fmt.Errorf("dashboard extract: archive entry %q escapes extraction directory", name)
	}
	return cleaned, nil
}
