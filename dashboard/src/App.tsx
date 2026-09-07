import { HashRouter, Route, Routes } from 'react-router-dom';
import { Layout } from './components/Layout';
import { Overview } from './pages/Overview';
import { NodePage } from './pages/Node';
import { ValidatorPage } from './pages/Validator';
import { VelocityPage } from './pages/Velocity';
import { ActivityPage } from './pages/Activity';
import { SystemPage } from './pages/System';
import { LogsPage } from './pages/Logs';
import { AlertsPage } from './pages/Alerts';
import { SettingsPage } from './pages/Settings';

// HashRouter avoids needing any server-side rewrite rule for client-side
// routing: the Agent's dashboard handler (cmd/agentd/dashboard.go) already
// falls back unknown paths to index.html, but HashRouter keeps this
// dashboard trivially deployable from any static file server too.
export function App() {
  return (
    <HashRouter>
      <Layout>
        <Routes>
          <Route path="/" element={<Overview />} />
          <Route path="/node" element={<NodePage />} />
          <Route path="/validator" element={<ValidatorPage />} />
          <Route path="/velocity" element={<VelocityPage />} />
          <Route path="/activity" element={<ActivityPage />} />
          <Route path="/system" element={<SystemPage />} />
          <Route path="/logs" element={<LogsPage />} />
          <Route path="/alerts" element={<AlertsPage />} />
          <Route path="/settings" element={<SettingsPage />} />
        </Routes>
      </Layout>
    </HashRouter>
  );
}
