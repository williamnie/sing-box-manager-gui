import { useState, useEffect, useRef } from 'react';
import { useStore } from '../store';

interface TrafficData {
  up: number;
  down: number;
  connected: boolean;
}

interface MemoryData {
  inuse: number;
  oslimit: number;
  connected: boolean;
}

const MAX_RECONNECT_ATTEMPTS = 10;

// Traffic Hook
export function useClashTraffic() {
  const settings = useStore(state => state.settings);
  const [data, setData] = useState<TrafficData>({ up: 0, down: 0, connected: false });
  const wsRef = useRef<WebSocket | null>(null);
  const reconnectTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const reconnectAttempts = useRef(0);
  const mountedRef = useRef(true);

  useEffect(() => {
    mountedRef.current = true;
    
    const connect = () => {
      if (!settings || !mountedRef.current) return;
      if (reconnectAttempts.current >= MAX_RECONNECT_ATTEMPTS) return;

      // 清理旧连接
      if (wsRef.current) {
        wsRef.current.onclose = null;
        wsRef.current.close();
      }

      const port = settings.clash_api_port || 9091;
      const secret = settings.clash_api_secret || '';
      const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
      const host = window.location.hostname;
      let url = `${protocol}//${host}:${port}/traffic`;
      if (secret) url += `?token=${secret}`;

      try {
        const ws = new WebSocket(url);
        wsRef.current = ws;

        ws.onopen = () => {
          if (!mountedRef.current) return;
          reconnectAttempts.current = 0;
          setData(prev => ({ ...prev, connected: true }));
        };

        ws.onmessage = (event) => {
          if (!mountedRef.current) return;
          try {
            const parsed = JSON.parse(event.data);
            setData({ up: parsed.up || 0, down: parsed.down || 0, connected: true });
          } catch {}
        };

        ws.onclose = () => {
          if (!mountedRef.current) return;
          setData({ up: 0, down: 0, connected: false });
          reconnectAttempts.current++;
          if (reconnectAttempts.current < MAX_RECONNECT_ATTEMPTS) {
            const delay = Math.min(1000 * Math.pow(2, reconnectAttempts.current), 30000);
            reconnectTimeoutRef.current = setTimeout(connect, delay);
          }
        };

        ws.onerror = () => {
          if (ws.readyState === WebSocket.OPEN) ws.close();
        };
      } catch {}
    };

    connect();

    return () => {
      mountedRef.current = false;
      if (reconnectTimeoutRef.current) clearTimeout(reconnectTimeoutRef.current);
      if (wsRef.current) {
        wsRef.current.onclose = null;
        wsRef.current.close();
        wsRef.current = null;
      }
    };
  }, [settings?.clash_api_port, settings?.clash_api_secret]);

  return data;
}

// Memory Hook
export function useClashMemory() {
  const settings = useStore(state => state.settings);
  const [data, setData] = useState<MemoryData>({ inuse: 0, oslimit: 0, connected: false });
  const wsRef = useRef<WebSocket | null>(null);
  const reconnectTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const reconnectAttempts = useRef(0);
  const mountedRef = useRef(true);

  useEffect(() => {
    mountedRef.current = true;
    
    const connect = () => {
      if (!settings || !mountedRef.current) return;
      if (reconnectAttempts.current >= MAX_RECONNECT_ATTEMPTS) return;

      if (wsRef.current) {
        wsRef.current.onclose = null;
        wsRef.current.close();
      }

      const port = settings.clash_api_port || 9091;
      const secret = settings.clash_api_secret || '';
      const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
      const host = window.location.hostname;
      let url = `${protocol}//${host}:${port}/memory`;
      if (secret) url += `?token=${secret}`;

      try {
        const ws = new WebSocket(url);
        wsRef.current = ws;

        ws.onopen = () => {
          if (!mountedRef.current) return;
          reconnectAttempts.current = 0;
          setData(prev => ({ ...prev, connected: true }));
        };

        ws.onmessage = (event) => {
          if (!mountedRef.current) return;
          try {
            const parsed = JSON.parse(event.data);
            setData({ inuse: parsed.inuse || 0, oslimit: parsed.oslimit || 0, connected: true });
          } catch {}
        };

        ws.onclose = () => {
          if (!mountedRef.current) return;
          setData({ inuse: 0, oslimit: 0, connected: false });
          reconnectAttempts.current++;
          if (reconnectAttempts.current < MAX_RECONNECT_ATTEMPTS) {
            const delay = Math.min(1000 * Math.pow(2, reconnectAttempts.current), 30000);
            reconnectTimeoutRef.current = setTimeout(connect, delay);
          }
        };

        ws.onerror = () => {
          if (ws.readyState === WebSocket.OPEN) ws.close();
        };
      } catch {}
    };

    connect();

    return () => {
      mountedRef.current = false;
      if (reconnectTimeoutRef.current) clearTimeout(reconnectTimeoutRef.current);
      if (wsRef.current) {
        wsRef.current.onclose = null;
        wsRef.current.close();
        wsRef.current = null;
      }
    };
  }, [settings?.clash_api_port, settings?.clash_api_secret]);

  return data;
}

// 格式化速度
export function formatSpeed(bytesPerSecond: number): string {
  if (bytesPerSecond < 1024) {
    return `${bytesPerSecond.toFixed(0)} B/s`;
  } else if (bytesPerSecond < 1024 * 1024) {
    return `${(bytesPerSecond / 1024).toFixed(1)} KB/s`;
  } else {
    return `${(bytesPerSecond / 1024 / 1024).toFixed(2)} MB/s`;
  }
}

// 格式化内存
export function formatMemory(bytes: number): string {
  if (bytes < 1024 * 1024) {
    return `${(bytes / 1024).toFixed(1)} KB`;
  } else if (bytes < 1024 * 1024 * 1024) {
    return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
  } else {
    return `${(bytes / 1024 / 1024 / 1024).toFixed(2)} GB`;
  }
}
