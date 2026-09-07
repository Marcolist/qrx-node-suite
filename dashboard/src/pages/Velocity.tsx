import { api } from '../api/client';
import { usePolling } from '../hooks/usePolling';
import { Card } from '../components/Card';
import { Row } from '../components/Row';
import { ValueDisplay } from '../components/ValueDisplay';

export function VelocityPage() {
  const { data } = usePolling(api.velocity, 10000);
  const lanes = data?.nonce_lanes.lanes.state === 'available' ? data.nonce_lanes.lanes.value : [];

  return (
    <div>
      <div className="section-title">VELOCITY</div>
      <Card title="About VELOCITY">
        <p className="muted" style={{ margin: 0 }}>
          VELOCITY is QRX Core's deterministic parallel execution engine. QRX Node Suite only displays metrics
          actually reported by QRX Core -- nothing here is estimated.
        </p>
      </Card>

      <div className="grid">
        <Card title="Engine">
          <div className="stack">
            <Row label="Status"><ValueDisplay v={data?.velocity.engine_status} /></Row>
            <Row label="Transaction Version"><ValueDisplay v={data?.velocity.transaction_version} /></Row>
            <Row label="Scheduler Version"><ValueDisplay v={data?.velocity.scheduler_version} /></Row>
            <Row label="Parallel Width"><ValueDisplay v={data?.velocity.parallel_width} /></Row>
          </div>
        </Card>

        <Card title="Execution">
          <div className="stack">
            <Row label="Execution Waves"><ValueDisplay v={data?.velocity.execution_waves} /></Row>
            <Row label="Conflicts"><ValueDisplay v={data?.velocity.conflicts} /></Row>
            <Row label="Selective Retries"><ValueDisplay v={data?.velocity.selective_retries} /></Row>
          </div>
        </Card>
      </div>

      <div className="section-title">Nonce Lanes</div>
      {lanes && lanes.length > 0 ? (
        <table>
          <thead>
            <tr>
              <th>Lane</th>
              <th>Next Nonce</th>
              <th>In Flight</th>
            </tr>
          </thead>
          <tbody>
            {lanes.map((lane) => (
              <tr key={lane.index}>
                <td>{lane.index}</td>
                <td className="mono">{lane.next_nonce}</td>
                <td>{lane.in_flight}</td>
              </tr>
            ))}
          </tbody>
        </table>
      ) : (
        <p className="muted">
          <ValueDisplay v={data?.nonce_lanes.lanes} />
        </p>
      )}
    </div>
  );
}
