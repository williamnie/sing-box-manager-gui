import { Link } from 'react-router-dom';
import { ArrowDown, ArrowUp, Activity } from 'lucide-react';
import { runtimeApi } from '../api/runtime';
import { usePanelPreferences } from '../store/panelPreferences';
import { formatBytes } from '../utils/runtimeView';
import useRuntimePolling from './runtime/useRuntimePolling';

export default function RuntimeOverview() {
  const [preferences] = usePanelPreferences();
  const { data, previous, error } = useRuntimePolling(runtimeApi.connections, preferences.refreshInterval);
  const seconds = data && previous && data.instance === previous.instance ? (data.sampled_at - previous.sampled_at) / 1000 : 0;
  const rate = (key: 'upload_total' | 'download_total') => data && previous && seconds > 0 && data[key] >= previous[key] && !error ? `${formatBytes((data[key] - previous[key]) / seconds)}/s` : '—';
  return <section aria-label="实时流量" className="rounded border border-zinc-200 bg-white dark:border-white/10 dark:bg-[#0d0e12]">
    <div className="flex flex-wrap items-center justify-between gap-2 border-b border-zinc-200 px-4 py-3 dark:border-white/10"><h2 className="flex items-center gap-2 text-sm font-semibold text-zinc-900 dark:text-white"><Activity className="size-4 text-[#ff5722]" />实时流量</h2><Link to="/connections" className="text-xs text-[#ff5722] hover:underline">查看连接 →</Link></div>
    {error && <p role="status" className="px-4 pt-3 text-xs text-amber-700 dark:text-amber-300">{error}。统计暂不可用。</p>}
    <div className="grid grid-cols-2 gap-4 p-4 lg:grid-cols-5">
      {[{ label: '上传速率', value: rate('upload_total'), icon: <ArrowUp className="size-3" /> }, { label: '下载速率', value: rate('download_total'), icon: <ArrowDown className="size-3" /> }, { label: '累计上传', value: data && !error ? formatBytes(data.upload_total) : '—' }, { label: '累计下载', value: data && !error ? formatBytes(data.download_total) : '—' }, { label: '活动连接', value: data && !error ? String(data.connections.length) : '—' }].map(item => <div key={item.label}><p className="mb-2 flex items-center gap-1 text-xs text-zinc-500">{item.icon}{item.label}</p><p className="font-mono text-xl tabular-nums text-zinc-900 dark:text-zinc-100">{item.value}</p></div>)}
    </div><p className="px-4 pb-3 text-[11px] text-zinc-500">统计来自当前内核，重启后重新计数。未经过该实例的流量不计入。</p>
  </section>;
}
