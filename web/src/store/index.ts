import { create } from 'zustand';
import { subscriptionApi, filterApi, ruleApi, ruleGroupApi, settingsApi, serviceApi, nodeApi, manualNodeApi, monitorApi } from '../api';
import { toast } from '../components/Toast';

// 类型定义
export interface Subscription {
  id: string;
  name: string;
  url: string;
  node_count: number;
  updated_at: string;
  expire_at?: string;
  traffic?: {
    total: number;
    used: number;
    remaining: number;
  };
  nodes: Node[];
  enabled: boolean;
}

export interface Node {
  tag: string;
  type: string;
  server: string;
  server_port: number;
  country?: string;
  country_emoji?: string;
  extra?: Record<string, any>;
}

export interface ManualNode {
  id: string;
  node: Node;
  enabled: boolean;
}

export interface CountryGroup {
  code: string;
  name: string;
  emoji: string;
  node_count: number;
}

export interface URLTestConfig {
  url: string;
  interval: string;
  tolerance: number;
}

export interface Filter {
  id: string;
  name: string;
  include: string[];
  exclude: string[];
  include_countries: string[];
  exclude_countries: string[];
  mode: string;
  urltest_config?: URLTestConfig;
  subscriptions: string[];
  all_nodes: boolean;
  enabled: boolean;
}

export interface Rule {
  id: string;
  name: string;
  rule_type: string;
  values: string[];
  outbound: string;
  enabled: boolean;
  priority: number;
}

export interface RuleGroup {
  id: string;
  name: string;
  site_rules: string[];
  ip_rules: string[];
  outbound: string;
  enabled: boolean;
}

export interface HostEntry {
  id: string;
  domain: string;
  ips: string[];
  enabled: boolean;
}

export interface Settings {
  singbox_path: string;
  config_path: string;
  mixed_port: number;
  tun_enabled: boolean;
  proxy_dns: string;
  direct_dns: string;
  hosts?: HostEntry[];           // DNS hosts 映射
  web_port: number;
  clash_api_port: number;
  clash_ui_path: string;
  final_outbound: string;
  ruleset_base_url: string;
  auto_apply: boolean;           // 配置变更后自动应用
  subscription_interval: number; // 订阅自动更新间隔 (分钟)
  github_proxy: string;          // GitHub 代理地址
}

export interface ServiceStatus {
  running: boolean;
  pid: number;
  version: string;
  sbm_version: string;
}

export interface ProcessStats {
  pid: number;
  cpu_percent: number;
  memory_mb: number;
}

export interface SystemInfo {
  sbm?: ProcessStats;
  singbox?: ProcessStats;
}

interface AppState {
  // 数据
  subscriptions: Subscription[];
  manualNodes: ManualNode[];
  countryGroups: CountryGroup[];
  filters: Filter[];
  rules: Rule[];
  ruleGroups: RuleGroup[];
  settings: Settings | null;
  serviceStatus: ServiceStatus | null;
  systemInfo: SystemInfo | null;

  // 加载状态
  loading: boolean;

  // 操作
  fetchSubscriptions: () => Promise<void>;
  fetchManualNodes: () => Promise<void>;
  fetchCountryGroups: () => Promise<void>;
  fetchFilters: () => Promise<void>;
  fetchRules: () => Promise<void>;
  fetchRuleGroups: () => Promise<void>;
  fetchSettings: () => Promise<void>;
  fetchServiceStatus: () => Promise<void>;
  fetchSystemInfo: () => Promise<void>;

  addSubscription: (name: string, url: string) => Promise<void>;
  updateSubscription: (id: string, name: string, url: string) => Promise<void>;
  deleteSubscription: (id: string) => Promise<void>;
  refreshSubscription: (id: string) => Promise<void>;
  toggleSubscription: (id: string, enabled: boolean) => Promise<void>;

  // 手动节点操作
  addManualNode: (node: Omit<ManualNode, 'id'>) => Promise<void>;
  updateManualNode: (id: string, node: Partial<ManualNode>) => Promise<void>;
  deleteManualNode: (id: string) => Promise<void>;

  updateSettings: (settings: Settings) => Promise<void>;

  // 规则组操作
  toggleRuleGroup: (id: string, enabled: boolean) => Promise<void>;
  updateRuleGroupOutbound: (id: string, outbound: string) => Promise<void>;

  // 自定义规则操作
  addRule: (rule: Omit<Rule, 'id'>) => Promise<void>;
  updateRule: (id: string, rule: Partial<Rule>) => Promise<void>;
  deleteRule: (id: string) => Promise<void>;

  // 过滤器操作
  addFilter: (filter: Omit<Filter, 'id'>) => Promise<void>;
  updateFilter: (id: string, filter: Partial<Filter>) => Promise<void>;
  deleteFilter: (id: string) => Promise<void>;
  toggleFilter: (id: string, enabled: boolean) => Promise<void>;
}

