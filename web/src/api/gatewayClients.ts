import { api } from './index';
import type { DevicePolicy } from '../store';

export interface ObservedGatewayClient {
  address: string;
  name: string;
  configured: boolean;
  policy: DevicePolicy;
  last_seen_at: number;
  dns_seen: boolean;
  proxy_seen: boolean;
  active_connections: number;
}

export interface GatewayClientSnapshot {
  clients: ObservedGatewayClient[];
  runtime_available: boolean;
  logs_available: boolean;
}

export const gatewayClientsApi = {
  list: async (signal?: AbortSignal): Promise<GatewayClientSnapshot> =>
    (await api.get('/gateway/clients', { signal })).data.data,
};
