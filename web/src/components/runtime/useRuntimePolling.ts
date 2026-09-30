import { useCallback, useEffect, useRef, useState } from 'react';
import { errorMessage } from '../../api';

export default function useRuntimePolling<T>(load: (signal?: AbortSignal) => Promise<T>, interval: number, paused = false) {
  const [state, setState] = useState<{ data: T | null; previous: T | null; error: string; loading: boolean }>({ data: null, previous: null, error: '', loading: true });
  const controller = useRef<AbortController | null>(null);
  const epoch = useRef(0);
  const mounted = useRef(true);
  const cancel = useCallback(() => {
    epoch.current += 1;
    controller.current?.abort();
  }, []);
  const refresh = useCallback(async () => {
    cancel();
    const requestEpoch = epoch.current;
    const request = new AbortController();
    controller.current = request;
    setState(previous => ({ ...previous, loading: true }));
    try {
      const data = await load(request.signal);
      if (mounted.current && requestEpoch === epoch.current) setState(previous => ({ data, previous: previous.data, error: '', loading: false }));
    } catch (error) {
      if (mounted.current && !request.signal.aborted && requestEpoch === epoch.current) {
        setState(previous => ({ ...previous, error: errorMessage(error, '运行接口不可用，请检查内核状态'), loading: false }));
      }
    }
  }, [cancel, load]);
  const accept = useCallback((data: T) => {
    cancel();
    if (mounted.current) setState(previous => ({ data, previous: previous.data, error: '', loading: false }));
  }, [cancel]);

  useEffect(() => {
    mounted.current = true;
    let stopped = false;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const poll = async () => {
      await refresh();
      if (!stopped) timer = setTimeout(poll, interval);
    };
    if (!paused) void poll();
    return () => {
      stopped = true;
      mounted.current = false;
      if (timer) clearTimeout(timer);
      cancel();
    };
  }, [cancel, interval, paused, refresh]);
  return { ...state, refresh, accept, cancel };
}
