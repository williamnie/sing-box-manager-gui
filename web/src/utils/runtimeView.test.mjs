import assert from 'node:assert/strict';
import test from 'node:test';
import { connectionRates, connectionSources, filterConnections, formatBytes, formatEndpoint, selectedChain, sortConnections } from './runtimeView.ts';

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

test('选择来源 IP 只匹配该设备，不匹配相同前缀的其他设备', () => {
  const rows = [connection(), connection({ id: 'b', source: '192.0.2.20' }), connection({ id: 'v6', source: 'fd00::a' }), connection({ id: 'v6-other', source: 'fd00::ab' })];
  const filters = { search: '', source: '192.0.2.2', group: '', network: '' };
  assert.deepEqual(filterConnections(rows, filters).map(row => row.id), ['a']);
  assert.deepEqual(filterConnections(rows, { ...filters, source: 'FD00::A' }).map(row => row.id), ['v6']);
  assert.equal(filterConnections(rows, { ...filters, source: '' }).length, rows.length);
});

test('来源选项合并活动、已结束和无活动连接的网关设备，去重并按 IP 排序', () => {
  const active = [connection(), connection({ id: 'b', source: '192.0.2.10' }), connection({ id: 'empty', source: '' })];
  const closed = [connection({ id: 'closed', source: '192.0.2.3' }), connection({ id: 'duplicate' })];
  const clients = [{ address: '192.0.2.2', name: '手机' }, { address: '192.0.2.100', name: '' }, { address: 'FD00::A', name: 'IPv6 设备' }];
  assert.deepEqual(connectionSources(active, closed, clients), [
    { address: '192.0.2.2', name: '手机' },
    { address: '192.0.2.3', name: '' },
    { address: '192.0.2.10', name: '' },
    { address: '192.0.2.100', name: '' },
    { address: 'fd00::a', name: 'IPv6 设备' },
  ]);
  assert.equal(active.length, 3);
  assert.equal(closed.length, 2);
});

test('网关设备为空时仍列出连接来源，IPv6 大小写不产生重复选项', () => {
  assert.deepEqual(connectionSources([], [], []), []);
  assert.deepEqual(connectionSources([connection({ source: 'FD00::A' })], [connection({ source: 'fd00::a' })], []), [{ address: 'fd00::a', name: '' }]);
  assert.deepEqual(connectionSources([], [], [{ address: '192.0.2.2', name: '仅 DNS' }]), [{ address: '192.0.2.2', name: '仅 DNS' }]);
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
