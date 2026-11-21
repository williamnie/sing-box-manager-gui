import { useMemo, useState } from 'react';
import { Button, Modal, ModalBody, ModalContent, ModalFooter, ModalHeader } from '@nextui-org/react';
import { ArrowDown, ArrowUp, ChevronLeft, ChevronRight, Columns3, Pause, Play, RefreshCw, Search, Unplug } from 'lucide-react';
import { errorMessage } from '../api';
import { runtimeApi, type RuntimeConnection } from '../api/runtime';
import { gatewayClientsApi } from '../api/gatewayClients';
import { usePanelPreferences } from '../store/panelPreferences';
import ConfirmModal from '../components/ConfirmModal';
import { toast } from '../components/Toast';
import RuntimeStatus from '../components/runtime/RuntimeStatus';
import useRuntimePolling from '../components/runtime/useRuntimePolling';
import { connectionRates, connectionSources, filterConnections, formatBytes, formatEndpoint, sortConnections, type ConnectionSort } from '../utils/runtimeView';
import { updateConnectionHistory, type ConnectionHistory, type ClosedConnection } from '../utils/connectionHistory';
import { t } from '../i18n';

const controlClass = 'rounded-[3px] border border-zinc-200 bg-white text-zinc-700 dark:border-white/10 dark:bg-[#12141d] dark:text-zinc-300';
const selectedViewClass = 'rounded-[3px] border border-[#ff5722]/50 bg-[#ff5722]/10 text-[#ff5722]';
const columns = [
  { key: 'source', label: t('来源设备'), width: 170 },
  { key: 'destination', label: t('目标'), width: 240 },
  { key: 'network', label: t('协议'), width: 90 },
  { key: 'chains', label: t('代理链'), width: 220 },
  { key: 'speed', label: t('实时速率 ↑ / ↓'), width: 190 },
  { key: 'traffic', label: t('累计流量 ↑ / ↓'), width: 190 },
  { key: 'started_at', label: t('开始时间'), width: 175 },
  { key: 'process', label: t('进程'), width: 180 },
  { key: 'rule', label: t('规则命中'), width: 180 },
];
const pageSize = 50;
const formatDate = (value: string) => value && !Number.isNaN(Date.parse(value)) ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '—';

