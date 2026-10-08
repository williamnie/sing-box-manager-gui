import assert from 'node:assert/strict';
import test from 'node:test';
import { updateDNSHighlights, dnsRecordKeys } from './dnsHighlights.ts';

const record = (domain, at = 1000) => ({ domain, at, source: '192.0.2.1', type: 'A' });
const snapshot = (records, domains, scope = 'first', latest = 1000) => ({ scope, data: { records, domains }, status: { stats: { last_query: latest } } });
const domain = (name, count, last_seen = 1000) => ({ domain: name, count, last_seen });

test('首次加载不标新，新查询保留标记，重复刷新不重复计数', () => {
  const first = updateDNSHighlights(null, snapshot([record('old.test')], [domain('old.test', 1)]));
  assert.equal(first.domains.size, 0);
  const data = snapshot([record('new.test', 2000), record('old.test')], [domain('new.test', 1, 2000), domain('old.test', 1)], 'first', 2000);
  const next = updateDNSHighlights(first, data);
  assert.deepEqual([...next.domains], ['new.test']);
  assert.equal(next.records.size, 1);
  const repeat = updateDNSHighlights(next, data);
  assert.equal(repeat.records.size, 1);
  assert.equal(repeat.domains.size, 1);
  const read = { ...repeat, records: new Set(), domains: new Set() };
  assert.equal(updateDNSHighlights(read, data).domains.size, 0);
});

test('同一秒重复请求也能识别，明细 key 不依赖行位置', () => {
  const r = record('same.test');
  const first = updateDNSHighlights(null, snapshot([r], [domain('same.test', 1)]));
  const next = updateDNSHighlights(first, snapshot([r, r], [domain('same.test', 2)]));
  assert.equal(next.domains.size, 1);
  assert.deepEqual([...next.records], [dnsRecordKeys([r, r])[0]]);
  assert.equal(dnsRecordKeys([record('other.test', 2000), r])[1], dnsRecordKeys([r])[0]);
});

test('筛选或翻页重建基线，历史行重新进入本页不标新', () => {
  const first = updateDNSHighlights(null, snapshot([record('recent.test', 2000)], [domain('recent.test', 1, 2000)], 'first', 2000));
  const old = snapshot([record('old.test')], [domain('old.test', 1)], 'first', 2000);
  assert.equal(updateDNSHighlights(first, old).domains.size, 0);
  assert.equal(updateDNSHighlights(first, old).records.size, 0);
  const switched = updateDNSHighlights(first, snapshot([record('new.test', 3000)], [domain('new.test', 1, 3000)], 'other', 3000));
  assert.equal(switched.domains.size, 0);
});

test('空列表首次建立基线，后续采集能标新；滑动时间窗口减少计数不标新', () => {
  const first = updateDNSHighlights(null, snapshot([], [], 'first', 0));
  const next = updateDNSHighlights(first, snapshot([record('a.test')], [domain('a.test', 2)]));
  assert.equal(next.domains.size, 1);
  const read = { ...next, records: new Set(), domains: new Set() };
  const less = updateDNSHighlights(read, snapshot([record('a.test')], [domain('a.test', 1)]));
  assert.equal(less.domains.size, 0);
});
