import type { Rule } from '../store';

type Match = Record<string, unknown>;

const protocols: Record<string, string> = { stun: 'STUN 流量', bittorrent: 'BT / PT 流量', dns: 'DNS 查询', http: 'HTTP 流量', tls: 'TLS 流量', quic: 'QUIC 流量' };
const labels: Record<string, string> = {
  domain: '域名', domain_suffix: '域名及子域名', domain_keyword: '域名包含',
  domain_regex: '域名表达式', ip_cidr: '目标 IP', rule_set: '规则集',
  port: '目标端口', port_range: '目标端口范围', process_name: '本机进程',
  source_port: '来源端口', source_port_range: '来源端口范围',
};

function values(value: unknown): string[] {
  if (Array.isArray(value)) return value.map(String);
  return value === undefined || value === null || value === '' ? [] : [String(value)];
}

// 摘要只翻译明确支持的字段；未知或组合条件保留诊断入口，不误报为全部流量。
export function describeMatch(rule: Match): { source: string; traffic: string; advanced: boolean } {
  const source = values(rule.source_ip_cidr).join('、');
  const parts: string[] = [];
  const ignored = new Set(['source_ip_cidr', 'outbound', 'action', 'type', 'invert']);
  let advanced = false;
  for (const [key, value] of Object.entries(rule)) {
    if (ignored.has(key) || value === undefined || value === null || (Array.isArray(value) && !value.length)) continue;
    const entries = values(value);
    if (key === 'protocol') parts.push(entries.map((entry) => protocols[entry] || `${entry} 协议`).join(' / '));
    else if (key === 'network') parts.push(`${entries.map((entry) => entry.toUpperCase()).join(' / ')} 传输`);
    else if (key === 'ip_is_private' && value === true) parts.push('内网目标地址');
    else if (labels[key]) parts.push(`${labels[key]}：${entries.join('、')}`);
    else advanced = true;
  }
  if (rule.type && rule.type !== 'default') advanced = true;
  if (rule.invert) advanced = true;
  // 不把多种目标匹配间的内核组合语义简写成 AND 或 OR。
  const destinations = ['domain', 'domain_suffix', 'domain_keyword', 'domain_regex', 'ip_cidr', 'rule_set'];
  if (destinations.filter((key) => values(rule[key]).length).length > 1) advanced = true;
  if (advanced) return {
    source: '来源与范围见详细条件',
    traffic: rule.invert ? '取反匹配条件' : rule.type === 'logical' ? '组合匹配条件' : `${parts.join(' · ') || '自定义匹配'} · 含高级条件`,
    advanced: true,
  };
  return { source: source ? `来自 ${source}` : '所有来源', traffic: parts.join(' · ') || '全部流量', advanced: false };
}

export function customRuleMatch(rule: Rule): Match {
  const match: Match = {
    source_ip_cidr: rule.source_cidrs, network: rule.network, protocol: rule.protocol,
    port: rule.ports, port_range: rule.port_ranges, process_name: rule.process_names,
  };
  if (rule.rule_type === 'geosite' || rule.rule_type === 'geoip') match.rule_set = (rule.values || []).map((value) => `${rule.rule_type}-${value}`);
  else if (rule.rule_type !== 'match') match[rule.rule_type] = rule.values;
  return match;
}

export function outboundLabel(tag: string, outbounds: unknown): string {
  const known = Array.isArray(outbounds) ? outbounds.find((item) => item && item.tag === tag) : undefined;
  if (known?.type === 'direct' || (!known && tag === 'DIRECT')) return tag === 'DIRECT' ? '直连' : `直连 · ${tag}`;
  if (known?.type === 'block' || (!known && tag === 'REJECT')) return tag === 'REJECT' ? '拒绝' : `拒绝 · ${tag}`;
  if (known?.type === 'selector' || known?.type === 'urltest' || (!known && /^(Managed )?(Proxy|Auto|Final)$/.test(tag))) return `选择组 · ${tag}`;
  return tag ? `出站 · ${tag}` : '未指定出站';
}

export function actionLabel(rule: Match, outbounds: unknown): string {
  const action = rule.action;
  // 动作优先，不能把非终结动作中的出站参数误当路由结果。
  if (action === 'reject') return '拒绝';
  if (action === 'bypass') return '绕过接管';
  if (action === 'hijack-dns') return '交给 DNS 处理';
  if (action === 'sniff') return '识别流量协议';
  if (action === 'resolve') return '解析域名，继续匹配';
  if (action === 'route-options') return '调整连接参数，继续匹配';
  if (action && action !== 'route') return '自定义动作（见高级诊断）';
  return outboundLabel(String(rule.outbound || ''), outbounds);
}

export function applicationStatus(state: { draft_error: string; applied_error: string; applied: unknown; changed: boolean | null }): string {
  if (state.draft_error) return '当前设置需要修正';
  if (state.applied_error) return '无法读取应用状态';
  if (!state.applied) return '尚未应用配置';
  if (state.changed === true) return '有修改待应用';
  if (state.changed === false) return '配置已应用';
  return '应用状态待确认';
}
