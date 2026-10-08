import axios from 'axios';
import { toast } from '../components/Toast';
import type { Settings } from '../store';

export const api = axios.create({
  baseURL: '/api',
  timeout: 30000,
  withCredentials: true,
});

export function errorMessage(error: unknown, fallback = '操作失败'): string {
  if (axios.isAxiosError(error)) return error.response?.data?.error || fallback;
  return error instanceof Error ? error.message : fallback;
}

api.interceptors.response.use((response) => {
  // 自动应用失败仍可能已保存数据，必须显示服务端警告。
  if (response.data?.warning) toast.info(response.data.warning);
  return response;
}, (error: unknown) => {
  if (axios.isAxiosError(error) && error.response?.status === 401) {
    window.dispatchEvent(new Event('sbm:unauthorized'));
  }
  return Promise.reject(error);
});

export const authApi = {
  status: () => api.get('/auth/status'),
  login: (password: string, rememberMe = false) => api.post('/auth/login', { password, remember_me: rememberMe }),
  setup: (password: string, setupToken: string, rememberMe = false) => api.post('/auth/setup', { password, setup_token: setupToken, remember_me: rememberMe }),
  logout: () => api.post('/auth/logout'),
  changePassword: (currentPassword: string, newPassword: string) => api.post('/auth/password', { current_password: currentPassword, new_password: newPassword }),
};

export const gatewayApi = {
  restartManager: () => api.post('/gateway/restart-manager'),
  status: () => api.get('/gateway/status'),
  save: (settings: Settings) => api.put('/gateway/settings', settings),
  preview: (settings: Settings) => api.post('/gateway/preview', { settings }),
  check: (settings: Settings) => api.post('/gateway/check', { settings }),
  apply: () => api.post('/gateway/apply', {}, { timeout: 120000 }),
  rollback: () => api.post('/gateway/rollback', {}, { timeout: 120000 }),
};

export const configImportApi = {
  preview: (config: string) => api.post('/config/import/preview', { config }),
  import: (config: string, hash: string, acknowledgeOmitted: boolean) => api.post('/config/import/apply', { config, hash, acknowledge_omitted: acknowledgeOmitted }),
  rollback: () => api.post('/config/import/rollback'),
};

// 保留旧代码调用入口，后端亦保留旧 URL。
export const migrationApi = {
  preview: (config: string) => api.post('/migration/preview', { config }),
  import: (config: string, hash: string, acknowledgeOmitted: boolean) => api.post('/migration/import', { config, hash, acknowledge_omitted: acknowledgeOmitted }),
  rollback: () => api.post('/migration/rollback'),
};

// 订阅 API
export const subscriptionApi = {
  getAll: () => api.get('/subscriptions'),
  add: (name: string, url: string) => api.post('/subscriptions', { name, url }),
  update: (id: string, data: unknown) => api.put(`/subscriptions/${id}`, data),
  delete: (id: string) => api.delete(`/subscriptions/${id}`),
  refresh: (id: string) => api.post(`/subscriptions/${id}/refresh`),
  refreshAll: () => api.post('/subscriptions/refresh-all'),
};

// 过滤器 API
export const filterApi = {
  getAll: () => api.get('/filters'),
  add: (data: unknown) => api.post('/filters', data),
  update: (id: string, data: unknown) => api.put(`/filters/${id}`, data),
  delete: (id: string) => api.delete(`/filters/${id}`),
};

// 规则 API
export const ruleApi = {
  updateImported: (index: number, revision: string, updates: Record<string, unknown>) => api.put(`/rules/imported/${index}`, { revision, updates }, { timeout: 120000 }),
  deleteImported: (index: number, revision: string) => api.delete(`/rules/imported/${index}`, { data: { revision }, timeout: 120000 }),
  overview: () => api.get('/rules/overview'),
  getAll: () => api.get('/rules'),
  add: (data: unknown) => api.post('/rules', data),
  update: (id: string, data: unknown) => api.put(`/rules/${id}`, data),
  delete: (id: string) => api.delete(`/rules/${id}`, { timeout: 120000 }),
};

// 规则组 API
export const ruleGroupApi = {
  getAll: () => api.get('/rule-groups'),
  update: (id: string, data: unknown) => api.put(`/rule-groups/${id}`, data),
};

// 规则集验证 API
export const ruleSetApi = {
  validate: (type: 'geosite' | 'geoip', name: string) =>
    api.get('/ruleset/validate', { params: { type, name } }),
};

// 设置 API
export const settingsApi = {
  get: () => api.get('/settings'),
  update: (data: unknown) => api.put('/settings', data),
  getSystemHosts: () => api.get('/system-hosts'),
};

// 配置 API
export const configApi = {
  versions: () => api.get('/config/versions'),
  restore: (hash: string) => api.post('/config/restore', { hash }),
  generate: () => api.post('/config/generate'),
  preview: () => api.get('/config/preview'),
  apply: () => api.post('/config/apply'),
  check: () => api.post('/config/check'),
  diff: () => api.get('/config/diff'),
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
  add: (data: unknown) => api.post('/manual-nodes', data),
  update: (id: string, data: unknown) => api.put(`/manual-nodes/${id}`, data),
  delete: (id: string) => api.delete(`/manual-nodes/${id}`),
};

// 内核管理 API
export const kernelApi = {
  getInfo: () => api.get('/kernel/info'),
  getReleases: () => api.get('/kernel/releases'),
  download: (version: string) => api.post('/kernel/download', { version }),
  getProgress: () => api.get('/kernel/progress'),
};

export default api;
