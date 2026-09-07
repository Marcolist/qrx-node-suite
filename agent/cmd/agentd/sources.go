package main

import (
	"fmt"

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
		return &sources.GitHubReleaseSource{Owner: cfg.GitHubOwner, Repo: cfg.GitHubRepo, AssetName: "manifest.json"}, nil
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
