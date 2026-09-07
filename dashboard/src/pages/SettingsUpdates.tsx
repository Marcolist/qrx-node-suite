import { useState } from 'react';
import { api, ApiError } from '../api/client';
import { usePolling } from '../hooks/usePolling';
import { Card } from '../components/Card';
import { StatusPill } from '../components/StatusPill';
import type { UpdateCheckResult } from '../api/types';

/**
 * Settings -> Updates: docs/updates.md's dashboard UX in full --
 * Current/Available/Channel/Pinned/Compatibility/Rollback per component,
 * plus Update/Rollback/Activate actions. Every write action is an admin
 * endpoint; without an admin token configured (Settings -> General) these
 * buttons will get a clear 403 from the Agent, which we surface as an
 * error banner rather than pretending the action worked.
 */
export function SettingsUpdatesTab() {
  const { data: results, error, refetch } = usePollingWithRefetch();
  const [busy, setBusy] = useState<string | null>(null);
  const [message, setMessage] = useState<{ kind: 'ok' | 'error'; text: string } | null>(null);

  async function runAction(component: string, action: () => Promise<unknown>) {
    setBusy(component);
    setMessage(null);
    try {
      await action();
      setMessage({ kind: 'ok', text: `${component}: action completed.` });
      await refetch();
    } catch (err) {
      const text = err instanceof ApiError ? `${component}: ${err.message}` : `${component}: ${String(err)}`;
      setMessage({ kind: 'error', text });
    } finally {
      setBusy(null);
    }
  }

  return (
    <div>
      <div className="section-title">Updates</div>
      {error ? <p className="value-error">Failed to load update status: {error}</p> : null}
      {message ? (
        <p className={message.kind === 'error' ? 'value-error' : ''}>{message.text}</p>
      ) : null}

      <div className="grid" style={{ gridTemplateColumns: '1fr' }}>
        {(results ?? []).map((r) => (
          <ComponentUpdateCard
            key={r.component}
            result={r}
            busy={busy === r.component}
            onCheck={() => runAction(r.component, () => api.updatesCheck(r.component))}
            onInstall={() => runAction(r.component, () => api.updatesInstall(r.component))}
            onRollback={() => runAction(r.component, () => api.updatesRollback(r.component))}
          />
        ))}
      </div>
    </div>
  );
}

function usePollingWithRefetch() {
  const [nonce, setNonce] = useState(0);
  const state = usePolling(() => api.updatesPlan([]), 20000, [nonce]);
  return { ...state, refetch: async () => setNonce((n) => n + 1) };
}

function ComponentUpdateCard({
  result,
  busy,
  onCheck,
  onInstall,
  onRollback,
}: {
  result: UpdateCheckResult;
  busy: boolean;
  onCheck: () => void;
  onInstall: () => void;
  onRollback: () => void;
}) {
  return (
    <Card
      title={result.component.toUpperCase()}
      action={
        <div className="row">
          <button onClick={onCheck} disabled={busy}>
            Check
          </button>
          {result.update_available && !result.blocked ? (
            <button className="primary" onClick={onInstall} disabled={busy}>
              Update
            </button>
          ) : null}
          {result.rollback_available ? (
            <button onClick={onRollback} disabled={busy}>
              Rollback
            </button>
          ) : null}
        </div>
      }
    >
      <div className="grid" style={{ gridTemplateColumns: 'repeat(4, minmax(120px, 1fr))', gap: '0.5rem' }}>
        <Field label="Current" value={result.current || '—'} />
        <Field label="Available" value={result.update_available ? result.latest || '—' : 'Latest'} />
        <Field label="Channel" value={result.channel} />
        <Field label="Pinned" value={result.pinned ? 'Yes' : 'No'} />
      </div>
      <div className="row" style={{ marginTop: '0.6rem', flexWrap: 'wrap', gap: '0.4rem' }}>
        {result.compatible ? (
          <StatusPill label="Compatible" tone="ok" />
        ) : result.blocked ? (
          <StatusPill label="Blocked" tone="critical" />
        ) : (
          <StatusPill label="Unknown" tone="neutral" />
        )}
        {result.restart_required ? <StatusPill label="Restart required" tone="warn" /> : null}
        {result.rollback_available ? <StatusPill label="Rollback available" tone="ok" /> : null}
      </div>
      {result.blocked && result.block_reason ? (
        <p className="value-error" style={{ margin: '0.5rem 0 0', fontSize: 12 }}>
          {result.block_reason}
        </p>
      ) : null}
      {result.check_error ? (
        <p className="muted" style={{ margin: '0.5rem 0 0', fontSize: 12 }}>
          Could not check for updates: {result.check_error}
        </p>
      ) : null}
    </Card>
  );
}

function Field({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <div style={{ fontSize: 15 }}>{value}</div>
      <div className="stat-label">{label}</div>
    </div>
  );
}
