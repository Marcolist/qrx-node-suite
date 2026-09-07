type Tone = 'ok' | 'warn' | 'critical' | 'neutral';

export function StatusPill({ label, tone }: { label: string; tone: Tone }) {
  return (
    <span className={`pill pill-${tone}`}>
      <span className="pill-dot" />
      {label}
    </span>
  );
}

/** Maps a Guardian health state to a pill tone + label. */
export function healthPill(health: string | undefined) {
  switch (health) {
    case 'HEALTHY':
      return <StatusPill label="Healthy" tone="ok" />;
    case 'DEGRADED':
      return <StatusPill label="Degraded" tone="warn" />;
    case 'UNHEALTHY':
      return <StatusPill label="Unhealthy" tone="critical" />;
    case 'OFFLINE':
      return <StatusPill label="Offline" tone="critical" />;
    default:
      return <StatusPill label="Unknown" tone="neutral" />;
  }
}

export function boolPill(value: boolean | undefined, onLabel: string, offLabel: string) {
  if (value === undefined) return <StatusPill label="Unknown" tone="neutral" />;
  return value ? <StatusPill label={onLabel} tone="ok" /> : <StatusPill label={offLabel} tone="neutral" />;
}
