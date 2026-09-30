import { useEffect, useMemo, useRef, useState } from 'react';
import { ArrowDown, Download, Pause, Play, RefreshCw, Search, Server, Terminal, Trash2 } from 'lucide-react';
import { api, authApi, errorMessage } from '../api';
import { toast } from '../components/Toast';
import { usePanelPreferences } from '../store/panelPreferences';
import { logLevel, type LogLevel } from '../utils/logPresentation';

type LogSource = 'singbox' | 'sbm';
type LogLine = { id: number; text: string; level: LogLevel };
type LogBuffer = { lines: LogLine[]; ready: boolean; gap: boolean; discarded: number };
type LogBatch = { lines: string[]; cursor: string; gap: boolean };
const cursorKey = (source: LogSource) => `sbm-log-cursor-v1:${source}`;
const emptyBuffer = (): LogBuffer => ({ lines: [], ready: false, gap: false, discarded: 0 });
const levelStyles: Record<LogLevel, string> = {
  error: 'text-rose-400', warn: 'text-amber-700 dark:text-amber-300', info: 'text-cyan-700 dark:text-cyan-300',
  debug: 'text-purple-600 dark:text-purple-300', trace: 'text-zinc-500', other: 'text-zinc-300',
};
const buttonStyle = 'inline-flex h-8 items-center justify-center gap-2 rounded-[3px] border border-white/10 bg-[#12141d] px-3 text-xs text-zinc-300 transition-colors hover:bg-white/10 disabled:cursor-not-allowed disabled:opacity-40';

function readCursor(source: LogSource): string {
  try { return sessionStorage.getItem(cursorKey(source)) || ''; } catch { return ''; }
}
function saveCursor(source: LogSource, cursor: string) {
  try { sessionStorage.setItem(cursorKey(source), cursor); } catch { /* 隐私模式下仍支持本页续接。 */ }
}
function limitBuffer(lines: LogLine[], limit: number): LogLine[] {
  let start = Math.max(0, lines.length - limit);
  let bytes = 0;
  for (let i = lines.length - 1; i >= start; i--) {
    bytes += lines[i].text.length * 2;
    if (bytes > 4 * 1024 * 1024) { start = i + 1; break; }
  }
  return lines.slice(start);
}