export default function Connections() {
  const [preferences, updatePreferences] = usePanelPreferences();
  const [paused, setPaused] = useState(false);
  const runtime = useRuntimePolling(runtimeApi.connections, preferences.refreshInterval, paused);
  const clients = useRuntimePolling(gatewayClientsApi.list, 5000, paused);
  const snapshot = runtime.data;
  const [history, setHistory] = useState<ConnectionHistory>({ snapshot: null, closed: [] });
  if (snapshot && snapshot !== history.snapshot) setHistory(updateConnectionHistory(history, snapshot));
  const [view, setView] = useState<'active' | 'closed'>('active');
  const [filters, setFilters] = useState({ search: '', source: '', group: '', network: '' });
  const [sort, setSort] = useState<ConnectionSort>('started_at');
  const [descending, setDescending] = useState(true);
  const [page, setPage] = useState(0);
  const [showColumns, setShowColumns] = useState(false);
  const [detail, setDetail] = useState<RuntimeConnection | ClosedConnection | null>(null);
  const [closeTarget, setCloseTarget] = useState<{ label: string; ids: string[]; instance: string } | null>(null);
  const [closing, setClosing] = useState(false);
  const unavailable = !snapshot || Boolean(runtime.error);
  const rates = useMemo(() => connectionRates(snapshot, runtime.previous), [snapshot, runtime.previous]);
  const connections = view === 'closed' ? history.closed : snapshot?.connections;
  const filtered = useMemo(() => filterConnections(connections ?? [], filters), [connections, filters]);
  const sorted = useMemo(() => sortConnections(filtered, sort, descending, rates), [filtered, sort, descending, rates]);
  const groups = useMemo(() => [...new Set((connections ?? []).flatMap(connection => connection.chains))].sort((a, b) => a.localeCompare(b)), [connections]);
  const networks = useMemo(() => [...new Set((connections ?? []).map(connection => connection.network).filter(Boolean))].sort(), [connections]);
  const sources = useMemo(() => connectionSources(snapshot?.connections ?? [], history.closed, clients.data?.clients ?? []), [snapshot, history.closed, clients.data]);
  const refresh = () => Promise.all([runtime.refresh(), clients.refresh()]);
  const pageCount = Math.max(1, Math.ceil(sorted.length / pageSize));
  const currentPage = Math.min(page, pageCount - 1);
  const pageConnections = sorted.slice(currentPage * pageSize, (currentPage + 1) * pageSize);
  const visibleColumns = columns.filter(column => preferences.connectionColumns.includes(column.key));
  const setFilter = (key: keyof typeof filters, value: string) => { setFilters(previous => ({ ...previous, [key]: value })); setPage(0); };
  const captureClose = (label: string, connections: RuntimeConnection[]) => {
    if (unavailable || closing || view !== 'active' || !snapshot || !connections.length) return;
    const activeIDs = new Set(snapshot.connections.map(connection => connection.id));
    const ids = connections.filter(connection => activeIDs.has(connection.id)).map(connection => connection.id);
    if (ids.length) setCloseTarget({ label, ids, instance: snapshot.instance });
  };
  const closeCaptured = async () => {
    if (!closeTarget) return;
    setClosing(true);
    try {
      const result = await runtimeApi.close(closeTarget.ids, closeTarget.instance);
      if (result.failed.length) toast.error(t('已关闭 {p1} 条，{p2} 条已结束，{p3} 条失败', { p1: result.closed.length, p2: result.missing.length, p3: result.failed.length }));
      else toast.success(t('已关闭 {closed} 条{n}', { closed: result.closed.length, n: result.missing.length ? t('，{p1} 条已结束', { p1: result.missing.length }) : '' }));
      setCloseTarget(null);
    } catch (error) { toast.error(errorMessage(error, t('关闭连接失败'))); }
    finally { await runtime.refresh(); setClosing(false); }
  };
  const toggleColumn = (key: string) => {
    updatePreferences({ connectionColumns: preferences.connectionColumns.includes(key) ? preferences.connectionColumns.filter(column => column !== key) : [...preferences.connectionColumns, key] });
  };
  const columnContent = (connection: RuntimeConnection, key: string) => {
    const rate = rates.get(connection.id);
    switch (key) {
      case 'source': return formatEndpoint(connection.source, connection.source_port);
      case 'destination': return <><span className="font-medium text-zinc-900 dark:text-zinc-100">{connection.host || connection.destination || '—'}</span>{connection.host && connection.destination && <span className="mt-1 block text-[10px] text-zinc-500">{formatEndpoint(connection.destination, connection.destination_port)}</span>}{!connection.host && connection.destination_port && <span className="ml-1 text-zinc-500">:{connection.destination_port}</span>}</>;
      case 'network': return <><span className="uppercase">{connection.network || '—'}</span>{connection.protocol && <span className="mt-1 block text-[10px] text-zinc-500">{connection.protocol}</span>}</>;
      case 'chains': return connection.chains.join(' → ') || '—';
      case 'speed': return view === 'active' && rate ? `${formatBytes(rate.upload)}/s ↑ · ${formatBytes(rate.download)}/s ↓` : '—';
      case 'traffic': return `${formatBytes(connection.upload)} ↑ · ${formatBytes(connection.download)} ↓`;
      case 'started_at': return formatDate(connection.started_at);
      case 'process': return connection.process || '—';
      case 'rule': return connection.rule || '—';
      default: return '—';
    }
  };

  return <div className="space-y-5">
    <header className="flex flex-wrap items-end justify-between gap-4 border-b border-zinc-200 pb-5 dark:border-white/10">
      <div><p className="mb-1.5 font-mono text-[11px] tracking-wider text-[#ff5722]">[ RUNTIME // CONNECTIONS ]</p><h1 className="text-2xl font-bold text-zinc-900 dark:text-white">{t('连接')}</h1><p className="mt-2 text-xs text-zinc-500">{t('当前 sing-box 实例经过的流量与活动连接')}</p></div>
      <div className="flex flex-wrap items-center gap-2"><label className="flex items-center gap-2 text-xs text-zinc-500">{t('刷新间隔')}<select aria-label={t('连接刷新间隔')} value={preferences.refreshInterval} onChange={event => updatePreferences({ refreshInterval: Number(event.target.value) as 1000 | 2000 | 5000 })} className={`h-8 px-2 ${controlClass}`}><option value="1000">{t('1 秒')}</option><option value="2000">{t('2 秒')}</option><option value="5000">{t('5 秒')}</option></select></label><Button size="sm" className={controlClass} startContent={paused ? <Play className="size-3.5" /> : <Pause className="size-3.5" />} onPress={() => setPaused(previous => !previous)}>{paused ? t('恢复刷新') : t('暂停刷新')}</Button><Button size="sm" className={controlClass} startContent={<RefreshCw className={`size-3.5 ${runtime.loading || clients.loading ? 'animate-spin' : ''}`} />} onPress={() => void refresh()}>{t('刷新')}</Button></div>
    </header>
    <RuntimeStatus error={runtime.error} loading={runtime.loading} hasData={Boolean(snapshot)} />
    <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
      <div className="border-l-2 border-[#ff5722] bg-white px-4 py-3 dark:bg-[#0d0e12]"><p className="text-[11px] text-zinc-500">{t('活动连接')}</p><p className="mt-1 font-mono text-2xl text-zinc-900 dark:text-white">{snapshot ? snapshot.connections.length : '—'}<span className="ml-2 text-xs text-zinc-400">{t('条')}</span></p></div>
      <div className="border-l-2 border-zinc-200 bg-white px-4 py-3 dark:border-zinc-700 dark:bg-[#0d0e12]"><p className="flex items-center gap-1 text-[11px] text-zinc-500"><ArrowUp className="size-3" />{t('内核累计上传')}</p><p className="mt-1 font-mono text-2xl text-zinc-900 dark:text-white">{snapshot ? formatBytes(snapshot.upload_total) : '—'}</p></div>
      <div className="border-l-2 border-zinc-200 bg-white px-4 py-3 dark:border-zinc-700 dark:bg-[#0d0e12]"><p className="flex items-center gap-1 text-[11px] text-zinc-500"><ArrowDown className="size-3" />{t('内核累计下载')}</p><p className="mt-1 font-mono text-2xl text-zinc-900 dark:text-white">{snapshot ? formatBytes(snapshot.download_total) : '—'}</p></div>
    </div>
    <div className="flex flex-wrap items-center gap-2" role="group" aria-label={t('连接视图')}>
      <Button size="sm" className={view === 'active' ? selectedViewClass : controlClass} aria-pressed={view === 'active'} onPress={() => { setView('active'); setPage(0); }}>{t('活动连接 ({p1})', { p1: snapshot?.connections.length ?? '—' })}</Button>
      <Button size="sm" className={view === 'closed' ? selectedViewClass : controlClass} aria-pressed={view === 'closed'} onPress={() => { setView('closed'); setPage(0); }}>{t('最近结束 ({p1})', { p1: history.closed.length })}</Button>
      <span className="text-xs text-zinc-500">{view === 'closed' ? t('保留最近 5 分钟内采样到的结束连接，最多 1000 条') : t('连接结束后移至「最近结束」；列表消失不代表网络异常断开')}</span>
    </div>
    <div className="space-y-3">
      <div className="flex flex-wrap gap-2">
        <label className={`flex min-w-[14rem] flex-1 items-center gap-2 px-3 ${controlClass}`}><Search className="size-4 shrink-0 text-zinc-400" /><input aria-label={t('搜索目标域名或 IP')} value={filters.search} onChange={event => setFilter('search', event.target.value)} placeholder={t('搜索目标域名、IP、进程…')} className="h-9 min-w-0 flex-1 bg-transparent text-xs outline-none" /></label>
        <select aria-label={t('筛选来源设备 IP')} aria-describedby="connection-sources-help" value={filters.source} onChange={event => setFilter('source', event.target.value)} className={`h-9 min-w-40 max-w-full px-2 text-xs sm:max-w-72 ${controlClass}`}>
          <option value="">{t('全部来源 IP')}</option>
          {filters.source && !sources.some(source => source.address === filters.source) && <option value={filters.source}>{filters.source}</option>}
          {sources.map(source => <option key={source.address} value={source.address}>{source.address}{source.name ? `（${source.name}）` : ''}</option>)}
        </select>
        <select aria-label={t('筛选代理组')} value={filters.group} onChange={event => setFilter('group', event.target.value)} className={`h-9 max-w-52 px-2 text-xs ${controlClass}`}><option value="">{t('全部代理链')}</option>{filters.group && !groups.includes(filters.group) && <option value={filters.group}>{filters.group}</option>}{groups.map(group => <option key={group} value={group}>{group}</option>)}</select>
        <select aria-label={t('筛选网络协议')} value={filters.network} onChange={event => setFilter('network', event.target.value)} className={`h-9 px-2 text-xs ${controlClass}`}><option value="">{t('全部协议')}</option>{filters.network && !networks.includes(filters.network) && <option value={filters.network}>{filters.network.toUpperCase()}</option>}{networks.map(network => <option key={network} value={network}>{network.toUpperCase()}</option>)}</select>
      </div>
      <p id="connection-sources-help" className="text-xs text-zinc-500">{t('来源 IP 自动汇总活动、最近结束连接及网关最近接入设备；网关接入记录保留 24 小时，最多 512 个来源。')}</p>
      {clients.error && <p role="alert" className="text-xs text-amber-600 dark:text-amber-400">{t('最近接入设备读取失败，暂用连接记录和已获取的设备 IP。')}{clients.error}</p>}
      <div className="flex flex-wrap items-center justify-between gap-3"><div className="flex flex-wrap items-center gap-2"><label className="flex items-center gap-2 text-xs text-zinc-500">{t('排序')}<select aria-label={t('连接排序')} value={sort} onChange={event => { setSort(event.target.value as ConnectionSort); setPage(0); }} className={`h-8 px-2 ${controlClass}`}><option value="started_at">{t('开始时间')}</option><option value="source">{t('来源设备')}</option><option value="host">{t('目标')}</option><option value="upload">{t('累计上传')}</option><option value="download">{t('累计下载')}</option><option value="uploadRate">{t('上传速率')}</option><option value="downloadRate">{t('下载速率')}</option></select></label><button aria-label={descending ? t('改为升序') : t('改为降序')} onClick={() => setDescending(previous => !previous)} className={`p-2 ${controlClass}`}>{descending ? <ArrowDown className="size-3.5" /> : <ArrowUp className="size-3.5" />}</button><Button size="sm" className={`h-8 ${controlClass}`} startContent={<Columns3 className="size-3.5" />} aria-expanded={showColumns} onPress={() => setShowColumns(previous => !previous)}>{t('表格列')}</Button></div><div className="flex gap-2"><Button size="sm" variant="light" className="h-8 text-xs text-rose-600 dark:text-rose-400" isDisabled={unavailable || closing || view !== 'active' || !filtered.length} onPress={() => captureClose(t('筛选结果'), filtered)}>{t('关闭筛选结果 ({p1})', { p1: filtered.length })}</Button><Button size="sm" className="h-8 rounded-[3px] border border-rose-500/25 bg-rose-500/10 text-xs text-rose-600 dark:text-rose-400" isDisabled={unavailable || closing || view !== 'active' || !snapshot?.connections.length} startContent={<Unplug className="size-3" />} onPress={() => captureClose(t('全部连接'), snapshot?.connections ?? [])}>{t('关闭全部')}</Button></div></div>
    </div>
    {showColumns && <div className="grid grid-cols-1 gap-3 rounded border border-zinc-200 bg-white p-4 sm:grid-cols-2 xl:grid-cols-3 dark:border-white/10 dark:bg-[#0d0e12]">{columns.map(column => <div key={column.key} className="flex items-center justify-between gap-3 text-xs text-zinc-600 dark:text-zinc-400"><label className="flex min-w-28 items-center gap-2"><input type="checkbox" className="accent-[#ff5722]" checked={preferences.connectionColumns.includes(column.key)} onChange={() => toggleColumn(column.key)} />{column.label}</label><input type="range" aria-label={t('{p1}列宽', { p1: column.label })} min="80" max="480" step="10" value={preferences.connectionColumnWidths[column.key] ?? column.width} onChange={event => updatePreferences({ connectionColumnWidths: { ...preferences.connectionColumnWidths, [column.key]: Number(event.target.value) } })} className="min-w-0 flex-1 accent-[#ff5722]" /><span className="w-8 font-mono">{preferences.connectionColumnWidths[column.key] ?? column.width}</span></div>)}</div>}
    <div className="overflow-hidden rounded-[4px] border border-zinc-200 bg-white dark:border-white/10 dark:bg-[#0d0e12]">
      <div className="flex flex-wrap items-center justify-between gap-2 border-b border-zinc-200 px-4 py-3 text-[11px] text-zinc-500 dark:border-white/10"><span className="flex items-center gap-2"><span className={`size-1.5 rounded-full ${runtime.error ? 'bg-amber-500' : paused ? 'bg-zinc-400' : 'bg-emerald-500'}`} />{runtime.error ? t('读取失败 · 显示旧快照') : paused ? t('已暂停 · 显示读取时的快照') : t('每 {p1} 秒刷新', { p1: preferences.refreshInterval / 1000 })}</span><span>{snapshot ? t('读取时间 {p1}', { p1: new Date(snapshot.sampled_at).toLocaleTimeString('zh-CN', { hour12: false }) }) : t('等待内核响应')}</span></div>
      <div className="max-h-[65vh] overflow-auto"><table className="w-full table-fixed text-left text-xs" style={{ minWidth: visibleColumns.reduce((sum, column) => sum + (preferences.connectionColumnWidths[column.key] ?? column.width), 0) + 110 }}><colgroup>{visibleColumns.map(column => <col key={column.key} style={{ width: preferences.connectionColumnWidths[column.key] ?? column.width }} />)}<col style={{ width: 110 }} /></colgroup><thead className="sticky top-0 z-10 bg-zinc-50 text-[11px] text-zinc-500 dark:bg-[#14161d]"><tr>{visibleColumns.map(column => <th key={column.key} className="px-3 py-3 font-medium">{column.label}</th>)}<th className="px-3 py-3 font-medium">{t('操作')}</th></tr></thead><tbody className="divide-y divide-zinc-100 text-zinc-600 dark:divide-white/5 dark:text-zinc-400">{pageConnections.map(connection => <tr key={connection.id} className="hover:bg-zinc-50 dark:hover:bg-white/[0.02]">{visibleColumns.map(column => <td key={column.key} className="break-all px-3 py-3 font-mono leading-relaxed">{columnContent(connection, column.key)}</td>)}<td className="px-3 py-3"><div className="flex items-center gap-2"><button className="text-[#ff5722] hover:underline" onClick={() => setDetail(connection)} aria-label={t('查看 {p1} 连接详情', { p1: connection.host || connection.destination || connection.id })}>{t('详情')}</button><button className="text-rose-600 hover:underline disabled:opacity-40 dark:text-rose-400" disabled={unavailable || closing || view !== 'active'} onClick={() => captureClose(t('此连接'), [connection])} aria-label={t('关闭 {p1} 连接', { p1: connection.host || connection.destination || connection.id })}>{view === 'closed' ? t('已结束') : t('关闭')}</button></div></td></tr>)}</tbody></table>{!pageConnections.length && <div className="p-10 text-center text-sm text-zinc-500">{runtime.error ? t('无法读取连接，请等待运行接口恢复') : !snapshot ? t('正在等待连接快照…') : connections?.length ? t('没有匹配筛选条件的连接') : view === 'closed' ? t('暂无采样到的结束连接') : t('当前没有活动连接，已结束的记录可在「最近结束」查看')}</div>}</div>
      <div className="flex flex-wrap items-center justify-between gap-2 border-t border-zinc-200 px-4 py-2 text-[11px] text-zinc-500 dark:border-white/10"><span>{t('匹配 {matched} / {total} 条 · 每页 {pageSize} 条', { matched: filtered.length, total: connections?.length ?? '—', pageSize })}</span><div className="flex items-center gap-2"><button aria-label={t('上一页')} disabled={!currentPage} onClick={() => setPage(currentPage - 1)} className="rounded p-1.5 enabled:hover:bg-zinc-100 disabled:opacity-30 dark:enabled:hover:bg-white/5"><ChevronLeft className="size-4" /></button><span>{currentPage + 1} / {pageCount}</span><button aria-label={t('下一页')} disabled={currentPage >= pageCount - 1} onClick={() => setPage(currentPage + 1)} className="rounded p-1.5 enabled:hover:bg-zinc-100 disabled:opacity-30 dark:enabled:hover:bg-white/5"><ChevronRight className="size-4" /></button></div></div>
    </div>
    <p className="text-xs text-zinc-500">{t('按所选间隔读取活动快照；短于采样间隔的连接可能未被捕获。最近结束的时间和流量以最后采样为准，仅保存在本页面内存中，离开页面或内核重启后清空。速率需连续两次采样；读取失败或暂停时保留旧快照。')}</p>
    <ConfirmModal title={t('关闭连接')} isOpen={Boolean(closeTarget)} busy={closing} onClose={() => setCloseTarget(null)} onConfirm={() => void closeCaptured()} confirmLabel={t('关闭 {p1} 条', { p1: closeTarget?.ids.length ?? 0 })}><p>将关闭「{closeTarget?.label}」在点击时捕获的 <strong>{closeTarget?.ids.length ?? 0}</strong>{' '}{t('条连接，应用可能自动重连。确认后新建的连接不受此操作影响。')}</p></ConfirmModal>
    <Modal isOpen={Boolean(detail)} onClose={() => setDetail(null)} size="2xl" scrollBehavior="inside" classNames={{ base: 'rounded-[4px] border border-zinc-200 bg-white text-zinc-800 dark:border-white/10 dark:bg-[#0d0e12] dark:text-zinc-200', header: 'border-b border-zinc-200 dark:border-white/10', footer: 'border-t border-zinc-200 dark:border-white/10' }}><ModalContent><ModalHeader className="text-sm">{t('连接详情 · 读取时的快照')}</ModalHeader><ModalBody className="py-5">{detail && <dl className="grid grid-cols-[5rem_minmax(0,1fr)] gap-x-3 gap-y-3 text-xs">{[[t('连接 ID'), detail.id], [t('来源'), formatEndpoint(detail.source, detail.source_port)], [t('目标域名'), detail.host], [t('目标地址'), formatEndpoint(detail.destination, detail.destination_port)], [t('网络'), detail.network], [t('协议'), detail.protocol], [t('代理链'), detail.chains.join(' → ')], [t('进程'), detail.process], [t('规则命中'), detail.rule], [t('开始时间'), formatDate(detail.started_at)], ...('ended_at' in detail ? [[t('观察到结束'), formatDate(new Date(Number(detail.ended_at)).toISOString())]] : []), [t('累计上传'), formatBytes(detail.upload)], [t('累计下载'), formatBytes(detail.download)]].map(([label, value]) => <div key={label} className="contents"><dt className="text-zinc-500">{label}</dt><dd className="break-all font-mono">{value || '—'}</dd></div>)}</dl>}</ModalBody><ModalFooter><Button size="sm" className={controlClass} onPress={() => setDetail(null)}>{t('关闭详情')}</Button></ModalFooter></ModalContent></Modal>
  </div>;
}
