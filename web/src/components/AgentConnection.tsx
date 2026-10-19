import { useEffect, useState } from 'react';
import { Button } from '@nextui-org/react';
import { Copy } from 'lucide-react';
import { agentApi, type AgentConnection as Connection } from '../api/agent';
import { errorMessage } from '../api';
import { agentConnectCommand } from '../utils/agentConnection';

export default function AgentConnection({ onCopy }: { onCopy: (text: string) => void }) {
  const [connection, setConnection] = useState<Connection | null>(null);
  const [error, setError] = useState('');
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    let active = true;
    void agentApi.connection().then(
      (response) => {
        if (active) { setConnection(response.data.data); setError(''); }
      },
      (reason) => { if (active) setError(errorMessage(reason, '读取接入信息失败')); },
    );
    return () => { active = false; };
  }, [attempt]);

  return (
    <section className="rounded border border-zinc-200 bg-white p-5 dark:border-white/10 dark:bg-zinc-950">
      <h2 className="font-semibold text-zinc-900 dark:text-zinc-100">在 Agent 所在电脑上接入</h2>
      <p className="mt-2 text-sm text-zinc-500">
        下面的命令在运行 Agent 的电脑（例如 Mac mini）上执行。管理服务留在当前服务器，不需要 SSH 登录服务器。
      </p>
      {error ? (
        <div className="mt-3 flex items-center gap-3 text-sm text-danger" role="alert">
          <span>{error}</span><Button size="sm" variant="flat" onPress={() => setAttempt((value) => value + 1)}>重试</Button>
        </div>
      ) : !connection ? <p className="mt-3 text-sm text-zinc-500">正在读取当前服务的接入信息…</p> : (
        <>
          <div className="mt-4 flex flex-wrap items-center gap-3">
            <span className="font-mono text-sm text-zinc-800 dark:text-zinc-200">{connection.server}</span>
            <span className="text-xs text-zinc-500">{connection.fingerprint ? '证书验证已自动配置' : connection.allow_insecure_http ? '当前服务使用明文 HTTP' : '本机 HTTP 连接'}</span>
          </div>
          <ol className="mt-4 list-decimal space-y-3 pl-5 text-sm text-zinc-600 dark:text-zinc-400">
            <li>
              <div className="flex flex-wrap items-center gap-3">
                <span>复制接入命令，在 Agent 所在电脑的终端执行。</span>
                <Button size="sm" color="primary" startContent={<Copy className="size-4" />} onPress={() => onCopy(agentConnectCommand(connection))}>复制接入命令</Button>
              </div>
              <pre className="mt-2 whitespace-pre-wrap break-all rounded bg-zinc-100 p-3 font-mono text-xs dark:bg-black/40">{agentConnectCommand(connection)}</pre>
            </li>
            <li>按提示粘贴上方创建的令牌并按回车。输入不会显示；地址、证书验证信息和令牌会在连接验证成功后保存到本机私有文件。</li>
            <li>以后直接运行 <code className="font-mono text-zinc-900 dark:text-zinc-100">sbm ctl status</code>，不用重复填写地址和指纹。</li>
          </ol>
          {connection.allow_insecure_http && <p className="mt-3 text-xs text-warning">此命令沿用当前服务的 HTTP 设置，令牌会明文传输，仅用于你信任的网络。</p>}
          <details className="mt-4 text-xs text-zinc-500">
            <summary className="cursor-pointer">连接详情与旧版客户端</summary>
            {connection.fingerprint && <p className="mt-2 break-all font-mono">证书 SHA-256：{connection.fingerprint}</p>}
            <p className="mt-2">提示 unknown command connect 时，请更新 Agent 电脑上的 sbm。通过 SSH 隧道打开此页面时，Agent 电脑需要能访问相同的隧道地址。连接参数也仍可通过原有命令行选项指定。</p>
          </details>
        </>
      )}
    </section>
  );
}
