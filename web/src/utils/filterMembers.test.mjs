import test from 'node:test';
import assert from 'node:assert/strict';
import { filterCandidates, matchesFilter } from './filterMembers.ts';
const dynamic = { all_nodes: true, subscriptions: [], include: [], exclude: [], include_countries: [], exclude_countries: [] };
const node = { tag: '家宽', country: 'US' };
test('explicit members use exact tags, empty or removed selection never includes other nodes', () => {
  assert.equal(matchesFilter(node, { ...dynamic, node_tags: ['家宽备用'] }), false);
  assert.equal(matchesFilter(node, { ...dynamic, node_tags: [] }), false);
  assert.equal(matchesFilter(node, { ...dynamic, include: ['其他'], node_tags: ['家宽'] }), true);
  assert.equal(matchesFilter(node, { ...dynamic, node_tags: null }), true);
});
test('dynamic members combine country and keyword conditions', () => {
  assert.equal(matchesFilter(node, { ...dynamic, include_countries: ['us'], include: ['家'] }), true);
  assert.equal(matchesFilter(node, { ...dynamic, exclude: ['宽'] }), false);
});
test('subscription scope excludes disabled subscriptions and manual nodes', () => {
  const subscriptions = [{ id: 'a', name: 'A', enabled: true, nodes: [node] }, { id: 'b', name: 'B', enabled: false, nodes: [node] }];
  const manual = [{ enabled: true, node: {tag: '手动'} }];
  assert.deepEqual(filterCandidates(subscriptions, manual, dynamic).map(x => x.node.tag), ['家宽', '手动']);
  assert.deepEqual(filterCandidates(subscriptions, manual, {...dynamic, all_nodes: false, subscriptions: ['a']}).map(x => x.node.tag), ['家宽']);
  assert.deepEqual(filterCandidates(subscriptions, manual, {...dynamic, all_nodes: false, subscriptions: ['deleted'] }), []);
});
test('legacy null filter arrays remain editable', () => {
  assert.equal(matchesFilter(node, { include: null, exclude: null, include_countries: null, exclude_countries: null }), true);
  assert.deepEqual(filterCandidates([], [], {all_nodes: false, subscriptions: null}), []);
});
