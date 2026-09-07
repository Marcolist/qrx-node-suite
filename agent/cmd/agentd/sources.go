package main

import (
	"fmt"
	"runtime"

	"qrx-node-suite/agent/config"
	"qrx-node-suite/agent/updates/sources"
)

// buildUpdateSource implements docs/updates.md#update-source-abstraction:
// which provider to use is configuration, never hardcoded.
func buildUpdateSource(cfg config.UpdatesConfig) (sources.Source, error) {
	switch cfg.SourceKind {
	case "", "development":
		return sources.NewDevelopmentSource(), nil
	case "github":
		if cfg.GitHubOwner == "" || cfg.GitHubRepo == "" {
			return nil, fmt.Errorf("updates.source_kind=github requires github_owner and github_repo")
		}
		// The manifest asset is architecture-specific: components.Agent's
		// Extract writes a component's artifact as a raw binary (not a
		// tarball with a wrapping directory or an arch-neutral format), so
		// the "agent" component entry a manifest points at has to be the
		// right architecture's build. The manifest format itself
		// (agent/updates/manifest) has no per-architecture field -- adding
		// one would touch the trust-critical parse/verify code every
		// external security re-review of this project has gone through --
		// so this picks a whole separate, per-architecture manifest asset
		// instead (see .github/workflows/release.yml's "Build OTA update
		// manifests" step, which publishes one per GOARCH this project
		// ships for). This only decides which file to ask for; the actual
		// download and every byte of verification is identical regardless
		// of architecture.
		assetName := fmt.Sprintf("manifest-linux-%s.json", runtime.GOARCH)
		return &sources.GitHubReleaseSource{Owner: cfg.GitHubOwner, Repo: cfg.GitHubRepo, AssetName: assetName}, nil
	case "static":
		if cfg.StaticURLTemplate == "" {
			return nil, fmt.Errorf("updates.source_kind=static requires static_url_template")
		}
		return &sources.StaticManifestSource{URLTemplate: cfg.StaticURLTemplate}, nil
	case "local":
		if cfg.LocalManifestPathTemplate == "" {
			return nil, fmt.Errorf("updates.source_kind=local requires local_manifest_path_template")
		}
		return &sources.LocalFileSource{ManifestPathTemplate: cfg.LocalManifestPathTemplate}, nil
	default:
		return nil, fmt.Errorf("unknown updates.source_kind %q", cfg.SourceKind)
	}
}
