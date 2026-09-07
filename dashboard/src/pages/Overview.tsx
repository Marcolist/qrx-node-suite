import { api } from '../api/client';
import { usePolling } from '../hooks/usePolling';
import { Card, Stat } from '../components/Card';
import { ValueDisplay, formatBytes, formatDuration, formatPercent } from '../components/ValueDisplay';
import { boolPill, healthPill } from '../components/StatusPill';

export function Overview() {
  const { data: status } = usePolling(api.status, 5000);
  const { data: alerts } = usePolling(() => api.alerts(true), 10000);
  const { data: updates } = usePolling(() => api.updatesStatus(), 30000);
  const { data: services } = usePolling(api.services, 10000);

  const node = status?.node;
  const system = status?.system;

  return (
    <div>
      <div className="section-title">Overview</div>
      <div className="grid">
        <Card title="QRX Node">
          <div className="stack">
            <div className="row-between">
              <span className="muted">Status</span>
              {boolPill(node?.online, 'Online', 'Offline')}
            </div>
            <div className="row-between">
              <span className="muted">Height</span>
              <ValueDisplay v={node?.height} />
            </div>
            <div className="row-between">
              <span className="muted">Peers</span>
              <ValueDisplay v={node?.peers} format={(p) => String(p.connected)} />
            </div>
            <div className="row-between">
              <span className="muted">Mempool</span>
              <ValueDisplay v={node?.mempool_tx_count} unit="tx" />
            </div>
          </div>
        </Card>

        <Card title="QRX Network">
          <div className="stack">
            <div className="row-between">
              <span className="muted">Network</span>
              <ValueDisplay v={node?.network} />
            </div>
            <div className="row-between">
              <span className="muted">QRX Version</span>
              <ValueDisplay v={node?.qrx_version} />
            </div>
            <div className="row-between">
              <span className="muted">Adapter</span>
              <span>{node?.adapter_name ?? '—'}</span>
            </div>
          </div>
        </Card>

        <Card title="Validator">
          <div className="stack muted">See the Validator page for details.</div>
        </Card>

        <Card title="VELOCITY">
          <div className="stack muted">See the VELOCITY page for details.</div>
        </Card>
      </div>

      <div className="grid">
        <Card title="Server Stats">
          <div className="grid" style={{ gridTemplateColumns: 'repeat(2, 1fr)', gap: '0.75rem' }}>
            <Stat
              value={system?.cpu_usage_percent?.state === 'available' ? formatPercent(system.cpu_usage_percent.value!) : '—'}
              label="CPU"
            />
            <Stat
              value={
                system?.memory_used_bytes?.state === 'available' ? formatBytes(system.memory_used_bytes.value!) : '—'
              }
              label="Memory used"
            />
            <Stat
              value={system?.disk_used_bytes?.state === 'available' ? formatBytes(system.disk_used_bytes.value!) : '—'}
              label="Disk used"
            />
            <Stat
              value={
                system?.system_uptime_seconds?.state === 'available'
                  ? formatDuration(system.system_uptime_seconds.value!)
                  : '—'
              }
              label="System uptime"
            />
          </div>
        </Card>

        <Card title="Alerts">
          {alerts && alerts.length > 0 ? (
            <div className="stack">
              {alerts.slice(0, 4).map((a) => (
                <div key={a.id} className="row-between">
                  <span>{a.title}</span>
                  {a.severity === 'critical' ? (
                    boolPill(true, 'Critical', 'Critical')
                  ) : (
                    <span className="muted">{a.severity}</span>
                  )}
                </div>
              ))}
            </div>
          ) : (
            <div className="muted">No active alerts.</div>
          )}
        </Card>

        <Card title="Service Status">
          <div className="stack">
            {(services ?? []).map((s) => (
              <div key={s.name} className="row-between">
                <span>{s.name}</span>
                {s.state === 'running' ? boolPill(true, 'Running', 'Running') : healthPill('OFFLINE')}
              </div>
            ))}
          </div>
        </Card>

        <Card title="Update Status">
          <div className="stack">
            {Object.values(updates ?? {})
              .filter((u) => u.component !== 'qrx_core')
              .map((u) => (
                <div key={u.component} className="row-between">
                  <span>{u.component}</span>
                  {u.update_available ? boolPill(true, 'Update available', '') : <span className="muted">Latest</span>}
                </div>
              ))}
          </div>
        </Card>
      </div>
    </div>
  );
}
