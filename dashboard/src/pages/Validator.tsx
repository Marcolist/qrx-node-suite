import { api } from '../api/client';
import { usePolling } from '../hooks/usePolling';
import { Card } from '../components/Card';
import { Row } from '../components/Row';
import { ValueDisplay } from '../components/ValueDisplay';
import { boolPill } from '../components/StatusPill';

export function ValidatorPage() {
  const { data } = usePolling(api.validator, 10000);

  return (
    <div>
      <div className="section-title">Validator</div>
      <div className="grid">
        <Card title="Status">
          <div className="stack">
            <Row label="Active">{boolPill(data?.validator.active.value, 'Active', 'Inactive')}</Row>
            <Row label="Address"><ValueDisplay v={data?.validator.address} /></Row>
            <Row label="Stake"><ValueDisplay v={data?.validator.stake} /></Row>
            <Row label="Delegated Stake"><ValueDisplay v={data?.validator.delegated_stake} /></Row>
            <Row label="Voting Weight"><ValueDisplay v={data?.validator.voting_weight} /></Row>
          </div>
        </Card>

        <Card title="Block Production">
          <div className="stack">
            <Row label="Producer Status"><ValueDisplay v={data?.block_producer.producer_status} /></Row>
            <Row label="Blocks Produced"><ValueDisplay v={data?.block_producer.blocks_produced} /></Row>
            <Row label="Missed Blocks"><ValueDisplay v={data?.block_producer.missed_blocks} /></Row>
          </div>
        </Card>
      </div>

      <p className="muted">
        Fields QRX Core does not expose (or that could not be confirmed against a real 0.0.7 node while this project
        was bootstrapped -- see docs/qrx-0.0.7-interface.md) show as "not exposed by this QRX Core" rather than a
        guessed value. Rewards and penalties are only ever shown when QRX Core reports an authoritative figure; this
        dashboard never computes them itself.
      </p>
    </div>
  );
}
