import { useCallback, useEffect, useState } from 'react';
import { Button } from '@nextui-org/react';
import { Link } from 'react-router-dom';
import { configApi, errorMessage, gatewayApi } from '../api';
import { useStore } from '../store';
import ConfirmModal from '../components/ConfirmModal';
import { toast } from '../components/Toast';

interface ConfigVersion {name: string; hash: string; updated_at: string; bytes: number; preview: string}
interface ConfigDiff { current: string; candidate: string; changed: boolean }

export default function Configuration() {
  const [versions, setVersions] = useState<ConfigVersion[]>([]);
  const [restoreHash,setRestoreHash] = useState('');
  const [diff, setDiff] = useState<ConfigDiff | null>(null);
  const [busy, setBusy] = useState('');
  const [error, setError] = useState('');
  const [checkResult, setCheckResult] = useState<unknown>(null);
  const [checked, setChecked] = useState(false);
  const [confirm, setConfirm] = useState(false);
  const [platform, setPlatform] = useState('未知');
  const settings = useStore((state) => state.settings);
  const refreshStatus = useStore((state) => state.fetchServiceStatus);

  const refresh = useCallback(async () => {
    setBusy('preview'); setError(''); setChecked(false);
    try {
      const [response, deployment, history] = await Promise.all([configApi.diff(), gatewayApi.status(), configApi.versions()]);
      setVersions(history.data.data);
      setDiff(response.data.data); setPlatform(deployment.data.data.platform);
    }
    catch (cause) { setError(errorMessage(cause, '配置预览失败')); }
    finally { setBusy(''); }
  }, []);
  useEffect(() => { void refresh(); }, [refresh]);

  const check = async () => {
    setBusy('check'); setError(''); setChecked(false);
    try { const response = await configApi.check(); setCheckResult(response.data.data || response.data.message || '候选配置校验通过'); setChecked(true); }
    catch (cause) { setError(errorMessage(cause, '候选配置校验失败')); }
    finally { setBusy(''); }
  };
  const apply = async () => {
    setBusy('apply'); setError('');
    try { await configApi.apply(); toast.success('配置已校验并应用；运行中的实例已通过健康检查'); await refreshStatus(); await refresh(); }
    catch (cause) { setError(errorMessage(cause, '应用失败，请查看恢复结果')); }
    finally { setBusy(''); setConfirm(false); }
  };

  const restore = async () => {
    setBusy('restore');setError('');
    try {const response=await configApi.restore(restoreHash);toast.success(response.data.message);await useStore.getState().fetchSettings();await refreshStatus();await refresh();}
    catch(cause){setError(errorMessage(cause,'版本恢复失败'));}
    finally{setBusy('');setRestoreHash('');}
  };
  return (
    <div className="space-y-6">
      {/* 极客头部 */}
      <div className="flex flex-col md:flex-row md:items-end justify-between gap-4 border-b border-zinc-200/80 dark:border-white/[0.08] pb-5">
        <div>
          <div className="flex items-center gap-2 mb-1.5 font-mono text-[11px] text-[#ff5722] tracking-wider uppercase font-semibold">
            <span>[ ARTIFACT // COMPILER & DIFF ]</span>
            <span className="text-zinc-400 dark:text-zinc-600">--</span>
            <span className="text-zinc-500 dark:text-zinc-400">SING-BOX JSON CONFIG</span>
          </div>
          <h1 className="text-2xl font-bold tracking-tight text-zinc-900 dark:text-white font-sans flex items-center gap-3">
            配置审阅与发布
          </h1>
        </div>

        <div className="flex items-center gap-2 font-mono text-xs">
          <span className="px-2 py-1 rounded-[2px] bg-zinc-100 dark:bg-white/[0.04] border border-zinc-200 dark:border-white/[0.08] text-zinc-700 dark:text-zinc-300">
            ROLE: <b className="text-zinc-900 dark:text-white">{settings?.deployment_role === 'gateway' ? 'GATEWAY' : 'STANDALONE'}</b>
          </span>
          <span className="px-2 py-1 rounded-[2px] bg-zinc-100 dark:bg-white/[0.04] border border-zinc-200 dark:border-white/[0.08] text-zinc-700 dark:text-zinc-300">
            OS: <b className="text-zinc-900 dark:text-white">{platform}</b>
          </span>
        </div>
      </div>

      <div className="flex flex-wrap items-center gap-4 text-xs font-mono">
        <Link className="text-[#ff5722] hover:text-[#ff6e40] flex items-center gap-1 transition-colors" to="/configuration/import">
          <span>&rarr; 导入外部 sing-box JSON 配置</span>
        </Link>
        <span className="text-zinc-700">|</span>
        <Link className="text-zinc-400 hover:text-white transition-colors" to="/rules">
          查看分流规则匹配优先级
        </Link>
      </div>

      <p className="text-xs font-mono text-zinc-500">
        比较已加载应用的原生 JSON 配置与根据当前设置实时编译的候选配置。敏感凭据已在服务端脱敏。
      </p>

      {error ? (
        <div role="alert" className="p-3.5 rounded-[3px] bg-rose-500/10 border border-rose-500/30 text-rose-400 font-mono text-xs whitespace-pre-wrap">
          [ERROR] {error}
        </div>
      ) : null}

      {/* 控制操作栏 */}
      <div className="flex flex-wrap items-center gap-2.5 font-mono text-xs">
        <Button
          size="sm"
          className="rounded-[3px] font-mono text-xs border border-white/[0.1] bg-[#12141d] hover:bg-[#181c28] text-zinc-200"
          isDisabled={!!busy}
          isLoading={busy === 'preview'}
          onPress={refresh}
        >
          重新拉取对比
        </Button>
        <Button
          size="sm"
          className="rounded-[3px] font-mono text-xs border border-cyan-500/30 bg-cyan-500/10 text-cyan-300 hover:bg-cyan-500/20"
          isDisabled={!!busy || !diff}
          isLoading={busy === 'check'}
          onPress={check}
        >
          校验候选配置语法
        </Button>
        <Button
          size="sm"
          className="rounded-[3px] font-mono text-xs bg-[#ff5722] hover:bg-[#ff6e40] text-black font-semibold uppercase tracking-wider shadow-geek-glow"
          isDisabled={!!busy || !checked || settings?.deployment_role === 'gateway'}
          onPress={() => setConfirm(true)}
        >
          立即原子应用配置
        </Button>
        {settings?.deployment_role === 'gateway' && (
          <Link to="/gateway" className="text-xs font-mono text-[#ff5722] hover:underline self-center ml-2">
            网关模式请在「部署与设备」中安全应用 &rarr;
          </Link>
        )}
      </div>

      {/* Diff 双栏代码编辑器视窗 */}
      {diff && (
        <div className="space-y-3 font-mono text-xs">
          <div className="flex items-center gap-2 text-zinc-400">
            <span className={`size-2 rounded-full ${diff.changed ? 'bg-amber-400' : 'bg-emerald-400'}`}></span>
            <span>{diff.changed ? 'DIFF STATUS: 存在未应用的改动 (PENDING CHANGES)' : 'DIFF STATUS: 候选配置与当前一致 (IN SYNC)'}</span>
          </div>

          <div className="grid xl:grid-cols-2 gap-4">
            {([
              { title: 'RUNNING // 当前已生效配置', content: diff.current, badge: 'ACTIVE' },
              { title: 'CANDIDATE // 编译后的候选草案', content: diff.candidate, badge: 'CANDIDATE' },
            ]).map((item) => (
              <div key={item.title} className="rounded-[4px] border border-white/[0.08] bg-[#07080b] overflow-hidden shadow-sm">
                <div className="h-9 px-4 border-b border-white/[0.08] bg-[#0e1017] flex items-center justify-between text-xs">
                  <span className="text-zinc-200 font-semibold">{item.title}</span>
                  <span className="text-[10px] px-1.5 py-0.5 rounded-[2px] bg-white/[0.05] border border-white/[0.08] text-[#ff5722]">
                    {item.badge}
                  </span>
                </div>
                <div className="p-4 bg-[#050608]">
                  <pre className="text-[11px] font-mono leading-relaxed text-zinc-300 whitespace-pre-wrap break-all max-h-[60vh] overflow-auto selection:bg-[#ff5722]">
                    {item.content || '// 尚无已应用配置'}
                  </pre>
                </div>
              </div>
            ))}
          </div>
        </div>
      )}

      {/* 校验结果控制台 */}
      {Boolean(checkResult) && (
        <div className="rounded-[4px] border border-emerald-500/30 bg-[#070b09] overflow-hidden font-mono text-xs">
          <div className="h-8 px-4 border-b border-emerald-500/20 bg-emerald-950/20 flex items-center justify-between text-emerald-400">
            <span>// 语法与运行时依赖校验通过 (PRE-FLIGHT VALIDATION PASSED)</span>
            <span className="text-[10px]">OK</span>
          </div>
          <pre className="p-4 text-[11px] text-zinc-300 whitespace-pre-wrap break-all">
            {typeof checkResult === 'string' ? checkResult : JSON.stringify(checkResult, null, 2)}
          </pre>
        </div>
      )}

      {/* 配置版本流水线 */}
      <div className="rounded-[4px] border border-white/[0.08] bg-[#0b0c10] overflow-hidden">
        <div className="h-9 px-4 border-b border-white/[0.08] bg-[#0e1017] flex items-center justify-between">
          <span className="font-mono text-xs text-white font-semibold">// 历史快照版本 (CONFIG REVISIONS)</span>
        </div>
        <div className="p-4 space-y-3 font-mono text-xs">
          <p className="text-zinc-500 text-[11px]">
            系统自动保留当前版本与上一次备份版本。若新配置出现意外，可一键校验并安全回滚。
          </p>
          <div className="space-y-2">
            {versions.map((version) => (
              <div key={version.name} className="flex flex-wrap items-center justify-between p-3 rounded-[3px] bg-[#07080b] border border-white/[0.05] gap-3">
                <div className="flex items-center gap-3">
                  <span className={`text-[10px] px-1.5 py-0.5 rounded-[2px] font-bold ${version.name === 'current' ? 'bg-emerald-500/10 text-emerald-400 border border-emerald-500/30' : 'bg-white/[0.05] text-zinc-400 border border-white/[0.08]'}`}>
                    {version.name === 'current' ? 'CURRENT' : 'PREVIOUS'}
                  </span>
                  <span className="text-zinc-300 font-semibold">{version.hash.slice(0, 12)}</span>
                  <span className="text-zinc-500 text-[11px]">{new Date(version.updated_at).toLocaleString()}</span>
                  <span className="text-zinc-600 text-[10px]">{version.bytes} bytes</span>
                </div>
                {version.name === 'previous' && (
                  <Button
                    size="sm"
                    className="rounded-[2px] font-mono text-xs bg-amber-500/10 border border-amber-500/20 text-amber-800 dark:text-amber-300 hover:bg-amber-500/20"
                    isDisabled={!!busy}
                    onPress={() => setRestoreHash(version.hash)}
                  >
                    恢复此版本
                  </Button>
                )}
              </div>
            ))}
          </div>
        </div>
      </div>

      <ConfirmModal
        isOpen={!!restoreHash}
        title="恢复前一配置快照"
        busy={busy === 'restore'}
        onClose={() => setRestoreHash('')}
        onConfirm={restore}
        confirmLabel="校验并回滚"
      >
        <div className="space-y-3 font-mono text-xs">
          <p>前一版本必须通过内核语法校验；运行中的实例会重启并做健康检查，失败自动恢复。</p>
          <pre className="max-h-60 overflow-auto text-[11px] p-3 rounded bg-black/60 border border-white/[0.06] text-zinc-400 whitespace-pre-wrap break-all">
            {versions.find((v) => v.hash === restoreHash)?.preview}
          </pre>
        </div>
      </ConfirmModal>

      <ConfirmModal
        isOpen={confirm}
        title="应用并激活候选配置"
        busy={busy === 'apply'}
        onClose={() => setConfirm(false)}
        onConfirm={apply}
        confirmLabel="原子应用并重启"
      >
        <p className="font-mono text-xs text-zinc-300 leading-relaxed">
          服务端会重新生成并校验候选配置，备份当前配置，再原子切换并检查核心进程健康状态。正在活跃的长连接可能会短暂重置。
        </p>
      </ConfirmModal>
    </div>
  );
}
