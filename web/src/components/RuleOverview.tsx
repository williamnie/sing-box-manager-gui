import { useEffect, useState } from 'react';
import { Button, Spinner, Switch, Tab, Tabs } from '@nextui-org/react';
import { ArrowRight, Pencil, RefreshCw, Search, Trash2 } from 'lucide-react';
import { Link } from 'react-router-dom';
import { errorMessage, ruleApi } from '../api';
import type { DevicePolicy, Rule, RuleGroup } from '../store';
import { actionLabel, applicationStatus, customRuleMatch, describeMatch, outboundLabel } from '../utils/rulePresentation';

interface RouteSnapshot { rules: Record<string, unknown>[]; final: string }
interface Overview {
  draft: RouteSnapshot | null; draft_error: string;
  applied: RouteSnapshot | null; applied_error: string;
  applied_hash: string; changed: boolean | null; running: boolean; role: string;
  imported_rules: Record<string, unknown>[];
  imported_rule_sets: Record<string, unknown>[]; imported_final: string;
  devices: { id: string; name: string; addresses: string[]; enabled: boolean; policy: DevicePolicy; outbound: string; dns: string; explanation: string }[];
}
interface Props {
  revision: number;
  autoApply: boolean;
  rules: Rule[];
  groups: RuleGroup[];
  outbounds: unknown;
  outboundOptions: { value: string; label: string }[];
  onEdit: (rule: Rule) => void;
  onDelete: (rule: Rule) => void;
  onToggle: (rule: Rule) => void;
  onToggleGroup: (id: string, enabled: boolean) => void;
  onGroupOutbound: (group: RuleGroup, outbound: string) => void;
  onEditBlocklist: () => void;
}
interface ReadableRule {
  id: string; title: string; source: string; traffic: string; outcome: string;
  origin: '自定义' | '预设' | '导入' | '设备'; enabled: boolean; advanced?: boolean;
  rule?: Rule; group?: RuleGroup; inactive?: string;
}

function savedRules(overview: Overview | null, props: Props): ReadableRule[] {
  const devices = (overview?.devices || []).map((device): ReadableRule => ({
    id: `device-${device.id}`, title: device.name || '未命名设备', origin: '设备',
    source: `来自 ${device.addresses.join('、')}`, enabled: device.enabled,
    traffic: device.policy === 'split' ? '按下面的分流规则处理' : '该设备的全部流量，优先于分流规则',
    outcome: device.policy === 'split' ? '普通分流' : device.policy === 'direct' ? '整机直连' : device.policy === 'bypass' ? '绕过接管' : `严格全代理 · ${device.outbound}`,
    inactive: overview?.role !== 'gateway' ? '仅网关模式使用' : undefined,
  }));
  const custom = [...props.rules].sort((a, b) => a.priority - b.priority).map((rule): ReadableRule => ({
    id: `custom-${rule.id}`, title: rule.name, ...describeMatch(customRuleMatch(rule)),
    origin: '自定义', enabled: rule.enabled, outcome: outboundLabel(rule.outbound, props.outbounds), rule,
  }));
  const groups = props.groups.map((group): ReadableRule => ({
    id: `group-${group.id}`, title: group.name, source: '所有来源',
    traffic: [group.site_rules?.length ? `网站分类：${group.site_rules.join('、')}` : '', group.ip_rules?.length ? `IP 分类：${group.ip_rules.join('、')}` : ''].filter(Boolean).join(' · ') || '尚未配置匹配条件',
    origin: '预设', enabled: group.enabled, outcome: outboundLabel(group.outbound, props.outbounds), group,
  }));
  const imported = (overview?.imported_rules || []).map((rule, index): ReadableRule => ({
    id: `imported-${index}`, title: '', ...describeMatch(rule), origin: '导入', enabled: true,
    outcome: actionLabel(rule, props.outbounds),
  }));
  return [...devices, ...custom, ...groups, ...imported];
}

