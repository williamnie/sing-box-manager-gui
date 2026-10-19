import type { ReactNode } from 'react';
import { Button } from '@nextui-org/react';
import { Copy } from 'lucide-react';
import type { AgentConnection as Connection } from '../api/agent';
import { agentConnectCommand, agentSetupCommand } from '../utils/agentConnection';

// 每一步标明在哪台设备上执行，避免把 Agent 电脑上的命令拿到服务器上运行。
export function DeviceTag({ children }: { children: ReactNode }) {
  return <span className="rounded border border-primary/40 bg-primary/5 px-1.5 py-0.5 text-xs font-medium text-primary">{children}</span>;
}

function Command({ text, label, onCopy }: { text: string; label: string; onCopy: (text: string) => void }) {
  return (
    <div className="mt-2 flex items-start gap-2">
      <pre className="min-w-0 flex-1 whitespace-pre-wrap break-all rounded bg-zinc-100 p-3 font-mono text-xs dark:bg-black/40">{text}</pre>
      <Button size="sm" color="primary" variant="flat" isIconOnly aria-label={label} onPress={() => onCopy(text)}><Copy className="size-4" /></Button>
    </div>
  );
}

interface Props {
  connection: Connection | null;
  error: string;
  onRetry: () => void;
  onCopy: (text: string) => void;
}

export default function AgentConnection({ connection, error, onRetry, onCopy }: Props) {
  return (
    <section className="rounded border border-zinc-200 bg-white p-5 dark:border-white/10 dark:bg-zinc-950">
      <div className="flex flex-wrap items-center gap-2">
        <h2 className="font-semibold text-zinc-900 dark:text-zinc-100">第 2 步 · 在 Agent 电脑上执行一条命令</h2>
        <DeviceTag>Agent 电脑（例如 Mac mini）</DeviceTag>
      </div>
      <p className="mt-2 text-sm text-zinc-500">
        「Agent 电脑」指运行 Claude Code 或 Codex 的那台电脑，不是这台网关服务器，也不需要 SSH 登录服务器。
      </p>
      {error ? (
        <div className="mt-3 flex items-center gap-3 text-sm text-danger" role="alert">
          <span>{error}</span><Button size="sm" variant="flat" onPress={onRetry}>重试</Button>
        </div>
      ) : !connection ? <p className="mt-3 text-sm text-zinc-500">正在读取当前服务的接入信息…</p> : (
        <>
          {connection.local_only && (
            <div className="mt-3 rounded border border-warning/40 bg-warning/5 p-3 text-xs text-zinc-600 dark:text-zinc-400">
              <p className="font-semibold text-warning">当前是通过本机地址 {connection.server} 打开的页面</p>
              <p className="mt-1">下面的命令只能在这台服务器自己身上使用。Agent 在另一台电脑上时，先在<b>网关服务器</b>上开启局域网访问，再用 <code className="font-mono">http://服务器局域网IP:端口</code> 打开本页重新复制命令：</p>
              <pre className="mt-2 whitespace-pre-wrap break-all rounded bg-zinc-100 p-2 font-mono dark:bg-black/40">sudo scripts/install-gateway.sh /绝对路径/sbm /绝对路径/sbm-gateway --install --lan-http 服务器局域网IP</pre>
              <p className="mt-1">非安装脚本部署时，给 sbm 加上 <code className="font-mono">-listen 服务器局域网IP -allow-insecure-http -allow-network 局域网网段</code> 后重启。</p>
            </div>
          )}
          <p className="mt-4 text-sm text-zinc-600 dark:text-zinc-400">在 Agent 电脑的终端（macOS 打开「终端」App）粘贴执行：</p>
          <Command text={agentSetupCommand(connection)} label="复制一键接入命令" onCopy={onCopy} />
          <ul className="mt-3 list-disc space-y-1 pl-5 text-xs text-zinc-500">
            <li>脚本从这台管理器下载同版本的 <code className="font-mono">sbm</code> 到 <code className="font-mono">~/.local/bin</code>，不经过 GitHub；再把 Skill 装进 Claude Code 和 Codex（<code className="font-mono">~/.claude/skills</code>、<code className="font-mono">~/.codex/skills</code>）。</li>
            <li>出现「粘贴管理页面创建的令牌」时，粘贴第 1 步的令牌并按回车。输入不会显示；验证成功后才保存到本机私有文件。</li>
            {connection.allow_insecure_http && <li className="text-warning">当前服务使用明文 HTTP，令牌会明文传输，仅在你信任的局域网使用；建议给令牌限制来源网段。</li>}
            {connection.pinned_pubkey && <li>当前服务是自签 HTTPS，命令已固定这台管理器的证书公钥，无需自己查找指纹。</li>}
          </ul>
          <details className="mt-3 text-xs text-zinc-500">
            <summary className="cursor-pointer">已经装好 sbm，或不想用一键脚本</summary>
            <p className="mt-2">只连接（同样在 Agent 电脑上执行）：</p>
            <Command text={agentConnectCommand(connection)} label="复制连接命令" onCopy={onCopy} />
            <p className="mt-2">只安装或更新 Skill：</p>
            <Command text="sbm ctl skill install" label="复制安装 Skill 命令" onCopy={onCopy} />
            {connection.fingerprint && <p className="mt-2 break-all font-mono">证书 SHA-256：{connection.fingerprint}</p>}
            <p className="mt-2">提示 unknown command 时，请重新执行一键命令更新 sbm。</p>
          </details>

          <div className="mt-6 flex flex-wrap items-center gap-2">
            <h2 className="font-semibold text-zinc-900 dark:text-zinc-100">第 3 步 · 验证</h2>
            <DeviceTag>Agent 电脑</DeviceTag>
          </div>
          <p className="mt-2 text-sm text-zinc-600 dark:text-zinc-400">
            重新打开 Claude Code 或 Codex，对它说「用 sbm 看看网关状态」。也可以在终端运行 <code className="font-mono text-zinc-900 dark:text-zinc-100">sbm ctl status</code>。
          </p>
        </>
      )}
    </section>
  );
}
