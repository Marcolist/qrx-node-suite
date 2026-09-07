import { useEffect, useRef, useState } from 'react';

interface PollState<T> {
  data: T | undefined;
  error: string | undefined;
  loading: boolean;
}

/**
 * Polls fetcher every intervalMs, keeping the last good value visible while
 * a refresh is in flight or fails (never flashes back to a loading/blank
 * state on every tick -- only the very first load shows loading:true).
 */
export function usePolling<T>(fetcher: () => Promise<T>, intervalMs = 5000, deps: unknown[] = []): PollState<T> {
  const [state, setState] = useState<PollState<T>>({ data: undefined, error: undefined, loading: true });
  const fetcherRef = useRef(fetcher);
  fetcherRef.current = fetcher;

  useEffect(() => {
    let cancelled = false;

    async function tick() {
      try {
        const data = await fetcherRef.current();
        if (!cancelled) setState({ data, error: undefined, loading: false });
      } catch (err) {
        if (!cancelled) {
          setState((prev) => ({ data: prev.data, error: (err as Error).message, loading: false }));
        }
      }
    }

    tick();
    const id = setInterval(tick, intervalMs);
    return () => {
      cancelled = true;
      clearInterval(id);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [intervalMs, ...deps]);

  return state;
}
