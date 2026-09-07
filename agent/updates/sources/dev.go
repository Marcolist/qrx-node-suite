package sources

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"qrx-node-suite/agent/updates/manifest"
)

// DevelopmentSource serves fixed, in-memory manifests and artifacts. No
// network, no filesystem -- for local development and tests, per
// docs/updates.md#update-source-abstraction. Never use in production: it
// has no way to receive a real, freshly-signed manifest.
type DevelopmentSource struct {
	Manifests map[string]*manifest.Manifest // keyed by channel
	Artifacts map[string][]byte             // keyed by ComponentUpdate.URL
}

func NewDevelopmentSource() *DevelopmentSource {
	return &DevelopmentSource{
		Manifests: map[string]*manifest.Manifest{},
		Artifacts: map[string][]byte{},
	}
}

func (s *DevelopmentSource) Name() string { return "development" }

func (s *DevelopmentSource) FetchManifest(ctx context.Context, channel string) (*manifest.Manifest, error) {
	m, ok := s.Manifests[channel]
	if !ok {
		return nil, fmt.Errorf("development: no manifest registered for channel %q", channel)
	}
	return m, nil
}

func (s *DevelopmentSource) OpenArtifact(ctx context.Context, url string) (io.ReadCloser, error) {
	b, ok := s.Artifacts[url]
	if !ok {
		return nil, fmt.Errorf("development: no artifact registered for url %q", url)
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}
