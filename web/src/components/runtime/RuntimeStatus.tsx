import { AlertCircle } from 'lucide-react';

export default function RuntimeStatus({ error, loading, hasData }: { error: string; loading: boolean; hasData: boolean }) {
  if (error) return <div role="status" className="flex items-start gap-2 rounded border border-amber-500/30 bg-amber-500/10 p-3 text-xs text-amber-800 dark:text-amber-300"><AlertCircle className="mt-0.5 size-4 shrink-0" /><div><p className="font-semibold">运行接口暂不可用</p><p className="mt-1 break-words">{error}</p>{hasData && <p className="mt-1">以下是上次读取的快照，恢复连接前无法执行操作。</p>}</div></div>;
  if (!hasData && loading) return <div role="status" className="p-8 text-center font-mono text-sm text-zinc-500">正在读取内核运行状态…</div>;
  return null;
}
