import { useCallback, useEffect, useState } from 'react';
import type { FormEvent, ReactNode } from 'react';
import { Button, Input, Spinner } from '@nextui-org/react';
import { LockKeyhole } from 'lucide-react';
import { authApi, errorMessage } from '../api';
import { useStore } from '../store';

interface AuthStatus { authenticated: boolean; setup_required: boolean }

export default function AuthGate({ children }: { children: ReactNode }) {
  const [status, setStatus] = useState<AuthStatus | null>(null);
  const [password, setPassword] = useState('');
  const [setupToken, setSetupToken] = useState('');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);

  const refresh = useCallback(async () => {
    try {
      const response = await authApi.status();
      setStatus(response.data.data);
      setError('');
    } catch (cause) {
      setError(errorMessage(cause, '无法连接管理服务，请检查服务是否启动'));
    }
  }, []);

  useEffect(() => {
    void refresh();
    const unauthorized = () => {
      setStatus((previous) => ({ authenticated: false, setup_required: previous?.setup_required ?? false }));
      // 退出后清除包含订阅和节点的内存状态，凭据始终不写浏览器存储。
      useStore.setState({ settings: null, subscriptions: [], manualNodes: [], filters: [], rules: [], ruleGroups: [], serviceStatus: null, countryGroups: [], systemInfo: null });
    };
    window.addEventListener('sbm:unauthorized', unauthorized);
    return () => window.removeEventListener('sbm:unauthorized', unauthorized);
  }, [refresh]);

  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const passwordBytes = new TextEncoder().encode(password).length;
    if (status?.setup_required && (passwordBytes < 12 || passwordBytes > 72)) {
      setError('密码需为 12–72 字节（中文等字符可能占多个字节）'); return;
    }
    setBusy(true);
    setError('');
    try {
      if (status?.setup_required) await authApi.setup(password, setupToken);
      else await authApi.login(password);
      setPassword('');
      setSetupToken('');
      await refresh();
    } catch (cause) {
      setError(errorMessage(cause, '认证失败'));
    } finally {
      setBusy(false);
    }
  };

  if (status?.authenticated) return children;
  return (
    <main className="min-h-screen bg-[#070709] bg-grid-tech flex items-center justify-center p-6 text-[#ededed]">
      <div className="w-full max-w-md rounded-[4px] border border-white/[0.12] bg-[#0b0c10] shadow-[0_20px_60px_rgba(0,0,0,0.9)] overflow-hidden">
        {/* 顶部极客标题栏 */}
        <div className="h-10 px-5 border-b border-white/[0.08] bg-[#0e1017] flex items-center justify-between select-none">
          <div className="flex items-center gap-2">
            <img src="/assets/icon.svg" alt="" width={20} height={20} className="size-5 shrink-0" />
            <span className="font-mono text-xs font-bold tracking-wider text-white uppercase">
              SingBox // AUTH GATE
            </span>
          </div>
          <div className="flex items-center gap-1.5 font-mono text-[10px] text-zinc-500">
            <span className="size-1.5 rounded-full bg-emerald-400"></span>
            <span>SECURE</span>
          </div>
        </div>

        <div className="p-6 sm:p-8 space-y-6">
          {!status ? (
            <div className="space-y-4 py-8 text-center">
              {error ? (
                <>
                  <div className="p-3 rounded-[3px] bg-rose-500/10 border border-rose-500/30 text-rose-400 font-mono text-xs text-left whitespace-pre-wrap">
                    [ERROR] {error}
                  </div>
                  <Button
                    size="sm"
                    className="font-mono text-xs bg-[#ff5722] text-black font-semibold rounded-[2px] mt-2"
                    onPress={refresh}
                  >
                    RETRY CONNECTION
                  </Button>
                </>
              ) : (
                <div className="flex flex-col items-center gap-3">
                  <Spinner size="sm" color="warning" />
                  <span className="font-mono text-xs text-zinc-400">CONNECTING TO DAEMON...</span>
                </div>
              )}
            </div>
          ) : (
            <form onSubmit={submit} className="space-y-4">
              <div className="space-y-1">
                <h2 className="text-lg font-bold font-sans tracking-tight text-white flex items-center gap-2">
                  <LockKeyhole className="size-4 text-[#ff5722]" />
                  {status.setup_required ? '初始化管理员密钥' : '管理员控制台验证'}
                </h2>
                <p className="text-xs text-zinc-400 font-mono">
                  {status.setup_required
                    ? '首次启动：请从数据目录 setup-token 文件读取初始令牌'
                    : '请输入凭据以访问底层网络转发控制台'}
                </p>
              </div>

              {status.setup_required && (
                <div className="space-y-3 pt-2">
                  <div className="p-2.5 rounded-[3px] bg-amber-500/10 border border-amber-500/20 text-[11px] font-mono text-amber-800 dark:text-amber-300/90 leading-relaxed">
                    从管理服务数据目录权限为 0600 的 setup-token 文件读取令牌。请设置 12–72 字节独立密码。
                  </div>
                  <Input
                    label="本机初始化令牌 (Setup Token)"
                    type="password"
                    autoComplete="off"
                    value={setupToken}
                    onValueChange={setSetupToken}
                    isRequired
                    variant="bordered"
                    classNames={{
                      inputWrapper: "bg-black/40 border-white/[0.1] hover:border-white/[0.2] focus-within:!border-[#ff5722] rounded-[3px]",
                      label: "text-zinc-400 font-mono text-xs",
                      input: "font-mono text-sm text-white",
                    }}
                  />
                </div>
              )}

              <Input
                label="管理员密码 (Master Password)"
                type="password"
                autoComplete={status.setup_required ? 'new-password' : 'current-password'}
                value={password}
                onValueChange={setPassword}
                isRequired
                variant="bordered"
                classNames={{
                  inputWrapper: "bg-black/40 border-white/[0.1] hover:border-white/[0.2] focus-within:!border-[#ff5722] rounded-[3px]",
                  label: "text-zinc-400 font-mono text-xs",
                  input: "font-mono text-sm text-white",
                }}
              />

              {error && (
                <div className="p-2.5 rounded-[2px] bg-rose-500/10 border border-rose-500/30 text-rose-400 font-mono text-xs">
                  [AUTH_FAILED] {error}
                </div>
              )}

              <Button
                type="submit"
                className="w-full h-10 font-mono text-xs uppercase tracking-wider rounded-[3px] bg-[#ff5722] hover:bg-[#ff6e40] text-black font-bold shadow-geek-glow transition-all"
                isLoading={busy}
              >
                {status.setup_required ? 'INITIALIZE & SIGN IN' : 'AUTHENTICATE & ENTER'}
              </Button>
            </form>
          )}

          {window.location.protocol === 'http:' && !['localhost', '127.0.0.1', '[::1]'].includes(window.location.hostname) && (
            <div className="p-2.5 rounded-[2px] bg-amber-500/10 border border-amber-500/30 text-amber-800 dark:text-amber-300 font-mono text-[11px]">
              [WARN] 当前为远端明文 HTTP 连接。建议使用 SSH 隧道或配置内置 HTTPS 避免密钥泄露。
            </div>
          )}

          <div className="pt-2 border-t border-white/[0.06] text-[11px] font-mono text-zinc-500 leading-normal">
            SingBox Manager 守护进程 · 本机受限管理 · 凭据不写入浏览器存储
          </div>
        </div>
      </div>
    </main>
  );
}
