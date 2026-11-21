import { useEffect, useState } from 'react';
import { Button, Modal, ModalBody, ModalContent, ModalFooter, ModalHeader } from '@nextui-org/react';
import type { Settings, Node } from '../store';
import { errorMessage, nodeApi } from '../api';
import { toast } from './Toast';
import { proxyPlanApi, type ProxyPlan, type ProxyPlanPreview } from '../api/proxyPlan';
import { t } from '../i18n';

export default function ProxyPlanPanel({ settings, preferredNode, onClose, onSaved }: {
  settings: Settings; preferredNode: string; onClose: () => void; onSaved: () => Promise<void>;
}) {
  const [nodes, setNodes] = useState<Node[]>([]);
  const [selected, setSelected] = useState(preferredNode);
  const [preview, setPreview] = useState<ProxyPlanPreview | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  useEffect(() => {
    let active = true;
    nodeApi.getAll().then(response => { if (active) setNodes(response.data.data ?? []); }).catch(cause => { if (active) setError(errorMessage(cause, t('读取节点失败'))); });
    return () => { active = false; };
  }, []);
  const target = nodes.some(node => node.tag === selected) ? selected : nodes[0]?.tag ?? '';
  const plan: ProxyPlan = { primary: 'Proxy', merge_groups: [], managed_only: true, default_node: target };
  const inspect = async () => {
    setBusy(true); setError(''); setPreview(null);
    try { setPreview(await proxyPlanApi.preview(plan)); }
    catch (cause) { setError(errorMessage(cause, t('无法预览节点迁移'))); }
    finally { setBusy(false); }
  };
  const save = async () => {
    if (!preview) return;
    setBusy(true); setError('');
    try {
      const response = await proxyPlanApi.save(plan, preview.revision);
      toast.success(response.application === 'applied' ? t('节点来源已统一并应用') : t('节点来源已保存，请应用配置后生效'));
      await onSaved();
      onClose();
    } catch (cause) { setError(errorMessage(cause, t('迁移失败'))); setPreview(null); }
    finally { setBusy(false); }
  };
  return <Modal isOpen onClose={onClose} isDismissable={!busy} isKeyboardDismissDisabled={busy} hideCloseButton={busy} size="2xl" scrollBehavior="inside">
    <ModalContent><ModalHeader>{t('统一节点来源')}</ModalHeader><ModalBody className="space-y-4">
      <p className="text-sm leading-relaxed text-default-600">{t('代理节点统一由订阅与手动节点提供，自动生成 GLOBAL、Proxy、国家组和已启用的过滤器。旧分组会被移除，分流规则改用 Proxy；仍被单独引用的专用节点会移入手动节点。')}</p>
      <label className="space-y-2 text-sm"><span>{t('默认代理先使用哪个节点')}</span><select aria-label={t('默认代理节点')} value={target} disabled={busy || !nodes.length} onChange={event => { setSelected(event.target.value); setPreview(null); }} className="block h-10 w-full rounded border border-default-300 bg-content1 px-2">
        {!nodes.length && <option value="">{t('暂无启用节点')}</option>}{nodes.map(node => <option key={node.tag}>{node.tag}</option>)}
      </select></label>
      {preview && <section className="space-y-3 rounded border border-[#ff5722]/30 bg-[#ff5722]/5 p-3 text-xs">
        <h3 className="text-sm font-medium">{t('分组：{p1} → {p2}', { p1: preview.before.groups.length, p2: preview.after.groups.length })}</h3>
        <p>{t('默认出口：{p1} →', { p1: preview.before.final || t('未知') })} <strong>Proxy → {target || t('拦截（无可用节点）')}</strong></p>
        <p>{t('保留 {p1} 个节点，移除 {p2} 个旧节点记录。', { p1: preview.after.nodes.length, p2: preview.removed_nodes.length })}</p>
        {!!preview.adopted_nodes?.length && <p>{t('移入手动节点：')}{preview.adopted_nodes.join(t('、'))}</p>}
        <p>{t('现有设置将在迁移前完整备份。直连、拦截和规则匹配条件保持不变。')}</p>
        {preview.before_error && <p className="text-amber-800 dark:text-amber-300">{t('原草案存在错误，原分组数量取自已应用配置：')}{preview.before_error}</p>}
        <p className="font-medium">{settings.auto_apply ? t('确认后会重新应用配置，连接可能需要重连。') : t('确认后只保存，需另行应用配置。')}</p>
      </section>}
      {error && <p role="alert" className="text-sm text-rose-600 dark:text-rose-400">{error}</p>}
    </ModalBody><ModalFooter>
      <Button variant="flat" isDisabled={busy} onPress={onClose}>{t('取消')}</Button>
      <Button variant="bordered" isDisabled={busy} onPress={() => void inspect()}>{t('预览迁移')}</Button>
      <Button color="primary" isDisabled={busy || !preview} isLoading={busy} onPress={() => void save()}>{t('确认迁移')}</Button>
    </ModalFooter></ModalContent>
  </Modal>;
}
