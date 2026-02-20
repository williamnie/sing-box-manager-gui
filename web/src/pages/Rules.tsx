import { useEffect, useState, useCallback, useRef, useMemo } from 'react';
import {
  Card,
  CardBody,
  CardHeader,
  Switch,
  Chip,
  Button,
  Modal,
  ModalContent,
  ModalHeader,
  ModalBody,
  ModalFooter,
  Input,
  Select,
  SelectItem,
  Textarea,
  useDisclosure,
  Spinner,
} from '@nextui-org/react';
import { Shield, Globe, Tv, MessageCircle, Github, Bot, Apple, Monitor, Plus, Pencil, Trash2, CheckCircle, XCircle, Search } from 'lucide-react';
import { useStore } from '../store';
import { ruleSetApi } from '../api';
import { useDebouncedValue } from '../hooks/useDebouncedValue';
import type { Rule, RuleGroup } from '../store';

// 规则集验证结果类型
interface ValidationResult {
  valid: boolean;
  url: string;
  tag: string;
  message: string;
}

const iconMap: Record<string, React.ReactNode> = {
  'ad-block': <Shield className="w-5 h-5" />,
  'ai-services': <Bot className="w-5 h-5" />,
  'google': <Globe className="w-5 h-5" />,
  'youtube': <Tv className="w-5 h-5" />,
  'github': <Github className="w-5 h-5" />,
  'telegram': <MessageCircle className="w-5 h-5" />,
  'twitter': <MessageCircle className="w-5 h-5" />,
  'netflix': <Tv className="w-5 h-5" />,
  'spotify': <Tv className="w-5 h-5" />,
  'apple': <Apple className="w-5 h-5" />,
  'microsoft': <Monitor className="w-5 h-5" />,
  'cn': <Globe className="w-5 h-5" />,
  'private': <Shield className="w-5 h-5" />,
};

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
    fetchRuleGroups,
    fetchRules,
    fetchFilters,
    fetchCountryGroups,
    toggleRuleGroup,
    updateRuleGroupOutbound,
    addRule,
    updateRule,
    deleteRule,
  } = useStore();

  const { isOpen, onOpen, onClose } = useDisclosure();
  const [editingRule, setEditingRule] = useState<Rule | null>(null);
  const [formData, setFormData] = useState<Omit<Rule, 'id'>>(defaultRule);
  const [valuesText, setValuesText] = useState('');
  const [searchQuery, setSearchQuery] = useState('');
  const debouncedSearchQuery = useDebouncedValue(searchQuery, 300);

  // 规则集验证状态
  const [validationResults, setValidationResults] = useState<Record<string, ValidationResult>>({});
  const [isValidating, setIsValidating] = useState(false);
  const validationTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const clearValidationResultsAsync = useCallback(() => {
    queueMicrotask(() => {
      setValidationResults({});
    });
  }, []);

  useEffect(() => {
    fetchRuleGroups();
    fetchRules();
    fetchFilters();
    fetchCountryGroups();
  }, [fetchCountryGroups, fetchFilters, fetchRuleGroups, fetchRules]);

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
      clearValidationResultsAsync();
      return;
    }

    const names = valuesText
      .split('\n')
      .map((v) => v.trim())
      .filter((v) => v);

    if (names.length === 0) {
      clearValidationResultsAsync();
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
  }, [clearValidationResultsAsync, valuesText, formData.rule_type, validateRuleSet]);

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

  // 获取所有可用的出站选项（包括国家节点组和过滤器）
  const getAllOutboundOptions = () => {
    const options = [...baseOutboundOptions];

    // 添加国家节点组
    countryGroups.forEach((group) => {
      const label = `${group.emoji} ${group.name}`;
      options.push({ value: label, label: `${label} (${group.node_count}节点)` });
    });

    // 添加过滤器
    filters.forEach((filter) => {
      if (filter.enabled) {
        options.push({ value: filter.name, label: `${filter.name} (过滤器)` });
      }
    });

    return options;
  };

  const handleToggle = async (id: string, enabled: boolean) => {
    await toggleRuleGroup(id, enabled);
  };

  const handleOutboundChange = async (group: RuleGroup, outbound: string) => {
    await updateRuleGroupOutbound(group.id, outbound);
  };

  const handleAddRule = () => {
    setEditingRule(null);
    setFormData(defaultRule);
    setValuesText('');
    setValidationResults({});
    onOpen();
  };

  const handleEditRule = (rule: Rule) => {
    setEditingRule(rule);
    setFormData({
      name: rule.name,
      rule_type: rule.rule_type,
      values: rule.values,
      outbound: rule.outbound,
      enabled: rule.enabled,
      priority: rule.priority,
    });
    setValuesText(rule.values.join('\n'));
    setValidationResults({});
    onOpen();
  };

  const handleDeleteRule = async (rule: Rule) => {
    if (confirm(`确定要删除规则 "${rule.name}" 吗？`)) {
      await deleteRule(rule.id);
    }
  };

  const handleSubmit = async () => {
    const values = valuesText
      .split('\n')
      .map((v) => v.trim())
      .filter((v) => v);

    const ruleData = {
      ...formData,
      values,
    };

    if (editingRule) {
      await updateRule(editingRule.id, ruleData);
    } else {
      await addRule(ruleData);
    }

    onClose();
  };

  const handleToggleCustomRule = async (rule: Rule) => {
    await updateRule(rule.id, { ...rule, enabled: !rule.enabled });
  };

  const filteredRules = useMemo(() => {
    const sortedRules = [...rules].sort((left, right) => left.priority - right.priority);
    const query = debouncedSearchQuery.trim().toLowerCase();

    if (!query) {
      return sortedRules;
    }

    return sortedRules.filter((rule) =>
      rule.name.toLowerCase().includes(query)
      || rule.rule_type.toLowerCase().includes(query)
      || rule.outbound.toLowerCase().includes(query)
      || rule.values.some((value) => value.toLowerCase().includes(query))
    );
  }, [debouncedSearchQuery, rules]);

  return (
    <div className="space-y-6">
      <div className="flex justify-between items-center">
        <h1 className="text-2xl font-bold text-gray-800 dark:text-white">规则管理</h1>
      </div>

      {/* 预设规则组 */}
      <Card>
        <CardHeader>
          <h2 className="text-lg font-semibold">预设规则组</h2>
        </CardHeader>
        <CardBody>
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            {ruleGroups.map((group) => (
              <div
                key={group.id}
                className="flex items-center justify-between p-4 bg-gray-50 dark:bg-gray-800 rounded-lg"
              >
                <div className="flex items-center gap-3">
                  <div className="p-2 bg-white dark:bg-gray-700 rounded-lg">
                    {iconMap[group.id] || <Globe className="w-5 h-5" />}
                  </div>
                  <div>
                    <h3 className="font-medium">{group.name}</h3>
                    <div className="flex gap-1 mt-1">
                      {group.site_rules.slice(0, 2).map((rule) => (
                        <Chip key={rule} size="sm" variant="flat">
                          {rule}
                        </Chip>
                      ))}
                      {group.site_rules.length > 2 && (
                        <Chip size="sm" variant="flat">
                          +{group.site_rules.length - 2}
                        </Chip>
                      )}
                    </div>
                  </div>
                </div>
                <div className="flex items-center gap-3">
                  <Select
                    size="sm"
                    className="w-32"
                    selectedKeys={[group.outbound]}
                    onChange={(e) => handleOutboundChange(group, e.target.value)}
                    aria-label="选择出站"
                  >
                    {getAllOutboundOptions().map((opt) => (
                      <SelectItem key={opt.value} value={opt.value}>
                        {opt.value}
                      </SelectItem>
                    ))}
                  </Select>
                  <Switch
                    isSelected={group.enabled}
                    onValueChange={(enabled) => handleToggle(group.id, enabled)}
                  />
                </div>
              </div>
            ))}
          </div>
        </CardBody>
      </Card>

      {/* 自定义规则 */}
      <Card>
        <CardHeader className="flex justify-between items-center">
          <h2 className="text-lg font-semibold">自定义规则</h2>
          <div className="flex items-center gap-2">
            <Input
              size="sm"
              value={searchQuery}
              onValueChange={setSearchQuery}
              placeholder="搜索规则..."
              startContent={<Search className="w-4 h-4 text-gray-400" />}
              className="w-56"
            />
            <Button
              color="primary"
              size="sm"
              startContent={<Plus className="w-4 h-4" />}
              onPress={handleAddRule}
            >
              添加规则
            </Button>
          </div>
        </CardHeader>
        <CardBody>
          {rules.length === 0 ? (
            <p className="text-gray-500 text-center py-8">
              暂无自定义规则，点击上方按钮添加
            </p>
          ) : filteredRules.length === 0 ? (
            <p className="text-gray-500 text-center py-8">
              未找到匹配规则
            </p>
          ) : (
            <div className="space-y-3">
              {filteredRules.map((rule) => (
                  <div
                    key={rule.id}
                    className="flex items-center justify-between p-4 bg-gray-50 dark:bg-gray-800 rounded-lg"
                  >
                    <div className="flex-1">
                      <div className="flex items-center gap-2">
                        <h3 className="font-medium">{rule.name}</h3>
                        <Chip size="sm" variant="flat" color="secondary">
                          {ruleTypeOptions.find((t) => t.value === rule.rule_type)?.label.split(' ')[0] || rule.rule_type}
                        </Chip>
                        <Chip
                          size="sm"
                          color={
                            rule.outbound === 'DIRECT'
                              ? 'success'
                              : rule.outbound === 'REJECT'
                              ? 'danger'
                              : 'primary'
                          }
                          variant="flat"
                        >
                          {rule.outbound}
                        </Chip>
                      </div>
                      <div className="flex gap-1 mt-2 flex-wrap">
                        {rule.values.slice(0, 3).map((val, idx) => (
                          <Chip key={idx} size="sm" variant="bordered">
                            {val}
                          </Chip>
                        ))}
                        {rule.values.length > 3 && (
                          <Chip size="sm" variant="bordered">
                            +{rule.values.length - 3} 条
                          </Chip>
                        )}
                      </div>
                    </div>
                    <div className="flex items-center gap-2">
                      <Chip size="sm" variant="flat">
                        优先级: {rule.priority}
                      </Chip>
                      <Button
                        isIconOnly
                        size="sm"
                        variant="light"
                        onPress={() => handleEditRule(rule)}
                      >
                        <Pencil className="w-4 h-4" />
                      </Button>
                      <Button
                        isIconOnly
                        size="sm"
                        variant="light"
                        color="danger"
                        onPress={() => handleDeleteRule(rule)}
                      >
                        <Trash2 className="w-4 h-4" />
                      </Button>
                      <Switch
                        isSelected={rule.enabled}
                        onValueChange={() => handleToggleCustomRule(rule)}
                      />
                    </div>
                  </div>
                ))}
            </div>
          )}
        </CardBody>
      </Card>

      {/* 添加/编辑规则弹窗 */}
      <Modal isOpen={isOpen} onClose={onClose} size="lg">
        <ModalContent>
          <ModalHeader>{editingRule ? '编辑规则' : '添加规则'}</ModalHeader>
          <ModalBody>
            <div className="space-y-4">
              <Input
                label="规则名称"
                placeholder="例如：屏蔽广告域名"
                value={formData.name}
                onChange={(e) => setFormData({ ...formData, name: e.target.value })}
              />

              <Select
                label="规则类型"
                selectedKeys={[formData.rule_type]}
                onChange={(e) => setFormData({ ...formData, rule_type: e.target.value })}
              >
                {ruleTypeOptions.map((opt) => (
                  <SelectItem key={opt.value} value={opt.value}>
                    {opt.label}
                  </SelectItem>
                ))}
              </Select>

              <Textarea
                label="规则值"
                placeholder={
                  formData.rule_type === 'domain_suffix'
                    ? '每行一个域名后缀，例如：\ngoogle.com\nyoutube.com'
                    : formData.rule_type === 'ip_cidr'
                    ? '每行一个 IP 段，例如：\n192.168.0.0/16\n10.0.0.0/8'
                    : formData.rule_type === 'geosite'
                    ? '每行一个 geosite 规则集名称，例如：\ngoogle\nyoutube\ncursor'
                    : formData.rule_type === 'geoip'
                    ? '每行一个 geoip 规则集名称，例如：\ncn\ngoogle'
                    : '每行一个值'
                }
                value={valuesText}
                onChange={(e) => setValuesText(e.target.value)}
                minRows={4}
              />

              {/* 规则集验证结果显示 */}
              {(formData.rule_type === 'geosite' || formData.rule_type === 'geoip') && valuesText.trim() && (
                <div className="space-y-2">
                  <div className="flex items-center gap-2 text-sm text-gray-600 dark:text-gray-400">
                    <span>规则集验证结果</span>
                    {isValidating && <Spinner size="sm" />}
                  </div>
                  <div className="space-y-1 max-h-32 overflow-y-auto">
                    {valuesText
                      .split('\n')
                      .map((v) => v.trim())
                      .filter((v) => v)
                      .map((name) => {
                        const result = validationResults[name];
                        if (!result) {
                          return (
                            <div key={name} className="flex items-center gap-2 text-sm text-gray-500">
                              <Spinner size="sm" />
                              <span>{name} - 验证中...</span>
                            </div>
                          );
                        }
                        return (
                          <div
                            key={name}
                            className={`flex items-center gap-2 text-sm ${
                              result.valid ? 'text-green-600 dark:text-green-400' : 'text-red-600 dark:text-red-400'
                            }`}
                          >
                            {result.valid ? (
                              <CheckCircle className="w-4 h-4" />
                            ) : (
                              <XCircle className="w-4 h-4" />
                            )}
                            <span className="font-medium">{name}</span>
                            <span className="text-xs opacity-75">- {result.message}</span>
                          </div>
                        );
                      })}
                  </div>
                </div>
              )}

              <Select
                label="出站"
                selectedKeys={[formData.outbound]}
                onChange={(e) => setFormData({ ...formData, outbound: e.target.value })}
              >
                {getAllOutboundOptions().map((opt) => (
                  <SelectItem key={opt.value} value={opt.value}>
                    {opt.label}
                  </SelectItem>
                ))}
              </Select>

              <Input
                type="number"
                label="优先级"
                placeholder="数字越小优先级越高"
                value={String(formData.priority)}
                onChange={(e) =>
                  setFormData({ ...formData, priority: parseInt(e.target.value) || 100 })
                }
              />

              <div className="flex items-center justify-between">
                <span>启用规则</span>
                <Switch
                  isSelected={formData.enabled}
                  onValueChange={(enabled) => setFormData({ ...formData, enabled })}
                />
              </div>
            </div>
          </ModalBody>
          <ModalFooter>
            <Button variant="flat" onPress={onClose}>
              取消
            </Button>
            <Button
              color="primary"
              onPress={handleSubmit}
              isDisabled={!formData.name || !valuesText.trim() || isValidating || !allValidationsPassed()}
            >
              {editingRule ? '保存' : '添加'}
            </Button>
          </ModalFooter>
        </ModalContent>
      </Modal>
    </div>
  );
}
