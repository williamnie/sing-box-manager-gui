import assert from 'node:assert/strict';
import test from 'node:test';
import { createSTUNRule, isScopedSTUNRule, isSTUNOutboundAllowed, splitGroupForNewDevice, validateSTUNRule } from './rulePolicy.ts';

test('STUN 便捷规则排在现有规则前，仅识别 STUN，不扩大到 UDP 或 BT 端口', () => {
  const rule = createSTUNRule([{ priority: -5 }, { priority: 100 }]);
  assert.equal(rule.priority, -6);
  assert.deepEqual(rule.protocol, ['stun']);
  assert.deepEqual(rule.source_cidrs, []);
  assert.equal(rule.network, undefined);
  assert.equal(rule.ports, undefined);
  assert.equal(rule.port_ranges, undefined);
  assert.match(validateSTUNRule(rule, ['Proxy', 'REJECT']), /来源/);
  const scoped = { ...rule, source_cidrs: ['192.0.2.10/32'] };
  assert.equal(isScopedSTUNRule(scoped), true);
  assert.equal(validateSTUNRule(scoped, ['Proxy', 'REJECT']), '');
  assert.equal(validateSTUNRule({ ...scoped, values: null }, ['Proxy', 'REJECT']), '');
  assert.match(validateSTUNRule({ ...scoped, source_cidrs: [] }, ['Proxy']), /来源/);
  assert.match(validateSTUNRule({ ...scoped, protocol: ['stun', 'bittorrent'] }, ['Proxy']), /仅联合匹配/);
  assert.match(validateSTUNRule({ ...scoped, network: ['udp'] }, ['Proxy']), /仅联合匹配/);
  assert.match(validateSTUNRule({ ...scoped, outbound: 'DIRECT' }, ['Proxy', 'REJECT']), /代理出站/);
  assert.equal(validateSTUNRule({ ...scoped, outbound: 'REJECT' }, ['Proxy', 'REJECT']), '');
});

test('新设备优先使用普通分流，保留现有严格模式分组', () => {
  const strict = { id: 'strict', name: '已选择严格模式', policy: 'strict', outbound: 'Proxy' };
  const split = { id: 'split', name: '通用设备', policy: 'split' };
  const groups = [strict, split];
  assert.equal(splitGroupForNewDevice(groups, 'new'), split);
  const created = splitGroupForNewDevice([strict], 'new');
  assert.equal(created.policy, 'split');
  assert.equal(created.id, 'new');
  assert.equal(strict.policy, 'strict');
  assert.equal(groups.length, 2);
});

test('STUN 出站保留内置和旧导入的 REJECT，禁止误占标签与直连', () => {
  assert.equal(isSTUNOutboundAllowed('REJECT'), true);
  assert.equal(isSTUNOutboundAllowed('REJECT', 'block'), true);
  assert.equal(isSTUNOutboundAllowed('REJECT', 'socks'), false);
  assert.equal(isSTUNOutboundAllowed('REJECT', 'direct'), false);
  assert.equal(isSTUNOutboundAllowed('DIRECT'), false);
  assert.equal(isSTUNOutboundAllowed('原直连出站', 'direct'), false);
  assert.equal(isSTUNOutboundAllowed('SMbox/代理', 'selector'), true);
});