export default function Logs() {
  const [preferences, updatePreferences] = usePanelPreferences();
  const [activeTab, setActiveTab] = useState<LogSource>('singbox');
  const [buffers, setBuffers] = useState<Record<LogSource, LogBuffer>>({ singbox: emptyBuffer(), sbm: emptyBuffer() });
  const [paused, setPaused] = useState(false);
  const [reconnect, setReconnect] = useState(0);
  const [connection, setConnection] = useState<{ source: LogSource; state: 'connecting' | 'live' | 'retrying' | 'expired' }>({ source: 'singbox', state: 'connecting' });
  const [search, setSearch] = useState('');
  const [level, setLevel] = useState<LogLevel | 'all'>('all');
  const [exporting, setExporting] = useState(false);
  const cursors = useRef<Record<LogSource, string>>({ singbox: readCursor('singbox'), sbm: readCursor('sbm') });
  const nextID = useRef(0);
  const logContainerRef = useRef<HTMLDivElement>(null);
  const bufferLimit = preferences.logBuffer;

  useEffect(() => {
    if (paused) return;
    let disposed = false;
    const params = new URLSearchParams({ source: activeTab });
    if (cursors.current[activeTab]) params.set('cursor', cursors.current[activeTab]);
    const stream = new EventSource(`/api/monitor/logs/stream?${params.toString()}`);
    stream.addEventListener('logs', (event: MessageEvent<string>) => {
      if (disposed) return;
      let batch: LogBatch;
      try { batch = JSON.parse(event.data) as LogBatch; } catch { return; }
      if (!Array.isArray(batch.lines) || typeof batch.cursor !== 'string') return;
      cursors.current[activeTab] = batch.cursor;
      saveCursor(activeTab, batch.cursor);
      const incoming = batch.lines.filter((line): line is string => typeof line === 'string').map((text) => ({ id: nextID.current++, text, level: logLevel(text) }));
      setBuffers((current) => {
        const previous = current[activeTab];
        const combined = [...previous.lines, ...incoming];
        const lines = limitBuffer(combined, bufferLimit);
        return { ...current, [activeTab]: { lines, ready: true, gap: previous.gap || batch.gap, discarded: previous.discarded + combined.length - lines.length } };
      });
      setConnection({ source: activeTab, state: 'live' });
    });
    stream.addEventListener('auth_expired', () => {
      if (disposed) return;
      stream.close();
      setConnection({ source: activeTab, state: 'expired' });
      window.dispatchEvent(new Event('sbm:unauthorized'));
    });
    stream.addEventListener('stream_error', () => {
      if (!disposed) setConnection({ source: activeTab, state: 'retrying' });
    });
    stream.onopen = () => { if (!disposed) setConnection({ source: activeTab, state: 'live' }); };
    stream.onerror = () => {
      if (disposed) return;
      setConnection({ source: activeTab, state: 'retrying' });
      // EventSource 隐藏 HTTP 状态；确认入口 401 后结束自动重连并回到登录。
      void authApi.status().then((response) => {
        if (!disposed && response.data.data?.authenticated === false) {
          stream.close();
          setConnection({ source: activeTab, state: 'expired' });
          window.dispatchEvent(new Event('sbm:unauthorized'));
        } else if (!disposed && stream.readyState === EventSource.CLOSED && cursors.current[activeTab]) {
          // 服务迁移或升级可能使旧游标失效；仅终止重连且已登录时重新获取尾部。
          cursors.current[activeTab] = '';
          saveCursor(activeTab, '');
          setBuffers((current) => ({ ...current, [activeTab]: { ...current[activeTab], gap: true } }));
          setReconnect((value) => value + 1);
        }
      }).catch(() => { /* 管理服务暂时离线时保留 EventSource 的自动重连。 */ });
    };
    return () => { disposed = true; stream.close(); };
  }, [activeTab, paused, reconnect, bufferLimit]);

  const current = buffers[activeTab];
  const visibleLines = useMemo(() => {
    const term = search.trim().toLowerCase();
    return current.lines.filter((line) => (level === 'all' || line.level === level) && (!term || line.text.toLowerCase().includes(term)));
  }, [current.lines, search, level]);

  useEffect(() => {
    if (preferences.logFollow && logContainerRef.current) logContainerRef.current.scrollTop = logContainerRef.current.scrollHeight;
  }, [visibleLines, activeTab, preferences.logFollow]);

  const clear = () => {
    // 保留已消费游标，手动重连、切换标签和刷新页面均不重放清屏前的历史。
    saveCursor(activeTab, cursors.current[activeTab]);
    setBuffers((current) => ({ ...current, [activeTab]: { ...emptyBuffer(), ready: true } }));
  };
  const download = async () => {
    setExporting(true);
    try {
      const response = await api.get('/monitor/logs/export', { params: { source: activeTab, lines: 1000 }, responseType: 'blob' });
      const url = URL.createObjectURL(response.data as Blob);
      const link = document.createElement('a');
      link.href = url;
      link.download = `${activeTab}-${new Date().toISOString().replace(/[:.]/g, '-')}.log`;
      link.click();
      setTimeout(() => URL.revokeObjectURL(url), 1000);
    } catch (error) { toast.error(errorMessage(error, '日志导出失败')); } finally { setExporting(false); }
  };
  const state = paused ? 'paused' : connection.source === activeTab ? connection.state : 'connecting';
  const statusLabel = { paused: '已暂停，恢复后接续', live: '增量连接正常', connecting: '正在连接', retrying: '连接中断，自动重连中', expired: '登录已失效' }[state];

  return (
    <div className="flex h-[calc(100dvh-6rem)] min-h-[30rem] flex-col gap-5 md:h-[calc(100dvh-7rem)]">
      <div className="flex shrink-0 flex-col justify-between gap-4 border-b border-white/[0.08] pb-4 md:flex-row md:items-end">
        <div>
          <div className="mb-1.5 font-mono text-[11px] uppercase tracking-wider text-[#ff5722]">[ CONSOLE // STREAM ]</div>
          <h1 className="text-2xl font-bold tracking-tight text-white">实时日志</h1>
          <p className="mt-1 text-xs text-zinc-500">从日志文件增量接续；显示过滤不改变内核输出级别。导出最近最多 1000 行 / 256 KiB 尾部。</p>
        </div>
        <div className="flex flex-wrap gap-2">
          <button className={buttonStyle} onClick={() => setPaused((value) => !value)}>{paused ? <Play className="size-3.5" /> : <Pause className="size-3.5" />}{paused ? '恢复' : '暂停'}</button>
          <button className={buttonStyle} disabled={paused} onClick={() => { setConnection({ source: activeTab, state: 'connecting' }); setReconnect((value) => value + 1); }}><RefreshCw className="size-3.5" />重连</button>
          <button className={buttonStyle} disabled={!current.ready} onClick={clear} title="只清空显示，保留接续位置，不删除日志文件"><Trash2 className="size-3.5" />清屏</button>
          <button className={buttonStyle} disabled={exporting} onClick={() => void download()} title="导出文件尾部最近 1000 行，最多读取 256 KiB，经过脱敏"><Download className="size-3.5" />{exporting ? '导出中' : '脱敏导出'}</button>
        </div>
      </div>
      <div className="flex min-h-0 flex-1 flex-col overflow-hidden rounded-[4px] border border-white/[0.09] bg-[#07080b]">
        <div className="flex shrink-0 flex-wrap items-center justify-between gap-3 border-b border-white/[0.08] bg-[#0e1017] px-3 py-2">
          <div className="flex gap-1" role="tablist" aria-label="日志来源">
            {(['singbox', 'sbm'] as const).map((source) => <button key={source} role="tab" aria-selected={activeTab === source} onClick={() => setActiveTab(source)} className={`flex items-center gap-2 rounded-[3px] px-3 py-2 text-xs ${activeTab === source ? 'bg-white/10 text-white' : 'text-zinc-500 hover:text-zinc-300'}`}>
              {source === 'singbox' ? <Terminal className="size-3.5 text-[#ff5722]" /> : <Server className="size-3.5 text-[#ff5722]" />}{source === 'singbox' ? '内核日志' : '管理器日志'}
            </button>)}
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <select aria-label="日志显示级别" value={level} onChange={(event) => setLevel(event.target.value as LogLevel | 'all')} className="h-8 rounded-[3px] border border-white/10 bg-[#12141d] px-2 text-xs text-zinc-300">
              <option value="all">全部级别</option><option value="error">ERROR</option><option value="warn">WARN</option><option value="info">INFO</option><option value="debug">DEBUG</option><option value="trace">TRACE</option><option value="other">未标级别</option>
            </select>
            <label className="relative"><Search className="pointer-events-none absolute left-2.5 top-2.5 size-3 text-zinc-500" /><input aria-label="过滤日志关键字" placeholder="过滤日志关键字" value={search} onChange={(event) => setSearch(event.target.value)} className="h-8 w-40 rounded-[3px] border border-white/10 bg-black/30 pl-7 pr-2 text-xs text-zinc-300 outline-none focus:border-[#ff5722] sm:w-52" /></label>
            <label className="flex items-center gap-1.5 px-1 text-xs text-zinc-400"><input type="checkbox" checked={preferences.logFollow} onChange={(event) => updatePreferences({ logFollow: event.target.checked })} className="accent-[#ff5722]" />跟随</label>
            <button aria-label="滚动到底部并开启跟随" className={`${buttonStyle} !px-2`} onClick={() => { updatePreferences({ logFollow: true }); if (logContainerRef.current) logContainerRef.current.scrollTop = logContainerRef.current.scrollHeight; }}><ArrowDown className="size-3.5" /></button>
          </div>
        </div>
        {current.gap && <div role="status" className="shrink-0 border-b border-amber-500/20 bg-amber-500/10 px-4 py-2 text-xs text-amber-700 dark:text-amber-300">接续位置已超出日志保留范围，或文件已重建。已接入当前尾部，期间部分日志不可恢复。</div>}
        <div ref={logContainerRef} onScroll={(event) => {
          const element = event.currentTarget;
          if (preferences.logFollow && element.scrollHeight - element.scrollTop - element.clientHeight > 48) updatePreferences({ logFollow: false });
        }} className="min-h-0 flex-1 overflow-auto bg-[#050608] p-3 font-mono text-xs leading-relaxed text-zinc-300 selection:bg-[#ff5722] selection:text-white">
          {visibleLines.length === 0 ? <div className="flex h-full min-h-24 flex-col items-center justify-center gap-2 text-zinc-500"><Terminal className="size-7" /><p>{search || level !== 'all' ? '没有符合过滤条件的日志' : current.ready ? '暂无新日志，或已清屏' : '正在读取日志尾部…'}</p></div> : visibleLines.map((line) => <div key={line.id} className="flex items-start gap-3 rounded-[2px] px-1 py-0.5 hover:bg-white/[0.03]" style={{ contentVisibility: 'auto', containIntrinsicSize: 'auto 24px' }}>
            <span className="w-10 shrink-0 select-none text-right text-[10px] text-zinc-600">{line.id + 1}</span><span className={`min-w-0 flex-1 whitespace-pre-wrap break-all ${levelStyles[line.level]}`}>{line.text}</span>
          </div>)}
        </div>
        <div className="flex shrink-0 flex-wrap items-center justify-between gap-2 border-t border-white/[0.08] bg-[#0c0d13] px-4 py-2 text-[11px] text-zinc-500">
          <span>显示 {visibleLines.length} / {current.lines.length} 行 · 缓冲上限 {bufferLimit} 行 / 4 MiB{current.discarded > 0 ? ` · 已淘汰 ${current.discarded} 行` : ''}</span>
          <span className="flex items-center gap-2" role="status"><span className={`size-1.5 rounded-full ${state === 'live' ? 'bg-emerald-500' : state === 'retrying' || state === 'expired' ? 'bg-amber-500' : 'bg-zinc-500'}`} />{statusLabel}</span>
        </div>
      </div>
    </div>
  );
}
