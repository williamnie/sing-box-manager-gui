import { api } from './index';
import type { Rule } from '../store';

export const domainBlocklistID = 'custom-domain-reject';
export interface DomainBlocklist {
  rule: Rule | null;
  revision: string;
  auto_apply: boolean;
  editable: boolean;
}
export interface BlocklistResult {
  data: DomainBlocklist;
  application: 'saved' | 'applied' | 'failed';
  added: boolean;
  warning?: string;
}
export const domainBlocklistApi = {
  get: async (signal?: AbortSignal): Promise<DomainBlocklist> => (await api.get('/rules/domain-blocklist', { signal })).data.data,
  add: async (domain: string): Promise<BlocklistResult> => (await api.post('/rules/domain-blocklist/domains', { domain }, { timeout: 120000 })).data,
  save: async (domains: string[], revision: string, enabled: boolean): Promise<BlocklistResult> =>
    (await api.put('/rules/domain-blocklist', { domains, revision, enabled }, { timeout: 120000 })).data,
};

export function blocklistSaveMessage(result: BlocklistResult): string {
  if (result.warning) return '';
  return result.application === 'applied' ? '拦截集合已保存并应用；实际处理仍取决于设备路径和规则优先级' : '拦截集合已保存，请到配置审阅检查并应用';
}
