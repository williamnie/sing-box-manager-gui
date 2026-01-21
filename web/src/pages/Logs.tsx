import { useEffect, useState, useRef, useMemo, useDeferredValue } from 'react';
import { Card, CardBody, CardHeader, Button, Tabs, Tab, Switch, Input } from '@nextui-org/react';
import { RefreshCw, Trash2, Terminal, Server, Pause, Play, Search, X, Download } from 'lucide-react';
import { monitorApi } from '../api';

type LogType = 'sbm' | 'singbox';
type LogLevel = 'trace' | 'debug' | 'info' | 'warn' | 'error' | 'fatal';

interface ParsedLogLine {
  raw: string;
  timestamp?: string;
  level?: LogLevel;
  source?: string;
  message: string;
}

const LOG_TIMESTAMP_REGEX = /^(\+\d{4}\s+)?(\d{4}[-/]\d{2}[-/]\d{2}[T\s]\d{2}:\d{2}:\d{2})/;
const LOG_LEVEL_REGEX = /\b(TRACE|DEBUG|INFO|WARN|WARNING|ERROR|FATAL)\b/i;
const LOG_SOURCE_REGEX = /\[([^\]]+)\]/;

const extractLogLevel = (value: string): LogLevel | undefined => {
  const normalized = value.trim().toLowerCase();
  if (normalized === 'warning' || normalized === 'warn') return 'warn';
  if (normalized === 'info') return 'info';
  if (normalized === 'error') return 'error';
  if (normalized === 'fatal') return 'fatal';
  if (normalized === 'debug') return 'debug';
  if (normalized === 'trace') return 'trace';
  return undefined;
};

const parseLogLine = (raw: string): ParsedLogLine => {
  let remaining = raw.trim();
  let timestamp: string | undefined;
  let level: LogLevel | undefined;
  let source: string | undefined;

  // 提取时间戳
  const tsMatch = remaining.match(LOG_TIMESTAMP_REGEX);
  if (tsMatch) {
    timestamp = tsMatch[2];
    remaining = remaining.slice(tsMatch[0].length).trim();
  }

  // 提取日志级别
  const lvlMatch = remaining.match(LOG_LEVEL_REGEX);
  if (lvlMatch) {
    level = extractLogLevel(lvlMatch[1]);
  }

  // 提取来源 [xxx]
  const sourceMatch = remaining.match(LOG_SOURCE_REGEX);
  if (sourceMatch) {
    source = sourceMatch[1];
    remaining = remaining.replace(sourceMatch[0], '').trim();
  }

  // 移除已匹配的级别关键字
  if (lvlMatch) {
    remaining = remaining.replace(lvlMatch[0], '').trim();
  }

  return {
    raw,
    timestamp,
    level,
    source,
    message: remaining,
  };
};

const INITIAL_DISPLAY_LINES = 200;
const LOAD_MORE_LINES = 200;

