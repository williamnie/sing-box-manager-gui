import { useEffect, useState, useCallback, useRef } from 'react';
import {
  Switch,
  Button,
  Modal,
  ModalContent,
  ModalHeader,
  ModalBody,
  ModalFooter,
  Input,
  Textarea,
  useDisclosure,
  Spinner,
} from '@nextui-org/react';
import { AppSelect, SelectItem } from '../components/AppSelect';
import { Plus, CheckCircle, XCircle } from 'lucide-react';
import { useStore } from '../store';
import { ruleSetApi } from '../api';
import type { Rule, RuleGroup } from '../store';
import ConfirmModal from '../components/ConfirmModal';
import ListInput from '../components/ListInput';
import { parseList } from '../utils/lists';
import { toast } from '../components/Toast';
import RuleOverview from '../components/RuleOverview';
import { createSTUNRule, isScopedSTUNRule, isSTUNOutboundAllowed, validateSTUNRule } from '../utils/rulePolicy';

// 规则集验证结果类型
interface ValidationResult {
  valid: boolean;
  url: string;
  tag: string;
  message: string;
}

const baseOutboundOptions = [
  { value: 'Proxy', label: 'Proxy (代理)' },
  { value: 'DIRECT', label: 'DIRECT (直连)' },
  { value: 'REJECT', label: 'REJECT (拦截)' },
];

const ruleTypeOptions = [
  { value: 'domain_suffix', label: '域名后缀 (domain_suffix)' },
  { value: 'domain_keyword', label: '域名关键字 (domain_keyword)' },
  { value: 'domain', label: '完整域名 (domain)' },
  { value: 'ip_cidr', label: 'IP 段 (ip_cidr)' },
  { value: 'geosite', label: 'GeoSite 规则集' },
  { value: 'geoip', label: 'GeoIP 规则集' },
  { value: 'port', label: '端口 (port)' },
  { value: 'port_range', label: '目标端口范围 (port_range)' },
  { value: 'match', label: '联合条件 (match)' },
  { value: 'process_name', label: '本机进程名 (process_name)' },
];

const defaultRule: Omit<Rule, 'id'> = {
  name: '',
  rule_type: 'domain_suffix',
  values: [],
  outbound: 'Proxy',
  enabled: true,
  priority: 100,
};

