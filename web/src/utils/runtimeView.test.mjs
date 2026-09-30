import assert from 'node:assert/strict';
import test from 'node:test';
import { connectionRates, filterConnections, formatBytes, formatEndpoint, selectedChain, sortConnections } from './runtimeView.ts';

const connection = (overrides = {}) => ({ id: 'a', source: '192.0.2.2', source_port: '1000', destination: '2001:db8::1', destination_port: '443', host: 'example.org', network: 'tcp', protocol: 'tls', process: '', chains: ['节点', 'Proxy'], rule: '', started_at: '2026-09-30T00:00:00Z', upload: 100, download: 200, ...overrides });
const snapshot = (connections, overrides = {}) => ({ instance: 'same', sampled_at: 1000, upload_total: 0, download_total: 0, connections, ...overrides });

test('嵌套代理链终止于叶子，循环与失效引用不会卡死', () => {
  const proxies = new Map([['代理', { selected: '自动', members: ['自动'] }], ['自动', { selected: '节点/中文', members: ['节点/中文'] }], ['节点/中文', { selected: '', members: [] }]]);
  assert.deepEqual(selectedChain('代理', proxies), ['代理', '自动', '节点/中文']);
  proxies.set('节点/中文', { selected: '代理', members: ['代理'] });
  assert.deepEqual(selectedChain('代理', proxies), ['代理', '自动', '节点/中文', '循环引用']);
  assert.deepEqual(selectedChain('不存在', proxies), ['不存在']);
});

test('连接速率使用实际毫秒采样间隔，实例重启与计数器回退不制造尖峰', () => {
  const before = snapshot([connection()]);
  const after = snapshot([connection({ upload: 300, download: 1200 }), connection({ id: 'new' })], { sampled_at: 3000 });
  assert.deepEqual(connectionRates(after, before).get('a'), { upload: 100, download: 500 });
  assert.equal(connectionRates(after, before).has('new'), false);
  assert.equal(connectionRates({ ...after, instance: 'restarted' }, before).size, 0);
  assert.equal(connectionRates({ ...after, sampled_at: 1000 }, before).size, 0);
  assert.equal(connectionRates(snapshot([connection({ upload: 50 })], { sampled_at: 3000 }), before).size, 0);
});

test('连接按来源、目标、代理链和协议交集筛选，不把链子串当命中', () => {
  const rows = [connection(), connection({ id: 'b', source: '192.0.2.20', host: 'other.test', network: 'udp', chains: ['Proxy-extra'] })];
  assert.deepEqual(filterConnections(rows, { search: 'EXAMPLE', source: '192.0.2.2', group: 'Proxy', network: 'TCP' }).map(row => row.id), ['a']);
  assert.deepEqual(filterConnections(rows, { search: '2001:db8', source: '', group: 'Proxy', network: '' }).map(row => row.id), ['a']);
  assert.deepEqual(filterConnections(rows, { search: '', source: '', group: '不存在', network: '' }), []);
});

test('数值排序和速率排序不修改快照，缺少样本保持稳定', () => {
  const rows = [connection(), connection({ id: 'b', upload: 500 })];
  assert.deepEqual(sortConnections(rows, 'upload', true, new Map()).map(row => row.id), ['b', 'a']);
  assert.deepEqual(sortConnections(rows, 'downloadRate', true, new Map([['b', { upload: 0, download: 100 }]])).map(row => row.id), ['b', 'a']);
  assert.deepEqual(rows.map(row => row.id), ['a', 'b']);
});

test('IPv6 端点和异常字节值可读', () => {
  assert.equal(formatEndpoint('2001:db8::1', '443'), '[2001:db8::1]:443');
  assert.equal(formatEndpoint('', ''), '—');
  assert.equal(formatBytes(-1), '0 B');
  assert.equal(formatBytes(Infinity), '0 B');
  assert.equal(formatBytes(1024), '1.0 KB');
});
