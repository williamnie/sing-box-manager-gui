import { api } from './index';

export type AgentScope = 'read' | 'runtime' | 'write' | 'deploy';

export interface AgentConnection {
  server: string;
  fingerprint: string;
  pinned_pubkey: string;
  allow_insecure_http: boolean;
  local_only: boolean;
  suggested_cidrs: string[];
}

export interface AgentToken {
  id: string;
  name: string;
  scopes: AgentScope[];
  cidrs: string[];
  created_at: string;
  expires_at: string;
  revoked_at?: string;
  last_used_at?: string;
  status: 'active' | 'revoked' | 'expired';
}

export interface AgentChangeset {
  id: string;
  title: string;
  actor: string;
  state: 'checked' | 'applying' | 'pending_confirm' | 'rolling_back' | 'committed' | 'rolled_back' | 'failed' | 'recovery_required' | 'expired' | 'resolved';
  changes: string[];
  config_changes: string[];
  warnings: string[];
  candidate_config: string;
  pending?: { digest: string; changes: string[] } | null;
  include_pending?: string;
  confirm_seconds?: number;
  runtime_before?: boolean;
  created_at: string;
  applied_at?: string;
  deadline?: string;
  finished_at?: string;
  error?: string;
  resolution?: string;
}

export interface AuditEntry {
  at: string;
  actor: string;
  action: string;
  target?: string;
  result: string;
  detail?: string;
}

export const agentApi = {
  connection: () => api.get<{ data: AgentConnection }>('/agent/connection'),
  tokens: () => api.get<{ data: AgentToken[]; max_days: number }>('/agent/tokens'),
  createToken: (body: { name: string; scopes: AgentScope[]; days: number; cidrs: string[] }) =>
    api.post<{ data: AgentToken; token: string }>('/agent/tokens', body),
  revokeToken: (id: string) => api.delete(`/agent/tokens/${encodeURIComponent(id)}`),
  state: () => api.get<{ data: { active: AgentChangeset | null; remaining_ms: number; journal_error: string; guarantee: string } }>('/agent/state'),
  changesets: () => api.get<{ data: AgentChangeset[] }>('/agent/changesets', { params: { limit: 100 } }),
  confirm: (cs: AgentChangeset) => api.post(`/agent/changesets/${encodeURIComponent(cs.id)}/confirm`, { candidate_config: cs.candidate_config }),
  rollback: (id: string) => api.post(`/agent/changesets/${encodeURIComponent(id)}/rollback`, {}, { timeout: 120000 }),
  revert: (id: string) => api.post<{ data: AgentChangeset & { apply_ready?: boolean; blocked_reason?: string } }>(`/agent/changesets/${encodeURIComponent(id)}/revert`, {}),
  apply: (id: string, confirmSeconds = 120) => api.post(`/agent/changesets/${encodeURIComponent(id)}/apply`, { confirm_seconds: confirmSeconds }, { timeout: 120000 }),
  resolve: (id: string, note: string) => api.post(`/agent/changesets/${encodeURIComponent(id)}/resolve`, { note }),
  audit: () => api.get<{ data: AuditEntry[] }>('/agent/audit', { params: { limit: 300 } }),
};
