// Package sources implements docs/updates.md#update-source-abstraction: an
// UpdateSource interface so GitHub releases are never hardcoded throughout
// the update pipeline, plus four providers (GitHub, a static manifest URL, a
// local file, and a fixed in-memory manifest for development/tests).
package sources

import (
	"context"
	"io"

	"qrx-node-suite/agent/updates/manifest"
)

// Source fetches manifests and downloads the artifacts they reference.
// agent/updates.Manager depends only on this interface, never on a specific
// provider.
type Source interface {
	// Name identifies the source for logging/audit (e.g. "github",
	// "static", "local", "development").
	Name() string
	// FetchManifest retrieves and JSON-decodes (but does not
	// cryptographically verify -- that's the caller's job, see
	// manifest.VerifyManifestSignature) the current manifest for a channel.
	FetchManifest(ctx context.Context, channel string) (*manifest.Manifest, error)
	// OpenArtifact returns a reader for the artifact at url. Callers must
	// Close it. url is exactly ComponentUpdate.URL from a manifest already
	// fetched via FetchManifest -- sources should not need to reinterpret
	// or rewrite it beyond resolving relative references.
	OpenArtifact(ctx context.Context, url string) (io.ReadCloser, error)
}
