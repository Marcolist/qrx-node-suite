import { api } from '../api/client';
import { usePolling } from '../hooks/usePolling';

export function ActivityPage() {
  const { data } = usePolling(api.activity, 15000);

  return (
    <div>
      <div className="section-title">Recent Blocks</div>
      <table>
        <thead>
          <tr>
            <th>Height</th>
            <th>Hash</th>
            <th>Producer</th>
            <th>Time</th>
          </tr>
        </thead>
        <tbody>
          {(data?.recent_blocks ?? []).map((b) => (
            <tr key={b.height}>
              <td>{b.height}</td>
              <td className="mono">{b.hash.slice(0, 16)}…</td>
              <td>{b.producer ?? '—'}</td>
              <td className="muted">{new Date(b.timestamp * 1000).toLocaleTimeString()}</td>
            </tr>
          ))}
        </tbody>
      </table>

      <div className="section-title">Recent Transactions</div>
      <table>
        <thead>
          <tr>
            <th>Hash</th>
            <th>From</th>
            <th>To</th>
            <th>Value</th>
            <th>Time</th>
          </tr>
        </thead>
        <tbody>
          {(data?.recent_transactions ?? []).map((tx) => (
            <tr key={tx.hash}>
              <td className="mono">{tx.hash.slice(0, 16)}…</td>
              <td className="mono">{tx.from ?? '—'}</td>
              <td className="mono">{tx.to ?? '—'}</td>
              <td>{tx.value ?? '—'}</td>
              <td className="muted">{new Date(tx.timestamp * 1000).toLocaleTimeString()}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
