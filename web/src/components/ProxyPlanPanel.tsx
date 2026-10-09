import { useState } from 'react';
import { Button, Modal, ModalBody, ModalContent, ModalFooter, ModalHeader } from '@nextui-org/react';
import { Link } from 'react-router-dom';
import type { Filter, Settings } from '../store';
import { errorMessage } from '../api';
import { proxyPlanApi, type ProxyPlan, type ProxyPlanPreview } from '../api/proxyPlan';

export default function ProxyPlanPanel({ settings, filters, onClose, onSaved }: {
  settings: Settings; filters: Filter[]; onClose: () => void; onSaved: () => Promise<void>;
}) {
  const groups = (Array.isArray(settings.imported_policy?.outbounds) ? settings.imported_policy.outbounds : [])
    .filter((o: Record<string, unknown>) => o.type === 'selector' || o.type === 'urltest')
    .map((o: Record<string, unknown>) => String(o.tag));
  const available = filters.filter(filter => filter.enabled);
  const [plan, setPlan] = useState<ProxyPlan>(settings.proxy_plan ?? { primary: available.find(filter => filter.mode !== 'urltest')?.name ?? available[0]?.name ?? '', merge_groups: groups });
  const [restore, setRestore] = useState(false);
  const [preview, setPreview] = useState<ProxyPlanPreview | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [result, setResult] = useState('');
  const edit = (next: ProxyPlan) => { setPlan(next); setRestore(false); setPreview(null); setResult(''); setError(''); };
  const inspect = async (undo = false) => {
    setBusy(true); setError(''); setPreview(null); setRestore(undo); setResult('');
    try { setPreview(await proxyPlanApi.preview(undo ? null : plan)); }
    catch (cause) { setError(errorMessage(cause, '无法预览分组整理')); }
    finally { setBusy(false); }
  };
  const save = async () => {
    if (!preview) return;
    setBusy(true); setError('');
    try {
      const response = await proxyPlanApi.save(restore ? null : plan, preview.revision);
      setResult(response.application === 'applied' ? '方案已保存并应用，代理页已刷新。' : response.application === 'failed' ? '方案已保存，但应用失败。请到配置审阅核对运行状态。' : '方案已保存，尚未应用到运行内核。请在配置审阅或部署与设备中应用。');
      setPreview(null);
      await onSaved();
    } catch (cause) { setError(errorMessage(cause, '保存整理方案失败')); setPreview(null); }
    finally { setBusy(false); }
  };
  return <Modal isOpen onClose={onClose} isDismissable={!busy} isKeyboardDismissDisabled={busy} hideCloseButton={busy} size="2xl" scrollBehavior="inside">
    <ModalContent><ModalHeader>整理代理分组</ModalHeader><ModalBody className="space-y-4">
      <p className="text-sm leading-relaxed text-default-600">统一默认代理入口，取消自动生成的国家组、Managed 组和分类选择组。原始导入配置会保留，可以预览并撤销整理。</p>
      {available.length ? <label className="space-y-2 text-sm"><span>默认代理组</span><select aria-label="默认代理组" value={plan.primary} disabled={busy} onChange={event => edit({ ...plan, primary: event.target.value })} className="block h-10 w-full rounded border border-default-300 bg-content1 px-2">
        <option value="" disabled>请选择</option>{available.map(filter => <option key={filter.id} value={filter.name}>{filter.name} · {filter.mode === 'urltest' ? '自动测速' : '手动选择'}</option>)}
      </select></label> : <p className="text-sm text-amber-800 dark:text-amber-300">请先在<Link to="/subscriptions?tab=filters" onClick={onClose} className="underline">节点管理的过滤器</Link>中创建并启用一个代理组。</p>}
      <p className="text-xs leading-relaxed text-default-500">默认组继续使用它自己的节点成员。需要固定出口时，设为手动选择；自动测速只在你主动启用的过滤器中保留。</p>
      {groups.length > 0 && <section className="space-y-2">
        <h3 className="text-sm font-medium">哪些旧分组并入默认代理？</h3>
        <p className="text-xs leading-relaxed text-default-500">勾选的分组及引用它们的规则都会改用默认代理。需要不同地区或独立出口的分组请取消勾选。直连、拦截动作不会合并。</p>
        <div className="flex gap-4 text-xs"><button disabled={busy} className="text-[#ff5722]" onClick={() => edit({ ...plan, merge_groups: groups })}>全部合并</button><button disabled={busy} className="text-[#ff5722]" onClick={() => edit({ ...plan, merge_groups: [] })}>全部保留独立</button></div>
        <div className="grid max-h-48 grid-cols-1 gap-2 overflow-auto rounded border border-default-200 p-3 sm:grid-cols-2">{groups.map(tag => <label key={tag} className="flex items-start gap-2 break-all text-xs"><input type="checkbox" disabled={busy} checked={plan.merge_groups.includes(tag)} onChange={event => edit({ ...plan, merge_groups: event.target.checked ? [...plan.merge_groups, tag] : plan.merge_groups.filter(value => value !== tag) })} />{tag}</label>)}</div>
      </section>}
      {preview && <section className="space-y-3 rounded border border-[#ff5722]/30 bg-[#ff5722]/5 p-3 text-xs">
        <h3 className="text-sm font-medium">{restore ? '撤销整理预览' : '整理预览'}：{preview.before.groups.length} → {preview.after.groups.length} 个分组</h3>
        <p>默认出口：{preview.before.final || '未知'} → <strong>{preview.after.final}</strong></p>
        <p className="break-words">保留分组：{preview.after.groups.join('、') || '无'}</p>
        <p>运行配置保留 {preview.after.nodes.length} 个节点；{preview.removed_nodes.length} 个不再被引用的节点将退出运行配置，原始数据仍保留。</p>
        {preview.removed_nodes.length > 0 && <details><summary className="cursor-pointer">查看退出运行配置的节点</summary><ul className="mt-2 max-h-36 list-inside list-disc overflow-auto break-words">{preview.removed_nodes.map(tag => <li key={tag}>{tag}</li>)}</ul></details>}
        {preview.before_error && <p className="text-amber-800 dark:text-amber-300">原草案无法生成：{preview.before_error}；上述原分组数量不完整。</p>}
        <p className="font-medium">{preview.auto_apply ? '自动应用已开启：确认后会更新运行配置，后续连接可能改用新的出口。' : '自动应用已关闭：确认后只保存方案，需要另行应用。'}</p>
      </section>}
      {error && <p role="alert" className="text-sm text-rose-600 dark:text-rose-400">{error}</p>}
      {result && <p role="status" className="text-sm text-default-700">{result}</p>}
    </ModalBody><ModalFooter className="flex-wrap">
      {settings.proxy_plan && <Button variant="light" size="sm" isDisabled={busy} onPress={() => void inspect(true)}>预览撤销整理</Button>}
      <Button variant="flat" isDisabled={busy} onPress={onClose}>关闭</Button>
      <Button variant="bordered" isDisabled={busy || !plan.primary} onPress={() => void inspect()}>预览整理</Button>
      <Button color="primary" isDisabled={busy || !preview} isLoading={busy} onPress={() => void save()}>{restore ? '确认撤销' : '确认保存方案'}</Button>
    </ModalFooter></ModalContent>
  </Modal>;
}
