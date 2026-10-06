import assert from 'node:assert/strict';
import test from 'node:test';
import { updateConnectionHistory } from './connectionHistory.ts';

const connection = (id, download = 100) => ({ id, source: '192.0.2.2', source_port: '1000', destination: '192.0.2.10', destination_port: '443', host: 'example.test', network: 'tcp', protocol: 'tls', process: '', chains: ['Proxy'], rule: '', started_at: '2026-10-06T00:00:00Z', upload: 10, download });
const snapshot = (sampled_at, connections, instance = 'core-a') => ({ instance, sampled_at, upload_total: 10, download_total: 100, connections });
const empty = () => ({ snapshot: null, closed: [] });

test('下一次快照缺失的连接保留为最近结束，持续连接仍是活动连接', () => {
  const first = updateConnectionHistory(empty(), snapshot(1000, [connection('short'), connection('long')]));
  const next = updateConnectionHistory(first, snapshot(2000, [connection('long', 200)]));
  assert.deepEqual(next.snapshot.connections.map(c => c.id), ['long']);
  assert.deepEqual(next.closed.map(c => [c.id, c.ended_at, c.download]), [['short', 2000, 100]]);
  assert.equal(first.closed.length, 0);
  assert.equal(next.snapshot.connections[0].download, 200);
});

test('重复快照不重复记录，重新出现的连接不同时显示为已结束', () => {
  let state = updateConnectionHistory(empty(), snapshot(1000, [connection('a')]));
  state = updateConnectionHistory(state, snapshot(2000, []));
  state = updateConnectionHistory(state, snapshot(3000, []));
  assert.equal(state.closed.length, 1);
  assert.equal(state.closed[0].ended_at, 2000);
  state = updateConnectionHistory(state, snapshot(4000, [connection('a')]));
  assert.equal(state.closed.length, 0);
});

test('重启或时间回退时清理旧实例历史，不把旧实例连接记为新实例结束记录', () => {
  let state = updateConnectionHistory(empty(), snapshot(1000, [connection('closed'), connection('active')]));
  state = updateConnectionHistory(state, snapshot(2000, [connection('active')]));
  assert.equal(updateConnectionHistory(state, snapshot(3000, [], 'core-b')).closed.length, 0);
  assert.equal(updateConnectionHistory(state, snapshot(500, [])).closed.length, 0);
});

test('最近结束按时间淘汰并限制为 1000 条，不截断活动连接', () => {
  let state = updateConnectionHistory(empty(), snapshot(1000, [connection('old')]));
  state = updateConnectionHistory(state, snapshot(2000, []));
  const many = Array.from({ length: 1001 }, (_, i) => connection(`c${i}`));
  state = updateConnectionHistory(state, snapshot(3000, many));
  assert.equal(state.snapshot.connections.length, 1001);
  state = updateConnectionHistory(state, snapshot(4000, []));
  assert.equal(state.closed.length, 1000);
  assert.ok(state.closed.every(c => c.id !== 'old'));
  state = updateConnectionHistory(state, snapshot(304000, []));
  assert.equal(state.closed.length, 0);
});

test('两次采样之间完成的连接不能凭流量增量伪造为历史记录', () => {
  const before = updateConnectionHistory(empty(), snapshot(1000, []));
  const after = updateConnectionHistory(before, { ...snapshot(2000, []), download_total: 5000 });
  assert.equal(after.closed.length, 0);
  assert.equal(after.snapshot.download_total, 5000);
});
