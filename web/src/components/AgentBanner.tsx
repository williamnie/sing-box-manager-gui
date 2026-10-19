import { useCallback, useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { Button } from '@nextui-org/react';
import { Bot } from 'lucide-react';
import { agentApi, type AgentChangeset } from '../api/agent';
import { errorMessage } from '../api';
import { toast } from './Toast';
import useVisibleInterval from '../hooks/useVisibleInterval';

// 确认窗口期间其他配置写入会被拒绝，因此在所有页面顶部提示并提供确认/回滚入口。
export default function AgentBanner() {
  const [active, setActive] = useState<AgentChangeset | null>(null);
  const [journalError, setJournalError] = useState('');
  const [now, setNow] = useState(() => performance.now());
  const [countdown, setCountdown] = useState({ remainingMs: 0, sampledAt: 0 });
  const [busy, setBusy] = useState(false);

  const refresh = useCallback(async () => {
    try {
      const requestedAt = performance.now();
      const response = await agentApi.state();
      const sampledAt = performance.now();
      setActive(response.data.data.active);
      setJournalError(response.data.data.journal_error);
      // 使用服务端剩余时长和本地单调时钟；保守扣除请求耗时，不依赖两端墙钟同步。
      setCountdown({
        remainingMs: Math.max(0, response.data.data.remaining_ms - (sampledAt - requestedAt)),
        sampledAt,
      });
      setNow(sampledAt);
    } catch {
      // 状态轮询失败不打断页面；下一轮重试。
    }
  }, []);

  useEffect(() => { void refresh(); }, [refresh]);
  useVisibleInterval(() => { void refresh(); }, 5000);
  // 只有存在待确认变更时才需要每秒更新倒计时。
  useVisibleInterval(() => setNow(performance.now()), 1000, active !== null);

  if (journalError) {
    return (
      <div role="alert" className="mb-4 rounded border border-danger/40 bg-danger/10 px-4 py-3 text-xs text-danger">
        Agent 变更日志无法读取，已暂停配置写入：{journalError}
      </div>
    );
  }
  if (!active) return null;

  const run = async (action: () => Promise<unknown>, success: string) => {
    setBusy(true);
    try {
      await action();
      toast.success(success);
    } catch (error) {
      toast.error(errorMessage(error));
    } finally {
      setBusy(false);
      void refresh();
    }
  };

  const recovery = active.state === 'recovery_required';
  const rolling = active.state === 'rolling_back';
  const remaining = Math.max(0, Math.ceil((countdown.remainingMs - Math.max(0, now - countdown.sampledAt)) / 1000));

  return (
    <div role="status" className={`mb-4 rounded border px-4 py-3 text-xs ${recovery ? 'border-danger/40 bg-danger/10' : 'border-warning/40 bg-warning/10'}`}>
      <div className="flex flex-wrap items-center gap-3">
        <Bot className="size-4 shrink-0" />
        <div className="min-w-0 flex-1">
          <p className="font-semibold text-zinc-900 dark:text-zinc-100">
            {recovery ? 'Agent 变更需要人工处理' : rolling ? 'Agent 变更正在回滚' : `Agent 变更等待确认 · 剩余 ${remaining} 秒`}
            <span className="ml-2 font-mono font-normal text-zinc-500">{active.actor}</span>
          </p>
          <p className="mt-0.5 truncate text-zinc-600 dark:text-zinc-400">
            {recovery ? active.error : (active.title || active.changes.join('；'))}
          </p>
          <p className="mt-0.5 text-zinc-500">
            {recovery ? '处理前暂停其他配置写入。' : '确认或回滚前暂停其他配置与运行态写入；到期未确认将自动回滚（管理器停机期间不保证准时）。'}
          </p>
        </div>
        {recovery ? (
          <Button as={Link} to="/agent" size="sm" variant="flat">查看并处理</Button>
        ) : rolling ? null : (
          <div className="flex gap-2">
            <Button size="sm" color="primary" isDisabled={busy || remaining === 0} onPress={() => run(() => agentApi.confirm(active), '已确认 Agent 变更')}>确认保留</Button>
            <Button size="sm" variant="flat" color="danger" isDisabled={busy} onPress={() => run(() => agentApi.rollback(active.id), '已回滚 Agent 变更')}>立即回滚</Button>
          </div>
        )}
      </div>
    </div>
  );
}
