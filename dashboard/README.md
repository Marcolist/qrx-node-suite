# QRX Dashboard

React + TypeScript + Vite web dashboard, served by the Agent (see
`agent/cmd/agentd/dashboard.go`) once built.

```sh
npm install
npm run dev      # dev server on :5173, proxying /api and /health to :8787
npm run build     # writes dist/ -- point the Agent's dashboard_dir at it
```

## Status

`package-lock.json` is committed. CI installs with `npm ci`, runs a full
`npm audit`, typechecks the application, and builds the production assets.

## Structure

```
src/
  api/          typed REST client + TS mirrors of agent/models' JSON shapes
  components/   Layout, Card, StatusPill, ValueDisplay, SimulationBanner, Row
  hooks/        usePolling (centralized polling, mirrors the Agent's own
                 "don't hit qrx-cli per request" principle -- one poll per
                 page, cached in component state), useEvents (SSE)
  pages/        Overview, Node, Validator, Velocity, Activity, System, Logs,
                 Alerts, Settings (General + the detailed Updates tab)
```

## Design notes

- **Simulation mode** (`docs/architecture.md` principle 6): `Layout.tsx`
  shows a persistent, unmissable banner whenever the active adapter
  reports `simulation_mode: true` — Mock mode is never visually
  indistinguishable from a real node.
- **Never fake unknown data**: every field from `agent/models.Value[T]`
  renders through `ValueDisplay`, which shows `unavailable`/`not exposed by
  this QRX Core`/`error` distinctly rather than a blank or a zero.
- **Admin actions**: the Settings → General tab verifies an admin bearer
  token before accepting it, keeps it in per-tab `sessionStorage`, and sends
  it only to protected administrative endpoints; every
  write action in Settings → Updates goes through it and surfaces a clear
  error (not a silent no-op) when it's missing or wrong.
