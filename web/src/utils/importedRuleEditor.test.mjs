import assert from 'node:assert/strict';
import test from 'node:test';
import { importedRuleInputs, importedRuleUpdates } from './importedRuleEditor.ts';
const flags = { invert: false, ip_is_private: false, source_ip_is_private: false };

test('修改原有拒绝列表仅提交域名，不覆盖来源、动作参数或扩展字段', () => {
  const original = { domain: ['phiclouds.phicomm.com'], action: 'reject', source_ip_cidr: '192.0.2.84/32', no_drop: true, extension: { retained: true } };
  const fields = importedRuleInputs(original);
  fields.domain += '\nads.example';
  assert.deepEqual(importedRuleUpdates(original, fields, flags, 'reject', ''), { domain: ['phiclouds.phicomm.com', 'ads.example'] });
  assert.equal(original.domain.length, 1);
  assert.deepEqual(importedRuleUpdates(original, importedRuleInputs(original), flags, 'reject', ''), {});
});

test('域名关键字、规则集、端口和动作按各自语义更新，保留通配符原文', () => {
  const original = { domain_keyword: '*.openai.com', rule_set: ['x0'], outbound: 'Proxy', port: 443 };
  const fields = importedRuleInputs(original);
  assert.deepEqual(importedRuleUpdates(original, fields, flags, 'route', 'Proxy'), {});
  fields.rule_set = 'x0\nclaude0'; fields.port = '443\n8443';
  assert.deepEqual(importedRuleUpdates(original, fields, flags, 'reject', ''), { rule_set: ['x0', 'claude0'], port: [443, 8443], action: 'reject', outbound: null });
  fields.port = '443oops';
  assert.throws(() => importedRuleUpdates(original, fields, flags, 'route', 'Proxy'), /端口/);
});

test('逻辑规则和高级动作不会被普通表单静默扁平化', () => {
  const original = { type: 'logical', mode: 'or', rules: [{ domain: ['a.example'] }, { domain: ['b.example'] }], action: 'sniff', timeout: '500ms' };
  assert.deepEqual(importedRuleUpdates(original, {}, flags, 'sniff', ''), {});
});
