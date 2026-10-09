import type { Filter, ManualNode, Node, Subscription } from '../store';

export interface FilterCandidate { node: Node; source: string; subscriptionID: string }

export function filterCandidates(subscriptions: Subscription[], manualNodes: ManualNode[], filter: Pick<Filter, 'all_nodes' | 'subscriptions'>): FilterCandidate[] {
  const scoped = !filter.all_nodes && (filter.subscriptions ?? []).length > 0;
  const candidates = subscriptions.filter(sub => sub.enabled && (!scoped || (filter.subscriptions ?? []).includes(sub.id)))
    .flatMap(sub => (sub.nodes ?? []).map(node => ({ node, source: sub.name, subscriptionID: sub.id })));
  if (!scoped) candidates.push(...manualNodes.filter(item => item.enabled).map(item => ({ node: item.node, source: '手动节点', subscriptionID: '' })));
  return candidates;
}

export function matchesFilter(node: Node, filter: Filter | Omit<Filter, 'id'>): boolean {
  if (filter.node_tags != null) return filter.node_tags.includes(node.tag);
  const country = (node.country ?? '').toLowerCase();
  const name = node.tag.toLowerCase();
  return (!(filter.include_countries ?? []).length || (filter.include_countries ?? []).some(value => value.toLowerCase() === country)) &&
    !(filter.exclude_countries ?? []).some(value => value.toLowerCase() === country) &&
    (!(filter.include ?? []).length || (filter.include ?? []).some(value => name.includes(value.toLowerCase()))) &&
    !(filter.exclude ?? []).some(value => name.includes(value.toLowerCase()));
}
