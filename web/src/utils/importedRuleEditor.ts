export const importedMatchFields = {
  domain: '完整域名', domain_suffix: '域名及其子域名', domain_keyword: '域名包含文字', domain_regex: '域名正则表达式',
  rule_set: '引用的规则集', ip_cidr: '目标 IP / CIDR', source_ip_cidr: '来源 IP / CIDR',
  port: '目标端口', port_range: '目标端口范围', source_port: '来源端口', source_port_range: '来源端口范围',
  network: '传输类型（tcp / udp）', protocol: '流量协议', process_name: '本机进程名',
} as const;
export const importedBooleanFields = { invert: '取反匹配', ip_is_private: '匹配内网目标', source_ip_is_private: '匹配内网来源' } as const;
export type ImportedField = keyof typeof importedMatchFields;
export type ImportedFlag = keyof typeof importedBooleanFields;
export type ImportedRule = Record<string, unknown>;

export function importedFieldText(value: unknown): string {
  return value == null ? '' : (Array.isArray(value) ? value : [value]).map(String).join('\n');
}
export function importedRuleInputs(rule: ImportedRule): Partial<Record<ImportedField, string>> {
  return Object.fromEntries(Object.keys(importedMatchFields).filter(key => rule[key] != null).map(key => [key, importedFieldText(rule[key])]));
}

// 只提交被修改的字段。原始标量、扩展字段和逻辑条件不会因打开表单而被重写。
export function importedRuleUpdates(rule: ImportedRule, fields: Partial<Record<ImportedField, string>>, flags: Record<ImportedFlag, boolean>, action: string, outbound: string): ImportedRule {
  const updates: ImportedRule = {};
  for (const key of Object.keys(importedMatchFields) as ImportedField[]) {
    const values = (fields[key] ?? '').split('\n').map(value => value.trim()).filter(Boolean);
    if (values.join('\n') === importedFieldText(rule[key])) continue;
    if (!values.length) { if (key in rule) updates[key] = null; continue; }
    if (key === 'port' || key === 'source_port') {
      if (values.some(value => !/^\d+$/.test(value) || Number(value) < 1 || Number(value) > 65535)) throw new Error('端口必须每行一个 1–65535 的整数');
      updates[key] = values.map(Number);
    } else updates[key] = [...new Set(values)];
  }
  for (const key of Object.keys(importedBooleanFields) as ImportedFlag[]) {
    if (flags[key] !== (rule[key] === true)) updates[key] = flags[key];
  }
  const previousAction = typeof rule.action === 'string' ? rule.action : 'route';
  if (action !== previousAction) updates.action = action;
  if (action === 'route') {
    if (!outbound) throw new Error('请选择目标出站');
    if (outbound !== rule.outbound) updates.outbound = outbound;
  } else if (action === 'reject' && rule.outbound != null) updates.outbound = null;
  return updates;
}
