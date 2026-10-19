import { useCallback, useEffect, useState } from 'react';
import type { FormEvent } from 'react';
import { Button, Checkbox, Input } from '@nextui-org/react';
import { Copy } from 'lucide-react';
import { agentApi, type AgentChangeset, type AgentConnection as Connection, type AgentScope, type AgentToken, type AuditEntry } from '../api/agent';
import { errorMessage } from '../api';
import { toast } from '../components/Toast';
import useVisibleInterval from '../hooks/useVisibleInterval';
import ConfirmModal from '../components/ConfirmModal';
import AgentConnection, { DeviceTag } from '../components/AgentConnection';

const scopeLabels: Record<AgentScope, string> = {
  read: '只读：状态、差异、日志、DNS、解释、审计',
  runtime: '运行态：切换节点、测延迟',
  write: '编辑：创建并校验变更集',
  deploy: '部署：应用、确认、回滚变更集',
};

const stateLabels: Record<AgentChangeset['state'], string> = {
  checked: '已校验未应用',
  applying: '应用中',
  pending_confirm: '等待确认',
  rolling_back: '回滚中',
  committed: '已确认',
  rolled_back: '已回滚',
  failed: '应用失败已恢复',
  recovery_required: '需人工处理',
  expired: '已过期',
  resolved: '已人工处理',
};

const stateTone: Record<AgentChangeset['state'], string> = {
  checked: 'text-zinc-500 border-zinc-300 dark:border-white/10',
  applying: 'text-warning border-warning/40',
  pending_confirm: 'text-warning border-warning/40',
  rolling_back: 'text-warning border-warning/40',
  committed: 'text-success border-success/40',
  rolled_back: 'text-zinc-500 border-zinc-300 dark:border-white/10',
  failed: 'text-danger border-danger/40',
  recovery_required: 'text-danger border-danger/40',
  expired: 'text-zinc-400 border-zinc-300 dark:border-white/10',
  resolved: 'text-zinc-500 border-zinc-300 dark:border-white/10',
};

const sectionClass = 'rounded border border-zinc-200 bg-white p-5 dark:border-white/10 dark:bg-zinc-950';

function formatTime(value?: string) {
  return value ? new Date(value).toLocaleString() : '—';
}

async function copy(text: string) {
  try {
    if (navigator.clipboard) {
      await navigator.clipboard.writeText(text);
    } else {
      // LAN HTTP 页面没有 Clipboard API，保留用户点击触发的兼容复制。
      const input = document.createElement('textarea');
      input.value = text;
      input.style.position = 'fixed';
      input.style.opacity = '0';
      document.body.appendChild(input);
      try {
        input.select();
        if (!document.execCommand('copy')) throw new Error('clipboard unavailable');
      } finally { input.remove(); }
    }
    toast.success('已复制');
  } catch { toast.error('复制失败，请手动选择并复制'); }
}

