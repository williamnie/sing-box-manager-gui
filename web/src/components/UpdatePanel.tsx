import { useEffect } from 'react';
import { useLocation } from 'react-router-dom';
import { Button, Switch } from '@nextui-org/react';
import { ArrowUpCircle, CheckCircle, ExternalLink, RefreshCw } from 'lucide-react';
import { useUpdateStore } from '../store/update';
import UpdateInstallButton from './UpdateInstallButton';

function formatTime(value?: string) {
  return value ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '尚未检查';
}

export default function UpdatePanel() {
  const { status, checking, fetchStatus, check, setAutoCheck } = useUpdateStore();
  const location = useLocation();

  useEffect(() => {
    void fetchStatus();
  }, [fetchStatus]);

  // 从更新提示条跳转过来时定位到本区块。
  const loaded = status !== null;
  useEffect(() => {
    if (loaded && location.hash === '#section-update') {
      document.getElementById('section-update')?.scrollIntoView({ behavior: 'smooth' });
    }
  }, [location.hash, loaded]);

  if (!status) return null;
  const latest = status.latest;

  return (
    <div id="section-update" className="rounded-[4px] border border-white/[0.08] bg-[#0a0a0e] overflow-hidden scroll-mt-16">
      <div className="flex items-center justify-between px-4 py-2.5 bg-[#0d0e12] border-b border-white/[0.08]">
        <div className="flex items-center gap-2">
          <ArrowUpCircle className="w-4 h-4 text-interface-orange" />
          <h2 className="font-mono text-xs font-semibold text-white tracking-wide uppercase">
            SBM_UPDATE // 版本更新
          </h2>
        </div>
        <span className="font-mono text-[10px] text-zinc-500">CURRENT: v{status.current_version}</span>
      </div>

      <div className="p-4 space-y-4">
        <div className="p-3 bg-[#0d0e12] border border-white/[0.08] rounded-[3px] flex items-center justify-between gap-4">
          <div>
            <p className="font-mono text-xs font-semibold text-zinc-200">自动检查更新 (AUTO_CHECK)</p>
            <p className="font-mono text-[10px] text-zinc-500 mt-0.5 leading-relaxed">
              每 12 小时检查 GitHub 最新发布，有新版本时在页面顶部提示；不会自动下载，点击“立即更新”后才安装
            </p>
          </div>
          <Switch
            size="sm"
            isSelected={status.auto_check}
            onValueChange={(enabled) => void setAutoCheck(enabled)}
            classNames={{ wrapper: 'group-data-[selected=true]:bg-[#ff5722]' }}
          />
        </div>

        <div className="flex flex-col sm:flex-row sm:items-center justify-between p-3.5 rounded-[3px] bg-[#0d0e12] border border-white/[0.08] gap-4">
          <div className="flex items-center gap-3 min-w-0">
            {status.available && latest ? (
              <div className="w-8 h-8 rounded-[3px] bg-interface-orange/10 border border-interface-orange/30 flex items-center justify-center text-interface-orange shrink-0">
                <ArrowUpCircle className="w-4 h-4" />
              </div>
            ) : (
              <div className="w-8 h-8 rounded-[3px] bg-emerald-500/10 border border-emerald-500/30 flex items-center justify-center text-emerald-400 shrink-0">
                <CheckCircle className="w-4 h-4" />
              </div>
            )}
            <div className="min-w-0">
              <p className="font-mono text-xs font-semibold text-white">
                {status.available && latest ? `发现新版本 v${latest.version}` : latest ? '当前已是最新版本' : '尚未获取最新版本'}
              </p>
              <p className="font-mono text-[11px] text-zinc-500 mt-0.5">
                最近检查：{formatTime(status.checked_at)}
                {latest && ` · 最新发布：v${latest.version}`}
              </p>
              {status.error && <p className="font-mono text-[11px] text-rose-400 mt-0.5">{status.error}</p>}
              {status.available && status.install_blocked && (
                <p className="font-mono text-[11px] text-amber-400 mt-0.5">{status.install_blocked}</p>
              )}
            </div>
          </div>

          <div className="flex flex-wrap gap-2 shrink-0">
            <Button
              size="sm"
              variant="flat"
              className="font-mono text-xs rounded-[3px] bg-white/[0.05] border border-white/[0.08] text-zinc-300 hover:text-white"
              startContent={checking ? null : <RefreshCw className="w-3.5 h-3.5" />}
              isLoading={checking}
              onPress={() => void check()}
            >
              检查更新
            </Button>
            <UpdateInstallButton className="font-mono text-xs rounded-[3px] bg-interface-orange text-white shadow-geek-glow" />
          </div>
        </div>

        {status.available && latest && (
          <div className="rounded-[3px] bg-[#060608] border border-white/[0.08]">
            <div className="flex items-center justify-between px-3 py-2 border-b border-white/[0.06]">
              <span className="font-mono text-[11px] text-zinc-400">RELEASE_NOTES // {latest.name || `v${latest.version}`}</span>
              {latest.url && (
                <a href={latest.url} target="_blank" rel="noreferrer" className="flex items-center gap-1 font-mono text-[11px] text-interface-orange hover:underline">
                  GitHub <ExternalLink className="w-3 h-3" />
                </a>
              )}
            </div>
            <pre className="max-h-60 overflow-auto whitespace-pre-wrap break-words p-3 font-mono text-[11px] text-zinc-300 leading-relaxed">
              {latest.notes || '该版本未提供更新说明'}
            </pre>
          </div>
        )}
      </div>
    </div>
  );
}
