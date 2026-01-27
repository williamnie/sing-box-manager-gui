import axios from 'axios';

const api = axios.create({
  baseURL: '/api',
  timeout: 30000,
});

// 订阅 API
export const subscriptionApi = {
  getAll: () => api.get('/subscriptions'),
  add: (name: string, url: string) => api.post('/subscriptions', { name, url }),
  update: (id: string, data: any) => api.put(`/subscriptions/${id}`, data),
  delete: (id: string) => api.delete(`/subscriptions/${id}`),
  refresh: (id: string) => api.post(`/subscriptions/${id}/refresh`),
  refreshAll: () => api.post('/subscriptions/refresh-all'),
  toggleNodeDisabled: (subId: string, nodeIndex: number) =>
    api.post(`/subscriptions/${subId}/nodes/${nodeIndex}/toggle`),
};

// 过滤器 API
export const filterApi = {
  getAll: () => api.get('/filters'),
  add: (data: any) => api.post('/filters', data),
  update: (id: string, data: any) => api.put(`/filters/${id}`, data),
  delete: (id: string) => api.delete(`/filters/${id}`),
};

// 规则 API
export const ruleApi = {
  getAll: () => api.get('/rules'),
  add: (data: any) => api.post('/rules', data),
  update: (id: string, data: any) => api.put(`/rules/${id}`, data),
  delete: (id: string) => api.delete(`/rules/${id}`),
};

// 规则组 API
export const ruleGroupApi = {
  getAll: () => api.get('/rule-groups'),
  update: (id: string, data: any) => api.put(`/rule-groups/${id}`, data),
};

// 规则集验证 API
export const ruleSetApi = {
  validate: (type: 'geosite' | 'geoip', name: string) =>
    api.get('/ruleset/validate', { params: { type, name } }),
};

// 设置 API
export const settingsApi = {
  get: () => api.get('/settings'),
  update: (data: any) => api.put('/settings', data),
  getSystemHosts: () => api.get('/system-hosts'),
  getInterfaces: () => api.get('/system/interfaces'),
};

// 配置 API
export const configApi = {
  generate: () => api.post('/config/generate'),
  preview: () => api.get('/config/preview'),
  apply: () => api.post('/config/apply'),
};

// 服务 API
export const serviceApi = {
  status: () => api.get('/service/status'),
  start: () => api.post('/service/start'),
  stop: () => api.post('/service/stop'),
  restart: () => api.post('/service/restart'),
  reload: () => api.post('/service/reload'),
};

// launchd API
export const launchdApi = {
  status: () => api.get('/launchd/status'),
  install: () => api.post('/launchd/install'),
  uninstall: () => api.post('/launchd/uninstall'),
  restart: () => api.post('/launchd/restart'),
};

// 统一守护进程 API（自动判断系统）
export const daemonApi = {
  status: () => api.get('/daemon/status'),
  install: () => api.post('/daemon/install'),
  uninstall: () => api.post('/daemon/uninstall'),
  restart: () => api.post('/daemon/restart'),
};

// 监控 API
export const monitorApi = {
  system: () => api.get('/monitor/system'),
  logs: () => api.get('/monitor/logs'),
  appLogs: (lines: number = 200) => api.get(`/monitor/logs/sbm?lines=${lines}`),
  singboxLogs: (lines: number = 200) => api.get(`/monitor/logs/singbox?lines=${lines}`),
};

// 节点 API
export const nodeApi = {
  getAll: () => api.get('/nodes'),
  getCountries: () => api.get('/nodes/countries'),
  getByCountry: (code: string) => api.get(`/nodes/country/${code}`),
  parse: (url: string) => api.post('/nodes/parse', { url }),
};

// 手动节点 API
export const manualNodeApi = {
  getAll: () => api.get('/manual-nodes'),
  add: (data: any) => api.post('/manual-nodes', data),
  update: (id: string, data: any) => api.put(`/manual-nodes/${id}`, data),
  delete: (id: string) => api.delete(`/manual-nodes/${id}`),
};

// 内核管理 API
export const kernelApi = {
  getInfo: () => api.get('/kernel/info'),
  getReleases: () => api.get('/kernel/releases'),
  download: (version: string) => api.post('/kernel/download', { version }),
  getProgress: () => api.get('/kernel/progress'),
};

// Clash API 工具
export const clashApi = {
  // 获取 Clash API 基础 URL
  getBaseUrl: (port: number) => {
    const host = window.location.hostname;
    return `http://${host}:${port}`;
  },

  // 获取所有代理
  getProxies: async (port: number, secret?: string) => {
    const headers: Record<string, string> = {};
    if (secret) headers['Authorization'] = `Bearer ${secret}`;
    const res = await fetch(`${clashApi.getBaseUrl(port)}/proxies`, { headers });
    return res.json();
  },

  // 测试单个节点延迟
  testDelay: async (port: number, nodeName: string, secret?: string, timeout = 3000) => {
    const headers: Record<string, string> = {};
    if (secret) headers['Authorization'] = `Bearer ${secret}`;
    try {
      const res = await fetch(
        `${clashApi.getBaseUrl(port)}/proxies/${encodeURIComponent(nodeName)}/delay?timeout=${timeout}&url=https://www.gstatic.com/generate_204`,
        { headers }
      );
      if (!res.ok) return { delay: 0, available: false };
      const data = await res.json();
      return { delay: data.delay || 0, available: data.delay > 0 };
    } catch {
      return { delay: 0, available: false };
    }
  },

  // 批量测试节点延迟
  testDelayBatch: async (port: number, nodeNames: string[], secret?: string, timeout = 5000, concurrency = 10) => {
    const results: Record<string, { delay: number; available: boolean }> = {};

    // 分批并发测试
    for (let i = 0; i < nodeNames.length; i += concurrency) {
      const batch = nodeNames.slice(i, i + concurrency);
      const batchResults = await Promise.all(
        batch.map(name => clashApi.testDelay(port, name, secret, timeout).then(r => ({ name, ...r })))
      );
      batchResults.forEach(r => {
        results[r.name] = { delay: r.delay, available: r.available };
      });
    }

    return results;
  },
};
// 订阅下发 API
export const subscribeApi = {
  getInfo: () => api.get('/subscribe/info'),
  generateToken: () => api.post('/subscribe/token'),
  export: (format: 'singbox' | 'clash', path?: string) =>
    api.post('/subscribe/export', { format, path }),
};

export default api;