export default function Agent() {
  const [tokens, setTokens] = useState<AgentToken[]>([]);
  const [changesets, setChangesets] = useState<AgentChangeset[]>([]);
  const [audit, setAudit] = useState<AuditEntry[]>([]);
  const [scopes, setScopes] = useState<AgentScope[]>(['read']);
  const [created, setCreated] = useState<{ token: string; name: string } | null>(null);
  const [busy, setBusy] = useState(false);
  const [revoking, setRevoking] = useState<AgentToken | null>(null);
  const [resolveNote, setResolveNote] = useState('');
  const [connection, setConnection] = useState<Connection | null>(null);
  const [connectionError, setConnectionError] = useState('');
  const [connectionAttempt, setConnectionAttempt] = useState(0);
  const [revertPreview, setRevertPreview] = useState<(AgentChangeset & { apply_ready?: boolean; blocked_reason?: string }) | null>(null);

  const startRevert = async (id: string) => {
    setBusy(true);
    try {
      const response = await agentApi.revert(id);
      setRevertPreview(response.data.data);
    } catch (error) {
      toast.error(errorMessage(error, '无法撤销'));
    } finally {
      setBusy(false);
      void load();
    }
  };

  const load = useCallback(async () => {
    try {
      const [t, c, a] = await Promise.all([agentApi.tokens(), agentApi.changesets(), agentApi.audit()]);
      setTokens(t.data.data);
      setChangesets(c.data.data);
      setAudit(a.data.data);
    } catch (error) {
      toast.error(errorMessage(error, '读取 Agent 信息失败'));
    }
  }, []);

  useEffect(() => {
    let active = true;
    void agentApi.connection().then(
      (response) => {
        if (active) { setConnection(response.data.data); setConnectionError(''); }
      },
      (reason) => { if (active) setConnectionError(errorMessage(reason, '读取接入信息失败')); },
    );
    return () => { active = false; };
  }, [connectionAttempt]);

  useEffect(() => { void load(); }, [load]);
  useVisibleInterval(() => { void load(); }, 10000);

  const createToken = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const form = event.currentTarget;
    const data = new FormData(form);
    const name = String(data.get('name') || '').trim();
    const days = Number(data.get('days') || 30);
    const cidrs = String(data.get('cidrs') || '').split(/[\s,]+/).filter(Boolean);
    setBusy(true);
    try {
      const response = await agentApi.createToken({ name, scopes, days, cidrs });
      setCreated({ token: response.data.token, name });
      form.reset();
      void load();
    } catch (error) {
      toast.error(errorMessage(error, '创建令牌失败'));
    } finally {
      setBusy(false);
    }
  };

  const act = async (action: () => Promise<unknown>, message: string) => {
    setBusy(true);
    try {
      await action();
      toast.success(message);
    } catch (error) {
      toast.error(errorMessage(error));
    } finally {
      setBusy(false);
      void load();
    }
  };

  const toggleScope = (scope: AgentScope, on: boolean) =>
    setScopes((current) => (on ? [...new Set([...current, scope])] : current.filter((s) => s !== scope)));

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-xl font-semibold text-zinc-900 dark:text-zinc-100">Agent 接入</h1>
        <p className="mt-1 text-sm text-zinc-500">
          为 AI Agent 或脚本签发受限令牌。Agent 只能通过变更集修改规则、DNS、订阅/节点、过滤器和普通设备策略，必须预览、应用并在确认窗口内确认；网关拓扑、内核、端口与路径不对 Agent 开放。
        </p>
        <p className="mt-2 rounded border border-warning/30 bg-warning/5 px-3 py-2 text-xs text-zinc-600 dark:text-zinc-400">
          自动回滚由管理器进程计时：管理器运行期间到期未确认会自动回滚；管理器重启时会在开放接口前优先恢复未确认变更。管理器崩溃或停机期间没有独立看门狗，不保证准时回滚。
        </p>
      </div>

      <section className={sectionClass}>
        <div className="flex flex-wrap items-center gap-2">
          <h2 className="font-semibold text-zinc-900 dark:text-zinc-100">第 1 步 · 创建令牌</h2>
          <DeviceTag>本页（浏览器）</DeviceTag>
        </div>
        <form onSubmit={createToken} className="mt-4 space-y-4">
          <div className="grid gap-4 sm:grid-cols-3">
            <Input name="name" label="名称" placeholder="例如 claude-on-laptop" isRequired maxLength={64} isDisabled={busy} />
            <Input name="days" label="有效期（天）" type="number" min={1} max={90} defaultValue="30" isRequired isDisabled={busy} />
            <Input key={connection?.suggested_cidrs.join(',')} name="cidrs" label="来源网段（可选）" placeholder="127.0.0.1/32, 192.0.2.0/24" defaultValue={connection?.suggested_cidrs.join(', ')} isDisabled={busy} />
          </div>
          <div className="grid gap-2 sm:grid-cols-2">
            {(Object.keys(scopeLabels) as AgentScope[]).map((scope) => (
              <Checkbox key={scope} isSelected={scopes.includes(scope)} onValueChange={(on) => toggleScope(scope, on)} isDisabled={busy}>
                <span className="text-sm"><b className="font-mono">{scope}</b> · {scopeLabels[scope]}</span>
              </Checkbox>
            ))}
          </div>
          <p className="text-xs text-zinc-500">只授予需要的权限。来源网段已按管理器允许的局域网预填，限制只有这些地址的电脑能使用令牌；通过 SSH 隧道访问时来源地址为 127.0.0.1。</p>
          <Button type="submit" size="sm" color="primary" isLoading={busy} isDisabled={scopes.length === 0}>创建令牌</Button>
        </form>
        {created && (
          <div className="mt-4 rounded border border-success/40 bg-success/5 p-3">
            <p className="text-sm font-semibold text-zinc-900 dark:text-zinc-100">令牌「{created.name}」只显示这一次，复制后在第 2 步中粘贴</p>
            <div className="mt-2 flex items-center gap-2">
              <code className="min-w-0 flex-1 break-all rounded bg-zinc-100 px-2 py-1 font-mono text-xs dark:bg-black/40">{created.token}</code>
              <Button size="sm" variant="flat" isIconOnly aria-label="复制令牌" onPress={() => copy(created.token)}><Copy className="size-4" /></Button>
            </div>
            <Button size="sm" variant="light" className="mt-2" onPress={() => setCreated(null)}>我已保存</Button>
          </div>
        )}
      </section>

      <AgentConnection connection={connection} error={connectionError} onRetry={() => setConnectionAttempt((value) => value + 1)} onCopy={copy} />

      <section className={sectionClass}>
        <h2 className="font-semibold text-zinc-900 dark:text-zinc-100">令牌</h2>
        {tokens.length === 0 ? <p className="mt-3 text-sm text-zinc-500">尚未创建令牌。</p> : (
          <div className="mt-3 overflow-x-auto">
            <table className="w-full min-w-[640px] text-left text-sm">
              <thead className="text-xs text-zinc-500"><tr><th className="py-2">名称</th><th>权限</th><th>状态</th><th>到期</th><th>最近使用</th><th /></tr></thead>
              <tbody>
                {tokens.map((t) => (
                  <tr key={t.id} className="border-t border-zinc-100 dark:border-white/5">
                    <td className="py-2"><span className="font-medium">{t.name}</span> <span className="font-mono text-xs text-zinc-400">#{t.id}</span>{t.cidrs.length > 0 && <div className="font-mono text-xs text-zinc-500">{t.cidrs.join(', ')}</div>}</td>
                    <td className="font-mono text-xs">{t.scopes.join(' ')}</td>
                    <td className={t.status === 'active' ? 'text-success' : 'text-zinc-400'}>{t.status === 'active' ? '有效' : t.status === 'revoked' ? '已吊销' : '已过期'}</td>
                    <td className="text-xs">{formatTime(t.expires_at)}</td>
                    <td className="text-xs">{formatTime(t.last_used_at)}</td>
                    <td className="text-right">{t.status === 'active' && <Button size="sm" variant="light" color="danger" onPress={() => setRevoking(t)}>吊销</Button>}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
        <p className="mt-2 text-xs text-zinc-500">最近使用时间只保存在内存中，管理器重启后清空。</p>
      </section>

      <section className={sectionClass}>
        <h2 className="font-semibold text-zinc-900 dark:text-zinc-100">变更历史</h2>
        <p className="mt-1 text-xs text-zinc-500">已确认 = 已写入运行配置并保留；运行配置只在内核运行时影响流量。是否有真实流量经过，请看连接页或 DNS 查询记录。</p>
        {changesets.length === 0 ? <p className="mt-3 text-sm text-zinc-500">暂无 Agent 变更。</p> : (
          <ul className="mt-3 space-y-3">
            {changesets.map((cs) => (
              <li key={cs.id} className="rounded border border-zinc-200 p-3 dark:border-white/10">
                <div className="flex flex-wrap items-center gap-2 text-sm">
                  <span className={`rounded border px-1.5 py-0.5 text-xs ${stateTone[cs.state]}`}>{stateLabels[cs.state]}</span>
                  <span className="font-medium">{cs.title || cs.changes[0]}</span>
                  <span className="ml-auto font-mono text-xs text-zinc-400">{cs.actor} · {formatTime(cs.created_at)}</span>
                </div>
                <ul className="mt-2 list-disc pl-5 text-xs text-zinc-600 dark:text-zinc-400">
                  {cs.changes.map((c, i) => <li key={i}>{c}</li>)}
                </ul>
                {cs.config_changes.length > 0 && (
                  <details className="mt-2 text-xs">
                    <summary className="cursor-pointer text-zinc-500">运行配置差异（{cs.config_changes.length}）</summary>
                    <ul className="mt-1 list-disc pl-5 font-mono text-zinc-500">{cs.config_changes.map((c, i) => <li key={i}>{c}</li>)}</ul>
                  </details>
                )}
                {cs.error && <p className="mt-2 text-xs text-danger">{cs.error}</p>}
                {cs.resolution && <p className="mt-2 text-xs text-zinc-500">{cs.resolution}</p>}
                {cs.state === 'pending_confirm' && (
                  <div className="mt-3 flex gap-2">
                    <Button size="sm" color="primary" isDisabled={busy} onPress={() => act(() => agentApi.confirm(cs), '已确认')}>确认保留</Button>
                    <Button size="sm" variant="flat" color="danger" isDisabled={busy} onPress={() => act(() => agentApi.rollback(cs.id), '已回滚')}>立即回滚</Button>
                  </div>
                )}
                {cs.state === 'committed' && (
                  <div className="mt-3">
                    <Button size="sm" variant="flat" isDisabled={busy} onPress={() => startRevert(cs.id)}>撤销此变更</Button>
                  </div>
                )}
                {cs.state === 'recovery_required' && (
                  <div className="mt-3 flex flex-wrap items-end gap-2">
                    <Input size="sm" className="max-w-md" label="人工核对说明" value={resolveNote} onValueChange={setResolveNote} />
                    <Button size="sm" color="danger" variant="flat" isDisabled={busy || !resolveNote.trim()} onPress={() => act(() => agentApi.resolve(cs.id, resolveNote), '已标记为已处理')}>已核对，解除写入暂停</Button>
                  </div>
                )}
              </li>
            ))}
          </ul>
        )}
      </section>

      <section className={sectionClass}>
        <details>
          <summary className="cursor-pointer font-semibold text-zinc-900 dark:text-zinc-100">审计日志（{audit.length}）</summary>
          <div className="mt-3 overflow-x-auto">
            <table className="w-full min-w-[640px] text-left text-xs">
              <thead className="text-zinc-500"><tr><th className="py-2">时间</th><th>操作者</th><th>动作</th><th>结果</th><th>说明</th></tr></thead>
              <tbody>
                {audit.map((a, i) => (
                  <tr key={i} className="border-t border-zinc-100 align-top dark:border-white/5">
                    <td className="whitespace-nowrap py-1.5 pr-3">{formatTime(a.at)}</td>
                    <td className="pr-3 font-mono">{a.actor}</td>
                    <td className="pr-3 font-mono">{a.action}</td>
                    <td className="pr-3">{a.result}</td>
                    <td className="break-all text-zinc-500">{a.detail}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </details>
      </section>

      <ConfirmModal
        title="撤销已确认的变更"
        isOpen={revertPreview !== null}
        busy={busy}
        onClose={() => setRevertPreview(null)}
        confirmLabel="应用撤销"
        onConfirm={() => {
          if (!revertPreview) return;
          if (revertPreview.apply_ready === false) {
            toast.error(revertPreview.blocked_reason || '当前不能应用撤销');
            return;
          }
          void act(() => agentApi.apply(revertPreview.id), '撤销已应用，请在确认窗口内检查并确认').then(() => setRevertPreview(null));
        }}
      >
        <p>将恢复到该变更应用前的状态。应用后同样进入确认窗口，确认前可随时回滚。</p>
        {revertPreview?.config_changes.length ? (
          <ul className="mt-2 list-disc pl-5 font-mono">{revertPreview.config_changes.map((c, i) => <li key={i}>{c}</li>)}</ul>
        ) : <p className="mt-2">运行配置无差异，只恢复已保存数据。</p>}
        {revertPreview?.warnings.map((w, i) => <p key={i} className="mt-2 text-warning">{w}</p>)}
      </ConfirmModal>

      <ConfirmModal
        title="吊销令牌"
        isOpen={revoking !== null}
        busy={busy}
        onClose={() => setRevoking(null)}
        confirmLabel="吊销"
        onConfirm={() => revoking && act(() => agentApi.revokeToken(revoking.id), '令牌已吊销').then(() => setRevoking(null))}
      >
        吊销后使用「{revoking?.name}」的 Agent 立即失去访问权限，无法恢复。
      </ConfirmModal>
    </div>
  );
}