function RuleSequence({ snapshot, error }: { snapshot: RouteSnapshot | null; error: string }) {
  if (error) return <p role="alert" className="text-rose-400 font-mono text-xs whitespace-pre-wrap p-3 rounded bg-rose-500/10 border border-rose-500/20">{error}</p>;
  if (!snapshot) return <p className="font-mono text-xs text-zinc-500">暂无可读取的配置流水线。</p>;
  return (
    <div className="space-y-3 font-mono text-xs">
      <ol className="space-y-1.5 max-h-96 overflow-auto">
        {(snapshot.rules || []).map((rule, index) => (
          <li key={index} className="p-2.5 bg-[#07080b] border border-white/[0.05] rounded-[2px]">
            <details>
              <summary className="cursor-pointer text-zinc-300 hover:text-white break-all flex items-center gap-2">
                <span className="text-[#ff5722] font-semibold">{String(index + 1).padStart(2, '0')}.</span>
                <span className="truncate">{JSON.stringify(rule)}</span>
              </summary>
              <pre className="mt-2 text-[11px] p-2 bg-black/60 rounded text-zinc-400 whitespace-pre-wrap break-all border border-white/[0.04]">
                {JSON.stringify(rule, null, 2)}
              </pre>
            </details>
          </li>
        ))}
      </ol>
      <div className="p-2.5 rounded-[2px] bg-black/40 border border-white/[0.08] flex items-center justify-between text-zinc-400">
        <span>FINAL OUTBOUND</span>
        <span className="text-white font-semibold">{snapshot.final || '内核默认出站'}</span>
      </div>
    </div>
  );
}

