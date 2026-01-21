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
  downloadTotal: number;
  uploadTotal: number;
  isConnected: boolean;
  error: string | null;
  reconnect: () => void;
}

const MAX_RECONNECT_ATTEMPTS = 10;

export function useClashConnections(): UseClashConnectionsReturn {
  const settings = useStore(state => state.settings);
  const [connections, setConnections] = useState<Connection[]>([]);
  const [downloadTotal, setDownloadTotal] = useState(0);
  const [uploadTotal, setUploadTotal] = useState(0);
  const [isConnected, setIsConnected] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const wsRef = useRef<WebSocket | null>(null);
  const reconnectTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const reconnectAttempts = useRef(0);
  const mountedRef = useRef(true);

  const connect = useCallback(() => {
    if (!settings || !mountedRef.current) return;
    if (reconnectAttempts.current >= MAX_RECONNECT_ATTEMPTS) return;

    const port = settings.clash_api_port || 9091;
    const secret = settings.clash_api_secret || '';
    
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const host = window.location.hostname;
    let wsUrl = `${protocol}//${host}:${port}/connections`;
    
    if (secret) {
      wsUrl += `?token=${encodeURIComponent(secret)}`;
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
          setConnections(data.connections || []);
          setDownloadTotal(data.downloadTotal || 0);
          setUploadTotal(data.uploadTotal || 0);
        } catch {}
      };

      ws.onerror = () => {
        if (ws.readyState === WebSocket.OPEN) ws.close();
      };

      ws.onclose = () => {
        if (!mountedRef.current) return;
        setIsConnected(false);
        wsRef.current = null;
        
        reconnectAttempts.current++;
        if (reconnectAttempts.current < MAX_RECONNECT_ATTEMPTS) {
          const delay = Math.min(1000 * Math.pow(2, reconnectAttempts.current), 30000);
          reconnectTimeoutRef.current = setTimeout(connect, delay);
        } else {
          setError('连接失败，已达最大重试次数');
        }
      };
    } catch {
      if (mountedRef.current) {
        setError('无法建立 WebSocket 连接');
      }
    }
  }, [settings]);

  const reconnect = useCallback(() => {
    reconnectAttempts.current = 0;
    if (reconnectTimeoutRef.current) {
      clearTimeout(reconnectTimeoutRef.current);
    }
    connect();
  }, [connect]);

  useEffect(() => {
    mountedRef.current = true;
    
    // 当 settings 加载完成后自动连接
    if (settings?.clash_api_port) {
      connect();
    }

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
  }, [settings?.clash_api_port, settings?.clash_api_secret, connect]);

  return {
    connections,
    downloadTotal,
    uploadTotal,
    isConnected,
    error,
    reconnect,
  };
}
