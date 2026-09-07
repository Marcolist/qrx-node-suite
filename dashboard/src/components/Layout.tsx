import type { ReactNode } from 'react';
import { NavLink } from 'react-router-dom';
import { usePolling } from '../hooks/usePolling';
import { api } from '../api/client';
import { SimulationBanner } from './SimulationBanner';
import { healthPill } from './StatusPill';

const NAV = [
  { to: '/', label: 'Overview', end: true },
  { to: '/node', label: 'Node' },
  { to: '/validator', label: 'Validator' },
  { to: '/velocity', label: 'VELOCITY' },
  { to: '/activity', label: 'Activity' },
  { to: '/system', label: 'System' },
  { to: '/logs', label: 'Logs' },
  { to: '/alerts', label: 'Alerts' },
  { to: '/settings', label: 'Settings' },
];

export function Layout({ children }: { children: ReactNode }) {
  const { data: status } = usePolling(api.status, 5000);
  const { data: version } = usePolling(api.version, 60000);

  return (
    <div className="app-shell">
      <aside className="sidebar">
        <div className="sidebar-brand">
          QRX Node Suite
          <small>Run. Monitor. Validate.</small>
        </div>
        <nav className="sidebar-nav">
          {NAV.map((item) => (
            <NavLink key={item.to} to={item.to} end={item.end} className={({ isActive }) => (isActive ? 'active' : '')}>
              {item.label}
            </NavLink>
          ))}
        </nav>
      </aside>
      <div className="main">
        {status?.node.simulation_mode ? <SimulationBanner /> : null}
        <header className="topbar">
          <div className="row">{healthPill(status?.health)}</div>
          <div className="row muted">
            {version ? `Agent ${version.agent_version} · API ${version.api_version}` : null}
          </div>
        </header>
        <div className="content">{children}</div>
        <footer className="footer-badges">
          <span>QRX Core {version?.qrx_core_version ?? '—'}</span>
          <span>
            Adapter {version?.adapter_name ?? '—'} {version?.adapter_version ?? ''}
          </span>
          <span>Agent {version?.agent_version ?? '—'}</span>
          <span>Dashboard {version?.dashboard_version ?? '—'}</span>
          <span>API {version?.api_version ?? '—'}</span>
        </footer>
      </div>
    </div>
  );
}