export default function Rules() {
  const {
    ruleGroups,
    rules,
    filters,
    countryGroups,
    subscriptions,
    manualNodes,
    fetchRuleGroups,
    fetchRules,
    fetchFilters,
    fetchCountryGroups,
    fetchSubscriptions,
    fetchManualNodes,
    fetchSettings,
    toggleRuleGroup,
    updateRuleGroupOutbound,
    addRule,
    updateRule,
    deleteRule,
    settings,
  } = useStore();

  const { isOpen, onOpen, onClose } = useDisclosure();
  const [editingRule, setEditingRule] = useState<Rule | null>(null);
  const [formData, setFormData] = useState<Omit<Rule, 'id'>>(defaultRule);
  const [valuesText, setValuesText] = useState('');
  const [deleteTarget, setDeleteTarget] = useState<Rule | null>(null);
  const [portsText, setPortsText] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [overviewRevision, setOverviewRevision] = useState(0);
  const [stunTemplate, setSTUNTemplate] = useState(false);
  const [requireSource, setRequireSource] = useState(false);
  const [sourceText, setSourceText] = useState('');

  // 规则集验证状态
  const [validationResults, setValidationResults] = useState<Record<string, ValidationResult>>({});
  const [isValidating, setIsValidating] = useState(false);
  const validationTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    fetchRuleGroups();
    fetchRules();
    fetchFilters();
    fetchCountryGroups();
    fetchSubscriptions();
    fetchManualNodes();
    fetchSettings();
  }, [fetchRuleGroups, fetchRules, fetchFilters, fetchCountryGroups, fetchSubscriptions, fetchManualNodes, fetchSettings]);

  // 验证规则集（防抖）
  const validateRuleSet = useCallback(async (type: 'geosite' | 'geoip', names: string[]) => {
    if (names.length === 0) {
      setValidationResults({});
      return;
    }

    setIsValidating(true);
    const results: Record<string, ValidationResult> = {};

    for (const name of names) {
      if (!name.trim()) continue;
      try {
        const response = await ruleSetApi.validate(type, name.trim());
        results[name] = response.data;
      } catch {
        results[name] = {
          valid: false,
          url: '',
          tag: '',
          message: '验证请求失败',
        };
      }
    }

    setValidationResults(results);
    setIsValidating(false);
  }, []);

  // 当规则值改变时触发验证（防抖 500ms）
  useEffect(() => {
    if (formData.rule_type !== 'geosite' && formData.rule_type !== 'geoip') {
      setValidationResults({});
      return;
    }

    const names = valuesText
      .split('\n')
      .map((v) => v.trim())
      .filter((v) => v);

    if (names.length === 0) {
      setValidationResults({});
      return;
    }

    if (validationTimerRef.current) {
      clearTimeout(validationTimerRef.current);
    }

    validationTimerRef.current = setTimeout(() => {
      validateRuleSet(formData.rule_type as 'geosite' | 'geoip', names);
    }, 500);

    return () => {
      if (validationTimerRef.current) {
        clearTimeout(validationTimerRef.current);
      }
    };
  }, [valuesText, formData.rule_type, validateRuleSet]);

  // 检查是否所有规则集都验证通过
  const allValidationsPassed = useCallback(() => {
    if (formData.rule_type !== 'geosite' && formData.rule_type !== 'geoip') {
      return true;
    }

    const names = valuesText
      .split('\n')
      .map((v) => v.trim())
      .filter((v) => v);

    if (names.length === 0) return false;

    return names.every((name) => validationResults[name]?.valid);
  }, [formData.rule_type, valuesText, validationResults]);

  // 合并导入出站与管理器节点，按标签去重；后端负责拒绝真实配置中的同名冲突。
  const getAllOutboundOptions = () => {
    const options = new Map(baseOutboundOptions.map((option) => [option.value, option]));
    const addOption = (value: string, label: string) => {
      if (!options.has(value)) options.set(value, { value, label });
    };
    const imported = settings?.imported_policy;
    if (Array.isArray(imported?.outbounds)) {
      for (const outbound of imported.outbounds) {
        if (outbound && typeof outbound === 'object' && 'tag' in outbound && typeof outbound.tag === 'string') {
          addOption(outbound.tag, `${outbound.tag} (已导入)`);
        }
      }
    }
    const nativeNodes = [
      ...subscriptions.filter((subscription) => subscription.enabled).flatMap((subscription) => subscription.nodes || []),
      ...manualNodes.filter((node) => node.enabled).map((node) => node.node),
    ];
    if (nativeNodes.length > 0) {
      for (const group of ['Auto', 'Proxy', 'Final']) {
        const tag = imported ? `Managed ${group}` : group;
        addOption(tag, `${tag} (管理器选择组)`);
      }
    }
    for (const node of nativeNodes) addOption(node.tag, `${node.tag} (节点)`);

    // 添加国家节点组
    countryGroups.forEach((group) => {
      const label = `${group.emoji} ${group.name}`;
      addOption(label, `${label} (${group.node_count}节点)`);
    });

    // 添加过滤器
    filters.forEach((filter) => {
      if (filter.enabled) {
        addOption(filter.name, `${filter.name} (过滤器)`);
      }
    });

    return [...options.values()];
  };

  const handleToggle = async (id: string, enabled: boolean) => {
    await toggleRuleGroup(id, enabled);
    setOverviewRevision((value) => value + 1);
  };

  const handleOutboundChange = async (group: RuleGroup, outbound: string) => {
    await updateRuleGroupOutbound(group.id, outbound);
    setOverviewRevision((value) => value + 1);
  };

  const handleAddRule = () => {
    setSTUNTemplate(false);
    setRequireSource(false);
    setSourceText('');
    setEditingRule(null);
    setFormData(defaultRule);
    setValuesText('');
    setPortsText('');
    setValidationResults({});
    onOpen();
  };

  const handleEditRule = (rule: Rule) => {
    const scopedSTUN = isScopedSTUNRule(rule);
    setRequireSource(scopedSTUN);
    setSTUNTemplate(scopedSTUN && !validateSTUNRule(rule, getSTUNOutboundOptions().map((option) => option.value)));
    setSourceText((rule.source_cidrs || []).join('\n'));
    setEditingRule(rule);
    setFormData({
      name: rule.name,
      rule_type: rule.rule_type,
      values: rule.values,
      outbound: rule.outbound,
      enabled: rule.enabled,
      priority: rule.priority,
      source_cidrs: rule.source_cidrs || [],
      network: rule.network || [],
      protocol: rule.protocol || [],
      ports: rule.ports || [],
      port_ranges: rule.port_ranges || [],
      process_names: rule.process_names || [],
    });
    setValuesText((rule.values || []).join('\n'));
    setPortsText((rule.ports || []).join(', '));
    setValidationResults({});
    onOpen();
  };

  const handleDeleteRule = (rule: Rule) => setDeleteTarget(rule);

  const getSTUNOutboundOptions = () => {
    const importedTypes = new Map<string, string>();
    const imported = settings?.imported_policy?.outbounds;
    if (Array.isArray(imported)) {
      for (const outbound of imported) {
        if (outbound && typeof outbound === 'object' && typeof outbound.tag === 'string') {
          importedTypes.set(outbound.tag, String(outbound.type));
        }
      }
    }
    return getAllOutboundOptions().filter((option) => isSTUNOutboundAllowed(option.value, importedTypes.get(option.value)));
  };
  const importedOutbounds = settings?.imported_policy?.outbounds;
  const stunRejectConflict = Array.isArray(importedOutbounds) && importedOutbounds.some((outbound) =>
    outbound && typeof outbound === 'object' && outbound.tag === 'REJECT' && outbound.type !== 'block');

  const handleAddSTUNRule = () => {
    const options = getSTUNOutboundOptions();
    const outbound = ['Managed Proxy', 'Proxy'].find((tag) => options.some((option) => option.value === tag))
      || options.find((option) => option.value !== 'REJECT')?.value || options[0]?.value || '';
    setEditingRule(null);
    setSTUNTemplate(true);
    setRequireSource(true);
    setSourceText('');
    setFormData(createSTUNRule(rules, outbound));
    setValuesText(''); setPortsText(''); setValidationResults({});
    onOpen();
  };

  const handleSubmit = async () => {
    const values = valuesText
      .split('\n')
      .map((v) => v.trim())
      .filter((v) => v);

    const ruleData = {
      ...formData,
      values,
      ports: parseList(portsText).map(Number),
    };
    if (requireSource && !ruleData.source_cidrs?.length) {
      toast.error('请填写来源 IP / CIDR，不能将此 STUN 规则扩展到所有设备'); return;
    }
    if (stunTemplate) {
      const error = validateSTUNRule(ruleData, getSTUNOutboundOptions().map((option) => option.value));
      if (error) { toast.error(error); return; }
    }
    if (ruleData.ports.some((port) => !Number.isInteger(port) || port < 1 || port > 65535)) {
      toast.error('目标端口必须是 1–65535 的整数'); return;
    }
    if (settings?.deployment_role === 'gateway' && (ruleData.rule_type === 'process_name' || ruleData.process_names?.length)) {
      toast.error('家庭网关不能识别局域网设备的远端进程名，请使用来源地址联合匹配'); return;
    }
    setSubmitting(true);
    try {
      if (editingRule) await updateRule(editingRule.id, ruleData);
      else await addRule(ruleData);
      setOverviewRevision((value) => value + 1);
      onClose();
    } catch {
      // Store 已显示错误，保留输入以便修正后重试。
    } finally { setSubmitting(false); }
  };

  const handleToggleCustomRule = async (rule: Rule) => {
    await updateRule(rule.id, { ...rule, enabled: !rule.enabled });
    setOverviewRevision((value) => value + 1);
  };

  return (
    <div className="space-y-6">
      {/* 极客头部与操作按钮 */}
      <div className="flex flex-col md:flex-row md:items-end justify-between gap-4 border-b border-zinc-200/80 dark:border-white/[0.08] pb-5">
        <div>
          <div className="flex items-center gap-2 mb-1.5 font-mono text-[11px] text-[#ff5722] tracking-wider uppercase font-semibold">
            <span>[ TRAFFIC // ROUTING POLICY ]</span>
            <span className="text-zinc-400 dark:text-zinc-600">--</span>
            <span className="text-zinc-500 dark:text-zinc-400">DECISION PIPELINE</span>
          </div>
          <h1 className="text-2xl font-bold tracking-tight text-zinc-900 dark:text-white font-sans flex items-center gap-3">
            分流规则引擎
          </h1>
        </div>

        <div className="flex flex-wrap items-center gap-2.5 font-mono text-xs">
          <Button
            size="sm"
            className="rounded-[3px] font-mono text-xs border border-zinc-200 dark:border-white/[0.1] bg-white dark:bg-[#12141d] hover:bg-zinc-100 dark:hover:bg-[#181c28] text-zinc-700 dark:text-zinc-300 shadow-2xs"
            onPress={handleAddSTUNRule}
          >
            添加 STUN 专用规则
          </Button>
          <Button
            size="sm"
            className="rounded-[3px] font-mono text-xs bg-[#ff5722] hover:bg-[#ff6e40] text-white font-semibold uppercase tracking-wider shadow-geek-glow"
            startContent={<Plus size={14} className="fill-current" />}
            onPress={handleAddRule}
          >
            添加分流规则
          </Button>
        </div>
      </div>

      {settings?.deployment_role === 'gateway' && settings.gateway?.access_mode === 'dns' && (
        <div className="relative rounded-[3px] border border-amber-500/20 bg-amber-500/5 p-3.5 text-xs text-amber-800 dark:text-amber-200/90 font-mono leading-relaxed overflow-hidden">
          <div className="absolute left-0 top-0 bottom-0 w-1 bg-amber-400"></div>
          <span className="font-bold text-amber-800 dark:text-amber-300 mr-2">[DNS_BYPASS_NOTICE]</span>
          DNS 分流旁路：域名规则决定真实地址或 FakeIP；端口、协议等条件仍仅匹配进入实例的连接，不扩大为整域 DNS 规则。STUN、硬编码 IP、应用自带 DoH 和 IPv6 旁路流量可能不经过实例。
        </div>
      )}

      <RuleOverview
        autoApply={settings?.auto_apply === true}
        revision={overviewRevision}
        rules={rules}
        groups={ruleGroups}
        outbounds={settings?.imported_policy?.outbounds}
        outboundOptions={getAllOutboundOptions()}
        onEdit={handleEditRule}
        onDelete={handleDeleteRule}
        onToggle={handleToggleCustomRule}
        onToggleGroup={handleToggle}
        onGroupOutbound={handleOutboundChange}
      />

      {/* 添加/编辑规则弹窗 */}
      <Modal
        isOpen={isOpen}
        onClose={onClose}
        size="2xl"
        scrollBehavior="inside"
        classNames={{
          base: "bg-[#0b0c10] border border-white/[0.12] text-zinc-100 rounded-[4px] shadow-[0_16px_50px_rgba(0,0,0,0.85)]",
          header: "border-b border-white/[0.08] font-mono text-sm tracking-wide text-white py-3.5 px-5",
          body: "py-5 px-5 max-h-[75vh] overflow-y-auto",
          footer: "border-t border-white/[0.08] py-3 px-5 bg-black/30",
        }}
      >
        <ModalContent>
          <ModalHeader className="flex items-center gap-2">
            <span className="size-2 rounded-full bg-[#ff5722]"></span>
            <span>{stunTemplate ? (editingRule ? '编辑限定来源的 STUN 规则' : '添加限定来源的 STUN 规则') : editingRule ? '编辑分流规则' : '添加分流规则'}</span>
          </ModalHeader>
          <ModalBody>
            <div className="space-y-4">
              <Input
                label="规则标识名称"
                placeholder="例如：流媒体直连、广告域名屏蔽"
                value={formData.name}
                onChange={(e) => setFormData({ ...formData, name: e.target.value })}
                variant="bordered"
                classNames={{
                  inputWrapper: "bg-black/40 border-white/[0.1] hover:border-white/[0.2] focus-within:!border-[#ff5722] rounded-[3px]",
                  label: "text-zinc-400 font-mono text-xs",
                  input: "font-mono text-xs text-white",
                }}
              />

              {stunTemplate ? (
                <div className="space-y-1.5 text-xs rounded-[3px] bg-cyan-500/10 border border-cyan-500/20 p-3 font-mono text-cyan-200/90 leading-relaxed">
                  <p>仅处理指定来源设备的 STUN 流量，不改变普通网站、全部 UDP 或 BT / PT 的分流。</p>
                  <p>请选择支持所需流量的真实代理出站，或 REJECT 拒绝。</p>
                </div>
              ) : (
                <AppSelect
                  label="匹配类型 (Rule Type)"
                  selectedKeys={[formData.rule_type]}
                  onChange={(e) => setFormData({ ...formData, rule_type: e.target.value })}
                >
                  {ruleTypeOptions.map((opt) => (
                    <SelectItem key={opt.value} value={opt.value}>
                      {opt.label}
                    </SelectItem>
                  ))}
                </AppSelect>
              )}

              {stunTemplate && stunRejectConflict && (
                <p role="alert" className="text-xs font-mono text-amber-800 dark:text-amber-300 p-2 rounded bg-amber-500/10 border border-amber-500/20">
                  [WARN] 导入配置的 REJECT 标签被非拒绝出站占用，已从此便捷设置中排除。
                </p>
              )}

              {formData.rule_type !== 'match' ? (
                <Textarea
                  label="规则匹配值 (Values)"
                  placeholder={
                    formData.rule_type === 'domain_suffix'
                      ? '每行一个域名后缀，例如：\ngoogle.com\nyoutube.com'
                      : formData.rule_type === 'ip_cidr'
                      ? '每行一个 IP 段，例如：\n192.0.2.0/24\n198.51.100.0/24'
                      : formData.rule_type === 'geosite'
                      ? '每行一个 geosite 规则集名称，例如：\ngoogle\nyoutube\ncursor'
                      : formData.rule_type === 'geoip'
                      ? '每行一个 geoip 规则集名称，例如：\ncn\ngoogle'
                      : '每行一个值'
                  }
                  value={valuesText}
                  onChange={(e) => setValuesText(e.target.value)}
                  minRows={4}
                  variant="bordered"
                  classNames={{
                    inputWrapper: "bg-black/40 border-white/[0.1] hover:border-white/[0.2] focus-within:!border-[#ff5722] rounded-[3px]",
                    label: "text-zinc-400 font-mono text-xs",
                    input: "font-mono text-xs text-white",
                  }}
                />
              ) : !stunTemplate ? (
                <p className="text-xs font-mono text-zinc-500">联合条件规则可匹配来源、网络、协议或端口，至少填写一项。</p>
              ) : null}

              {stunTemplate && settings?.devices?.some((device) => device.enabled && device.addresses?.length) && (
                <AppSelect
                  label="从已保存设备快速填入来源"
                  selectedKeys={[]}
                  onChange={(event) => {
                    const device = settings.devices?.find((item) => item.id === event.target.value);
                    if (device) { setSourceText(device.addresses.join('\n')); setFormData({ ...formData, source_cidrs: device.addresses }); }
                  }}
                >
                  {settings.devices.filter((device) => device.enabled && device.addresses?.length).map((device) => (
                    <SelectItem key={device.id} textValue={device.name || device.id}>
                      {device.name || '未命名设备'} · {device.addresses.join(', ')}
                    </SelectItem>
                  ))}
                </AppSelect>
              )}

              <div className="grid sm:grid-cols-2 gap-4" key={editingRule?.id || 'new'}>
                <Textarea
                  label="来源 IP / CIDR"
                  isRequired={requireSource}
                  value={sourceText}
                  onValueChange={(value) => { setSourceText(value); setFormData({ ...formData, source_cidrs: parseList(value) }); }}
                  placeholder="例如 192.0.2.10/32"
                  description={requireSource ? '必填，只处理可观察到的指定来源' : '留空表示匹配所有设备来源'}
                  minRows={1}
                  maxRows={5}
                  variant="bordered"
                  classNames={{
                    inputWrapper: "bg-black/40 border-white/[0.1] rounded-[3px]",
                    label: "text-zinc-400 font-mono text-xs",
                    input: "font-mono text-xs text-white",
                  }}
                />
                {!stunTemplate ? (
                  <>
                    <AppSelect
                      label="传输网络"
                      selectionMode="multiple"
                      selectedKeys={formData.network || []}
                      onSelectionChange={(keys) => setFormData({ ...formData, network: Array.from(keys) as ('tcp' | 'udp')[] })}
                    >
                      <SelectItem key="tcp">TCP</SelectItem>
                      <SelectItem key="udp">UDP</SelectItem>
                    </AppSelect>
                    <ListInput label="识别协议" values={formData.protocol || []} onChange={(protocol) => setFormData({ ...formData, protocol })} placeholder="例如 dns、http、tls、stun" />
                    <Input
                      label="目标端口"
                      value={portsText}
                      onValueChange={setPortsText}
                      placeholder="443, 853"
                      variant="bordered"
                      classNames={{
                        inputWrapper: "bg-black/40 border-white/[0.1] rounded-[3px]",
                        label: "text-zinc-400 font-mono text-xs",
                        input: "font-mono text-xs text-white",
                      }}
                    />
                    <ListInput label="目标端口范围" values={formData.port_ranges || []} onChange={(port_ranges) => setFormData({ ...formData, port_ranges })} placeholder="例如 6881:6999" description="宽范围可能误命中普通流量" />
                    {settings?.deployment_role !== 'gateway' ? (
                      <ListInput label="本机进程名" values={formData.process_names || []} onChange={(process_names) => setFormData({ ...formData, process_names })} description="仅适用于单机本地进程" />
                    ) : (
                      <p className="text-xs font-mono text-amber-800 dark:text-amber-300 p-2.5 rounded bg-amber-500/10 border border-amber-500/20">网关模式无法识别局域网设备上的进程名。</p>
                    )}
                  </>
                ) : (
                  <p className="text-xs font-mono text-zinc-400 p-2.5 rounded bg-black/40 border border-white/[0.06]">
                    协议: STUN；不限定端口，不误伤其他 UDP。
                  </p>
                )}
              </div>

              {/* 规则集验证结果显示 */}
              {(formData.rule_type === 'geosite' || formData.rule_type === 'geoip') && valuesText.trim() && (
                <div className="space-y-2 p-3 rounded-[3px] bg-black/40 border border-white/[0.06]">
                  <div className="flex items-center gap-2 font-mono text-xs text-zinc-400">
                    <span>规则集预检结果:</span>
                    {isValidating && <Spinner size="sm" />}
                  </div>
                  <div className="space-y-1 max-h-32 overflow-y-auto font-mono text-xs">
                    {valuesText
                      .split('\n')
                      .map((v) => v.trim())
                      .filter((v) => v)
                      .map((name) => {
                        const result = validationResults[name];
                        if (!result) {
                          return (
                            <div key={name} className="flex items-center gap-2 text-zinc-500">
                              <Spinner size="sm" />
                              <span>{name} - 验证中...</span>
                            </div>
                          );
                        }
                        return (
                          <div
                            key={name}
                            className={`flex items-center gap-2 ${
                              result.valid ? 'text-emerald-400' : 'text-rose-400'
                            }`}
                          >
                            {result.valid ? <CheckCircle className="size-3.5" /> : <XCircle className="size-3.5" />}
                            <span className="font-semibold">{name}</span>
                            <span className="text-[10px] text-zinc-500">- {result.message}</span>
                          </div>
                        );
                      })}
                  </div>
                </div>
              )}

              <AppSelect
                label="目标出站 (Outbound)"
                selectedKeys={[formData.outbound]}
                onChange={(e) => setFormData({ ...formData, outbound: e.target.value })}
              >
                {(stunTemplate ? getSTUNOutboundOptions() : getAllOutboundOptions()).map((opt) => (
                  <SelectItem key={opt.value} value={opt.value}>
                    {opt.label}
                  </SelectItem>
                ))}
              </AppSelect>

              <Input
                type="number"
                label="优先级 (Priority)"
                placeholder="数字越小优先级越高 (如 10, 50, 100)"
                value={String(formData.priority)}
                onChange={(e) =>
                  setFormData({ ...formData, priority: e.target.value === '' ? 100 : Number(e.target.value) })
                }
                variant="bordered"
                classNames={{
                  inputWrapper: "bg-black/40 border-white/[0.1] rounded-[3px]",
                  label: "text-zinc-400 font-mono text-xs",
                  input: "font-mono text-xs text-white",
                }}
              />

              <div className="flex items-center justify-between p-2.5 rounded-[3px] bg-white/[0.02] border border-white/[0.05]">
                <span className="font-mono text-xs text-zinc-300">是否立即启用该规则</span>
                <Switch
                  size="sm"
                  isSelected={formData.enabled}
                  onValueChange={(enabled) => setFormData({ ...formData, enabled })}
                  classNames={{ wrapper: "group-data-[selected=true]:bg-[#ff5722]" }}
                />
              </div>
            </div>
          </ModalBody>
          <ModalFooter>
            <Button
              size="sm"
              variant="flat"
              onPress={onClose}
              className="rounded-[2px] font-mono text-xs border border-white/[0.1] bg-white/[0.05] text-zinc-300 hover:text-white"
            >
              取消
            </Button>
            <Button
              size="sm"
              className="rounded-[2px] font-mono text-xs bg-[#ff5722] hover:bg-[#ff6e40] text-black font-semibold uppercase tracking-wider"
              onPress={handleSubmit}
              isLoading={submitting}
              isDisabled={!formData.name.trim() || (requireSource && !formData.source_cidrs?.length) || (formData.rule_type !== 'match' && !valuesText.trim()) || isValidating || !allValidationsPassed()}
            >
              {editingRule ? '保存修改' : '确认添加'}
            </Button>
          </ModalFooter>
        </ModalContent>
      </Modal>
      <ConfirmModal isOpen={!!deleteTarget} title="删除规则" onClose={() => setDeleteTarget(null)} onConfirm={async () => { if (deleteTarget) await deleteRule(deleteTarget.id); setDeleteTarget(null); setOverviewRevision((value) => value + 1); }} confirmLabel="删除"><p>删除规则「{deleteTarget?.name}」后，启用自动应用时会触发配置校验与应用。</p></ConfirmModal>
    </div>
  );
}