export const useStore = create<AppState>((set, get) => ({
  subscriptions: [],
  manualNodes: [],
  countryGroups: [],
  filters: [],
  rules: [],
  ruleGroups: [],
  settings: null,
  serviceStatus: null,
  systemInfo: null,
  loading: false,

  fetchSubscriptions: async () => {
    try {
      const res = await subscriptionApi.getAll();
      set({ subscriptions: res.data.data || [] });
    } catch (error) {
      console.error('获取订阅失败:', error);
    }
  },

  fetchManualNodes: async () => {
    try {
      const res = await manualNodeApi.getAll();
      set({ manualNodes: res.data.data || [] });
    } catch (error) {
      console.error('获取手动节点失败:', error);
    }
  },

  fetchCountryGroups: async () => {
    try {
      const res = await nodeApi.getCountries();
      set({ countryGroups: res.data.data || [] });
    } catch (error) {
      console.error('获取国家分组失败:', error);
    }
  },

  fetchFilters: async () => {
    try {
      const res = await filterApi.getAll();
      set({ filters: res.data.data || [] });
    } catch (error) {
      console.error('获取过滤器失败:', error);
    }
  },

  fetchRules: async () => {
    try {
      const res = await ruleApi.getAll();
      set({ rules: res.data.data || [] });
    } catch (error) {
      console.error('获取规则失败:', error);
    }
  },

  fetchRuleGroups: async () => {
    try {
      const res = await ruleGroupApi.getAll();
      set({ ruleGroups: res.data.data || [] });
    } catch (error) {
      console.error('获取规则组失败:', error);
    }
  },

  fetchSettings: async () => {
    try {
      const res = await settingsApi.get();
      set({ settings: res.data.data });
    } catch (error) {
      console.error('获取设置失败:', error);
    }
  },

  fetchServiceStatus: async () => {
    try {
      const res = await serviceApi.status();
      set({ serviceStatus: res.data.data });
    } catch (error) {
      console.error('获取服务状态失败:', error);
    }
  },

  fetchSystemInfo: async () => {
    try {
      const res = await monitorApi.system();
      set({ systemInfo: res.data.data });
    } catch (error) {
      console.error('获取系统信息失败:', error);
    }
  },

  addSubscription: async (name: string, url: string) => {
    set({ loading: true });
    try {
      await subscriptionApi.add(name, url);
      await get().fetchSubscriptions();
      toast.success('订阅添加成功');
    } catch (error: any) {
      toast.error(error.response?.data?.error || '添加订阅失败');
      throw error;
    } finally {
      set({ loading: false });
    }
  },

  updateSubscription: async (id: string, name: string, url: string) => {
    set({ loading: true });
    try {
      await subscriptionApi.update(id, { name, url });
      await get().fetchSubscriptions();
      toast.success('订阅更新成功');
    } catch (error: any) {
      toast.error(error.response?.data?.error || '更新订阅失败');
      throw error;
    } finally {
      set({ loading: false });
    }
  },

  deleteSubscription: async (id: string) => {
    try {
      await subscriptionApi.delete(id);
      await get().fetchSubscriptions();
      toast.success('订阅已删除');
    } catch (error: any) {
      console.error('删除订阅失败:', error);
      toast.error(error.response?.data?.error || '删除订阅失败');
    }
  },

  refreshSubscription: async (id: string) => {
    set({ loading: true });
    try {
      const res = await subscriptionApi.refresh(id);
      await get().fetchSubscriptions();
      await get().fetchCountryGroups();
      // 检查后端返回的 warning
      if (res.data.warning) {
        toast.info(res.data.warning);
      } else {
        toast.success('订阅刷新成功');
      }
    } catch (error: any) {
      console.error('刷新订阅失败:', error);
      toast.error(error.response?.data?.error || '刷新订阅失败');
    } finally {
      set({ loading: false });
    }
  },

  toggleSubscription: async (id: string, enabled: boolean) => {
    const subscription = get().subscriptions.find(s => s.id === id);
    if (subscription) {
      try {
        const res = await subscriptionApi.update(id, { ...subscription, enabled });
        await get().fetchSubscriptions();
        await get().fetchCountryGroups();
        if (res.data.warning) {
          toast.info(res.data.warning);
        } else {
          toast.success(`订阅已${enabled ? '启用' : '禁用'}`);
        }
      } catch (error: any) {
        console.error('切换订阅状态失败:', error);
        toast.error(error.response?.data?.error || '切换订阅状态失败');
      }
    }
  },

  addManualNode: async (node: Omit<ManualNode, 'id'>) => {
    try {
      const res = await manualNodeApi.add(node);
      await get().fetchManualNodes();
      await get().fetchCountryGroups();
      if (res.data.warning) {
        toast.info(res.data.warning);
      } else {
        toast.success('节点添加成功');
      }
    } catch (error: any) {
      console.error('添加手动节点失败:', error);
      toast.error(error.response?.data?.error || '添加节点失败');
      throw error;
    }
  },

  updateManualNode: async (id: string, node: Partial<ManualNode>) => {
    try {
      const res = await manualNodeApi.update(id, node);
      await get().fetchManualNodes();
      await get().fetchCountryGroups();
      if (res.data.warning) {
        toast.info(res.data.warning);
      } else {
        toast.success('节点更新成功');
      }
    } catch (error: any) {
      console.error('更新手动节点失败:', error);
      toast.error(error.response?.data?.error || '更新节点失败');
      throw error;
    }
  },

  deleteManualNode: async (id: string) => {
    try {
      const res = await manualNodeApi.delete(id);
      await get().fetchManualNodes();
      await get().fetchCountryGroups();
      if (res.data.warning) {
        toast.info(res.data.warning);
      } else {
        toast.success('节点已删除');
      }
    } catch (error: any) {
      console.error('删除手动节点失败:', error);
      toast.error(error.response?.data?.error || '删除节点失败');
    }
  },

  updateSettings: async (settings: Settings) => {
    try {
      await settingsApi.update(settings);
      set({ settings });
    } catch (error) {
      console.error('更新设置失败:', error);
    }
  },

  toggleRuleGroup: async (id: string, enabled: boolean) => {
    const ruleGroup = get().ruleGroups.find(r => r.id === id);
    if (ruleGroup) {
      try {
        const res = await ruleGroupApi.update(id, { ...ruleGroup, enabled });
        await get().fetchRuleGroups();
        if (res.data.warning) {
          toast.info(res.data.warning);
        } else {
          toast.success(`规则组已${enabled ? '启用' : '禁用'}`);
        }
      } catch (error: any) {
        console.error('更新规则组失败:', error);
        toast.error(error.response?.data?.error || '更新规则组失败');
      }
    }
  },

  updateRuleGroupOutbound: async (id: string, outbound: string) => {
    const ruleGroup = get().ruleGroups.find(r => r.id === id);
    if (ruleGroup) {
      try {
        const res = await ruleGroupApi.update(id, { ...ruleGroup, outbound });
        await get().fetchRuleGroups();
        if (res.data.warning) {
          toast.info(res.data.warning);
        } else {
          toast.success('规则组出站已更新');
        }
      } catch (error: any) {
        console.error('更新规则组出站失败:', error);
        toast.error(error.response?.data?.error || '更新规则组出站失败');
      }
    }
  },

  addRule: async (rule: Omit<Rule, 'id'>) => {
    try {
      const res = await ruleApi.add(rule);
      await get().fetchRules();
      if (res.data.warning) {
        toast.info(res.data.warning);
      } else {
        toast.success('规则添加成功');
      }
    } catch (error: any) {
      console.error('添加规则失败:', error);
      toast.error(error.response?.data?.error || '添加规则失败');
      throw error;
    }
  },

  updateRule: async (id: string, rule: Partial<Rule>) => {
    try {
      const res = await ruleApi.update(id, rule);
      await get().fetchRules();
      if (res.data.warning) {
        toast.info(res.data.warning);
      } else {
        toast.success('规则更新成功');
      }
    } catch (error: any) {
      console.error('更新规则失败:', error);
      toast.error(error.response?.data?.error || '更新规则失败');
      throw error;
    }
  },

  deleteRule: async (id: string) => {
    try {
      const res = await ruleApi.delete(id);
      await get().fetchRules();
      if (res.data.warning) {
        toast.info(res.data.warning);
      } else {
        toast.success('规则已删除');
      }
    } catch (error: any) {
      console.error('删除规则失败:', error);
      toast.error(error.response?.data?.error || '删除规则失败');
    }
  },

  addFilter: async (filter: Omit<Filter, 'id'>) => {
    try {
      await filterApi.add(filter);
      await get().fetchFilters();
      toast.success('过滤器添加成功');
    } catch (error: any) {
      console.error('添加过滤器失败:', error);
      toast.error(error.response?.data?.error || '添加过滤器失败');
      throw error;
    }
  },

  updateFilter: async (id: string, filter: Partial<Filter>) => {
    try {
      await filterApi.update(id, filter);
      await get().fetchFilters();
      toast.success('过滤器更新成功');
    } catch (error: any) {
      console.error('更新过滤器失败:', error);
      toast.error(error.response?.data?.error || '更新过滤器失败');
      throw error;
    }
  },

  deleteFilter: async (id: string) => {
    try {
      await filterApi.delete(id);
      await get().fetchFilters();
      toast.success('过滤器已删除');
    } catch (error: any) {
      console.error('删除过滤器失败:', error);
      toast.error(error.response?.data?.error || '删除过滤器失败');
      throw error;
    }
  },

  toggleFilter: async (id: string, enabled: boolean) => {
    const filter = get().filters.find(f => f.id === id);
    if (filter) {
      try {
        await filterApi.update(id, { ...filter, enabled });
        await get().fetchFilters();
        toast.success(`过滤器已${enabled ? '启用' : '禁用'}`);
      } catch (error: any) {
        console.error('切换过滤器状态失败:', error);
        toast.error(error.response?.data?.error || '切换过滤器状态失败');
      }
    }
  },
}));
