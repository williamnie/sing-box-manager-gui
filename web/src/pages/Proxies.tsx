import { useEffect, useMemo, useRef, useState } from 'react';
import { Button } from '@nextui-org/react';
import { Check, ChevronDown, ChevronRight, Eye, EyeOff, Globe, RefreshCw, Search, Unplug, Zap } from 'lucide-react';
import { errorMessage } from '../api';
import { runtimeApi, type DelayResult, type RuntimeProxy } from '../api/runtime';
import { usePanelPreferences } from '../store/panelPreferences';
import ConfirmModal from '../components/ConfirmModal';
import { toast } from '../components/Toast';
import RuntimeStatus from '../components/runtime/RuntimeStatus';
import useRuntimePolling from '../components/runtime/useRuntimePolling';
import { selectedChain } from '../utils/runtimeView';

const controlClass = 'rounded-[3px] border border-zinc-200 bg-white text-zinc-700 dark:border-white/10 dark:bg-[#12141d] dark:text-zinc-300';

export default function Proxies() {
  const [preferences, updatePreferences] = usePanelPreferences();
  const [search, setSearch] = useState('');
  const [switching, setSwitching] = useState('');
  const runtime = useRuntimePolling(runtimeApi.proxies, preferences.refreshInterval, Boolean(switching));
  const [testing, setTesting] = useState<string[]>([]);
  const [tested, setTested] = useState<{ instance: string; results: Map<string, DelayResult> }>({ instance: '', results: new Map() });
  const testController = useRef<AbortController | null>(null);
  const [preparingClose, setPreparingClose] = useState('');
  const [closing, setClosing] = useState(false);
  const [closeTarget, setCloseTarget] = useState<{ group: string; ids: string[]; instance: string } | null>(null);
  const snapshot = runtime.data;
  const proxies = useMemo(() => new Map((snapshot?.proxies ?? []).map(proxy => [proxy.tag, proxy])), [snapshot]);
  const unavailable = !snapshot || Boolean(runtime.error);
  const actionBusy = Boolean(switching) || closing;
  const query = search.trim().toLocaleLowerCase();
  const groups = (snapshot?.proxies ?? []).filter(proxy => proxy.members.length || proxy.selectable || /urltest|selector/i.test(proxy.type));
  const visibleGroups = groups.filter(group => !preferences.hiddenGroups.includes(group.tag) && (!query || group.tag.toLocaleLowerCase().includes(query) || group.members.some(member => member.toLocaleLowerCase().includes(query))));

  useEffect(() => () => testController.current?.abort(), []);

  const delayFor = (tag: string) => tested.instance === snapshot?.instance && tested.results.has(tag) ? tested.results.get(tag)?.delay ?? null : proxies.get(tag)?.delay ?? null;
  const togglePreference = (key: 'collapsedGroups' | 'hiddenGroups', tag: string) => {
    updatePreferences({ [key]: preferences[key].includes(tag) ? preferences[key].filter(value => value !== tag) : [...preferences[key], tag] });
  };
  const select = async (group: RuntimeProxy, member: string) => {
    if (!snapshot || unavailable || actionBusy || !group.selectable || group.selected === member) return;
    runtime.cancel();
    setSwitching(group.tag);
    try {
      const next = await runtimeApi.select(group.tag, member, snapshot.instance);
      runtime.accept(next);
      const actual = next.proxies.find(proxy => proxy.tag === group.tag)?.selected;
      if (actual === member) toast.success(`「${group.tag}」已切换到「${member}」`);
      else toast.info(`内核返回的当前选择为「${actual || '未选择'}」，请核对运行状态`);
    } catch (error) {
      toast.error(errorMessage(error, '切换失败，正在重新读取当前选择'));
      await runtime.refresh();
    } finally { setSwitching(''); }
  };
  const test = async (tags: string[]) => {
    if (!snapshot || unavailable || testing.length) return;
    const unique = [...new Set(tags)];
    if (!unique.length) return;
    const controller = new AbortController();
    testController.current = controller;
    setTesting(unique);
    try {
      const result = await runtimeApi.test(unique, preferences.testURL, preferences.testTimeout, snapshot.instance, controller.signal);
      if (controller.signal.aborted) return;
      setTested(previous => ({ instance: snapshot.instance, results: new Map([...(previous.instance === snapshot.instance ? previous.results : []), ...result.results.map(item => [item.tag, item] as const)]) }));
      const failed = result.results.filter(item => item.error || item.delay === null).length;
      const reason = result.results.find(item => item.error)?.error;
      toast.info(`延迟测试完成：${result.results.length - failed} 个成功${failed ? `，${failed} 个未完成` : ''}${reason ? `。${reason}` : ''}`);
    } catch (error) {
      if (!controller.signal.aborted) toast.error(errorMessage(error, '延迟测试失败'));
    } finally { if (!controller.signal.aborted) setTesting([]); }
  };
  const prepareClose = async (group: string) => {
    if (!snapshot || unavailable) return;
    setPreparingClose(group);
    try {
      const connections = await runtimeApi.connections();
      if (connections.instance !== snapshot.instance) { toast.info('内核实例已变化，请刷新后重试'); await runtime.refresh(); return; }
      setCloseTarget({ group, ids: connections.connections.filter(connection => connection.chains.includes(group)).map(connection => connection.id), instance: connections.instance });
    } catch (error) { toast.error(errorMessage(error, '无法读取相关连接')); }
    finally { setPreparingClose(''); }
  };
  const closeCaptured = async () => {
    if (!closeTarget) return;
    if (!closeTarget.ids.length) { setCloseTarget(null); return; }
    setClosing(true);
    try {
      const result = await runtimeApi.close(closeTarget.ids, closeTarget.instance);
      if (result.failed.length) toast.error(`已关闭 ${result.closed.length} 条，${result.missing.length} 条已结束，${result.failed.length} 条失败`);
      else toast.success(`已关闭 ${result.closed.length} 条连接${result.missing.length ? `，${result.missing.length} 条已结束` : ''}`);
      setCloseTarget(null);
    } catch (error) { toast.error(errorMessage(error, '关闭连接失败')); }
    finally { await runtime.refresh(); setClosing(false); }
  };

  return <div className="space-y-5">
    <header className="flex flex-wrap items-end justify-between gap-4 border-b border-zinc-200 pb-5 dark:border-white/10">
      <div><p className="mb-1.5 font-mono text-[11px] tracking-wider text-[#ff5722]">[ RUNTIME // PROXIES ]</p><h1 className="text-2xl font-bold text-zinc-900 dark:text-white">代理</h1><p className="mt-2 text-xs text-zinc-500">直接切换运行内核的出口 · 无需重新应用配置</p></div>
      <Button size="sm" className={controlClass} isDisabled={Boolean(switching)} onPress={() => void runtime.refresh()} startContent={<RefreshCw className={`size-3.5 ${runtime.loading ? 'animate-spin' : ''}`} />}>刷新</Button>
    </header>
    <div className="flex flex-wrap gap-3">
      <label className={`flex min-w-0 flex-1 items-center gap-2 px-3 ${controlClass}`}><Search className="size-4 shrink-0 text-zinc-400" /><input aria-label="搜索代理组或节点" className="h-9 min-w-0 flex-1 bg-transparent text-sm outline-none" placeholder="搜索代理组或节点…" value={search} onChange={event => setSearch(event.target.value)} /></label>
      <label className="flex items-center gap-2 text-xs text-zinc-500">节点排序<select aria-label="节点排序" value={preferences.proxySort} onChange={event => updatePreferences({ proxySort: event.target.value as typeof preferences.proxySort })} className={`h-9 px-2 ${controlClass}`}><option value="default">配置顺序</option><option value="name">名称</option><option value="delay">延迟</option></select></label>
      {preferences.hiddenGroups.length > 0 && <Button size="sm" className={controlClass} startContent={<Eye className="size-3.5" />} onPress={() => updatePreferences({ hiddenGroups: [] })}>恢复隐藏组 ({preferences.hiddenGroups.length})</Button>}
    </div>
    <RuntimeStatus error={runtime.error} loading={runtime.loading} hasData={Boolean(snapshot)} />
    {snapshot && <div className="flex flex-wrap gap-x-5 gap-y-2 text-[11px] text-zinc-500"><span className="font-mono">{snapshot.version || '版本未知'}</span><span>{visibleGroups.length} / {groups.length} 个代理组</span><span>手动测速结果仅供参考，自动组选择由内核决定</span></div>}
    {snapshot && !visibleGroups.length && !runtime.error && <div className="rounded border border-dashed border-zinc-300 p-10 text-center text-sm text-zinc-500 dark:border-zinc-700">{query ? '没有匹配的代理组或节点' : groups.length ? '代理组均已隐藏，点击上方按钮恢复' : '当前内核没有可展示的代理组'}</div>}
    <div className="grid grid-cols-1 items-start gap-4 xl:grid-cols-2">
      {visibleGroups.map(group => {
        const collapsed = preferences.collapsedGroups.includes(group.tag);
        const members = group.members.filter(member => !query || group.tag.toLocaleLowerCase().includes(query) || member.toLocaleLowerCase().includes(query));
        if (preferences.proxySort !== 'default') members.sort((a, b) => preferences.proxySort === 'name' ? a.localeCompare(b, 'zh-CN', { numeric: true }) : (delayFor(a) ?? Infinity) - (delayFor(b) ?? Infinity) || a.localeCompare(b));
        return <section key={group.tag} className="min-w-0 overflow-hidden rounded-[4px] border border-zinc-200 bg-white dark:border-white/10 dark:bg-[#0d0e12]">
          <div className="flex items-start gap-2 p-4">
            <button aria-label={`${collapsed ? '展开' : '折叠'} ${group.tag}`} aria-expanded={!collapsed} onClick={() => togglePreference('collapsedGroups', group.tag)} className="mt-0.5 rounded p-1 text-zinc-500 hover:bg-zinc-100 dark:hover:bg-white/5">{collapsed ? <ChevronRight className="size-4" /> : <ChevronDown className="size-4" />}</button>
            <div className="min-w-0 flex-1"><div className="flex flex-wrap items-center gap-2"><h2 className="break-all text-sm font-semibold text-zinc-900 dark:text-white">{group.tag}</h2><span className="rounded bg-zinc-100 px-1.5 py-0.5 font-mono text-[10px] text-zinc-500 dark:bg-white/5">{group.type} · {group.members.length}</span>{switching === group.tag && <span role="status" className="text-xs text-[#ff5722]">正在切换…</span>}</div><p className="mt-2 flex items-start gap-1.5 text-xs text-zinc-500"><Globe className="mt-0.5 size-3 shrink-0 text-[#ff5722]" /><span className="break-all">{selectedChain(group.tag, proxies).slice(1).join(' → ') || '当前未选择出口'}</span></p></div>
            <button aria-label={`隐藏 ${group.tag}`} title="隐藏代理组" onClick={() => togglePreference('hiddenGroups', group.tag)} className="rounded p-1.5 text-zinc-400 hover:bg-zinc-100 dark:hover:bg-white/5"><EyeOff className="size-3.5" /></button>
          </div>
          {!collapsed && <><div className="flex flex-wrap items-center justify-between gap-2 border-y border-zinc-100 bg-zinc-50/80 px-4 py-2 dark:border-white/5 dark:bg-white/[0.02]"><span className="text-[11px] text-zinc-500">{group.selectable ? '选择节点即可热切换' : /urltest/i.test(group.type) ? '自动测速组 · 由内核选择' : '内核兼容视图 · 只读'}</span><div className="flex gap-2"><Button size="sm" variant="light" className="h-7 min-w-0 px-2 text-xs text-[#ff5722]" isDisabled={unavailable || Boolean(testing.length)} startContent={<Zap className="size-3" />} onPress={() => void test(group.members)}>整组测速</Button><Button size="sm" variant="light" className="h-7 min-w-0 px-2 text-xs text-zinc-500" isDisabled={unavailable || actionBusy || Boolean(preparingClose)} isLoading={preparingClose === group.tag} startContent={<Unplug className="size-3" />} onPress={() => void prepareClose(group.tag)}>关闭相关连接</Button></div></div>
          <div className="grid grid-cols-1 gap-2 p-3 sm:grid-cols-2 2xl:grid-cols-3">{members.map(tag => {
            const member = proxies.get(tag);
            const selected = group.selected === tag;
            const delay = delayFor(tag);
            const testError = tested.instance === snapshot?.instance ? tested.results.get(tag)?.error : undefined;
            return <div key={tag} className={`flex min-w-0 items-stretch rounded-[3px] border ${selected ? 'border-[#ff5722]/60 bg-[#ff5722]/[0.08]' : 'border-zinc-200 bg-zinc-50 dark:border-white/5 dark:bg-white/[0.03]'}`}>
              <button type="button" aria-label={`选择 ${group.tag} 的 ${tag}`} aria-pressed={selected} disabled={!group.selectable || unavailable || actionBusy || selected} onClick={() => void select(group, tag)} className="min-w-0 flex-1 p-2.5 text-left disabled:cursor-default enabled:hover:bg-[#ff5722]/5 focus-visible:outline focus-visible:outline-2 focus-visible:outline-[#ff5722]"><div className="flex items-start gap-1"><span className="break-all text-xs font-medium text-zinc-800 dark:text-zinc-200">{tag}</span>{selected && <Check className="mt-0.5 size-3 shrink-0 text-[#ff5722]" />}</div><div className="mt-2 flex flex-wrap items-center gap-x-2 gap-y-1 font-mono text-[10px] text-zinc-500"><span>{member?.type || '未知类型'}</span><span title={testError} className={testError ? 'text-rose-600 dark:text-rose-400' : delay === null ? '' : delay < 500 ? 'text-emerald-700 dark:text-emerald-400' : 'text-amber-700 dark:text-amber-300'}>{testing.includes(tag) ? '测试中…' : testError ? '测试失败' : delay === null ? '未测速' : `${delay} ms`}</span></div></button>
              <button aria-label={`测试 ${tag} 延迟`} title="测试延迟" disabled={unavailable || Boolean(testing.length)} onClick={() => void test([tag])} className="self-center rounded p-2 text-zinc-400 enabled:hover:text-[#ff5722] disabled:opacity-40"><Zap className="size-3.5" /></button>
            </div>;
          })}</div></>}
        </section>;
      })}
    </div>
    <p className="text-xs leading-relaxed text-zinc-500">切换立即影响新连接。已建立的连接是否中断由内核配置决定；需要主动重连时，可单独关闭该组的相关连接。</p>
    <ConfirmModal title="关闭代理组相关连接" isOpen={Boolean(closeTarget)} busy={closing} onClose={() => setCloseTarget(null)} onConfirm={() => void closeCaptured()} confirmLabel={closeTarget?.ids.length ? `关闭 ${closeTarget.ids.length} 条` : '确认'}><p>将关闭「{closeTarget?.group}」在读取时匹配的 <strong>{closeTarget?.ids.length ?? 0}</strong> 条连接。应用可能自动重连；之后新建的连接不受此操作影响。</p></ConfirmModal>
  </div>;
}
