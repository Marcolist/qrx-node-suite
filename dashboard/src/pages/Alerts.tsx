import { useState } from 'react';
import { api } from '../api/client';
import { usePolling } from '../hooks/usePolling';
import { StatusPill } from '../components/StatusPill';
import type { AlertSeverity } from '../api/types';

function severityTone(s: AlertSeverity) {
  if (s === 'critical') return 'critical' as const;
  if (s === 'warning') return 'warn' as const;
  return 'neutral' as const;
}

export function AlertsPage() {
  const [unresolvedOnly, setUnresolvedOnly] = useState(true);
  const { data: alerts } = usePolling(() => api.alerts(unresolvedOnly), 5000, [unresolvedOnly]);

  return (
    <div>
      <div className="row-between">
        <div className="section-title">Alerts</div>
        <label className="row muted" style={{ fontSize: 12 }}>
          <input type="checkbox" checked={unresolvedOnly} onChange={(e) => setUnresolvedOnly(e.target.checked)} />
          Unresolved only
        </label>
      </div>
      <table>
        <thead>
          <tr>
            <th>Severity</th>
            <th>Title</th>
            <th>Message</th>
            <th>Created</th>
            <th>Status</th>
          </tr>
        </thead>
        <tbody>
          {(alerts ?? []).map((a) => (
            <tr key={a.id}>
              <td>
                <StatusPill label={a.severity} tone={severityTone(a.severity)} />
              </td>
              <td>{a.title}</td>
              <td className="muted">{a.message}</td>
              <td className="muted">{new Date(a.created_at).toLocaleString()}</td>
              <td>{a.resolved ? <span className="muted">Resolved</span> : <StatusPill label="Open" tone="warn" />}</td>
            </tr>
          ))}
        </tbody>
      </table>
      {(alerts ?? []).length === 0 ? <p className="muted">No alerts.</p> : null}
    </div>
  );
}
