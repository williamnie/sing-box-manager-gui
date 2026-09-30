import type { DeviceGroup, Rule } from '../store';

export const devicePolicyLabels = {
  split: '普通分流（推荐）',
  strict: '严格全代理',
  direct: '整机直连',
  bypass: '绕过接管',
};

export function splitGroupForNewDevice(groups: DeviceGroup[], newID: string): DeviceGroup {
  return groups.find((group) => group.policy === 'split') || {
    id: newID, name: '普通分流', policy: 'split', outbound: 'Proxy',
  };
}

export function createSTUNRule(rules: Rule[], outbound = 'Proxy'): Omit<Rule, 'id'> {
  return {
    name: '设备 STUN 代理', rule_type: 'match', values: [], source_cidrs: [],
    protocol: ['stun'], outbound, enabled: true,
    priority: rules.reduce((lowest, rule) => Math.min(lowest, rule.priority), 100) - 1,
  };
}

// 来源限定的 STUN 规则再次编辑时，也不能无意中清空来源后扩展到所有设备。
export function isScopedSTUNRule(rule: Pick<Rule, 'rule_type' | 'protocol' | 'source_cidrs'>): boolean {
  return rule.rule_type === 'match' && rule.protocol?.length === 1 && rule.protocol[0] === 'stun'
    && (rule.source_cidrs?.length || 0) > 0;
}

export function isSTUNOutboundAllowed(tag: string, importedType?: string): boolean {
  if (tag === 'REJECT') return importedType === undefined || importedType === 'block';
  return tag !== 'DIRECT' && !['direct', 'block', 'dns'].includes(importedType || '');
}

export function validateSTUNRule(rule: Omit<Rule, 'id'>, outboundOptions: string[]): string {
  if (!rule.source_cidrs?.length || rule.source_cidrs.some((source) => !source.trim())) {
    return '请填写此规则适用的来源 IP / CIDR，不能扩展为所有设备';
  }
  if (rule.rule_type !== 'match' || rule.protocol?.length !== 1 || rule.protocol[0] !== 'stun'
    || rule.values?.length || rule.network?.length || rule.ports?.length || rule.port_ranges?.length || rule.process_names?.length) {
    return 'STUN 便捷规则仅联合匹配来源和嗅探到的 stun 协议';
  }
  if (!outboundOptions.includes(rule.outbound)) return '请选择可用的代理出站或 REJECT';
  return '';
}
