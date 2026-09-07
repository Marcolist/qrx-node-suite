import { useEffect, useRef, useState } from 'react';

export interface AgentEvent {
  type: string;
  timestamp: string;
  data?: unknown;
}

/** Subscribes to GET /api/v1/events (SSE) and keeps the last `limit` events. */
export function useEvents(limit = 50): AgentEvent[] {
  const [events, setEvents] = useState<AgentEvent[]>([]);
  const sourceRef = useRef<EventSource | null>(null);

  useEffect(() => {
    const source = new EventSource('/api/v1/events');
    sourceRef.current = source;
    source.onmessage = (e) => {
      try {
        const parsed = JSON.parse(e.data) as AgentEvent;
        setEvents((prev) => [parsed, ...prev].slice(0, limit));
      } catch {
        // ignore malformed events rather than crash the dashboard
      }
    };
    return () => source.close();
  }, [limit]);

  return events;
}
