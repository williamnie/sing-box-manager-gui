import { toast } from '../components/Toast';
import { errorMessage } from '../api';
import { useEffect, useState, useRef, useMemo } from 'react';
import { Button, Switch } from '@nextui-org/react';
import { RefreshCw, Trash2, Terminal, Server, Search, ArrowDown } from 'lucide-react';
import { monitorApi } from '../api';

type LogType = 'sbm' | 'singbox';

export default function Logs() {
  const [activeTab, setActiveTab] = useState<LogType>('singbox');
  const [sbmLogs, setSbmLogs] = useState<string[]>([]);
  const [singboxLogs, setSingboxLogs] = useState<string[]>([]);
  const [loading, setLoading] = useState(false);
  const [autoRefresh, setAutoRefresh] = useState(true);
  const [searchFilter, setSearchFilter] = useState('');
  const logContainerRef = useRef<HTMLDivElement>(null);
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null);

  const fetchLogs = async (type: LogType) => {
    try {
      setLoading(true);
      if (type === 'sbm') {
        const res = await monitorApi.appLogs(500);
        setSbmLogs(res.data.data || []);
      } else {
        const res = await monitorApi.singboxLogs(500);
        setSingboxLogs(res.data.data || []);
      }
    } catch (error) {
      toast.error(errorMessage(error, '请求失败，请检查管理服务'));
    } finally {
      setLoading(false);
    }
  };

  const fetchCurrentLogs = () => {
    fetchLogs(activeTab);
  };

  // 滚动到底部
  const scrollToBottom = () => {
    if (logContainerRef.current) {
      logContainerRef.current.scrollTop = logContainerRef.current.scrollHeight;
    }
  };

  // 初始加载
  useEffect(() => {
    fetchLogs('sbm');
    fetchLogs('singbox');
  }, []);

  // 自动刷新
  useEffect(() => {
    if (autoRefresh) {
      intervalRef.current = setInterval(() => {
        fetchLogs(activeTab);
      }, 5000);
    }

    return () => {
      if (intervalRef.current) {
        clearInterval(intervalRef.current);
        intervalRef.current = null;
      }
    };
  }, [autoRefresh, activeTab]);

  // 日志更新后滚动到底部
  useEffect(() => {
    scrollToBottom();
  }, [sbmLogs, singboxLogs, activeTab]);

  const handleClear = () => {
    if (activeTab === 'sbm') {
      setSbmLogs([]);
    } else {
      setSingboxLogs([]);
    }
  };

  const currentLogs = activeTab === 'sbm' ? sbmLogs : singboxLogs;

  const filteredLogs = useMemo(() => {
    if (!searchFilter.trim()) return currentLogs;
    const term = searchFilter.toLowerCase();
    return currentLogs.filter((line) => line.toLowerCase().includes(term));
  }, [currentLogs, searchFilter]);

  // 语法高亮分流解析
  const formatLogLine = (line: string) => {
    const isError = /error|fatal|err|fail/i.test(line);
    const isWarn = /warn|warning/i.test(line);
    const isInfo = /info|notice/i.test(line);
    const isDebug = /debug|trace/i.test(line);

    let textColor = 'text-zinc-300';
    let tag = null;

    if (isError) {
      textColor = 'text-rose-400 font-semibold';
      tag = <span className="text-[10px] bg-rose-500/20 text-rose-300 px-1 py-0.5 rounded-[2px] mr-1.5 font-mono">ERR</span>;
    } else if (isWarn) {
      textColor = 'text-amber-800 dark:text-amber-300';
      tag = <span className="text-[10px] bg-amber-500/20 text-amber-800 dark:text-amber-300 px-1 py-0.5 rounded-[2px] mr-1.5 font-mono">WRN</span>;
    } else if (isInfo) {
      textColor = 'text-cyan-300';
      tag = <span className="text-[10px] bg-cyan-500/20 text-cyan-300 px-1 py-0.5 rounded-[2px] mr-1.5 font-mono">INF</span>;
    } else if (isDebug) {
      textColor = 'text-purple-300';
      tag = <span className="text-[10px] bg-purple-500/20 text-purple-300 px-1 py-0.5 rounded-[2px] mr-1.5 font-mono">DBG</span>;
    }

    return (
      <span className={textColor}>
        {tag}
        {line}
      </span>
    );
  };

  return (
    // 扣除全局状态栏（3rem）和 main 上下内边距，日志正文占满剩余高度。
    <div className="flex h-[calc(100dvh-6rem)] min-h-[24rem] flex-col gap-6 md:h-[calc(100dvh-7rem)]">
      {/* 极客终端头部 */}
      <div className="flex shrink-0 flex-col md:flex-row md:items-end justify-between gap-4 border-b border-white/[0.08] pb-5">
        <div>
          <div className="flex items-center gap-2 mb-1.5 font-mono text-[11px] text-[#ff5722] tracking-wider uppercase">
            <span>[ CONSOLE // STREAM ]</span>
            <span className="text-zinc-600">--</span>
            <span className="text-zinc-400">LOG RUNTIME PIPELINE</span>
          </div>
          <h1 className="text-2xl font-bold tracking-tight text-white font-sans flex items-center gap-3">
            实时日志终端
          </h1>
        </div>

        {/* 顶部工具按钮 */}
        <div className="flex flex-wrap items-center gap-3 font-mono text-xs">
          {/* 自动刷新开关 */}
          <div className="flex items-center gap-2 px-3 py-1.5 rounded-[3px] border border-white/[0.08] bg-[#0d0e14]">
            {autoRefresh ? (
              <span className="relative flex size-2">
                <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-emerald-400 opacity-75"></span>
                <span className="relative inline-flex rounded-full size-2 bg-emerald-500"></span>
              </span>
            ) : (
              <span className="size-2 rounded-full bg-zinc-600"></span>
            )}
            <span className="text-[11px] text-zinc-400">{autoRefresh ? 'POLL: 5s' : 'PAUSED'}</span>
            <Switch
              size="sm"
              isSelected={autoRefresh}
              onValueChange={setAutoRefresh}
              classNames={{
                wrapper: "group-data-[selected=true]:bg-[#ff5722]",
              }}
            />
          </div>

          <Button
            size="sm"
            className="font-mono text-xs rounded-[3px] border border-white/[0.1] bg-[#12141d] hover:bg-[#181c28] text-zinc-200 transition-all"
            startContent={<RefreshCw className={`size-3 text-[#ff5722] ${loading ? 'animate-spin' : ''}`} />}
            onPress={fetchCurrentLogs}
            isDisabled={loading}
          >
            刷新
          </Button>

          <Button
            size="sm"
            className="font-mono text-xs rounded-[3px] border border-rose-500/20 bg-rose-500/10 hover:bg-rose-500/20 text-rose-300 transition-all"
            startContent={<Trash2 className="size-3" />}
            onPress={handleClear}
          >
            清空
          </Button>
        </div>
      </div>

      {/* 终端卡片外壳 */}
      <div className="flex min-h-0 flex-1 flex-col rounded-[4px] border border-white/[0.09] bg-[#07080b] shadow-[0_12px_40px_rgba(0,0,0,0.8)] overflow-hidden">
        {/* 终端顶栏控制台 */}
        <div className="min-h-10 shrink-0 px-4 py-1 border-b border-white/[0.08] bg-[#0e1017] flex flex-wrap items-center justify-between gap-3 select-none">
          {/* 左侧 Tab 切换 */}
          <div className="flex items-center gap-1">
            <button
              onClick={() => setActiveTab('singbox')}
              className={`flex items-center gap-2 px-3 py-1.5 rounded-[3px] font-mono text-xs tracking-wider uppercase transition-all cursor-pointer ${
                activeTab === 'singbox'
                  ? 'bg-white/[0.08] text-white border border-white/[0.15] shadow-sm'
                  : 'text-zinc-500 hover:text-zinc-300 hover:bg-white/[0.02]'
              }`}
            >
              <Terminal className={`size-3.5 ${activeTab === 'singbox' ? 'text-[#ff5722]' : 'text-zinc-600'}`} />
              <span>sing-box.stdout</span>
            </button>

            <button
              onClick={() => setActiveTab('sbm')}
              className={`flex items-center gap-2 px-3 py-1.5 rounded-[3px] font-mono text-xs tracking-wider uppercase transition-all cursor-pointer ${
                activeTab === 'sbm'
                  ? 'bg-white/[0.08] text-white border border-white/[0.15] shadow-sm'
                  : 'text-zinc-500 hover:text-zinc-300 hover:bg-white/[0.02]'
              }`}
            >
              <Server className={`size-3.5 ${activeTab === 'sbm' ? 'text-[#ff5722]' : 'text-zinc-600'}`} />
              <span>sbm-daemon.log</span>
            </button>
          </div>

          {/* 右侧终端快捷搜索过滤 */}
          <div className="flex items-center gap-3">
            <div className="relative flex items-center">
              <Search className="size-3 text-zinc-500 absolute left-2.5 pointer-events-none" />
              <input
                type="text"
                placeholder="过滤日志关键字..."
                value={searchFilter}
                onChange={(e) => setSearchFilter(e.target.value)}
                className="w-44 sm:w-56 h-7 pl-7 pr-2.5 text-xs font-mono bg-black/60 text-zinc-200 border border-white/[0.08] rounded-[2px] focus:outline-none focus:border-[#ff5722] transition-colors placeholder:text-zinc-600"
              />
              {searchFilter && (
                <button
                  onClick={() => setSearchFilter('')}
                  className="absolute right-2 text-zinc-500 hover:text-white text-xs font-mono cursor-pointer"
                >
                  &times;
                </button>
              )}
            </div>

            <button
              onClick={scrollToBottom}
              title="滚动到底部"
              className="p-1 rounded-[2px] bg-white/[0.05] border border-white/[0.08] text-zinc-400 hover:text-white cursor-pointer transition-colors"
            >
              <ArrowDown className="size-3.5" />
            </button>
          </div>
        </div>

        {/* 终端主体输出流 */}
        <div
          ref={logContainerRef}
          className="min-h-0 flex-1 p-4 overflow-auto font-mono text-xs leading-relaxed bg-[#050608] text-zinc-300 selection:bg-[#ff5722] selection:text-white"
        >
          {filteredLogs.length === 0 ? (
            <div className="h-full flex flex-col items-center justify-center font-mono text-xs text-zinc-600 space-y-2">
              <Terminal className="size-8 text-zinc-700 stroke-[1.5]" />
              <p>{searchFilter ? `未找到匹配 "${searchFilter}" 的日志行` : '暂无日志输出或已清空'}</p>
            </div>
          ) : (
            <div className="space-y-0.5">
              {filteredLogs.map((line, index) => (
                <div
                  key={index}
                  className="group flex items-start gap-3 hover:bg-white/[0.03] px-2 py-0.5 rounded-[2px] transition-colors"
                >
                  <span className="w-10 shrink-0 text-right text-[10px] text-zinc-700 select-none group-hover:text-zinc-500 font-mono">
                    {(index + 1).toString().padStart(3, '0')}
                  </span>
                  <div className="flex-1 whitespace-pre-wrap break-all">
                    {formatLogLine(line)}
                  </div>
                </div>
              ))}
              <div className="pt-2 pl-12 flex items-center gap-2">
                <span className="terminal-cursor"></span>
                <span className="text-[10px] font-mono text-zinc-600 uppercase">
                  READY // LISTENING ON TAIL
                </span>
              </div>
            </div>
          )}
        </div>

        {/* 终端底栏状态 */}
        <div className="min-h-8 shrink-0 px-4 py-1 border-t border-white/[0.08] bg-[#0c0d13] flex flex-wrap gap-2 items-center justify-between text-[11px] font-mono text-zinc-500 select-none">
          <div className="flex items-center gap-4">
            <span>
              TOTAL LINES: <span className="text-zinc-300 font-semibold">{filteredLogs.length}</span>
              {searchFilter && <span className="text-zinc-500"> (FILTERED FROM {currentLogs.length})</span>}
            </span>
            <span className="hidden sm:inline text-zinc-700">|</span>
            <span className="hidden sm:inline">BUFFER: 500 ENTRIES</span>
          </div>

          <div className="flex items-center gap-2">
            <span className="size-1.5 rounded-full bg-[#ff5722]"></span>
            <span>{autoRefresh ? 'AUTO-SYNC ACTIVE (5s)' : 'SYNC PAUSED'}</span>
          </div>
        </div>
      </div>
    </div>
  );
}
