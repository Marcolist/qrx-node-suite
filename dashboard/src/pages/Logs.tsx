import { useEvents } from '../hooks/useEvents';

/**
 * There is no persisted, paginated Agent log file exposed over the API
 * yet (agent/logging writes structured logs to stdout, captured by the
 * process supervisor -- see docs/deployment.md). This page shows the live
 * event stream instead, which is real, current data rather than a fake
 * placeholder log.
 */
export function LogsPage() {
  const events = useEvents(200);

  return (
    <div>
      <div className="section-title">Logs</div>
      <p className="muted">
        Live event stream (GET /api/v1/events). Persisted, searchable Agent logs are not yet exposed over the API --
        see the Agent's own stdout/systemd journal for the full structured log.
      </p>
      <table>
        <thead>
          <tr>
            <th>Time</th>
            <th>Event</th>
            <th>Data</th>
          </tr>
        </thead>
        <tbody>
          {events.map((e, i) => (
            <tr key={i}>
              <td className="muted">{new Date(e.timestamp).toLocaleTimeString()}</td>
              <td className="mono">{e.type}</td>
              <td className="mono">{e.data !== undefined ? JSON.stringify(e.data) : ''}</td>
            </tr>
          ))}
        </tbody>
      </table>
      {events.length === 0 ? <p className="muted">No events yet -- waiting for the next state change.</p> : null}
    </div>
  );
}
