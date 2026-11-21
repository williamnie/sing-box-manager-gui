import { useCallback, useDeferredValue, useState } from 'react';
import { Link } from 'react-router-dom';
import { Button } from '@nextui-org/react';
import { CheckCheck, Copy, Download, Pause, Play, RefreshCw, Search, ShieldCheck, ShieldBan } from 'lucide-react';
import { dnsQueriesApi } from '../api/dnsQueries';
import type { DNSFilters } from '../api/dnsQueries';
import { errorMessage } from '../api';
import { useStore } from '../store';
import useRuntimePolling from '../components/runtime/useRuntimePolling';
import ConfirmModal from '../components/ConfirmModal';
import { toast } from '../components/Toast';
import DNSDomainDetails from '../components/runtime/DNSDomainDetails';
import type { DNSInspection } from '../components/runtime/DNSDomainDetails';
import { dnsRecordKeys, updateDNSHighlights } from '../utils/dnsHighlights';
import type { DNSHighlights } from '../utils/dnsHighlights';
import { domainBlocklistApi, blocklistSaveMessage } from '../api/domainBlocklist';
import DomainBlocklistEditor from '../components/DomainBlocklistEditor';
import { t } from '../i18n';

const control = 'rounded-[3px] border border-zinc-200 bg-white text-zinc-700 dark:border-white/10 dark:bg-[#12141d] dark:text-zinc-200';
const field = `h-9 min-w-0 px-3 text-xs outline-none focus:border-[#ff5722] ${control}`;
const pageSize = 50;
const timestamp = (at: number) => at ? new Date(at).toLocaleString('zh-CN', { hour12: false }) : '—';

