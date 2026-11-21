import type { Rule } from '../store';
import { t } from '../i18n';

type Match = Record<string, unknown>;

const protocols: Record<string, string> = { stun: 'STUN 流量', bittorrent: 'BT / PT 流量', dns: 'DNS 查询', http: 'HTTP 流量', tls: 'TLS 流量', quic: 'QUIC 流量' };
const labels: Record<string, string> = {
  domain: t('域名'), domain_suffix: t('域名及子域名'), domain_keyword: t('域名包含'),
  domain_regex: t('域名表达式'), ip_cidr: t('目标 IP'), rule_set: t('规则集'),
  port: t('目标端口'), port_range: t('目标端口范围'), process_name: t('本机进程'),
  source_port: t('来源端口'), source_port_range: t('来源端口范围'),
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
    if (key === 'protocol') parts.push(entries.map((entry) => t(protocols[entry] || '{entry} 协议', { entry })).join(' / '));
    else if (key === 'network') parts.push(t('{p1} 传输', { p1: entries.map((entry) => entry.toUpperCase()).join(' / ') }));
    else if (key === 'ip_is_private' && value === true) parts.push(t('内网目标地址'));
    else if (labels[key]) parts.push(`${labels[key]}：${entries.join('、')}`);
    else advanced = true;
  }
  if (rule.type && rule.type !== 'default') advanced = true;
  if (rule.invert) advanced = true;
  // 不把多种目标匹配间的内核组合语义简写成 AND 或 OR。
  const destinations = ['domain', 'domain_suffix', 'domain_keyword', 'domain_regex', 'ip_cidr', 'rule_set'];
  if (destinations.filter((key) => values(rule[key]).length).length > 1) advanced = true;
  if (advanced) return {
    source: t('来源与范围见详细条件'),
    traffic: rule.invert ? t('取反匹配条件') : rule.type === 'logical' ? t('组合匹配条件') : t('{p1} · 含高级条件', { p1: parts.join(' · ') || t('自定义匹配') }),
    advanced: true,
  };
  return { source: source ? t('来自 {source}', { source: source }) : t('所有来源'), traffic: parts.join(' · ') || t('全部流量'), advanced: false };
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
  if (known?.type === 'direct' || (!known && tag === 'DIRECT')) return tag === 'DIRECT' ? t('直连') : t('直连 · {tag}', { tag: tag });
  if (known?.type === 'block' || (!known && tag === 'REJECT')) return tag === 'REJECT' ? t('拒绝') : t('拒绝 · {tag}', { tag: tag });
  if (known?.type === 'selector' || known?.type === 'urltest' || (!known && /^(Managed )?(Proxy|Auto|Final)$/.test(tag))) return t('选择组 · {tag}', { tag: tag });
  return tag ? t('出站 · {tag}', { tag: tag }) : t('未指定出站');
}

export function actionLabel(rule: Match, outbounds: unknown): string {
  const action = rule.action;
  // 动作优先，不能把非终结动作中的出站参数误当路由结果。
  if (action === 'reject') return t('拒绝');
  if (action === 'bypass') return t('绕过接管');
  if (action === 'hijack-dns') return t('交给 DNS 处理');
  if (action === 'sniff') return t('识别流量协议');
  if (action === 'resolve') return t('解析域名，继续匹配');
  if (action === 'route-options') return t('调整连接参数，继续匹配');
  if (action && action !== 'route') return t('自定义动作（见高级诊断）');
  return outboundLabel(String(rule.outbound || ''), outbounds);
}



export function isConfigurationApplied(state: { draft_error: string; applied_error: string; applied: unknown; changed: boolean | null }): boolean {
  return !state.draft_error && !state.applied_error && Boolean(state.applied) && state.changed === false;
}

export function applicationStatus(state: { draft_error: string; applied_error: string; applied: unknown; changed: boolean | null }): string {
  if (state.draft_error) return t('当前设置需要修正');
  if (state.applied_error) return t('无法读取应用状态');
  if (!state.applied) return t('尚未应用配置');
  if (state.changed === true) return t('有修改待应用');
  if (state.changed === false) return t('配置已应用');
  return t('应用状态待确认');
}
