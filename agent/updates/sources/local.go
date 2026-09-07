package sources

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"qrx-node-suite/agent/updates/manifest"
)

// LocalFileSource reads a manifest and its artifacts from local disk, for
// docs/updates.md#offline-installation ("Upload Update Package" / "Install
// from Local File"). The same verification pipeline in
// agent/updates/manifest applies regardless of source -- this type only
// changes where bytes come from, never what's trusted about them.
type LocalFileSource struct {
	// ManifestPathTemplate may contain "{channel}", e.g.
	// "/var/lib/qrx-node-suite/offline/{channel}-manifest.json". For a
	// one-off "install from this exact file" flow, set it to a fixed path
	// with no placeholder.
	ManifestPathTemplate string
	// ArtifactBaseDir resolves artifact URLs that are relative paths. A
	// component URL that is already an absolute path or a "file://" URL is
	// used as-is.
	ArtifactBaseDir string
}

func (s *LocalFileSource) Name() string { return "local" }

func (s *LocalFileSource) manifestPath(channel string) string {
	return strings.ReplaceAll(s.ManifestPathTemplate, "{channel}", channel)
}

func (s *LocalFileSource) FetchManifest(ctx context.Context, channel string) (*manifest.Manifest, error) {
	path := s.manifestPath(channel)
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("local: open manifest %s: %w", path, err)
	}
	defer f.Close()
	return manifest.Parse(f)
}

func (s *LocalFileSource) resolvePath(url string) string {
	if p, ok := strings.CutPrefix(url, "file://"); ok {
		return p
	}
	if filepath.IsAbs(url) {
		return url
	}
	return filepath.Join(s.ArtifactBaseDir, url)
}

func (s *LocalFileSource) OpenArtifact(ctx context.Context, url string) (io.ReadCloser, error) {
	path := s.resolvePath(url)
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("local: open artifact %s: %w", path, err)
	}
	return f, nil
}