export default function RuleOverview(props: Props) {
  const [overview, setOverview] = useState<Overview | null>(null);
  const [error, setError] = useState('');
  const [refresh, setRefresh] = useState(0);
  const [query, setQuery] = useState('');
  const [limit, setLimit] = useState(20);
  const [diagnosticsOpen, setDiagnosticsOpen] = useState(false);

  useEffect(() => {
    let active = true;
    void ruleApi.overview().then((response) => {
      if (active) { setOverview(response.data.data); setError(''); }
    }).catch((cause) => { if (active) { setOverview(null); setError(errorMessage(cause, '无法读取规则状态')); } });
    return () => { active = false; };
  }, [refresh, props.revision]);

  const allRules = savedRules(overview, props);
  const filtered = allRules.filter((row) => `${row.title} ${row.source} ${row.traffic} ${row.outcome} ${row.origin}`.toLowerCase().includes(query.trim().toLowerCase()));
  const reviewLink = overview?.role === 'gateway' ? '/gateway' : '/configuration';
  const configStatus = overview ? applicationStatus(overview) : '';

  return (
    <div className="space-y-5">
      {/* 极客状态条 */}
      <div className="flex flex-wrap items-center justify-between gap-3 rounded-[3px] border border-white/[0.08] px-4 py-2.5 bg-[#0b0c10]" role="status">
        <div className="flex flex-wrap items-center gap-3 font-mono text-xs">
          {overview ? (
            <>
              <span className="flex items-center gap-2">
                <span className={`size-2 rounded-full ${overview.running ? 'bg-emerald-400 animate-ping' : 'bg-zinc-600'}`} />
                <span className="text-zinc-300">{overview.running ? 'CORE: ACTIVE' : 'CORE: OFFLINE'}</span>
              </span>
              <span className={`text-[10px] px-2 py-0.5 rounded-[2px] border ${
                configStatus === '配置已应用'
                  ? 'bg-emerald-500/10 border-emerald-500/30 text-emerald-400'
                  : 'bg-amber-500/10 border-amber-500/30 text-amber-800 dark:text-amber-300'
              }`}>
                STATUS: {configStatus}
              </span>
            </>
          ) : !error ? (
            <div className="flex items-center gap-2 text-zinc-500">
              <Spinner size="sm" color="warning" />
              <span>READING RULESET PIPELINE...</span>
            </div>
          ) : null}
          {error && <span className="text-rose-400 font-mono text-xs">{error}</span>}
        </div>
        <div className="flex items-center gap-3 font-mono text-xs">
          <Link className="text-[#ff5722] hover:text-[#ff6e40] transition-colors" to={reviewLink}>
            检查与应用 &rarr;
          </Link>
          <Button
            isIconOnly
            size="sm"
            className="size-7 rounded-[2px] bg-white/[0.05] border border-white/[0.08] text-zinc-400 hover:text-white"
            aria-label="刷新规则状态"
            onPress={() => setRefresh((value) => value + 1)}
          >
            <RefreshCw className="size-3 text-[#ff5722]" />
          </Button>
        </div>
      </div>

      {error && allRules.length ? (
        <div className="p-3 rounded-[3px] bg-amber-500/10 border border-amber-500/20 text-xs text-amber-800 dark:text-amber-300 font-mono">
          [WARN] 导入与设备规则暂时无法读取，以下仅显示已加载的可编辑规则。
        </div>
      ) : null}

      {overview?.draft_error || overview?.applied_error ? (
        <div role="alert" className="p-3 rounded-[3px] bg-rose-500/10 border border-rose-500/30 text-xs font-mono text-rose-400">
          {overview.draft_error || overview.applied_error}
        </div>
      ) : null}

      {/* 规则主体表格卡片 */}
      <div className="rounded-[4px] border border-white/[0.08] bg-[#0b0c10] overflow-hidden shadow-sm">
        <div className="flex flex-wrap justify-between items-center gap-4 p-4 border-b border-white/[0.08] bg-[#0e1017]">
          <div>
            <h2 className="font-mono text-sm font-bold text-white flex items-center gap-2">
              <span>// 已保存规则策略</span>
              <span className="text-xs font-normal text-zinc-500">({allRules.length} TOTAL)</span>
            </h2>
            <p className="text-[11px] font-mono text-zinc-500 mt-0.5">
              流量匹配与出站分流策略 · {props.autoApply ? '自动应用模式' : '保存后需手动检查并应用'}
            </p>
          </div>
          <div className="relative flex items-center">
            <Search className="size-3 text-zinc-500 absolute left-2.5 pointer-events-none" />
            <input
              type="text"
              placeholder="搜索域名、设备或出站..."
              value={query}
              onChange={(e) => { setQuery(e.target.value); setLimit(20); }}
              className="w-56 sm:w-64 h-7 pl-7 pr-2.5 text-xs font-mono bg-black/60 text-zinc-200 border border-white/[0.08] rounded-[2px] focus:outline-none focus:border-[#ff5722] transition-colors placeholder:text-zinc-600"
            />
          </div>
        </div>

        {!overview && !allRules.length ? (
          <div className="p-8 text-center font-mono text-xs text-zinc-500">
            {error ? '规则状态读取失败，请点击右上角刷新重试' : '正在读取分流规则流水线...'}
          </div>
        ) : filtered.length ? (
          <div className="overflow-x-auto">
            <table className="w-full text-xs text-left font-mono">
              <thead className="text-[10px] text-zinc-500 uppercase tracking-wider bg-black/40 border-b border-white/[0.06]">
                <tr>
                  <th className="py-2.5 px-4 font-semibold">MATCH CRITERIA (匹配流量)</th>
                  <th className="py-2.5 px-3 font-semibold">ACTION / ROUTE (出站行为)</th>
                  <th className="py-2.5 px-3 font-semibold">SOURCE (来源)</th>
                  <th className="py-2.5 px-4 font-semibold text-right">CONTROLS (操作)</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-white/[0.04]">
                {filtered.slice(0, limit).map((row) => (
                  <tr key={row.id} className={`hover:bg-white/[0.02] transition-colors ${!row.enabled || row.inactive ? 'opacity-40' : ''}`}>
                    <td className="py-3 px-4 min-w-56 max-w-xl break-words">
                      <div className="font-semibold text-zinc-200">{row.title || row.traffic}</div>
                      {row.title && <div className="text-[11px] text-zinc-400 mt-0.5">{row.traffic}</div>}
                      {row.group?.id === 'ad-block' && <button className="mt-1 text-[11px] text-[#ff5722] hover:underline" onClick={props.onEditBlocklist}>补充 / 编辑自定义拦截域名</button>}
                      <div className="text-[10px] text-zinc-500 mt-1 flex items-center gap-1.5">
                        <span>{row.source}</span>
                        {!row.enabled && <span className="text-zinc-600">· DISABLED</span>}
                        {row.inactive && <span className="text-amber-800 dark:text-amber-500/80">· {row.inactive}</span>}
                      </div>
                      {row.advanced && (
                        <a href="#rule-diagnostics" className="text-[10px] text-[#ff5722] hover:underline inline-block mt-1" onClick={() => setDiagnosticsOpen(true)}>
                          查看详细条件 &rarr;
                        </a>
                      )}
                    </td>
                    <td className="py-3 px-3 min-w-40 max-w-xs break-words">
                      {row.group ? (
                        <select
                          value={row.group.outbound}
                          onChange={(e) => props.onGroupOutbound(row.group!, e.target.value)}
                          className="h-7 text-xs font-mono bg-black/60 text-zinc-200 border border-white/[0.1] rounded-[2px] px-2 focus:border-[#ff5722] focus:outline-none"
                        >
                          {props.outboundOptions.map((opt) => (
                            <option key={opt.value} value={opt.value}>{opt.label}</option>
                          ))}
                        </select>
                      ) : (
                        <span className="inline-flex items-center gap-1.5 px-2 py-0.5 rounded-[2px] bg-white/[0.04] border border-white/[0.08] text-zinc-300">
                          <ArrowRight className="size-3 text-[#ff5722]" />
                          <span>{row.outcome}</span>
                        </span>
                      )}
                    </td>
                    <td className="py-3 px-3">
                      <span className="text-[10px] px-1.5 py-0.5 rounded-[2px] bg-white/[0.04] border border-white/[0.08] text-zinc-400">
                        {row.origin}
                      </span>
                    </td>
                    <td className="py-3 px-4 text-right">
                      <div className="flex justify-end items-center gap-1.5 whitespace-nowrap">
                        {row.rule ? (
                          <>
                            <Button
                              isIconOnly
                              size="sm"
                              className="size-6 rounded-[2px] bg-white/[0.04] border border-white/[0.08] text-zinc-400 hover:text-white"
                              aria-label={`编辑规则 ${row.title}`}
                              onPress={() => props.onEdit(row.rule!)}
                            >
                              <Pencil className="size-3" />
                            </Button>
                            <Button
                              isIconOnly
                              size="sm"
                              className="size-6 rounded-[2px] bg-rose-500/10 border border-rose-500/20 text-rose-400 hover:bg-rose-500/20"
                              aria-label={`删除规则 ${row.title}`}
                              onPress={() => props.onDelete(row.rule!)}
                            >
                              <Trash2 className="size-3" />
                            </Button>
                            <Switch
                              size="sm"
                              aria-label={`启用规则 ${row.title}`}
                              isSelected={row.enabled}
                              onValueChange={() => props.onToggle(row.rule!)}
                              classNames={{ wrapper: "group-data-[selected=true]:bg-[#ff5722]" }}
                            />
                          </>
                        ) : row.group ? (
                          <Switch
                            size="sm"
                            aria-label={`启用预设 ${row.title}`}
                            isSelected={row.enabled}
                            onValueChange={(enabled) => props.onToggleGroup(row.group!.id, enabled)}
                            classNames={{ wrapper: "group-data-[selected=true]:bg-[#ff5722]" }}
                          />
                        ) : (
                          <Link className="text-[#ff5722] hover:underline" to={row.origin === '设备' ? '/gateway' : '/configuration/import'}>
                            {row.origin === '设备' ? '设备' : '导入'}
                          </Link>
                        )}
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : (
          <div className="p-8 text-center font-mono text-xs text-zinc-500">
            {query.trim() ? '未找到符合条件的规则' : '暂未设置分流规则，点击上方添加规则'}
          </div>
        )}

        {filtered.length > limit && (
          <div className="p-3 border-t border-white/[0.06] text-center">
            <Button
              size="sm"
              className="rounded-[2px] font-mono text-xs bg-white/[0.04] border border-white/[0.08] text-zinc-300 hover:text-white"
              onPress={() => setLimit((v) => v + 20)}
            >
              加载更多 (剩余 {filtered.length - limit} 条)
            </Button>
          </div>
        )}

        {overview?.draft && (
          <div className="border-t border-white/[0.08] px-4 py-3 bg-[#07080c] flex flex-wrap items-center justify-between gap-2 text-xs font-mono text-zinc-400">
            <div className="flex items-center gap-2">
              <span className="text-zinc-500">FINAL FALLBACK:</span>
              <ArrowRight className="size-3 text-[#ff5722]" />
              <span className="text-white font-semibold">{outboundLabel(overview.draft.final, props.outbounds)}</span>
            </div>
            <Link className="text-[#ff5722] hover:underline" to="/configuration">
              管理完整候选配置 &rarr;
            </Link>
          </div>
        )}
      </div>

      {/* 高级诊断终端折叠框 */}
      {overview && (
        <details id="rule-diagnostics" open={diagnosticsOpen} onToggle={(e) => setDiagnosticsOpen(e.currentTarget.open)} className="rounded-[4px] border border-white/[0.08] bg-[#0b0c10] p-4 font-mono text-xs">
          <summary className="cursor-pointer font-semibold text-zinc-300 hover:text-white flex items-center justify-between select-none">
            <span className="flex items-center gap-2">
              <span className="text-[#ff5722]">//</span>
              <span>高级流水线诊断与执行顺序 (DIAGNOSTICS)</span>
            </span>
            <span className="text-[10px] text-zinc-500 uppercase">{diagnosticsOpen ? '[- COLLAPSE]' : '[+ EXPAND]'}</span>
          </summary>
          {diagnosticsOpen && (
            <div className="space-y-4 mt-4 pt-3 border-t border-white/[0.06]">
              <p className="text-zinc-500 text-[11px] leading-relaxed">
                路由匹配管道：嗅探 / DNS &rarr; 设备策略 &rarr; hosts &rarr; 自定义规则 (按优先级升序) &rarr; 预设规则组 &rarr; 导入规则次序 &rarr; 最终兜底出站。
              </p>
              <Tabs
                aria-label="诊断版本"
                variant="underlined"
                classNames={{
                  tabList: "gap-4 border-b border-white/[0.08] p-0 font-mono text-xs",
                  cursor: "bg-[#ff5722]",
                  tab: "h-8 text-zinc-400 data-[selected=true]:text-white",
                }}
              >
                <Tab key="draft" title="当前保存草案">
                  <div className="pt-3">
                    <RuleSequence snapshot={overview.draft} error={overview.draft_error} />
                  </div>
                </Tab>
                <Tab key="applied" title="上次已应用">
                  <div className="pt-3">
                    <RuleSequence snapshot={overview.applied} error={overview.applied_error} />
                  </div>
                </Tab>
                <Tab key="imported" title="导入原始规则">
                  <div className="pt-3">
                    <pre className="text-[11px] p-3 rounded bg-black/60 border border-white/[0.06] max-h-96 overflow-auto text-zinc-400 whitespace-pre-wrap break-all">
                      {JSON.stringify({ rules: overview.imported_rules, rule_sets: overview.imported_rule_sets, final: overview.imported_final }, null, 2)}
                    </pre>
                  </div>
                </Tab>
              </Tabs>
            </div>
          )}
        </details>
      )}
    </div>
  );
}
