import { useState, useEffect, useRef, useCallback } from 'react';
import { useStore } from '../store';

// Clash API 连接数据类型
export interface ConnectionMetadata {
  network: string;
  type: string;
  sourceIP: string;
  destinationIP: string;
  sourcePort: string;
  destinationPort: string;
  host: string;
  dnsMode: string;
  processPath?: string;
}

export interface Connection {
  id: string;
  metadata: ConnectionMetadata;
  upload: number;
  download: number;
  uploadSpeed?: number;
  downloadSpeed?: number;
  start: string;
  chains: string[];
  rule: string;
  rulePayload: string;
}

export interface ConnectionsMessage {
  downloadTotal: number;
  uploadTotal: number;
  connections: Connection[];
}

interface UseClashConnectionsReturn {
  connections: Connection[];
  closedConnections: Connection[];
  downloadTotal: number;
  uploadTotal: number;
  isConnected: boolean;
  error: string | null;
  reconnect: () => void;
  clearClosedConnections: () => void;
}

const BASE_RECONNECT_DELAY_MS = 1000;
const MAX_RECONNECT_DELAY_MS = 30000;
const MAX_BACKOFF_EXPONENT = 8;

export function useClashConnections(): UseClashConnectionsReturn {
  const settings = useStore(state => state.settings);
  const [connections, setConnections] = useState<Connection[]>([]);
  const [closedConnections, setClosedConnections] = useState<Connection[]>([]);
  const [downloadTotal, setDownloadTotal] = useState(0);
  const [uploadTotal, setUploadTotal] = useState(0);
  const [isConnected, setIsConnected] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const wsRef = useRef<WebSocket | null>(null);
  const reconnectTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const reconnectAttempts = useRef(0);
  const mountedRef = useRef(true);
  const connectRef = useRef<() => void>(() => {});
  const previousConnectionsRef = useRef<Connection[]>([]);

  const scheduleReconnect = useCallback(() => {
    if (!mountedRef.current) return;

    reconnectAttempts.current++;
    const exponent = Math.min(reconnectAttempts.current, MAX_BACKOFF_EXPONENT);
    const delay = Math.min(BASE_RECONNECT_DELAY_MS * Math.pow(2, exponent), MAX_RECONNECT_DELAY_MS);

    setError(`连接已断开，${Math.ceil(delay / 1000)} 秒后自动重连`);

    if (reconnectTimeoutRef.current) {
      clearTimeout(reconnectTimeoutRef.current);
    }
    reconnectTimeoutRef.current = setTimeout(() => {
      reconnectTimeoutRef.current = null;
      connectRef.current();
    }, delay);
  }, []);

  const connect = useCallback(() => {
    if (!settings || !mountedRef.current) return;

    const port = settings.clash_api_port || 9091;
    const secret = settings.clash_api_secret || '';
    
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const host = window.location.hostname;
    let wsUrl = `${protocol}//${host}:${port}/connections`;
    
    if (secret) {
      wsUrl += `?token=${encodeURIComponent(secret)}`;
    }

    if (reconnectTimeoutRef.current) {
      clearTimeout(reconnectTimeoutRef.current);
      reconnectTimeoutRef.current = null;
    }

    // 清理旧连接
    if (wsRef.current) {
      wsRef.current.onclose = null;
      wsRef.current.close();
    }

    try {
      const ws = new WebSocket(wsUrl);
      wsRef.current = ws;

      ws.onopen = () => {
        if (!mountedRef.current) return;
        setIsConnected(true);
        setError(null);
        reconnectAttempts.current = 0;
      };

      ws.onmessage = (event) => {
        if (!mountedRef.current) return;
        try {
          const data: ConnectionsMessage = JSON.parse(event.data);
          const currentConnections = data.connections || [];
          const currentIds = new Set(currentConnections.map(c => c.id));
          const closed = previousConnectionsRef.current.filter(c => !currentIds.has(c.id));

          if (closed.length > 0) {
            setClosedConnections(prev => [...closed, ...prev].slice(0, 100));
          }

          previousConnectionsRef.current = currentConnections;
          setConnections(currentConnections);
          setDownloadTotal(data.downloadTotal || 0);
          setUploadTotal(data.uploadTotal || 0);
        } catch {
          return;
        }
      };

      ws.onerror = () => {
        if (ws.readyState === WebSocket.OPEN) ws.close();
      };

      ws.onclose = () => {
        if (!mountedRef.current) return;
        setIsConnected(false);
        wsRef.current = null;

        scheduleReconnect();
      };
    } catch {
      if (!mountedRef.current) return;
      setIsConnected(false);
      wsRef.current = null;
      scheduleReconnect();
    }
  }, [scheduleReconnect, settings]);

  useEffect(() => {
    connectRef.current = connect;
  }, [connect]);

  const reconnect = useCallback(() => {
    reconnectAttempts.current = 0;
    if (reconnectTimeoutRef.current) {
      clearTimeout(reconnectTimeoutRef.current);
      reconnectTimeoutRef.current = null;
    }
    connectRef.current();
  }, []);

  const clearClosedConnections = useCallback(() => {
    setClosedConnections([]);
  }, []);

  useEffect(() => {
    mountedRef.current = true;

    connectRef.current();

    return () => {
      mountedRef.current = false;
      if (reconnectTimeoutRef.current) {
        clearTimeout(reconnectTimeoutRef.current);
      }
      if (wsRef.current) {
        wsRef.current.onclose = null;
        wsRef.current.close();
        wsRef.current = null;
      }
    };
  }, [connect]);

  return {
    connections,
    closedConnections,
    downloadTotal,
    uploadTotal,
    isConnected,
    error,
    reconnect,
    clearClosedConnections,
  };
}
