import type { Value } from '../api/types';

/**
 * Renders a models.Value<T> honestly: available data as-is, and
 * unavailable/unsupported/error as a visually distinct, explicit state --
 * never silently as "0" or blank (docs/architecture.md principle 11/28:
 * "UI must never silently substitute mock or estimated values for real
 * data").
 */
export function ValueDisplay<T>({
  v,
  format,
  unit,
}: {
  v: Value<T> | undefined;
  format?: (value: T) => string;
  unit?: string;
}) {
  if (!v || v.state === 'unavailable') {
    return <span className="value-unavailable" title={v?.reason}>unavailable</span>;
  }
  if (v.state === 'unsupported') {
    return (
      <span className="value-unsupported" title={v.reason}>
        not exposed by this QRX Core
      </span>
    );
  }
  if (v.state === 'error') {
    return (
      <span className="value-error" title={v.reason}>
        error
      </span>
    );
  }
  if (v.value === undefined) {
    return <span className="value-unavailable">unavailable</span>;
  }
  const text = format ? format(v.value) : String(v.value);
  return (
    <span>
      {text}
      {unit ? <span className="muted"> {unit}</span> : null}
    </span>
  );
}

export function formatBytes(bytes: number): string {
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  let value = bytes;
  let i = 0;
  while (value >= 1024 && i < units.length - 1) {
    value /= 1024;
    i++;
  }
  return `${value.toFixed(value >= 10 || i === 0 ? 0 : 1)} ${units[i]}`;
}

export function formatDuration(seconds: number): string {
  if (seconds < 60) return `${Math.floor(seconds)}s`;
  const d = Math.floor(seconds / 86400);
  const h = Math.floor((seconds % 86400) / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  const parts: string[] = [];
  if (d > 0) parts.push(`${d}d`);
  if (h > 0 || d > 0) parts.push(`${h}h`);
  parts.push(`${m}m`);
  return parts.join(' ');
}

export function formatPercent(v: number): string {
  return `${v.toFixed(1)}%`;
}
