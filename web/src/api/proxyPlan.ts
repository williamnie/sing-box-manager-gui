import { api } from './index';
export interface ProxyPlan { primary: string; merge_groups: string[] }
interface Summary { final: string; groups: string[]; nodes: string[] }
export interface ProxyPlanPreview { revision: string; before: Summary; after: Summary; removed_nodes: string[]; before_error: string; auto_apply: boolean }
export const proxyPlanApi = {
  preview: async (plan: ProxyPlan | null): Promise<ProxyPlanPreview> => (await api.post('/proxy-plan/preview', { plan })).data.data,
  save: async (plan: ProxyPlan | null, revision: string): Promise<{ application: 'applied' | 'saved' | 'failed'; checked: boolean }> => (await api.put('/proxy-plan', { plan, revision }, { timeout: 120000 })).data.data,
};
