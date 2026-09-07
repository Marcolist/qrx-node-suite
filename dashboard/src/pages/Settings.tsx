import { useState } from 'react';
import { getAdminToken, setAdminToken } from '../api/client';
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
  const [token, setToken] = useState(getAdminToken());
  const [saved, setSaved] = useState(false);

  return (
    <div>
      <div className="section-title">General</div>
      <Card title="Admin session">
        <p className="muted" style={{ marginTop: 0 }}>
          Administrative actions (installing/rolling back updates, switching adapters, restarting services) require
          the Agent's admin token. It is stored only in this browser's local storage and sent as a bearer token --
          never persisted on the Agent itself beyond its own config. Leave blank to disable admin actions from this
          browser.
        </p>
        <div className="row">
          <input
            type="password"
            placeholder="admin token"
            value={token}
            onChange={(e) => {
              setToken(e.target.value);
              setSaved(false);
            }}
            style={{ minWidth: 280 }}
          />
          <button
            className="primary"
            onClick={() => {
              setAdminToken(token);
              setSaved(true);
            }}
          >
            Save
          </button>
          {saved ? <span className="muted">Saved.</span> : null}
        </div>
      </Card>
    </div>
  );
}
