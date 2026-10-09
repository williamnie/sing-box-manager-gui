import { useState } from 'react';
import type { Filter, ManualNode, Subscription } from '../store';
import { filterCandidates, matchesFilter } from '../utils/filterMembers';

type Form = Omit<Filter, 'id'>;
export default function FilterMembers({ form, onChange, subscriptions, manualNodes }: {
  form: Form; onChange: (form: Form) => void; subscriptions: Subscription[]; manualNodes: ManualNode[];
}) {
  const [search, setSearch] = useState('');
  const candidates = filterCandidates(subscriptions, manualNodes, form);
  const manual = form.node_tags != null;
  const matched = candidates.filter(({ node }) => matchesFilter(node, form));
  const available = new Set(candidates.map(({ node }) => node.tag));
  const missing = (form.node_tags ?? []).filter(tag => !available.has(tag));
  const visible = candidates.filter(({ node, source }) => `${node.tag} ${source}`.toLowerCase().includes(search.toLowerCase()));
  const toggle = (tag: string, checked: boolean) => onChange({ ...form, node_tags: checked ? [...(form.node_tags ?? []), tag] : (form.node_tags ?? []).filter(value => value !== tag) });
  return <section className="space-y-3 rounded border border-default-200 p-3 dark:border-white/10">
    <div className="flex flex-wrap items-center justify-between gap-2">
      <h4 className="text-sm font-medium">组内节点 · 当前匹配 {matched.length} 个</h4>
      <label className="flex items-center gap-2 text-xs"><input type="checkbox" checked={manual} onChange={event => onChange({ ...form, node_tags: event.target.checked ? [...new Set(matched.map(({ node }) => node.tag))] : null })} />逐个选择节点</label>
    </div>
    <p className="text-xs leading-relaxed text-default-500">{manual ? '只使用勾选的节点，不再按国家或关键字筛选；订阅新增节点不会自动加入。' : '按下方国家和关键字动态筛选，订阅更新后自动重新匹配。也可开启逐个选择，直接增减成员。'}</p>
    <input aria-label="搜索组内候选节点" placeholder="搜索节点名称或来源" value={search} onChange={event => setSearch(event.target.value)} className="h-9 w-full rounded border border-default-300 bg-transparent px-2 text-xs" />
    <div className="max-h-52 space-y-1 overflow-y-auto">
      {visible.map(({ node, source, subscriptionID }) => <label key={`${subscriptionID}:${node.tag}`} className="flex items-start gap-2 rounded bg-default-100/60 p-2 text-xs dark:bg-white/5">
        <input aria-label={`加入 ${node.tag}`} type="checkbox" className="mt-0.5" checked={matchesFilter(node, form)} disabled={!manual} onChange={event => toggle(node.tag, event.target.checked)} />
        <span className="min-w-0"><span className="block break-all">{node.tag}</span><span className="text-[11px] text-default-500">{source} · {node.type.toUpperCase()}</span></span>
      </label>)}
      {!visible.length && <p className="py-3 text-xs text-default-500">没有可选节点，请检查订阅范围或搜索条件。</p>}
      {missing.map(tag => <label key={tag} className="flex items-start gap-2 p-2 text-xs text-amber-800 dark:text-amber-300"><input aria-label={`移除不可用节点 ${tag}`} type="checkbox" checked onChange={() => toggle(tag, false)} /><span>{tag} · 已删除、停用或不在当前范围，取消勾选可移除记录</span></label>)}
    </div>
    {!matched.length && <p role="status" className="text-xs text-amber-800 dark:text-amber-300">当前没有可用成员，启用的过滤器需要至少一个可用节点。</p>}
  </section>;
}
