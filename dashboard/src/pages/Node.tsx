import { api } from '../api/client';
import { usePolling } from '../hooks/usePolling';
import { Card } from '../components/Card';
import { Row } from '../components/Row';
import { ValueDisplay, formatDuration } from '../components/ValueDisplay';
import { boolPill } from '../components/StatusPill';

export function NodePage() {
  const { data: node } = usePolling(api.node, 5000);
  const { data: network } = usePolling(api.network, 10000);
  const { data: mempool } = usePolling(api.mempool, 10000);

  return (
    <div>
      <div className="section-title">Node</div>
      <div className="grid">
        <Card title="Identity">
          <div className="stack">
            <Row label="Status">{boolPill(node?.online, 'Online', 'Offline')}</Row>
            <Row label="Network"><ValueDisplay v={node?.network} /></Row>
            <Row label="QRX Version"><ValueDisplay v={node?.qrx_version} /></Row>
            <Row label="Adapter">{node?.adapter_name ?? '—'}</Row>
            <Row label="Node Uptime">
              <ValueDisplay v={node?.node_uptime_seconds} format={formatDuration} />
            </Row>
            <Row label="Last Successful Query">{node?.last_successful_poll ?? '—'}</Row>
          </div>
        </Card>

        <Card title="Chain">
          <div className="stack">
            <Row label="Height"><ValueDisplay v={node?.height} /></Row>
            <Row label="Finalized Height"><ValueDisplay v={node?.finalized_height} /></Row>
            <Row label="Sync">
              <ValueDisplay v={node?.sync} format={(s) => (s.syncing ? `syncing (${s.current_height})` : 'synced')} />
            </Row>
          </div>
        </Card>

        <Card title="Network">
          <div className="stack">
            <Row label="Peers (connected)">
              <ValueDisplay v={node?.peers} format={(p) => String(p.connected)} />
            </Row>
            <Row label="Peers (in/out)">
              <ValueDisplay v={node?.peers} format={(p) => `${p.inbound} / ${p.outbound}`} />
            </Row>
            <Row label="Protocol Version"><ValueDisplay v={network?.protocol_version} /></Row>
          </div>
        </Card>

        <Card title="Mempool">
          <div className="stack">
            <Row label="Transactions"><ValueDisplay v={mempool?.tx_count} /></Row>
            <Row label="Size"><ValueDisplay v={mempool?.size_bytes} unit="bytes" /></Row>
          </div>
        </Card>
      </div>

      {node?.last_error ? (
        <Card title="Last Error">
          <span className="value-error">{node.last_error}</span>
        </Card>
      ) : null}
    </div>
  );
}
