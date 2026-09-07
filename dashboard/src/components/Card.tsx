import type { ReactNode } from 'react';

export function Card({ title, children, action }: { title: string; children: ReactNode; action?: ReactNode }) {
  return (
    <div className="card">
      <div className="row-between">
        <h3>{title}</h3>
        {action}
      </div>
      {children}
    </div>
  );
}

export function Stat({ value, label }: { value: ReactNode; label: string }) {
  return (
    <div>
      <div className="stat">{value}</div>
      <div className="stat-label">{label}</div>
    </div>
  );
}