export default function Logs() {
  const [activeTab, setActiveTab] = useState<LogType>('singbox');
  const [sbmLogs, setSbmLogs] = useState<string[]>([]);
  const [singboxLogs, setSingboxLogs] = useState<string[]>([]);
  const [loading, setLoading] = useState(false);
  const [autoRefresh, setAutoRefresh] = useState(true);
  const [searchQuery, setSearchQuery] = useState('');
  const [visibleLines, setVisibleLines] = useState(INITIAL_DISPLAY_LINES);
  const deferredSearch = useDeferredValue(searchQuery);
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
      console.error('获取日志失败:', error);
    } finally {
      setLoading(false);
    }
  };

  const fetchCurrentLogs = () => {
    fetchLogs(activeTab);
  };

  const scrollToBottom = () => {
    if (logContainerRef.current) {
      logContainerRef.current.scrollTop = logContainerRef.current.scrollHeight;
    }
  };

  useEffect(() => {
    fetchLogs('sbm');
    fetchLogs('singbox');
  }, []);

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

  useEffect(() => {
    scrollToBottom();
  }, [sbmLogs, singboxLogs, activeTab]);

  // 切换标签时重置可见行数
  useEffect(() => {
    setVisibleLines(INITIAL_DISPLAY_LINES);
  }, [activeTab]);

  const handleClear = () => {
    if (activeTab === 'sbm') {
      setSbmLogs([]);
    } else {
      setSingboxLogs([]);
    }
  };

  const handleDownload = () => {
    const logs = activeTab === 'sbm' ? sbmLogs : singboxLogs;
    const text = logs.join('\n');
    const blob = new Blob([text], { type: 'text/plain' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `${activeTab}-logs.txt`;
    a.click();
    URL.revokeObjectURL(url);
  };

  const handleScroll = () => {
    const container = logContainerRef.current;
    if (!container) return;
    
    // 滚动到顶部时加载更多
    if (container.scrollTop < 50 && visibleLines < currentLogs.length) {
      const prevScrollHeight = container.scrollHeight;
      setVisibleLines(prev => Math.min(prev + LOAD_MORE_LINES, currentLogs.length));
      // 保持滚动位置
      requestAnimationFrame(() => {
        container.scrollTop = container.scrollHeight - prevScrollHeight;
      });
    }
  };

  const currentLogs = activeTab === 'sbm' ? sbmLogs : singboxLogs;

  // 过滤和解析日志
  const { filteredLogs, parsedLogs } = useMemo(() => {
    let logs = currentLogs;
    
    // 搜索过滤
    if (deferredSearch.trim()) {
      const query = deferredSearch.toLowerCase();
      logs = logs.filter(line => line.toLowerCase().includes(query));
    }

    // 只显示最后 visibleLines 行
    const displayLogs = logs.slice(-visibleLines);
    const parsed = displayLogs.map(line => parseLogLine(line));

    return { filteredLogs: logs, parsedLogs: parsed };
  }, [currentLogs, deferredSearch, visibleLines]);

  const levelColors: Record<LogLevel, string> = {
    trace: 'bg-gray-500',
    debug: 'bg-gray-600',
    info: 'bg-blue-500',
    warn: 'bg-yellow-500',
    error: 'bg-red-500',
    fatal: 'bg-red-700',
  };

  const levelTextColors: Record<LogLevel, string> = {
    trace: 'text-gray-400',
    debug: 'text-gray-400',
    info: 'text-blue-400',
    warn: 'text-yellow-400',
    error: 'text-red-400',
    fatal: 'text-red-500',
  };

  return (
    <div className="h-full flex flex-col gap-4">
      <div className="flex justify-between items-center shrink-0">
        <h1 className="text-2xl font-bold text-gray-800 dark:text-white">日志</h1>
        <div className="flex items-center gap-4">
          <div className="flex items-center gap-2">
            {autoRefresh ? (
              <Pause className="w-4 h-4 text-gray-500" />
            ) : (
              <Play className="w-4 h-4 text-gray-500" />
            )}
            <span className="text-sm text-gray-500 dark:text-gray-400">自动刷新</span>
            <Switch
              size="sm"
              isSelected={autoRefresh}
              onValueChange={setAutoRefresh}
            />
          </div>
          <Button
            size="sm"
            color="primary"
            variant="flat"
            startContent={<RefreshCw className={`w-4 h-4 ${loading ? 'animate-spin' : ''}`} />}
            onPress={fetchCurrentLogs}
            isDisabled={loading}
          >
            刷新
          </Button>
          <Button
            size="sm"
            variant="flat"
            startContent={<Download className="w-4 h-4" />}
            onPress={handleDownload}
            isDisabled={currentLogs.length === 0}
          >
            下载
          </Button>
          <Button
            size="sm"
            color="danger"
            variant="flat"
            startContent={<Trash2 className="w-4 h-4" />}
            onPress={handleClear}
          >
            清空
          </Button>
        </div>
      </div>

      <Card className="flex-1 flex flex-col min-h-0">
        <CardHeader className="pb-0 flex-col items-start gap-3 shrink-0">
          <Tabs
            selectedKey={activeTab}
            onSelectionChange={(key) => setActiveTab(key as LogType)}
            aria-label="日志类型"
          >
            <Tab
              key="singbox"
              title={
                <div className="flex items-center gap-2">
                  <Terminal className="w-4 h-4" />
                  <span>sing-box 日志</span>
                </div>
              }
            />
            <Tab
              key="sbm"
              title={
                <div className="flex items-center gap-2">
                  <Server className="w-4 h-4" />
                  <span>应用日志</span>
                </div>
              }
            />
          </Tabs>
          <Input
            placeholder="搜索日志..."
            value={searchQuery}
            onValueChange={setSearchQuery}
            startContent={<Search className="w-4 h-4 text-gray-400" />}
            endContent={
              searchQuery && (
                <button onClick={() => setSearchQuery('')}>
                  <X className="w-4 h-4 text-gray-400 hover:text-gray-600" />
                </button>
              )
            }
            size="sm"
            className="max-w-xs"
          />
        </CardHeader>
        <CardBody className="flex-1 flex flex-col min-h-0">
          <div
            ref={logContainerRef}
            onScroll={handleScroll}
            className="bg-gray-900 dark:bg-gray-950 rounded-lg p-4 flex-1 min-h-0 overflow-auto font-mono text-sm"
          >
            {parsedLogs.length === 0 ? (
              <div className="text-gray-500 text-center py-8">
                {deferredSearch ? '未找到匹配的日志' : '暂无日志'}
              </div>
            ) : (
              <>
                {visibleLines < filteredLogs.length && (
                  <div className="text-center text-gray-500 text-xs py-2 border-b border-gray-700 mb-2">
                    向上滚动加载更多 · 还有 {filteredLogs.length - visibleLines} 行
                  </div>
                )}
                {parsedLogs.map((line, index) => (
                  <div
                    key={index}
                    className={`py-1 border-b border-gray-800/50 hover:bg-gray-800/30 flex flex-wrap items-start gap-2 ${
                      line.level === 'error' || line.level === 'fatal'
                        ? 'bg-red-900/10'
                        : line.level === 'warn'
                        ? 'bg-yellow-900/10'
                        : ''
                    }`}
                    onDoubleClick={() => {
                      navigator.clipboard.writeText(line.raw);
                    }}
                    title="双击复制"
                  >
                    {/* 时间戳 */}
                    {line.timestamp && (
                      <span className="text-gray-500 text-xs shrink-0">
                        {line.timestamp}
                      </span>
                    )}

                    {/* 日志级别 */}
                    {line.level && (
                      <span
                        className={`text-xs px-1.5 py-0.5 rounded font-medium shrink-0 ${levelColors[line.level]} text-white`}
                      >
                        {line.level.toUpperCase()}
                      </span>
                    )}

                    {/* 来源 */}
                    {line.source && (
                      <span className="text-cyan-400 text-xs shrink-0">
                        [{line.source}]
                      </span>
                    )}

                    {/* 消息内容 */}
                    <span
                      className={`flex-1 break-all ${
                        line.level ? levelTextColors[line.level] : 'text-gray-300'
                      }`}
                    >
                      {line.message}
                    </span>
                  </div>
                ))}
              </>
            )}
          </div>
          <div className="mt-2 text-sm text-gray-500 dark:text-gray-400 flex justify-between">
            <span>
              {deferredSearch
                ? `匹配 ${filteredLogs.length} 行 / 共 ${currentLogs.length} 行`
                : `显示 ${parsedLogs.length} 行 / 共 ${currentLogs.length} 行`}
            </span>
            <span>
              {autoRefresh ? '每 5 秒自动刷新' : '自动刷新已暂停'}
            </span>
          </div>
        </CardBody>
      </Card>
    </div>
  );
}
