import { useState } from 'react';
import { Button, Modal, ModalBody, ModalContent, ModalFooter, ModalHeader, Switch, Textarea } from '@nextui-org/react';
import { ExternalLink, Plus, X } from 'lucide-react';
import { errorMessage, ruleApi } from '../api';
import { toast } from './Toast';
import { actionLabel } from '../utils/rulePresentation';
import { importedMatchFields, importedBooleanFields, importedRuleInputs, importedRuleUpdates } from '../utils/importedRuleEditor';
import type { ImportedField, ImportedFlag, ImportedRule } from '../utils/importedRuleEditor';

export interface ImportedRuleSelection { index: number; rule: ImportedRule; revision: string; ruleSets: ImportedRule[] }
interface Props { selection: ImportedRuleSelection; outbounds: { value: string; label: string }[]; autoApply: boolean; onClose: () => void; onSaved: () => void }

export default function ImportedRuleEditor({ selection, outbounds, autoApply, onClose, onSaved }: Props) {
  const { rule, index, revision, ruleSets } = selection;
  const [fields, setFields] = useState(() => importedRuleInputs(rule));
  const [flags, setFlags] = useState<Record<ImportedFlag, boolean>>(() => ({ invert: rule.invert === true, ip_is_private: rule.ip_is_private === true, source_ip_is_private: rule.source_ip_is_private === true }));
  const [action, setAction] = useState(typeof rule.action === 'string' ? rule.action : 'route');
  const [outbound, setOutbound] = useState(typeof rule.outbound === 'string' ? rule.outbound : '');
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');
  const [adding, setAdding] = useState('');
  const canChangeAction = (!rule.action || rule.action === 'route' || rule.action === 'reject') && !['override_address', 'override_port', 'network_strategy', 'fallback_delay', 'udp_disable_domain_unmapping', 'udp_connect', 'udp_timeout', 'tls_fragment', 'tls_fragment_fallback_delay', 'tls_record_fragment'].some(key => key in rule);
  const advanced = Object.fromEntries(Object.entries(rule).filter(([key]) => !(key in importedMatchFields) && !(key in importedBooleanFields) && key !== 'outbound' && key !== 'action' && !(key === 'type' && rule.type === 'default')));
  const options = outbounds.some(option => option.value === outbound) || !outbound ? outbounds : [{ value: outbound, label: `${outbound}（当前值）` }, ...outbounds];
  const inputClass = 'w-full rounded border border-zinc-200 bg-white px-3 py-2 text-sm text-zinc-800 dark:border-white/10 dark:bg-[#191c26] dark:text-zinc-100';
  const save = async () => {
    setError('');
    try {
      const updates = importedRuleUpdates(rule, fields, flags, action, outbound);
      if (!Object.keys(updates).length) { onClose(); return; }
      setSaving(true);
      const result = await ruleApi.updateImported(index, revision, updates);
      if (!result.data.warning) toast.success(result.data.application === 'applied' ? '导入规则已更新并应用' : result.data.application === 'unchanged' ? '规则内容未变化' : '导入规则已保存，请在配置审阅中检查并应用');
      onSaved(); onClose();
    } catch (cause) { setError(errorMessage(cause, '保存失败，输入已保留')); }
    finally { setSaving(false); }
  };
  return <Modal isOpen onClose={onClose} isDismissable={!saving} isKeyboardDismissDisabled={saving} hideCloseButton={saving} size="3xl" scrollBehavior="inside" classNames={{ base: 'rounded bg-white text-zinc-800 dark:bg-[#12141d] dark:text-zinc-200' }}>
    <ModalContent>
      <ModalHeader>编辑导入规则 · 第 {index + 1} 条</ModalHeader>
      <ModalBody className="gap-4 py-4">
        <p className="text-xs leading-relaxed text-zinc-500 dark:text-zinc-400">修改保存在这条原规则中，顺序和未修改的条件保持不变，无需重新导入。列表每行一项；清空某类条件会移除该限制，至少保留一个匹配条件。</p>
        {(Object.keys(fields) as ImportedField[]).map(key => <div key={key} className="space-y-2">
          <div className="flex items-start gap-2"><Textarea aria-label={importedMatchFields[key]} label={importedMatchFields[key]} value={fields[key] ?? ''} onValueChange={value => setFields(current => ({ ...current, [key]: value }))} minRows={key === 'domain' ? 5 : 2} maxRows={10} isDisabled={saving} classNames={{ input: 'font-mono text-xs' }} /><Button isIconOnly size="sm" variant="light" isDisabled={saving} aria-label={`移除条件 ${importedMatchFields[key]}`} onPress={() => setFields(current => { const next = { ...current }; delete next[key]; return next; })}><X className="size-4" /></Button></div>
          {key === 'domain_keyword' && <p className="text-xs text-zinc-500">按文字包含关系匹配，星号不会自动变成通配符；现有内容会原样保留。</p>}
          {(key === 'port_range' || key === 'source_port_range') && <p className="text-xs text-zinc-500">每行填写一个范围，例如 6881:6999。</p>}
          {key === 'rule_set' && <div className="space-y-1 text-xs text-zinc-500 dark:text-zinc-400"><p>这里修改引用的规则集名称。远程库内的域名由上游维护，可通过自定义规则补充。</p>{(fields[key] ?? '').split('\n').filter(Boolean).map(tag => {
            const set = ruleSets.find(item => item.tag === tag.trim());
            const url = typeof set?.url === 'string' && /^https?:\/\//i.test(set.url) ? set.url : '';
            return <p key={tag} className="break-all">{tag}：{url ? <a href={url} target="_blank" rel="noopener noreferrer" className="text-[#ff5722] underline">查看规则集来源 <ExternalLink className="inline size-3" /></a> : set?.type === 'inline' ? '随配置导入的内联规则集' : '请使用现有规则集名称'}</p>;
          })}</div>}
        </div>)}
        <div className="flex gap-2"><select aria-label="添加匹配条件" value={adding} onChange={event => setAdding(event.target.value)} disabled={saving} className={inputClass}><option value="">选择要添加的条件</option>{Object.entries(importedMatchFields).filter(([key]) => !(key in fields)).map(([key, label]) => <option key={key} value={key}>{label}</option>)}</select><Button size="sm" className="shrink-0" isDisabled={!adding || saving} startContent={<Plus className="size-3.5" />} onPress={() => { setFields(current => ({ ...current, [adding]: '' })); setAdding(''); }}>添加条件</Button></div>
        <div className="flex flex-wrap gap-4">{(Object.keys(importedBooleanFields) as ImportedFlag[]).map(key => <Switch key={key} size="sm" isDisabled={saving} isSelected={flags[key]} onValueChange={value => setFlags(current => ({ ...current, [key]: value }))} classNames={{ wrapper: "group-data-[selected=true]:bg-[#ff5722]" }}>{importedBooleanFields[key]}</Switch>)}</div>
        <div className="grid gap-3 sm:grid-cols-2"><label className="space-y-1 text-xs"><span>处理方式</span>{canChangeAction ? <select aria-label="导入规则处理方式" value={action} onChange={event => setAction(event.target.value)} disabled={saving} className={inputClass}><option value="route">指定出站</option><option value="reject">拒绝（REJECT）</option></select> : <p className={inputClass}>{actionLabel(rule, [])}（保留当前动作）</p>}</label>{action === 'route' && <label className="space-y-1 text-xs"><span>目标出站</span><select aria-label="导入规则目标出站" value={outbound} onChange={event => setOutbound(event.target.value)} disabled={saving} className={inputClass}><option value="" disabled>请选择出站</option>{options.map(option => <option key={option.value} value={option.value}>{option.label}</option>)}</select></label>}</div>
        {Object.keys(advanced).length > 0 && <details className="text-xs text-zinc-500 dark:text-zinc-400"><summary className="cursor-pointer">保留的高级条件和动作参数</summary><p className="my-2">下列内容不由此表单改写，组合规则仍按原有逻辑执行。</p><pre className="max-h-52 overflow-auto whitespace-pre-wrap break-all rounded bg-zinc-100 p-3 dark:bg-black/30">{JSON.stringify(advanced, null, 2)}</pre></details>}
        <p className="text-xs text-zinc-500 dark:text-zinc-400">{autoApply ? '保存会自动检查并应用配置，运行中的内核可能短暂重启。' : '当前关闭了自动应用，保存后需在配置审阅中检查并应用。'} 重新导入完整配置可能替换这里的修改。</p>
        {error && <p role="alert" className="rounded bg-rose-500/10 p-3 text-sm text-rose-700 dark:text-rose-300">{error}</p>}
      </ModalBody>
      <ModalFooter><Button variant="light" isDisabled={saving} onPress={onClose}>取消</Button><Button className="rounded bg-[#ff5722] text-white" isLoading={saving} onPress={() => void save()}>保存规则</Button></ModalFooter>
    </ModalContent>
  </Modal>;
}
