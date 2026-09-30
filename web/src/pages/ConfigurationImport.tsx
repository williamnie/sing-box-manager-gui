import { useState } from 'react';
import { Button, Checkbox, Textarea } from '@nextui-org/react';
import { Link } from 'react-router-dom';
import { errorMessage, configImportApi } from '../api';
import { useStore } from '../store';
import { toast } from '../components/Toast';
import ConfirmModal from '../components/ConfirmModal';
import { ArrowLeft, Upload, FileCode, AlertOctagon, AlertTriangle, CheckCircle2, RotateCcw, Copy, ChevronDown, ChevronUp } from 'lucide-react';

interface ImportPreview {
  hash: string;
  summary: { outbounds: number; rules: number; rule_sets: number };
  warnings: string[];
  blockers: string[];
  omitted: string[];
  policy: unknown;
}

export default function ConfigurationImport() {
  const [config, setConfig] = useState('');
  const [preview, setPreview] = useState<ImportPreview | null>(null);
  const [acknowledged, setAcknowledged] = useState(false);
  const [busy, setBusy] = useState('');
  const [error, setError] = useState('');
  const [confirmation, setConfirmation] = useState<'import' | 'rollback' | null>(null);
  const [imported, setImported] = useState(false);
  const [showPolicy, setShowPolicy] = useState(false);
  const [showGuide, setShowGuide] = useState(false);
  const fetchSettings = useStore((state) => state.fetchSettings);

  const inspect = async () => {
    setBusy('preview');
    setError('');
    setPreview(null);
    setAcknowledged(false);
    try {
      const response = await configImportApi.preview(config);
      setPreview(response.data.data);
      toast.success('导入预览分析完成');
    } catch (cause) {
      setError(errorMessage(cause, '无法解析导入配置'));
    } finally {
      setBusy('');
    }
  };

  const execute = async () => {
    if (!confirmation) return;
    setBusy(confirmation);
    setError('');
    try {
      if (confirmation === 'import' && preview) {
        await configImportApi.import(config, preview.hash, acknowledged);
        setImported(true);
        setConfig('');
        setPreview(null);
        toast.success('已导入草案，自动应用与网关接管已进入安全锁定状态');
      } else {
        await configImportApi.rollback();
        setImported(false);
        toast.success('已恢复导入前数据快照，请重新审阅配置');
      }
      await fetchSettings();
    } catch (cause) {
      setError(errorMessage(cause));
    } finally {
      setBusy('');
      setConfirmation(null);
    }
  };

  const copyPolicy = () => {
    if (!preview) return;
    navigator.clipboard.writeText(JSON.stringify(preview.policy, null, 2));
    toast.success('策略结构已复制到剪贴板');
  };

  return (
    <div className="space-y-6">
      {/* 头部标题与操作 */}
      <div className="flex flex-col md:flex-row md:items-center justify-between gap-4 pb-4 border-b border-white/[0.08]">
        <div>
          <div className="flex items-center gap-2 mb-1.5">
            <span className="font-mono text-[10px] uppercase tracking-wider text-interface-orange/80 bg-interface-orange/10 px-2 py-0.5 rounded-[2px] border border-interface-orange/20">
              07 // CONFIG_MIGRATION
            </span>
            <Link
              to="/configuration"
              className="inline-flex items-center gap-1 font-mono text-xs text-zinc-400 hover:text-white transition-colors"
            >
              <ArrowLeft className="w-3.5 h-3.5" /> 返回配置管理
            </Link>
          </div>
          <h1 className="text-xl md:text-2xl font-semibold text-white tracking-tight font-mono">
            导入外部 sing-box 配置
          </h1>
          <p className="text-xs text-zinc-400 mt-1 max-w-2xl leading-relaxed">
            粘贴任意来源最终生成的 sing-box JSON 或本地文件。系统仅导入路由与分流策略，保留草案沙盒模式，不会立即接管网关或启动新实例。
          </p>
        </div>

        <Button
          size="sm"
          variant="flat"
          className="font-mono text-xs text-amber-800 dark:text-amber-400 bg-amber-500/10 border border-amber-500/20 hover:bg-amber-500/20"
          startContent={<RotateCcw className="w-3.5 h-3.5" />}
          isDisabled={!!busy}
          onPress={() => setConfirmation('rollback')}
        >
          恢复导入前数据快照
        </Button>
      </div>

      {/* 错误提示栏 */}
      {error && (
        <div className="p-3.5 rounded-[4px] bg-red-950/30 border border-red-500/30 flex items-start gap-3">
          <AlertOctagon className="w-4 h-4 text-red-400 shrink-0 mt-0.5" />
          <div className="space-y-1">
            <p className="font-mono text-xs font-semibold text-red-400 uppercase tracking-wider">
              [PARSER_ERROR] 解析中断
            </p>
            <p className="font-mono text-xs text-red-300 whitespace-pre-wrap break-all leading-relaxed">
              {error}
            </p>
          </div>
        </div>
      )}

      {/* 成功导入提示 */}
      {imported && (
        <div className="p-3.5 rounded-[4px] bg-emerald-950/30 border border-emerald-500/30 flex items-center justify-between gap-4">
          <div className="flex items-center gap-2.5">
            <CheckCircle2 className="w-4 h-4 text-emerald-400 shrink-0" />
            <span className="font-mono text-xs text-emerald-300">
              配置草案已安全写入。请转至配置中心审阅并执行差异核验。
            </span>
          </div>
          <div className="flex gap-2 shrink-0">
            <Link
              to="/configuration"
              className="font-mono text-xs px-2.5 py-1 bg-emerald-500/20 border border-emerald-500/30 text-emerald-300 rounded-[2px] hover:bg-emerald-500/30"
            >
              配置审查 →
            </Link>
            <Link
              to="/gateway"
              className="font-mono text-xs px-2.5 py-1 bg-white/[0.05] border border-white/[0.08] text-zinc-300 rounded-[2px] hover:text-white"
            >
              部署与设备
            </Link>
          </div>
        </div>
      )}

      {/* 代码录入终端视窗 */}
      <div className="rounded-[4px] border border-white/[0.08] bg-[#0a0a0e] overflow-hidden">
        <div className="flex items-center justify-between px-3 py-2 bg-[#0d0e12] border-b border-white/[0.08]">
          <div className="flex items-center gap-2">
            <span className="w-2 h-2 rounded-full bg-interface-orange/80 animate-pulse" />
            <span className="font-mono text-[11px] text-zinc-400 font-medium tracking-wide">
              JSON_PAYLOAD_BUFFER
            </span>
            <span className="font-mono text-[10px] text-zinc-600">
              ({config.length} CHARS)
            </span>
          </div>
          <div className="flex items-center gap-2">
            {config && (
              <button
                type="button"
                onClick={() => {
                  setConfig('');
                  setPreview(null);
                  setAcknowledged(false);
                }}
                className="font-mono text-[11px] text-zinc-500 hover:text-red-400 transition-colors px-2 py-0.5"
              >
                [清空]
              </button>
            )}
            <label className="inline-flex items-center gap-1.5 font-mono text-[11px] text-interface-orange hover:text-orange-400 cursor-pointer bg-interface-orange/10 px-2.5 py-1 rounded-[2px] border border-interface-orange/20 transition-colors">
              <Upload className="w-3 h-3" />
              选择本地 JSON
              <input
                type="file"
                accept=".json,application/json"
                className="sr-only"
                onChange={async (event) => {
                  const file = event.target.files?.[0];
                  if (!file) return;
                  if (file.size > 3 * 1024 * 1024) {
                    setError('请选择不超过 3 MiB 的配置文件');
                    return;
                  }
                  try {
                    setConfig(await file.text());
                    setPreview(null);
                    setAcknowledged(false);
                  } catch {
                    setError('读取文件失败');
                  }
                  event.target.value = '';
                }}
              />
            </label>
          </div>
        </div>

        <div className="p-3">
          <Textarea
            value={config}
            onValueChange={(value) => {
              setConfig(value);
              setPreview(null);
              setAcknowledged(false);
            }}
            minRows={12}
            maxRows={24}
            placeholder={'{\n  "log": { "level": "info" },\n  "inbounds": [...],\n  "outbounds": [...],\n  "route": { ... }\n}'}
            classNames={{
              input: 'font-mono text-xs text-zinc-200 bg-transparent selection:bg-interface-orange/30 placeholder:text-zinc-600',
              inputWrapper: 'bg-[#060608] border border-white/[0.06] rounded-[2px] shadow-none hover:border-white/[0.12] focus-within:!border-interface-orange/60',
            }}
          />
        </div>

        <div className="flex flex-col sm:flex-row sm:items-center justify-between px-3 py-2.5 bg-[#08080b] border-t border-white/[0.06] gap-3">
          <span className="font-mono text-[11px] text-zinc-500">
            * 仅提交至本地 sbm 服务分析内核模型；不会写入公网或云端。
          </span>
          <Button
            size="sm"
            color="primary"
            className="font-mono text-xs font-medium rounded-[3px] bg-interface-orange text-white shadow-geek-glow px-4"
            isLoading={busy === 'preview'}
            isDisabled={!!busy || !config.trim()}
            onPress={inspect}
            startContent={!busy && <FileCode className="w-3.5 h-3.5" />}
          >
            执行解析与预览
          </Button>
        </div>
      </div>

      {/* 导入预览结果面板 */}
      {preview && (
        <div className="rounded-[4px] border border-interface-orange/30 bg-[#0a0a0e] overflow-hidden space-y-4 p-4 shadow-geek-glow">
          <div className="flex items-center justify-between pb-3 border-b border-white/[0.08]">
            <div className="flex items-center gap-2">
              <span className="w-2 h-2 rounded-full bg-interface-orange" />
              <h2 className="font-mono text-sm font-semibold text-white tracking-wide uppercase">
                PREVIEW_DIAGNOSTICS // 导入分析结果
              </h2>
            </div>
            <span className="font-mono text-[10px] text-zinc-500">
              HASH: {preview.hash.slice(0, 16)}...
            </span>
          </div>

          {/* 指标三列网格 */}
          <div className="grid grid-cols-1 md:grid-cols-3 gap-3">
            {[
              { label: 'OUTBOUNDS // 出站节点', count: preview.summary.outbounds, desc: '提取的节点与分流组' },
              { label: 'ROUTE_RULES // 路由规则', count: preview.summary.rules, desc: '域名/IP/协议分流条目' },
              { label: 'RULE_SETS // 规则集源', count: preview.summary.rule_sets, desc: '引用的远程或内联规则集' },
            ].map((item) => (
              <div
                key={item.label}
                className="p-3 bg-[#0d0e12] border border-white/[0.08] rounded-[3px]"
              >
                <div className="font-mono text-[10px] text-zinc-400">{item.label}</div>
                <div className="font-mono text-2xl font-bold text-interface-orange mt-1">
                  {item.count}
                </div>
                <div className="text-[11px] text-zinc-500 mt-1">{item.desc}</div>
              </div>
            ))}
          </div>

          {/* 阻止导入的严重问题 */}
          {preview.blockers?.length ? (
            <div className="p-3 bg-red-950/20 border border-red-500/40 rounded-[3px] space-y-2">
              <div className="flex items-center gap-2 text-red-400 font-mono text-xs font-semibold uppercase">
                <AlertOctagon className="w-4 h-4" />
                BLOCKERS: 存在阻止导入的架构冲突 ({preview.blockers.length})
              </div>
              <ul className="space-y-1 pl-4 list-disc text-xs text-red-300 font-mono">
                {preview.blockers.map((item, idx) => (
                  <li key={idx} className="leading-relaxed">{item}</li>
                ))}
              </ul>
            </div>
          ) : null}

          {/* 警告需要复核项 */}
          {preview.warnings?.length ? (
            <div className="p-3 bg-amber-950/20 border border-amber-500/30 rounded-[3px] space-y-2">
              <div className="flex items-center gap-2 text-amber-800 dark:text-amber-400 font-mono text-xs font-semibold uppercase">
                <AlertTriangle className="w-4 h-4" />
                WARNINGS: 需人工复核的规则变更 ({preview.warnings.length})
              </div>
              <ul className="space-y-1 pl-4 list-disc text-xs text-amber-800 dark:text-amber-300 font-mono">
                {preview.warnings.map((item, idx) => (
                  <li key={idx} className="leading-relaxed">{item}</li>
                ))}
              </ul>
            </div>
          ) : null}

          {/* 未导入配置项与确认复选框 */}
          {preview.omitted?.length ? (
            <div className="p-3 bg-[#0d0e12] border border-white/[0.08] rounded-[3px] space-y-3">
              <div className="flex items-center justify-between">
                <div className="font-mono text-xs text-zinc-300 font-semibold uppercase tracking-wide">
                  OMITTED: 不参与导入的环境配置项 ({preview.omitted.length})
                </div>
                <span className="font-mono text-[10px] text-zinc-500">
                  (TUN/网关接管/日志等级等将维持本地安全状态)
                </span>
              </div>
              <ul className="space-y-1 pl-4 list-disc text-xs text-zinc-400 font-mono">
                {preview.omitted.map((item, idx) => (
                  <li key={idx}>{item}</li>
                ))}
              </ul>
              <div className="pt-2 border-t border-white/[0.06]">
                <Checkbox
                  isSelected={acknowledged}
                  onValueChange={setAcknowledged}
                  classNames={{
                    label: 'font-mono text-xs text-zinc-300',
                    wrapper: 'before:border-white/30 after:bg-interface-orange',
                  }}
                >
                  我已知晓并复核以上未导入项，将在应用后根据网络环境手动补齐必要策略
                </Checkbox>
              </div>
            </div>
          ) : null}

          {/* 脱敏策略结构折叠代码块 */}
          <div className="border border-white/[0.08] rounded-[3px] bg-[#070709] overflow-hidden">
            <button
              type="button"
              onClick={() => setShowPolicy(!showPolicy)}
              className="w-full flex items-center justify-between px-3 py-2 bg-[#0c0d11] hover:bg-[#101117] transition-colors text-left font-mono text-xs text-zinc-300"
            >
              <span className="flex items-center gap-2">
                <FileCode className="w-3.5 h-3.5 text-interface-orange" />
                查看解析提取的策略 AST 树 (凭据已脱敏)
              </span>
              <div className="flex items-center gap-2">
                {showPolicy && (
                  <button
                    type="button"
                    onClick={(e) => {
                      e.stopPropagation();
                      copyPolicy();
                    }}
                    className="p-1 text-zinc-400 hover:text-white"
                    title="复制 JSON"
                  >
                    <Copy className="w-3 h-3" />
                  </button>
                )}
                {showPolicy ? <ChevronUp className="w-3.5 h-3.5" /> : <ChevronDown className="w-3.5 h-3.5" />}
              </div>
            </button>
            {showPolicy && (
              <pre className="p-3 font-mono text-[11px] text-zinc-300 bg-[#040406] max-h-96 overflow-auto border-t border-white/[0.06] whitespace-pre-wrap break-all leading-relaxed">
                {JSON.stringify(preview.policy, null, 2)}
              </pre>
            )}
          </div>

          {/* 确认导入动作条 */}
          <div className="pt-2 flex justify-end">
            <Button
              color="primary"
              className="font-mono text-xs font-semibold rounded-[3px] bg-interface-orange text-white shadow-geek-glow px-6 py-2"
              isDisabled={
                !!busy ||
                !!preview.blockers?.length ||
                (!!preview.omitted?.length && !acknowledged)
              }
              onPress={() => setConfirmation('import')}
            >
              确认导入并生成草案
            </Button>
          </div>
        </div>
      )}

      {/* 并行验证与切换指引（折叠面板） */}
      <div className="border border-white/[0.08] rounded-[4px] bg-[#0a0a0e] overflow-hidden">
        <button
          type="button"
          onClick={() => setShowGuide(!showGuide)}
          className="w-full flex items-center justify-between px-4 py-3 bg-[#0d0e12] hover:bg-[#111218] transition-colors text-left"
        >
          <div className="flex items-center gap-2">
            <span className="font-mono text-[10px] text-zinc-500 uppercase tracking-widest">DOCS // RUNBOOK</span>
            <span className="font-mono text-xs font-medium text-zinc-200">
              替换已有代理实例时的并行验证与无缝切换方案
            </span>
          </div>
          {showGuide ? <ChevronUp className="w-4 h-4 text-zinc-400" /> : <ChevronDown className="w-4 h-4 text-zinc-400" />}
        </button>

        {showGuide && (
          <div className="p-4 space-y-4 border-t border-white/[0.06] font-mono text-xs text-zinc-400 leading-relaxed bg-[#08080b]">
            <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
              {[
                {
                  step: '01',
                  title: '隔离与数据独立',
                  desc: '新旧实例保持独立数据目录及端口，保持 TUN、DHCP 与网关接管关闭，避免与宿主服务冲突。',
                },
                {
                  step: '02',
                  title: '单机代理与规则核验',
                  desc: '使用单台测试设备的 HTTP/SOCKS5 显式代理接入，验证新节点连通性与分流准确率。',
                },
                {
                  step: '03',
                  title: '多协议与路由测试',
                  desc: '在隔离 Linux 环境检查 TCP/UDP、IPv4/IPv6、回程路由及容器网络。严禁新旧 TUN 同时接管相同流量。',
                },
                {
                  step: '04',
                  title: '原子切换与回滚准备',
                  desc: '安排维护窗口，先停止旧接管，再在控制台启用新网关；保留原有配置以便随时一键回退。',
                },
              ].map((item) => (
                <div key={item.step} className="p-3 bg-[#0d0e12] border border-white/[0.06] rounded-[3px]">
                  <div className="text-interface-orange font-bold text-[11px] mb-1">
                    STEP {item.step} // {item.title}
                  </div>
                  <div className="text-zinc-400 text-[11px] leading-relaxed">
                    {item.desc}
                  </div>
                </div>
              ))}
            </div>
            <p className="text-[10px] text-zinc-500 pt-2 border-t border-white/[0.04]">
              * 提示：逐步切换方案适用于替代生产环境已有代理实例。导入操作本身绝不会修改路由器、客户端或非受控服务。
            </p>
          </div>
        )}
      </div>

      {/* 确认模态框 */}
      <ConfirmModal
        isOpen={confirmation !== null}
        busy={!!busy}
        title={confirmation === 'import' ? '确认保存配置草案' : '恢复导入前数据快照'}
        onClose={() => setConfirmation(null)}
        onConfirm={execute}
        confirmLabel={confirmation === 'import' ? '执行导入' : '恢复数据快照'}
      >
        <p className="font-mono text-xs text-zinc-300 leading-relaxed">
          {confirmation === 'import'
            ? '系统将备份并更新当前管理器策略，自动应用与网关接管将强制维持安全关闭。现有规则将迁移至导入前备份，导入策略将替换当前工作区草案。订阅与已有节点保持不变。'
            : '系统将调取导入前的本地安全快照并原子恢复管理器数据，导入后的所有草案修改将被回滚。运行中的代理内核与网关接管需分别确认。'}
        </p>
      </ConfirmModal>
    </div>
  );
}
