import { api, errorMessage } from './index';
import { t } from '../i18n';

export interface RuntimeProxy {
  tag: string; type: string; selectable: boolean; members: string[]; selected: string; delay: number | null;
}
export interface ProxySnapshot { route_final?: string; instance: string; version: string; proxies: RuntimeProxy[] }
export interface DelayResult { tag: string; delay: number | null; error?: string }
export interface RuntimeConnection {
  id: string; source: string; source_port: string; destination: string; destination_port: string;
  host: string; network: string; protocol: string; process: string; chains: string[]; rule: string;
  started_at: string; upload: number; download: number;
}
export interface ConnectionSnapshot {
  instance: string; sampled_at: number; upload_total: number; download_total: number; connections: RuntimeConnection[];
}
export interface CloseResult { closed: string[]; missing: string[]; failed: string[] }
export class RuntimeCloseError extends Error {
  partial: CloseResult;
  unattempted: string[];
  constructor(partial: CloseResult, unattempted: string[], cause: unknown) {
    super(t('批量关闭中断：已确认关闭 {p1} 条，已结束 {p2} 条，失败 {p3} 条；其余 {p4} 条尚未确认。{p5}', { p1: partial.closed.length, p2: partial.missing.length, p3: partial.failed.length, p4: unattempted.length, p5: errorMessage(cause, t('请求失败，请刷新后核对')) }));
    this.partial = partial;
    this.unattempted = unattempted;
  }
}

export const runtimeApi = {
  proxies: async (signal?: AbortSignal): Promise<ProxySnapshot> => (await api.get('/runtime/proxies', { signal })).data.data,
  select: async (group: string, member: string, instance: string): Promise<ProxySnapshot> =>
    (await api.post('/runtime/select', { group, member, instance })).data.data,
  test: async (tags: string[], url: string, timeout: number, instance: string, signal?: AbortSignal): Promise<{ results: DelayResult[] }> =>
    (await api.post('/runtime/delay', { tags, url, timeout, instance }, { signal, timeout: 120000 })).data.data,
  connections: async (signal?: AbortSignal): Promise<ConnectionSnapshot> => (await api.get('/runtime/connections', { signal })).data.data,
  close: async (ids: string[], instance: string): Promise<CloseResult> => {
    const result: CloseResult = { closed: [], missing: [], failed: [] };
    for (let offset = 0; offset < ids.length; offset += 2000) {
      try {
        const batch: CloseResult = (await api.post('/runtime/connections/close', { ids: ids.slice(offset, offset + 2000), instance })).data.data;
        result.closed.push(...batch.closed); result.missing.push(...batch.missing); result.failed.push(...batch.failed);
      } catch (error) { throw new RuntimeCloseError(result, ids.slice(offset), error); }
    }
    return result;
  },
};
