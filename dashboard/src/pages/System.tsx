import { api } from '../api/client';
import { usePolling } from '../hooks/usePolling';
import { Card, Stat } from '../components/Card';
import { Row } from '../components/Row';
import { ValueDisplay, formatBytes, formatDuration, formatPercent } from '../components/ValueDisplay';

export function SystemPage() {
  const { data } = usePolling(api.system, 5000);

  return (
    <div>
      <div className="section-title">System</div>
      <div className="grid">
        <Card title="CPU">
          <Stat
            value={data?.cpu_usage_percent.state === 'available' ? formatPercent(data.cpu_usage_percent.value!) : '—'}
            label={`${data?.cpu_core_count.value ?? '?'} cores`}
          />
        </Card>
        <Card title="Memory">
          <Stat
            value={data?.memory_used_bytes.state === 'available' ? formatBytes(data.memory_used_bytes.value!) : '—'}
            label={data?.memory_total_bytes.state === 'available' ? `of ${formatBytes(data.memory_total_bytes.value!)}` : ''}
          />
        </Card>
        <Card title="Disk">
          <Stat
            value={data?.disk_used_bytes.state === 'available' ? formatBytes(data.disk_used_bytes.value!) : '—'}
            label={data?.disk_free_bytes.state === 'available' ? `${formatBytes(data.disk_free_bytes.value!)} free` : ''}
          />
        </Card>
        <Card title="Uptime">
          <Stat
            value={
              data?.system_uptime_seconds.state === 'available' ? formatDuration(data.system_uptime_seconds.value!) : '—'
            }
            label="system"
          />
        </Card>
      </div>

      <div className="grid">
        <Card title="Load Average">
          <div className="stack">
            <Row label="1 min"><ValueDisplay v={data?.load_average_1} /></Row>
            <Row label="5 min"><ValueDisplay v={data?.load_average_5} /></Row>
            <Row label="15 min"><ValueDisplay v={data?.load_average_15} /></Row>
          </div>
        </Card>

        <Card title="Network">
          <div className="stack">
            <Row label="Received"><ValueDisplay v={data?.network_rx_bytes} format={formatBytes} /></Row>
            <Row label="Transmitted"><ValueDisplay v={data?.network_tx_bytes} format={formatBytes} /></Row>
          </div>
        </Card>

        <Card title="QRX Process">
          <div className="stack">
            <Row label="CPU"><ValueDisplay v={data?.qrx_process_cpu_percent} format={formatPercent} /></Row>
            <Row label="Memory"><ValueDisplay v={data?.qrx_process_memory_bytes} format={formatBytes} /></Row>
          </div>
        </Card>

        <Card title="Platform">
          <div className="stack">
            <Row label="OS / Arch">
              {data ? `${data.platform.os} / ${data.platform.arch}` : '—'}
            </Row>
            <Row label="Model">{data?.platform.model ?? (data?.platform.is_raspberry_pi ? 'Raspberry Pi' : '—')}</Row>
            <Row label="Temperature"><ValueDisplay v={data?.temperature_celsius} unit="°C" /></Row>
            <Row label="Throttled"><ValueDisplay v={data?.throttled} format={(t) => (t ? 'yes' : 'no')} /></Row>
          </div>
        </Card>
      </div>
    </div>
  );
}
