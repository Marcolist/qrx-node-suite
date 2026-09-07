package sources

import (
	"context"
	"io"
	"net/http"
	"strings"

	"qrx-node-suite/agent/updates/manifest"
)

// StaticManifestSource fetches a manifest from a fixed URL template, e.g.
// "https://updates.example.com/manifests/{channel}.json". Simpler than
// GitHubReleaseSource for operators who publish their own update server
// rather than using GitHub releases.
type StaticManifestSource struct {
	URLTemplate string // must contain "{channel}"
	HTTPClient  *http.Client
}

func (s *StaticManifestSource) Name() string { return "static" }

func (s *StaticManifestSource) manifestURL(channel string) string {
	return strings.ReplaceAll(s.URLTemplate, "{channel}", channel)
}

func (s *StaticManifestSource) FetchManifest(ctx context.Context, channel string) (*manifest.Manifest, error) {
	body, err := httpGet(ctx, defaultClient(s.HTTPClient), s.manifestURL(channel))
	if err != nil {
		return nil, err
	}
	defer body.Close()
	return manifest.Parse(body)
}

func (s *StaticManifestSource) OpenArtifact(ctx context.Context, url string) (io.ReadCloser, error) {
	return httpGet(ctx, defaultClient(s.HTTPClient), url)
}
