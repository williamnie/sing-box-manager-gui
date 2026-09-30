import assert from 'node:assert/strict';
import test from 'node:test';
import { actionLabel, applicationStatus, customRuleMatch, describeMatch, outboundLabel } from './rulePresentation.ts';

test('易读摘要保留来源、协议和目标限制', () => {
  assert.deepEqual(describeMatch({ source_ip_cidr: ['192.0.2.10/32'], protocol: 'stun', outbound: 'Proxy' }), {
    source: '来自 192.0.2.10/32', traffic: 'STUN 流量', advanced: false,
  });
  assert.equal(describeMatch({ domain_suffix: ['baidu.com'], outbound: 'DIRECT' }).traffic, '域名及子域名：baidu.com');
  const match = customRuleMatch({ rule_type: 'port_range', values: ['6881:6999'], source_cidrs: ['192.0.2.20'], protocol: ['bittorrent'] });
  assert.match(describeMatch(match).traffic, /BT \/ PT.*6881:6999/);
  assert.equal(describeMatch(match).source, '来自 192.0.2.20');
});

test('未知、组合和取反条件不能被简化成所有来源或全部流量', () => {
  for (const match of [
    { source_ip_is_private: true },
    { type: 'logical', mode: 'or', rules: [{ protocol: 'stun' }, { source_ip_cidr: ['192.0.2.10'] }] },
    { source_ip_cidr: ['192.0.2.10'], protocol: 'stun', invert: true },
    { domain: ['example.org'], rule_set: ['country-cn'] },
  ]) {
    const summary = describeMatch(match);
    assert.equal(summary.advanced, true);
    assert.notEqual(summary.source, '所有来源');
    assert.notEqual(summary.traffic, '全部流量');
  }
});

test('出站按真实类型解释，保留用户标签且不把选择组承诺为代理', () => {
  const outbounds = [{ tag: 'SMbox/Direct', type: 'direct' }, { tag: 'REJECT', type: 'socks' }, { tag: 'Proxy', type: 'selector' }];
  assert.equal(outboundLabel('SMbox/Direct', outbounds), '直连 · SMbox/Direct');
  assert.equal(outboundLabel('REJECT', outbounds), '出站 · REJECT');
  assert.equal(outboundLabel('Proxy', outbounds), '选择组 · Proxy');
  assert.equal(actionLabel({ action: 'reject', outbound: 'Proxy' }, outbounds), '拒绝');
  assert.equal(actionLabel({ action: 'resolve', server: 'dns_direct' }, outbounds), '解析域名，继续匹配');
});

test('应用状态区分未应用、读取失败、无效草案与相同配置', () => {
  const state = { draft_error: '', applied_error: '', applied: null, changed: null };
  assert.equal(applicationStatus(state), '尚未应用配置');
  assert.equal(applicationStatus({ ...state, applied_error: 'unreadable' }), '无法读取应用状态');
  assert.equal(applicationStatus({ ...state, applied: {}, changed: false }), '配置已应用');
  assert.equal(applicationStatus({ ...state, applied: {}, changed: true }), '有修改待应用');
  assert.equal(applicationStatus({ ...state, draft_error: 'invalid', applied: {}, changed: false }), '当前设置需要修正');
});
