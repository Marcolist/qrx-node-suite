import { useState } from 'react';
import { api, getAdminToken, setAdminToken } from '../api/client';
import { Card } from '../components/Card';
import { SettingsUpdatesTab } from './SettingsUpdates';

const TABS = ['General', 'Updates'] as const;
type Tab = (typeof TABS)[number];

export function SettingsPage() {
  const [tab, setTab] = useState<Tab>('General');

  return (
    <div>
      <div className="row" style={{ marginBottom: '1rem' }}>
        {TABS.map((t) => (
          <button key={t} className={tab === t ? 'primary' : ''} onClick={() => setTab(t)}>
            {t}
          </button>
        ))}
      </div>
      {tab === 'General' ? <GeneralTab /> : <SettingsUpdatesTab />}
    </div>
  );
}

function GeneralTab() {
  const [token, setToken] = useState('');
  const [saved, setSaved] = useState(() => getAdminToken() !== '');
  const [checking, setChecking] = useState(false);
  const [error, setError] = useState('');

  return (
    <div>
      <div className="section-title">General</div>
      <Card title="Admin session">
        <p className="muted" style={{ marginTop: 0 }}>
          Administrative actions (installing/rolling back updates, switching adapters, restarting services) require
          the Agent's admin token. It is checked before login, kept only for this browser tab's session, and sent
          only with administrative requests. Use admin access only over a trusted LAN, VPN, SSH tunnel, or HTTPS.
        </p>
        <div className="row">
          <input
            type="password"
            placeholder="admin token"
            value={token}
            onChange={(e) => {
              setToken(e.target.value);
              setSaved(false);
              setError('');
            }}
            style={{ minWidth: 280 }}
          />
          <button
            className="primary"
            disabled={checking}
            onClick={async () => {
              setSaved(false);
              setError('');
              if (!token) {
                setAdminToken('');
                setError('Enter the admin token.');
                return;
              }
              setChecking(true);
              try {
                await api.verifyAdmin(token);
                setAdminToken(token);
                setToken('');
                setSaved(true);
              } catch (err) {
                setAdminToken('');
                setError(err instanceof Error ? err.message : 'Login failed.');
              } finally {
                setChecking(false);
              }
            }}
          >
            {checking ? 'Checking…' : 'Log in'}
          </button>
          <button
            onClick={() => {
              setAdminToken('');
              setToken('');
              setSaved(false);
              setError('');
            }}
          >
            Log out
          </button>
          {saved ? <span className="muted">Authenticated for this tab.</span> : null}
          {error ? <span>{error}</span> : null}
        </div>
      </Card>
    </div>
  );
}
