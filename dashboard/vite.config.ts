import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// Dev server proxies /api and /health to a locally running Agent
// (docs/development.md: `go run ./cmd/agentd` on :8787) so the dashboard
// can be developed without CORS configuration on the Agent side.
export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      '/api': { target: 'http://127.0.0.1:8787', changeOrigin: true },
      '/health': { target: 'http://127.0.0.1:8787', changeOrigin: true },
    },
  },
  build: {
    outDir: 'dist',
  },
});
