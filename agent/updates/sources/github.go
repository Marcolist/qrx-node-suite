package sources

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"qrx-node-suite/agent/updates/manifest"
)

// GitHubReleaseSource resolves a manifest from a GitHub repository's
// releases: the "stable" channel is the repository's latest release; other
// channels (beta, nightly, development) are the newest release whose tag
// starts with "<channel>-" (e.g. "beta-v0.3.0"). This is a convention this
// project defines, not a GitHub feature -- release tagging must follow it
// for beta/nightly channels to resolve.
type GitHubReleaseSource struct {
	Owner      string
	Repo       string
	AssetName  string // e.g. "manifest.json", the release asset holding the signed manifest
	HTTPClient *http.Client
	// BaseURL overrides the GitHub API base (default https://api.github.com)
	// -- exists so tests can point this at an httptest.Server.
	BaseURL string
}

func (s *GitHubReleaseSource) Name() string { return "github" }

func (s *GitHubReleaseSource) baseURL() string {
	if s.BaseURL != "" {
		return s.BaseURL
	}
	return "https://api.github.com"
}

type ghRelease struct {
	TagName     string    `json:"tag_name"`
	PublishedAt string    `json:"published_at"`
	Assets      []ghAsset `json:"assets"`
	Prerelease  bool      `json:"prerelease"`
}

type ghAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

func (s *GitHubReleaseSource) findAssetURL(rel ghRelease) (string, error) {
	for _, a := range rel.Assets {
		if a.Name == s.AssetName {
			return a.BrowserDownloadURL, nil
		}
	}
	return "", fmt.Errorf("github: release %s has no asset named %q", rel.TagName, s.AssetName)
}

func (s *GitHubReleaseSource) resolveRelease(ctx context.Context, channel string) (ghRelease, error) {
	client := defaultClient(s.HTTPClient)
	if channel == "stable" {
		url := fmt.Sprintf("%s/repos/%s/%s/releases/latest", s.baseURL(), s.Owner, s.Repo)
		body, err := httpGet(ctx, client, url)
		if err != nil {
			return ghRelease{}, err
		}
		defer body.Close()
		var rel ghRelease
		if err := json.NewDecoder(body).Decode(&rel); err != nil {
			return ghRelease{}, fmt.Errorf("github: decode latest release: %w", err)
		}
		return rel, nil
	}

	// Non-stable channels: list releases, pick the newest tagged "<channel>-".
	url := fmt.Sprintf("%s/repos/%s/%s/releases", s.baseURL(), s.Owner, s.Repo)
	body, err := httpGet(ctx, client, url)
	if err != nil {
		return ghRelease{}, err
	}
	defer body.Close()
	var releases []ghRelease
	if err := json.NewDecoder(body).Decode(&releases); err != nil {
		return ghRelease{}, fmt.Errorf("github: decode releases list: %w", err)
	}
	prefix := channel + "-"
	var matches []ghRelease
	for _, r := range releases {
		if strings.HasPrefix(r.TagName, prefix) {
			matches = append(matches, r)
		}
	}
	if len(matches) == 0 {
		return ghRelease{}, fmt.Errorf("github: no release found for channel %q (expected a tag prefixed %q)", channel, prefix)
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].PublishedAt > matches[j].PublishedAt })
	return matches[0], nil
}

func (s *GitHubReleaseSource) FetchManifest(ctx context.Context, channel string) (*manifest.Manifest, error) {
	rel, err := s.resolveRelease(ctx, channel)
	if err != nil {
		return nil, err
	}
	assetURL, err := s.findAssetURL(rel)
	if err != nil {
		return nil, err
	}
	body, err := httpGet(ctx, defaultClient(s.HTTPClient), assetURL)
	if err != nil {
		return nil, fmt.Errorf("github: fetch manifest asset: %w", err)
	}
	defer body.Close()
	return manifest.Parse(body)
}

func (s *GitHubReleaseSource) OpenArtifact(ctx context.Context, url string) (io.ReadCloser, error) {
	return httpGet(ctx, defaultClient(s.HTTPClient), url)
}
