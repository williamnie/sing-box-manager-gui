import { useEffect, useState } from 'react';
import { Button, Modal, ModalBody, ModalContent, ModalFooter, ModalHeader, Spinner, Switch, Textarea } from '@nextui-org/react';
import { domainBlocklistApi, blocklistSaveMessage } from '../api/domainBlocklist';
import type { DomainBlocklist } from '../api/domainBlocklist';
import { errorMessage } from '../api';
import { toast } from './Toast';

export default function DomainBlocklistEditor({ isOpen, onClose, onSaved }: { isOpen: boolean; onClose: () => void; onSaved: () => void }) {
  const [data, setData] = useState<DomainBlocklist | null>(null);
  const [domains, setDomains] = useState('');
  const [enabled, setEnabled] = useState(true);
  const [error, setError] = useState('');
  const [saving, setSaving] = useState(false);
  const [loading, setLoading] = useState(false);
  const [reload, setReload] = useState(0);
  useEffect(() => {
    if (!isOpen) return;
    const controller = new AbortController();
    setLoading(true); setData(null); setError('');
    void domainBlocklistApi.get(controller.signal).then(value => {
      if (controller.signal.aborted) return;
      setData(value); setDomains((value.rule?.values ?? []).join('\n')); setEnabled(value.rule?.enabled ?? true);
      if (!value.editable) setError('此集合已被改为其他匹配方式，请在分流规则中使用原规则的编辑按钮。');
    }).catch(cause => { if (!controller.signal.aborted) setError(errorMessage(cause, '读取拦截集合失败')); })
      .finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [isOpen, reload]);

  const save = async () => {
    if (!data || !data.editable) return;
    setSaving(true); setError('');
    try {
      const result = await domainBlocklistApi.save(domains.split('\n').map(value => value.trim()).filter(Boolean), data.revision, enabled);
      const message = blocklistSaveMessage(result);
      if (message) toast.success(message);
      onSaved(); onClose();
    } catch (cause) { setError(errorMessage(cause, '保存失败，输入已保留')); }
    finally { setSaving(false); }
  };

  return <Modal isOpen={isOpen} onClose={onClose} isDismissable={!saving} isKeyboardDismissDisabled={saving} hideCloseButton={saving} size="2xl" scrollBehavior="inside" classNames={{ base: 'rounded bg-white text-zinc-800 dark:bg-[#12141d] dark:text-zinc-200' }}>
    <ModalContent>
      <ModalHeader>编辑自定义拦截集合</ModalHeader>
      <ModalBody className="gap-4 text-sm">
        <p className="text-xs leading-relaxed text-zinc-500 dark:text-zinc-400">每行一个完整域名，例如 api-access.pangolin-sdk-toutiao1.com。此集合使用 REJECT 拒绝，仅匹配填写的域名，不扩大到其他子域名。DNS 页的一键拦截也保存在这里，上游广告库更新不会覆盖。</p>
        {loading ? <Spinner label="正在读取拦截集合" /> : <>
          <Textarea aria-label="自定义拦截域名" label="拦截域名" placeholder="ads.example.com\ntracking.example.com" value={domains} onValueChange={setDomains} minRows={10} maxRows={18} isDisabled={saving || !data?.editable} classNames={{ input: 'font-mono text-xs' }} />
          <div className="flex items-center justify-between gap-3"><span className="text-xs text-zinc-500">最多 5000 个域名；保存时去重。清空列表会移除此自定义集合。</span><Switch size="sm" isSelected={enabled} onValueChange={setEnabled} isDisabled={saving || !data?.editable} classNames={{ wrapper: "group-data-[selected=true]:bg-[#ff5722]" }}>启用拦截</Switch></div>
          <p className="text-xs leading-relaxed text-zinc-500 dark:text-zinc-400">{data?.auto_apply ? '保存会自动应用配置，运行中的内核可能短暂重启。' : '保存后需在配置审阅中手动应用。'} 设备整机策略、hosts 或更优先的规则可能先匹配；已有连接和应用缓存不代表新规则失效。</p>
        </>}
        {error && <div role="alert" className="rounded bg-rose-500/10 p-3 text-xs text-rose-700 dark:text-rose-300"><p>{error}</p><Button size="sm" variant="light" isDisabled={saving} onPress={() => setReload(value => value + 1)}>重新载入（替换当前输入）</Button></div>}
      </ModalBody>
      <ModalFooter><Button variant="light" isDisabled={saving} onPress={onClose}>取消</Button><Button className="rounded bg-[#ff5722] text-white" isLoading={saving} isDisabled={loading || !data?.editable} onPress={() => void save()}>保存拦截集合</Button></ModalFooter>
    </ModalContent>
  </Modal>;
}
