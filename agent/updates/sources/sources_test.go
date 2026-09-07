package sources_test

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"qrx-node-suite/agent/updates/manifest"
	"qrx-node-suite/agent/updates/sources"
)

func signedTestManifest(t *testing.T, priv ed25519.PrivateKey, assetURL string) *manifest.Manifest {
	t.Helper()
	sha := "aa1122334455667788990011223344556677889900112233445566778899aa"
	sig, err := manifest.SignComponentChecksum(sha, priv)
	if err != nil {
		t.Fatalf("SignComponentChecksum: %v", err)
	}
	m := &manifest.Manifest{
		ManifestVersion: 1,
		Channel:         "stable",
		SuiteVersion:    "0.2.0",
		ReleasedAt:      time.Now().UTC().Format(time.RFC3339),
		Components: map[string]manifest.ComponentUpdate{
			"agent": {Version: "0.2.0", URL: assetURL, SHA256: sha, Signature: sig},
		},
	}
	manifest.SignManifest(m, priv)
	return m
}

func TestGitHubReleaseSourceStableChannel(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(nil)

	// m is assigned after the server starts (its manifest embeds the
	// server's own URL as the artifact URL); the handlers below close over
	// the pointer and only read it once a request actually arrives, by
	// which point m is set.
	var m *manifest.Manifest
	mux := http.NewServeMux()
	mux.HandleFunc("/manifest.json", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(m)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	mux.HandleFunc("/repos/acme/qrx-node-suite/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"tag_name":     "v0.2.0",
			"published_at": "2026-01-01T00:00:00Z",
			"assets": []map[string]string{
				{"name": "manifest.json", "browser_download_url": srv.URL + "/manifest.json"},
			},
		})
	})
	m = signedTestManifest(t, priv, srv.URL+"/artifact.bin")

	src := &sources.GitHubReleaseSource{
		Owner: "acme", Repo: "qrx-node-suite", AssetName: "manifest.json",
		BaseURL: srv.URL,
	}

	got, err := src.FetchManifest(context.Background(), "stable")
	if err != nil {
		t.Fatalf("FetchManifest: %v", err)
	}
	if got.SuiteVersion != "0.2.0" {
		t.Errorf("SuiteVersion = %q, want 0.2.0", got.SuiteVersion)
	}
	if got.Components["agent"].Version != "0.2.0" {
		t.Errorf("agent version = %q, want 0.2.0", got.Components["agent"].Version)
	}
}

func TestGitHubReleaseSourceBetaChannelTagPrefix(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()

	_, priv, _ := ed25519.GenerateKey(nil)
	m := signedTestManifest(t, priv, srv.URL+"/artifact.bin")

	mux.HandleFunc("/repos/acme/qrx-node-suite/releases", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]map[string]any{
			{"tag_name": "v0.2.0", "published_at": "2026-01-01T00:00:00Z", "assets": []map[string]string{}},
			{"tag_name": "beta-v0.3.0-rc1", "published_at": "2026-02-01T00:00:00Z", "assets": []map[string]string{
				{"name": "manifest.json", "browser_download_url": srv.URL + "/beta-manifest.json"},
			}},
		})
	})
	mux.HandleFunc("/beta-manifest.json", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(m)
	})

	src := &sources.GitHubReleaseSource{Owner: "acme", Repo: "qrx-node-suite", AssetName: "manifest.json", BaseURL: srv.URL}
	got, err := src.FetchManifest(context.Background(), "beta")
	if err != nil {
		t.Fatalf("FetchManifest(beta): %v", err)
	}
	if got.SuiteVersion != "0.2.0" {
		t.Errorf("got suite version %q", got.SuiteVersion)
	}
}

func TestGitHubReleaseSourceNoMatchingChannelErrors(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	mux.HandleFunc("/repos/acme/qrx-node-suite/releases", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]map[string]any{
			{"tag_name": "v0.2.0", "published_at": "2026-01-01T00:00:00Z"},
		})
	})
	src := &sources.GitHubReleaseSource{Owner: "acme", Repo: "qrx-node-suite", AssetName: "manifest.json", BaseURL: srv.URL}
	if _, err := src.FetchManifest(context.Background(), "nightly"); err == nil {
		t.Fatal("expected an error when no release matches the nightly- tag prefix")
	}
}

func TestDevelopmentSource(t *testing.T) {
	src := sources.NewDevelopmentSource()
	m := &manifest.Manifest{ManifestVersion: 1, Channel: "development", SuiteVersion: "0.0.1-dev"}
	src.Manifests["development"] = m
	src.Artifacts["dev://agent"] = []byte("fake agent binary")

	got, err := src.FetchManifest(context.Background(), "development")
	if err != nil {
		t.Fatalf("FetchManifest: %v", err)
	}
	if got != m {
		t.Error("expected the exact registered manifest back")
	}

	rc, err := src.OpenArtifact(context.Background(), "dev://agent")
	if err != nil {
		t.Fatalf("OpenArtifact: %v", err)
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(data) != "fake agent binary" {
		t.Errorf("artifact data = %q", data)
	}

	if _, err := src.FetchManifest(context.Background(), "nightly"); err == nil {
		t.Error("expected an error for an unregistered channel")
	}
}
