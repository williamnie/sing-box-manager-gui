import { api } from './index';

export interface DNSRecord { at: number; source: string; domain: string; type: string }
export interface DNSDomain { domain: string; count: number; last_seen: number }
export interface DNSDomainMatch { rule: string; kind: 'domain' | 'domain_suffix' }
export interface DNSDomainListStatus { name: string; url: string; ready: boolean; count: number; updated_at: number; version: string; error: string }
export interface DNSClassification { status: DNSDomainListStatus; matches: Record<string, DNSDomainMatch> }
export interface DNSStatus {
  enabled: boolean; applied: boolean; auto_apply: boolean; supported: boolean;
  retention_days: number; max_records: number; max_bytes: number;
  stats: { count: number; last_scan: number; last_query: number; gaps: number; error: string };
}
export interface DNSQueryResponse {
  data: { records: DNSRecord[]; domains: DNSDomain[]; sources: string[]; total: number; domain_total: number };
  status: DNSStatus;
  classification: DNSClassification;
}
export interface DNSFilters { source?: string; search?: string; type?: string; since?: number; until?: number; offset?: number; limit?: number; sort?: 'recent' | 'count' }

export const dnsQueriesApi = {
  list: async (params: DNSFilters, signal?: AbortSignal): Promise<DNSQueryResponse> =>
    (await api.get<DNSQueryResponse>('/dns/queries', { params, signal })).data,
  settings: async (enabled: boolean) =>
    (await api.put<{ status: DNSStatus; warning?: string }>('/dns/queries/settings', { enabled }, { timeout: 120000 })).data,
  export: (params: DNSFilters) => api.get<Blob>('/dns/queries/export', { params, responseType: 'blob' }),
  refreshDomainList: () => api.post('/dns/queries/domain-list/refresh'),
};
