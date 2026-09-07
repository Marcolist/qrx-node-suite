package main

import (
	"net/http"
	"os"
	"path/filepath"
)

// dashboardHandler serves the built dashboard (docs/updates.md#dashboard-ota-updates:
// "The Agent serves the currently active dashboard build") from disk at
// dir, with a client-side-routing fallback to index.html for any path that
// doesn't match a real file, and a clear placeholder page if the directory
// hasn't been built yet -- this sandbox has no network access to run
// `npm install && npm run build` (see docs/development.md), so shipping a
// go:embed'd dist/ isn't possible here; a release build process should
// build the dashboard first and go:embed the result instead of serving
// from disk. See docs/deployment.md.
func dashboardHandler(dir string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		indexPath := filepath.Join(dir, "index.html")
		if _, err := os.Stat(indexPath); err != nil {
			servePlaceholder(w)
			return
		}
		requested := filepath.Join(dir, filepath.Clean(r.URL.Path))
		if info, err := os.Stat(requested); err == nil && !info.IsDir() {
			http.ServeFile(w, r, requested)
			return
		}
		http.ServeFile(w, r, indexPath)
	})
}

const placeholderHTML = `<!doctype html>
<html><head><title>QRX Node Suite</title>
<style>body{font:14px system-ui,sans-serif;background:#0b0f14;color:#d8dee8;padding:3rem;max-width:40rem;margin:0 auto}
code{background:#1a212b;padding:.15em .4em;border-radius:.3em}</style></head>
<body>
<h1>QRX Node Suite</h1>
<p>The Agent is running, but no dashboard build was found.</p>
<p>Build it with:</p>
<pre><code>cd dashboard
npm install
npm run build</code></pre>
<p>Then restart the Agent, or point <code>dashboard_dir</code> in its config
at the built <code>dist/</code> directory.</p>
<p>The API is available at <a href="/api/v1/status">/api/v1/status</a>.</p>
</body></html>`

func servePlaceholder(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(placeholderHTML))
}
