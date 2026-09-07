import type { ReactNode } from 'react';

export function Row({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="row-between">
      <span className="muted">{label}</span>
      {children}
    </div>
  );
}