export default function DNSQueries() {
  const [search, setSearch] = useState('');
  const deferredSearch = useDeferredValue(search);
  const [source, setSource] = useState('');
  const [type, setType] = useState('');
  const [range, setRange] = useState('60');
  const [since, setSince] = useState('');
  const [until, setUntil] = useState('');
  const [view, setView] = useState<'domains' | 'records'>('domains');
  const [sort, setSort] = useState<'recent' | 'count'>('recent');
  const [page, setPage] = useState(0);
  const [paused, setPaused] = useState(false);
  const [pendingEnabled, setPendingEnabled] = useState<boolean | null>(null);
  const [saving, setSaving] = useState(false);
  const [exporting, setExporting] = useState(false);
  const [updatingList, setUpdatingList] = useState(false);
  const [inspection, setInspection] = useState<DNSInspection | null>(null);
  const [highlights, setHighlights] = useState<DNSHighlights | null>(null);
  const [blocking, setBlocking] = useState<string | null>(null);
  const [blocklistOpen, setBlocklistOpen] = useState(false);
  const fetchSettings = useStore(state => state.fetchSettings);

  const filters = useCallback((): DNSFilters => ({
    search: deferredSearch.trim(), source, type,
    since: range === 'custom' ? (since ? new Date(since).getTime() : undefined) : Date.now() - Number(range) * 60000,
    until: range === 'custom' && until ? new Date(until).getTime() : undefined,
  }), [deferredSearch, source, type, range, since, until]);
  const scope = JSON.stringify([deferredSearch.trim(), source, type, range, since, until, view, page, sort]);
  const load = useCallback(async (signal?: AbortSignal) => {
    const [data, blocklist] = await Promise.all([
      dnsQueriesApi.list({ ...filters(), sort, offset: page * pageSize, limit: pageSize }, signal),
      domainBlocklistApi.get(signal).catch(() => null),
    ]);
    return { ...data, blocklist, scope };
  }, [filters, page, scope, sort]);
  const query = useRuntimePolling(load, 5000, paused);
  const response = query.data;
  const status = response?.status;
  const total = view === 'domains' ? response?.data.domain_total ?? 0 : response?.data.total ?? 0;
  const pageCount = Math.max(1, Math.ceil(total / pageSize));
  const sources = Array.from(new Set([...(response?.data.sources ?? []), ...(source ? [source] : [])]));
  const currentHighlights = response && highlights?.snapshot !== response ? updateDNSHighlights(highlights, response) : highlights;
  if (currentHighlights !== highlights) setHighlights(currentHighlights);
  const newCount = (view === 'domains' ? currentHighlights?.domains.size : currentHighlights?.records.size) ?? 0;
  const recordKeys = dnsRecordKeys(response?.data.records ?? []);
  const classification = response?.classification;
  const listStatus = classification?.status;
  const customBlocklist = response?.blocklist;
  const listedDomains = new Set(customBlocklist?.editable ? customBlocklist.rule?.values ?? [] : []);
  const listStale = !!listStatus?.ready && Date.now() - listStatus.updated_at > 7 * 86400000;
  const newRowClass = 'bg-sky-50 shadow-[inset_3px_0_0_#0ea5e9] dark:bg-sky-500/10';
  const newBadge = <span className="ml-2 inline-block whitespace-nowrap rounded bg-sky-100 px-1.5 py-0.5 font-sans text-[10px] font-semibold text-sky-800 dark:bg-sky-500/20 dark:text-sky-200">{t('新增')}</span>;

  const refreshDomainList = async () => {
    setUpdatingList(true);
    try { await dnsQueriesApi.refreshDomainList(); toast.success(t('规则库已更新，域名判断结果已刷新')); }
    catch (error) { toast.error(errorMessage(error, t('规则库更新失败'))); }
    finally { await query.refresh(); setUpdatingList(false); }
  };
  const domainAssessment = (domain: string) => {
    const match = classification?.matches[domain];
    return <button onClick={() => setInspection({ domain, match, status: listStatus })} aria-label={t('查看域名判断 {domain}', { domain: domain })} className={`whitespace-nowrap rounded px-2 py-1 text-[11px] ${match ? 'bg-amber-500/10 font-medium text-amber-800 dark:text-amber-200' : 'bg-zinc-100 text-zinc-500 dark:bg-white/5 dark:text-zinc-400'}`}>
      {match ? t('疑似广告 / 跟踪') : listStatus?.ready ? t('未命中 · 查询') : t('待比对 · 查询')}
    </button>;
  };

  const copyDomain = async (domain: string) => {
    try { await navigator.clipboard.writeText(domain); toast.success(t('域名已复制，可粘贴到自定义拦截规则')); }
    catch { toast.error(t('复制失败，请选中域名手动复制')); }
  };
  const blockDomain = async (domain: string) => {
    if (blocking) return;
    setBlocking(domain);
    try {
      const result = await domainBlocklistApi.add(domain);
      const message = result.added ? blocklistSaveMessage(result) : t('该域名已在自定义拦截集合中，可到分流规则查看应用状态');
      if (message) toast.success(message);
      await query.refresh();
    } catch (error) { toast.error(errorMessage(error, t('添加拦截失败'))); }
    finally { setBlocking(null); }
  };
  const domainActions = (domain: string) => <div className="flex items-center gap-3 whitespace-nowrap">
    <button aria-label={t('复制域名 {domain}', { domain: domain })} className="flex items-center gap-1 text-[#ff5722]" onClick={() => void copyDomain(domain)}><Copy className="size-3.5" />{t('复制')}</button>
    <Button size="sm" variant="light" className="h-7 min-w-0 rounded px-2 text-rose-700 dark:text-rose-300" aria-label={t('拦截域名 {domain}', { domain: domain })} isLoading={blocking === domain} isDisabled={!!blocking || !customBlocklist || listedDomains.has(domain)} startContent={!blocking && <ShieldBan className="size-3.5" />} onPress={() => void blockDomain(domain)}>{listedDomains.has(domain) ? customBlocklist?.rule?.enabled ? t('已加入') : t('已加入 · 集合停用') : t('拦截')}</Button>
  </div>;
  const save = async () => {
    if (pendingEnabled === null) return;
    setSaving(true);
    try {
      const result = await dnsQueriesApi.settings(pendingEnabled);
      setPendingEnabled(null);
      await Promise.all([query.refresh(), fetchSettings()]);
      if (!result.warning) toast.success(result.status.enabled ? (result.status.applied ? t('DNS 查询采集已开启') : t('设置已保存，请应用配置后开始采集')) : t('采集已关闭，已有记录保留'));
    } catch (error) { toast.error(errorMessage(error, t('保存 DNS 采集设置失败'))); }
    finally { setSaving(false); }
  };
  const exportCSV = async () => {
    setExporting(true);
    try {
      const result = await dnsQueriesApi.export(filters());
      const url = URL.createObjectURL(result.data);
      const link = document.createElement('a'); link.href = url; link.download = `dns-queries-${new Date().toISOString().slice(0, 10)}.csv`; link.click();
      setTimeout(() => URL.revokeObjectURL(url), 1000);
      if (Number(result.headers['x-dns-filtered-total']) > 10000) toast.info(t('已导出最近 10000 条匹配记录，可缩小时间范围后继续导出'));
    } catch (error) { toast.error(errorMessage(error, t('DNS 记录导出失败'))); }
    finally { setExporting(false); }
  };

  return (
    <div className="mx-auto w-full max-w-7xl space-y-5 p-4 sm:p-6">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div><h1 className="text-2xl font-bold tracking-tight text-zinc-900 dark:text-white">{t('DNS 查询记录')}</h1><p className="mt-2 max-w-2xl text-xs leading-relaxed text-zinc-500 dark:text-zinc-400">{t('按设备和时间查找应用请求过的域名，定位遗漏的广告规则。后台持续保存，关闭页面后也会继续采集。')}</p></div>
        <div className="flex flex-wrap gap-2">
          <Button size="sm" className={control} onPress={() => setBlocklistOpen(true)}>{t('编辑拦截集合')}</Button>
          <Button size="sm" className={control} isLoading={exporting} isDisabled={!response} startContent={<Download className="size-3.5" />} onPress={() => void exportCSV()}>{t('导出查询 CSV')}</Button>
          <Button size="sm" className={status?.enabled ? control : 'rounded-[3px] bg-[#ff5722] text-white'} isDisabled={!status || (!status.enabled && !status.supported)} onPress={() => setPendingEnabled(!status?.enabled)}>{status?.enabled ? t('关闭采集') : t('开启采集')}</Button>
        </div>
      </div>

      <div className="space-y-2 border-l-2 border-[#ff5722] bg-white px-4 py-3 text-xs leading-relaxed dark:bg-[#0d0e12]">
        <p className="font-medium text-zinc-900 dark:text-zinc-100">{!status ? t('正在读取采集状态…') : !status.supported ? t('当前模式没有家庭 DNS 入站，请先配置家庭接入') : !status.enabled ? t('采集未开启 · 已有历史仍可查询') : status.stats.error ? t('采集出现错误') : !status.applied ? t('采集已开启 · 等待内核配置生效') : t('后台采集已开启')}<span className="ml-3 font-normal text-zinc-500">{t('已保存 {p1} 条', { p1: status?.stats.count ?? '—' })}</span></p>
        <p className="text-zinc-500 dark:text-zinc-400">{t('保留最近 {days} 天，最多 10 万条 / 64 MiB。最后查询：{time}。CSV 最多导出最近 1 万条匹配记录。', { days: status?.retention_days ?? 7, time: timestamp(status?.stats.last_query ?? 0) })}</p>
        {status?.enabled && !status.applied && <p className="text-amber-700 dark:text-amber-300">{t('请在')}{' '}<Link to="/configuration" className="underline">{t('配置审阅')}</Link>{' '}{t('中应用配置，并确认内核正在运行。启用查询记录会提高日志详细程度；对运行中的内核应用会短暂重启。')}</p>}
        {status?.stats.error && <p role="alert" className="text-rose-600 dark:text-rose-400">{status.stats.error}</p>}
        {!!status?.stats.gaps && <p role="status" className="text-amber-700 dark:text-amber-300">{t('曾发生 {p1} 次日志接续缺口，期间部分查询无法恢复。', { p1: status.stats.gaps })}</p>}
      </div>

      <div className="flex flex-wrap items-center justify-between gap-3 rounded border border-zinc-200 bg-white px-4 py-3 dark:border-white/10 dark:bg-[#0d0e12]">
        <div className="space-y-1.5 text-xs">
          <p className="flex items-center gap-2 font-medium text-zinc-800 dark:text-zinc-200"><ShieldCheck className="size-4 text-zinc-400" />{t('广告域名辅助判断')}<span className="font-normal text-zinc-500">anti-AD{listStatus?.ready ? t(' · {p1} 条规则', { p1: listStatus.count.toLocaleString() }) : t(' · 尚未下载')}</span></p>
          <p className="text-zinc-500 dark:text-zinc-400">{listStatus?.ready ? t('本地更新：{p1}。此库仅辅助判断，不参与拦截；命中标签可查看依据。', { p1: timestamp(listStatus.updated_at) }) : t('下载公开规则库后在本地比对，不上传 DNS 记录。仅作参考，不自动拦截。')}</p>
          {(listStatus?.error || listStale) && <p role="status" className="text-amber-800 dark:text-amber-200">{listStatus?.error ? t('{p1}。{p2}', { p1: listStatus.error, p2: listStatus.ready ? t('当前继续使用上次版本。') : '' }) : t('规则库已超过 7 天未更新，建议更新后再判断。')}</p>}
        </div>
        <Button size="sm" className={control} isLoading={updatingList} startContent={!updatingList && <Download className="size-3.5" />} onPress={() => void refreshDomainList()}>{listStatus?.ready ? t('更新规则库') : t('下载规则库')}</Button>
      </div>

      <div className="flex flex-wrap items-center gap-2">
        <label className="relative min-w-48 flex-1"><Search className="pointer-events-none absolute left-3 top-3 size-3.5 text-zinc-400" /><input aria-label={t('搜索 DNS 域名')} placeholder={t('搜索域名或关键字')} value={search} onChange={event => { setSearch(event.target.value); setPage(0); }} className={`${field} w-full pl-9`} /></label>
        <select aria-label={t('DNS 来源设备')} value={source} onChange={event => { setSource(event.target.value); setPage(0); }} className={field}><option value="">{t('全部设备')}</option>{sources.map(value => <option key={value} value={value}>{value}</option>)}</select>
        <select aria-label={t('DNS 查询类型')} value={type} onChange={event => { setType(event.target.value); setPage(0); }} className={field}><option value="">{t('全部类型')}</option>{['A', 'AAAA', 'HTTPS', 'SVCB', 'TXT', 'MX', 'SRV', 'PTR', 'NS', 'ANY'].map(value => <option key={value} value={value}>{value}</option>)}</select>
        <select aria-label={t('DNS 查询时间范围')} value={range} onChange={event => { setRange(event.target.value); setPage(0); }} className={field}><option value="15">{t('最近 15 分钟')}</option><option value="60">{t('最近 1 小时')}</option><option value="1440">{t('最近 24 小时')}</option><option value="10080">{t('最近 7 天')}</option><option value="custom">{t('自定义时间')}</option></select>
        {range === 'custom' && <><label className="flex items-center gap-2 text-xs text-zinc-500">{t('从')}<input aria-label={t('DNS 查询开始时间')} type="datetime-local" value={since} onChange={event => { setSince(event.target.value); setPage(0); }} className={field} /></label><label className="flex items-center gap-2 text-xs text-zinc-500">{t('到')}<input aria-label={t('DNS 查询结束时间')} type="datetime-local" value={until} onChange={event => { setUntil(event.target.value); setPage(0); }} className={field} /></label></>}
      </div>

      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex gap-2"><Button size="sm" className={view === 'domains' ? 'rounded-[3px] bg-orange-500/10 text-[#ff5722]' : control} aria-pressed={view === 'domains'} onPress={() => { setView('domains'); setPage(0); }}>{t('域名汇总 ({p1})', { p1: response?.data.domain_total ?? '—' })}</Button><Button size="sm" className={view === 'records' ? 'rounded-[3px] bg-orange-500/10 text-[#ff5722]' : control} aria-pressed={view === 'records'} onPress={() => { setView('records'); setPage(0); }}>{t('查询明细 ({p1})', { p1: response?.data.total ?? '—' })}</Button></div>
        <div className="flex items-center gap-2"><span className="text-xs text-zinc-500">{paused ? t('页面刷新已暂停 · 后台采集继续') : t('每 5 秒刷新')}</span><Button size="sm" className={control} startContent={paused ? <Play className="size-3.5" /> : <Pause className="size-3.5" />} onPress={() => setPaused(value => !value)}>{paused ? t('恢复刷新') : t('暂停刷新')}</Button><Button size="sm" aria-label={t('刷新 DNS 查询记录')} className={control} startContent={<RefreshCw className={`size-3.5 ${query.loading ? 'animate-spin' : ''}`} />} onPress={() => void query.refresh()}>{t('刷新')}</Button></div>
      </div>
      <div className="flex flex-wrap items-center justify-between gap-2 text-xs">
        <div className="flex flex-wrap items-center gap-2 text-zinc-500 dark:text-zinc-400"><span className="size-2 rounded-full bg-sky-500" /><span>{newCount ? t('本页 {newCount} 项有新查询', { newCount: newCount }) : t('新查询将以蓝色背景标出')}{' '}{t('· 保留至清除，切换筛选或翻页重新对照')}</span><Button size="sm" variant="light" isDisabled={!newCount} className="h-7 min-w-0 rounded text-sky-700 dark:text-sky-300" startContent={<CheckCheck className="size-3.5" />} onPress={() => setHighlights(value => value ? { ...value, domains: new Set(), records: new Set() } : value)}>{t('标为已读')}</Button></div>
        {view === 'domains' && <select aria-label={t('DNS 域名排序')} value={sort} onChange={event => { setSort(event.target.value as 'recent' | 'count'); setPage(0); }} className={field}><option value="recent">{t('最近查询优先')}</option><option value="count">{t('查询次数优先')}</option></select>}
      </div>
      {query.error && <p role="alert" className="rounded border border-amber-500/20 bg-amber-500/10 px-4 py-3 text-xs text-amber-700 dark:text-amber-300">{t('读取失败：')}{query.error}{t('。')}{response ? t('当前显示上次读取的记录。') : ''}</p>}

      <div className="overflow-hidden rounded-[4px] border border-zinc-200 bg-white dark:border-white/10 dark:bg-[#0d0e12]">
        <div className="overflow-x-auto"><table className="w-full min-w-[780px] text-left text-xs"><thead className="bg-zinc-50 text-zinc-500 dark:bg-[#14161d]"><tr><th className="px-4 py-3">{t('域名')}</th>{view === 'domains' ? <><th className="px-4 py-3">{t('查询次数')}</th><th className="px-4 py-3">{t('最近查询')}</th></> : <><th className="px-4 py-3">{t('来源设备')}</th><th className="px-4 py-3">{t('类型')}</th><th className="px-4 py-3">{t('查询时间')}</th></>}<th className="px-4 py-3">{t('辅助判断')}</th><th className="px-4 py-3">{t('操作')}</th></tr></thead>
          <tbody className="divide-y divide-zinc-100 text-zinc-700 dark:divide-white/5 dark:text-zinc-300">
            {view === 'domains' ? response?.data.domains.map(row => <tr key={row.domain} className={currentHighlights?.domains.has(row.domain) ? newRowClass : undefined}><td className="max-w-sm break-all px-4 py-3 font-mono">{row.domain}{currentHighlights?.domains.has(row.domain) && newBadge}</td><td className="px-4 py-3 font-mono">{row.count}</td><td className="whitespace-nowrap px-4 py-3 text-zinc-500">{timestamp(row.last_seen)}</td><td className="px-4 py-3">{domainAssessment(row.domain)}</td><td className="px-4 py-3">{domainActions(row.domain)}</td></tr>) : response?.data.records.map((row, index) => <tr key={recordKeys[index]} className={currentHighlights?.records.has(recordKeys[index]) ? newRowClass : undefined}><td className="max-w-sm break-all px-4 py-3 font-mono">{row.domain}{currentHighlights?.records.has(recordKeys[index]) && newBadge}</td><td className="px-4 py-3 font-mono">{row.source}</td><td className="px-4 py-3 font-mono">{row.type}</td><td className="whitespace-nowrap px-4 py-3 text-zinc-500">{timestamp(row.at)}</td><td className="px-4 py-3">{domainAssessment(row.domain)}</td><td className="px-4 py-3">{domainActions(row.domain)}</td></tr>)}
          </tbody></table></div>
        {!total && <p className="px-4 py-12 text-center text-sm text-zinc-500">{!response ? t('正在读取 DNS 记录…') : status?.enabled ? t('当前筛选范围内暂无 DNS 查询，试着打开目标应用或放宽筛选') : t('开启采集后，经过家庭 DNS 的查询会保存在这里')}</p>}
        <div className="flex items-center justify-between gap-3 border-t border-zinc-200 px-4 py-3 text-xs text-zinc-500 dark:border-white/10"><span>{t('匹配 {total} 条 · 每页 {pageSize} 条', { total, pageSize })}</span><div className="flex items-center gap-3"><button disabled={page === 0} onClick={() => setPage(value => value - 1)} className="disabled:opacity-30">{t('上一页')}</button><span>{page + 1} / {pageCount}</span><button disabled={page + 1 >= pageCount} onClick={() => setPage(value => value + 1)} className="disabled:opacity-30">{t('下一页')}</button></div></div>
      </div>
      <p className="text-xs leading-relaxed text-zinc-500 dark:text-zinc-400">{t('查询记录也包含被拒绝的请求，出现记录不等于访问成功。点击“拦截”会把完整域名加入自定义拦截集合，作用于采用分流规则的设备；“已加入”仅表示已保存规则。')}{status?.auto_apply ? t('当前自动应用开启，操作会应用配置并可能短暂重启内核。') : t('当前需在配置审阅手动应用。')} 可在 <Link to="/rules" className="text-[#ff5722] underline">{t('分流规则')}</Link>{' '}{t('编辑、停用或删除。缓存、其他 DNS 和加密隧道内的查询可能不可见。')}</p>
      {response && !customBlocklist && <p role="alert" className="text-xs text-amber-700 dark:text-amber-300">{t('自定义拦截集合读取失败，请刷新后再操作。')}</p>}

      <DNSDomainDetails inspection={inspection} onClose={() => setInspection(null)} onBlock={domain => void blockDomain(domain)} blocking={!!blocking} listed={!!inspection && listedDomains.has(inspection.domain)} blockAvailable={!!customBlocklist} />
      <DomainBlocklistEditor isOpen={blocklistOpen} onClose={() => setBlocklistOpen(false)} onSaved={() => void query.refresh()} />

      <ConfirmModal title={pendingEnabled ? t('开启 DNS 查询采集') : t('关闭 DNS 查询采集')} isOpen={pendingEnabled !== null} busy={saving} onClose={() => setPendingEnabled(null)} onConfirm={() => void save()} confirmLabel={t('保存设置')}>
        <p>{pendingEnabled ? t('开启后持续保存来源设备、域名、查询类型和时间，方便查找广告域名。详细日志也会占用原有日志空间，两类记录均有容量限制。') : t('关闭后停止归档新查询，已有历史保留至过期。内核日志恢复你在系统设置中选择的级别。')}</p>
        <p className="mt-3">{status?.auto_apply ? t('保存会自动应用配置，并短暂重启正在运行的内核。') : t('当前关闭了自动应用；保存后需在配置审阅中手动应用，内核详细日志才会更新。')}</p>
      </ConfirmModal>
    </div>
  );
}
