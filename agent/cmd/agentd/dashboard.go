package main

import (
	"net/http"
	"os"
	"path/filepath"

	"qrx-node-suite/agent/updates/store"
)

// dashboardHandler serves the built dashboard (docs/updates.md#dashboard-ota-updates:
// "The Agent serves the currently active dashboard build") with a
// client-side-routing fallback to index.html for any path that doesn't
// match a real file, and a clear placeholder page if no build is found at
// all.
//
// Which directory that is, per request: st.CurrentDir() (the OTA store's
// "current" pointer for the dashboard component) when one exists,
// otherwise installDir. Current installers seed the verified bundled build
// into that store; the fallback also supports developer/manual installs
// and older deployments that predate store seeding. Without
// this fallback-to-store-first resolution (fixed for an external audit's
// F07 finding), a "successful" dashboard OTA install would verify, stage,
// and promote correctly, yet every request would keep being served from
// the original install.sh-copied directory forever -- Dashboard's own
// RequiresRestart()==false design assumes the opposite: that the very
// next request after Promote() sees the new build, with no Agent restart.
// Resolving per request (not once at startup) is what makes that true.
//
// Release builds compile and package the dashboard separately so dashboard
// OTA updates remain possible without replacing the Agent binary. See
// docs/deployment.md.
func dashboardHandler(installDir string, st *store.Store) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dir := installDir
		if cur, err := st.CurrentDir(); err == nil {
			dir = cur
		}
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
