import { useEffect, useState, useMemo, useCallback } from 'react';
import {
  Card, CardBody, Button, Input, Modal, ModalContent, ModalHeader, ModalBody, ModalFooter,
  useDisclosure, Chip, Accordion, AccordionItem, Spinner, Tabs, Tab, Select, SelectItem, Switch,
  Progress, Tooltip
} from '@nextui-org/react';
import { Plus, RefreshCw, Trash2, Globe, Pencil, Link, Filter as FilterIcon, Search, Copy, Eye } from 'lucide-react';
import { useStore } from '../store';
import { nodeApi, clashApi, subscriptionApi } from '../api';
import { toast } from '../components/Toast';
import type { Subscription, ManualNode, Node, Filter } from '../store';

// 测速结果类型
type DelayResults = Record<string, { delay: number; available: boolean }>;

function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B';
  const k = 1024;
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
}

const nodeTypeOptions = [
  { value: 'shadowsocks', label: 'Shadowsocks' },
  { value: 'vmess', label: 'VMess' },
  { value: 'vless', label: 'VLESS' },
  { value: 'trojan', label: 'Trojan' },
  { value: 'hysteria2', label: 'Hysteria2' },
  { value: 'tuic', label: 'TUIC' },
  { value: 'socks', label: 'SOCKS' },
];

const countryOptions = [
  { code: 'HK', name: '香港', emoji: '🇭🇰' },
  { code: 'TW', name: '台湾', emoji: '🇹🇼' },
  { code: 'JP', name: '日本', emoji: '🇯🇵' },
  { code: 'KR', name: '韩国', emoji: '🇰🇷' },
  { code: 'SG', name: '新加坡', emoji: '🇸🇬' },
  { code: 'US', name: '美国', emoji: '🇺🇸' },
  { code: 'GB', name: '英国', emoji: '🇬🇧' },
  { code: 'DE', name: '德国', emoji: '🇩🇪' },
  { code: 'FR', name: '法国', emoji: '🇫🇷' },
  { code: 'NL', name: '荷兰', emoji: '🇳🇱' },
  { code: 'AU', name: '澳大利亚', emoji: '🇦🇺' },
  { code: 'CA', name: '加拿大', emoji: '🇨🇦' },
  { code: 'RU', name: '俄罗斯', emoji: '🇷🇺' },
  { code: 'IN', name: '印度', emoji: '🇮🇳' },
];

const defaultNode: Node = {
  tag: '', type: 'shadowsocks', server: '', server_port: 443, country: 'HK', country_emoji: '🇭🇰',
};

export default function Subscriptions() {
  const {
    subscriptions, manualNodes, countryGroups, filters, loading, settings,
    fetchSubscriptions, fetchManualNodes, fetchCountryGroups, fetchFilters, fetchSettings,
    addSubscription, updateSubscription, deleteSubscription, refreshSubscription,
    addManualNode, updateManualNode, deleteManualNode,
    addFilter, updateFilter, deleteFilter, toggleFilter,
  } = useStore();

  const { isOpen: isSubOpen, onOpen: onSubOpen, onClose: onSubClose } = useDisclosure();
  const { isOpen: isNodeOpen, onOpen: onNodeOpen, onClose: onNodeClose } = useDisclosure();
  const { isOpen: isFilterOpen, onOpen: onFilterOpen, onClose: onFilterClose } = useDisclosure();
  const { isOpen: isDetailOpen, onOpen: onDetailOpen, onClose: onDetailClose } = useDisclosure();
  
  const [name, setName] = useState('');
  const [url, setUrl] = useState('');
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [editingSubscription, setEditingSubscription] = useState<Subscription | null>(null);
  const [editingNode, setEditingNode] = useState<ManualNode | null>(null);
  const [nodeForm, setNodeForm] = useState<Node>(defaultNode);
  const [nodeEnabled, setNodeEnabled] = useState(true);
  const [nodeUrl, setNodeUrl] = useState('');
  const [isParsing, setIsParsing] = useState(false);
  const [parseError, setParseError] = useState('');
  const [editingFilter, setEditingFilter] = useState<Filter | null>(null);
  const [searchQuery, setSearchQuery] = useState('');
  const [selectedSub, setSelectedSub] = useState<Subscription | null>(null);
  
  // 测速相关状态
  const [testResults, setTestResults] = useState<Record<string, DelayResults>>({});
  const [testingSubId, setTestingSubId] = useState<string | null>(null);
  
  const defaultFilterForm: Omit<Filter, 'id'> = {
    name: '', include: [], exclude: [], include_countries: [], exclude_countries: [],
    mode: 'urltest', urltest_config: { url: 'https://www.gstatic.com/generate_204', interval: '5m', tolerance: 50 },
    subscriptions: [], all_nodes: true, enabled: true,
  };
  const [filterForm, setFilterForm] = useState<Omit<Filter, 'id'>>(defaultFilterForm);

  useEffect(() => {
    fetchSubscriptions(); fetchManualNodes(); fetchCountryGroups(); fetchFilters(); fetchSettings();
  }, []);

  // 同步 selectedSub 与 subscriptions
  useEffect(() => {
    if (selectedSub) {
      const updated = subscriptions.find(s => s.id === selectedSub.id);
      if (updated && JSON.stringify(updated) !== JSON.stringify(selectedSub)) {
        setSelectedSub(updated);
      }
    }
  }, [subscriptions]);

  // 测速功能
  const handleTestNodes = useCallback(async (sub: Subscription) => {
    if (!settings || !sub.nodes?.length) return;
    
    const port = settings.clash_api_port || 9091;
    const secret = settings.clash_api_secret || '';
    const nodeNames = sub.nodes.map(n => n.tag);
    
    setTestingSubId(sub.id);
    setTestResults(prev => ({ ...prev, [sub.id]: {} }));
    
    const allResults: DelayResults = {};
    
    try {
      // 逐批测试并实时更新结果
      for (let i = 0; i < nodeNames.length; i += 5) {
        const batch = nodeNames.slice(i, i + 5);
        const batchResults = await Promise.all(
          batch.map(name => 
            clashApi.testDelay(port, name, secret, 5000)
              .then(r => ({ name, ...r }))
          )
        );
        
        batchResults.forEach(r => {
          allResults[r.name] = { delay: r.delay, available: r.available };
        });
        
        setTestResults(prev => ({ ...prev, [sub.id]: { ...allResults } }));
      }
      
      // 统计结果
      const available = Object.values(allResults).filter(r => r.available).length;
      toast.success(`测速完成: ${available}/${nodeNames.length} 可用`);
    } catch (error) {
      toast.error('测速失败');
    } finally {
      setTestingSubId(null);
    }
  }, [settings]);

  // 搜索过滤
  const filteredSubscriptions = useMemo(() => {
    if (!searchQuery.trim()) return subscriptions;
    const q = searchQuery.toLowerCase();
    return subscriptions.filter(s => s.name.toLowerCase().includes(q) || s.url.toLowerCase().includes(q));
  }, [subscriptions, searchQuery]);

  const filteredManualNodes = useMemo(() => {
    if (!searchQuery.trim()) return manualNodes;
    const q = searchQuery.toLowerCase();
    return manualNodes.filter(n => n.node.tag.toLowerCase().includes(q) || n.node.server.toLowerCase().includes(q));
  }, [manualNodes, searchQuery]);

  const filteredFilters = useMemo(() => {
    if (!searchQuery.trim()) return filters;
    const q = searchQuery.toLowerCase();
    return filters.filter(f => f.name.toLowerCase().includes(q));
  }, [filters, searchQuery]);

  const handleOpenAddSubscription = () => { setEditingSubscription(null); setName(''); setUrl(''); onSubOpen(); };
  const handleOpenEditSubscription = (sub: Subscription) => { setEditingSubscription(sub); setName(sub.name); setUrl(sub.url); onSubOpen(); };

  const handleSaveSubscription = async () => {
    if (!name || !url) return;
    setIsSubmitting(true);
    try {
      if (editingSubscription) await updateSubscription(editingSubscription.id, name, url);
      else await addSubscription(name, url);
      setName(''); setUrl(''); setEditingSubscription(null); onSubClose();
    } finally { setIsSubmitting(false); }
  };

  const handleRefresh = async (id: string) => { await refreshSubscription(id); };
  const handleDeleteSubscription = async (id: string) => { if (confirm('确定删除？')) await deleteSubscription(id); };

  const handleOpenAddNode = () => { setEditingNode(null); setNodeForm(defaultNode); setNodeEnabled(true); setNodeUrl(''); setParseError(''); onNodeOpen(); };
  const handleOpenEditNode = (mn: ManualNode) => { setEditingNode(mn); setNodeForm(mn.node); setNodeEnabled(mn.enabled); setNodeUrl(''); setParseError(''); onNodeOpen(); };

  const handleParseUrl = async () => {
    if (!nodeUrl.trim()) return;
    setIsParsing(true); setParseError('');
    try {
      const response = await nodeApi.parse(nodeUrl.trim());
      setNodeForm(response.data.data as Node);
    } catch (error: any) {
      setParseError(error.response?.data?.error || '解析失败');
    } finally { setIsParsing(false); }
  };

  const handleSaveNode = async () => {
    if (!nodeForm.tag || !nodeForm.server) return;
    setIsSubmitting(true);
    try {
      const country = countryOptions.find(c => c.code === nodeForm.country);
      const nodeData = { ...nodeForm, country_emoji: country?.emoji || '🌐' };
      if (editingNode) await updateManualNode(editingNode.id, { node: nodeData, enabled: nodeEnabled });
      else await addManualNode({ node: nodeData, enabled: nodeEnabled });
      onNodeClose();
    } finally { setIsSubmitting(false); }
  };

  const handleDeleteNode = async (id: string) => { if (confirm('确定删除？')) await deleteManualNode(id); };
  const handleToggleNode = async (mn: ManualNode) => { await updateManualNode(mn.id, { ...mn, enabled: !mn.enabled }); };

  const handleOpenAddFilter = () => { setEditingFilter(null); setFilterForm(defaultFilterForm); onFilterOpen(); };
  const handleOpenEditFilter = (filter: Filter) => {
    setEditingFilter(filter);
    setFilterForm({
      name: filter.name, include: filter.include || [], exclude: filter.exclude || [],
      include_countries: filter.include_countries || [], exclude_countries: filter.exclude_countries || [],
      mode: filter.mode || 'urltest',
      urltest_config: filter.urltest_config || { url: 'https://www.gstatic.com/generate_204', interval: '5m', tolerance: 50 },
      subscriptions: filter.subscriptions || [], all_nodes: filter.all_nodes ?? true, enabled: filter.enabled,
    });
    onFilterOpen();
  };

  const handleSaveFilter = async () => {
    if (!filterForm.name) return;
    setIsSubmitting(true);
    try {
      if (editingFilter) await updateFilter(editingFilter.id, filterForm);
      else await addFilter(filterForm);
      onFilterClose();
    } finally { setIsSubmitting(false); }
  };

  const handleDeleteFilter = async (id: string) => { if (confirm('确定删除？')) await deleteFilter(id); };
  const handleToggleFilter = async (filter: Filter) => { await toggleFilter(filter.id, !filter.enabled); };

  const handleViewDetail = (sub: Subscription) => { setSelectedSub(sub); onDetailOpen(); };
  const handleCopyUrl = (url: string) => { navigator.clipboard.writeText(url); toast.success('已复制到剪贴板'); };

  return (
    <div className="space-y-6">
      {/* 顶部标签和搜索 */}
      <div className="flex flex-col sm:flex-row gap-4 justify-between items-start sm:items-center">
        <Tabs aria-label="节点管理" variant="light" color="primary" classNames={{ tabList: "gap-2 p-1 bg-gray-100 dark:bg-gray-800 rounded-lg" }}>
          <Tab key="all" title={<span className="text-sm px-2">全部</span>} />
          <Tab key="subscriptions" title={<span className="text-sm px-2">订阅 {subscriptions.length}</span>} />
          <Tab key="manual" title={<span className="text-sm px-2">手动 {manualNodes.length}</span>} />
          <Tab key="filters" title={<span className="text-sm px-2">过滤器 {filters.length}</span>} />
        </Tabs>
        <div className="flex gap-2 items-center">
          <Input
            placeholder="搜索..."
            value={searchQuery}
            onValueChange={setSearchQuery}
            startContent={<Search className="w-4 h-4 text-gray-400" />}
            size="sm"
            className="w-48"
          />
          <Button size="sm" variant="flat" startContent={<Plus className="w-4 h-4" />} onPress={handleOpenAddSubscription}>
            订阅
          </Button>
        </div>
      </div>

      {/* 订阅卡片网格 */}
      <div>
        <div className="flex justify-between items-center mb-4">
          <h2 className="text-lg font-semibold text-gray-800 dark:text-white">订阅 ({filteredSubscriptions.length})</h2>
          <div className="flex gap-2">
            <Button size="sm" variant="flat" startContent={<FilterIcon className="w-4 h-4" />} onPress={handleOpenAddFilter}>
              过滤器
            </Button>
            <Button size="sm" variant="flat" startContent={<Plus className="w-4 h-4" />} onPress={handleOpenAddNode}>
              节点
            </Button>
          </div>
        </div>

        {filteredSubscriptions.length === 0 ? (
          <Card><CardBody className="py-12 text-center text-gray-400">
            <Globe className="w-12 h-12 mx-auto mb-3 opacity-50" />
            <p>暂无订阅</p>
          </CardBody></Card>
        ) : (
          <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-4">
            {filteredSubscriptions.map((sub) => (
              <Card key={sub.id} className="relative">
                <CardBody className="p-4">
                  {/* 标签 */}
                  <div className="absolute top-3 left-0">
                    <Chip
                      size="sm"
                      className="rounded-l-none rounded-r-full"
                      color={sub.enabled ? 'primary' : 'default'}
                    >
                      {sub.enabled ? '启用' : '禁用'}
                    </Chip>
                  </div>

                  {/* 标题 */}
                  <div className="mt-6 mb-3">
                    <h3 className="font-semibold text-gray-800 dark:text-white truncate" title={sub.name}>
                      {sub.name}
                    </h3>
                    <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                      节点: {sub.node_count} · 更新: {new Date(sub.updated_at).toLocaleDateString()}
                    </p>
                  </div>

                  {/* 节点可用性 */}
                  <NodeAvailability 
                    nodes={sub.nodes || []}
                    testResults={testResults[sub.id]}
                    onTest={() => handleTestNodes(sub)}
                    testing={testingSubId === sub.id}
                  />

                  {/* 流量统计 */}
                  {sub.traffic ? (
                    <div className="mb-4">
                      <div className="flex justify-between text-sm mb-2">
                        <span className="text-green-600">已用: {formatBytes(sub.traffic.used)}</span>
                        <span className="text-gray-500">剩余: {formatBytes(sub.traffic.remaining)}</span>
                      </div>
                      <div className="flex items-center gap-2">
                        <Progress
                          size="sm"
                          value={(sub.traffic.used / sub.traffic.total) * 100}
                          color={sub.traffic.used / sub.traffic.total > 0.8 ? 'danger' : 'primary'}
                          className="flex-1"
                          aria-label="流量使用进度"
                        />
                        <span className="text-xs text-gray-500 w-12 text-right">
                          {((sub.traffic.used / sub.traffic.total) * 100).toFixed(1)}%
                        </span>
                      </div>
                      {sub.expire_at && (
                        <p className="text-xs text-gray-400 mt-2">到期: {new Date(sub.expire_at).toLocaleDateString()}</p>
                      )}
                    </div>
                  ) : null}

                  {/* 操作按钮 */}
                  <div className="flex justify-end gap-1 pt-2 border-t border-gray-100 dark:border-gray-800">
                    <Tooltip content="复制链接">
                      <Button isIconOnly size="sm" variant="light" onPress={() => handleCopyUrl(sub.url)}>
                        <Copy className="w-4 h-4" />
                      </Button>
                    </Tooltip>
                    <Tooltip content="查看节点">
                      <Button isIconOnly size="sm" variant="light" onPress={() => handleViewDetail(sub)}>
                        <Eye className="w-4 h-4" />
                      </Button>
                    </Tooltip>
                    <Tooltip content="刷新">
                      <Button isIconOnly size="sm" variant="light" onPress={() => handleRefresh(sub.id)} isDisabled={loading}>
                        {loading ? <Spinner size="sm" /> : <RefreshCw className="w-4 h-4" />}
                      </Button>
                    </Tooltip>
                    <Tooltip content="编辑">
                      <Button isIconOnly size="sm" variant="light" onPress={() => handleOpenEditSubscription(sub)}>
                        <Pencil className="w-4 h-4" />
                      </Button>
                    </Tooltip>
                    <Tooltip content="删除">
                      <Button isIconOnly size="sm" variant="light" color="danger" onPress={() => handleDeleteSubscription(sub.id)}>
                        <Trash2 className="w-4 h-4" />
                      </Button>
                    </Tooltip>
                  </div>
                </CardBody>
              </Card>
            ))}
          </div>
        )}
      </div>

      {/* 手动节点 */}
      {filteredManualNodes.length > 0 && (
        <div>
          <h2 className="text-lg font-semibold text-gray-800 dark:text-white mb-4">手动节点 ({filteredManualNodes.length})</h2>
          <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-4">
            {filteredManualNodes.map((mn) => (
              <Card key={mn.id}>
                <CardBody className="p-4">
                  <div className="absolute top-3 left-0">
                    <Chip size="sm" className="rounded-l-none rounded-r-full" color={mn.enabled ? 'success' : 'default'}>
                      {mn.node.type}
                    </Chip>
                  </div>
                  <div className="mt-6 mb-3 flex items-center gap-2">
                    <span className="text-2xl">{mn.node.country_emoji || '🌐'}</span>
                    <div>
                      <h3 className="font-semibold">{mn.node.tag}</h3>
                      <p className="text-xs text-gray-500">{mn.node.server}:{mn.node.server_port}</p>
                    </div>
                  </div>
                  <div className="flex justify-end gap-1 pt-2 border-t border-gray-100 dark:border-gray-800">
                    <Tooltip content="编辑">
                      <Button isIconOnly size="sm" variant="light" onPress={() => handleOpenEditNode(mn)}>
                        <Pencil className="w-4 h-4" />
                      </Button>
                    </Tooltip>
                    <Tooltip content={mn.enabled ? '禁用' : '启用'}>
                      <Button isIconOnly size="sm" variant="light" color={mn.enabled ? 'success' : 'default'} onPress={() => handleToggleNode(mn)}>
                        <Globe className="w-4 h-4" />
                      </Button>
                    </Tooltip>
                    <Tooltip content="删除">
                      <Button isIconOnly size="sm" variant="light" color="danger" onPress={() => handleDeleteNode(mn.id)}>
                        <Trash2 className="w-4 h-4" />
                      </Button>
                    </Tooltip>
                  </div>
                </CardBody>
              </Card>
            ))}
          </div>
        </div>
      )}

      {/* 过滤器 */}
      {filteredFilters.length > 0 && (
        <div>
          <h2 className="text-lg font-semibold text-gray-800 dark:text-white mb-4">过滤器 ({filteredFilters.length})</h2>
          <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-4">
            {filteredFilters.map((filter) => (
              <Card key={filter.id}>
                <CardBody className="p-4">
                  <div className="absolute top-3 left-0">
                    <Chip size="sm" className="rounded-l-none rounded-r-full" color={filter.enabled ? 'secondary' : 'default'}>
                      {filter.mode === 'urltest' ? '测速' : '选择'}
                    </Chip>
                  </div>
                  <div className="mt-6 mb-3">
                    <h3 className="font-semibold">{filter.name}</h3>
                    <div className="flex gap-1 mt-2 flex-wrap">
                      {filter.include_countries?.map(c => (
                        <span key={c} className="text-sm">{countryOptions.find(o => o.code === c)?.emoji}</span>
                      ))}
                      {filter.include?.length > 0 && (
                        <Chip size="sm" variant="flat" className="h-5 text-xs">{filter.include.join('|')}</Chip>
                      )}
                    </div>
                  </div>
                  <div className="flex justify-end gap-1 pt-2 border-t border-gray-100 dark:border-gray-800">
                    <Tooltip content="编辑">
                      <Button isIconOnly size="sm" variant="light" onPress={() => handleOpenEditFilter(filter)}>
                        <Pencil className="w-4 h-4" />
                      </Button>
                    </Tooltip>
                    <Tooltip content={filter.enabled ? '禁用' : '启用'}>
                      <Button isIconOnly size="sm" variant="light" color={filter.enabled ? 'secondary' : 'default'} onPress={() => handleToggleFilter(filter)}>
                        <FilterIcon className="w-4 h-4" />
                      </Button>
                    </Tooltip>
                    <Tooltip content="删除">
                      <Button isIconOnly size="sm" variant="light" color="danger" onPress={() => handleDeleteFilter(filter.id)}>
                        <Trash2 className="w-4 h-4" />
                      </Button>
                    </Tooltip>
                  </div>
                </CardBody>
              </Card>
            ))}
          </div>
        </div>
      )}

      {/* 按地区统计 */}
      {countryGroups.length > 0 && (
        <div>
          <h2 className="text-lg font-semibold text-gray-800 dark:text-white mb-4">按地区 ({countryGroups.length})</h2>
          <div className="flex flex-wrap gap-3">
            {countryGroups.map((group) => (
              <Chip key={group.code} variant="flat" size="lg" className="px-3 py-2">
                <span className="mr-1">{group.emoji}</span>
                <span className="font-medium">{group.name}</span>
                <span className="ml-2 text-gray-500">{group.node_count}</span>
              </Chip>
            ))}
          </div>
        </div>
      )}

      {/* 订阅弹窗 */}
      <Modal isOpen={isSubOpen} onClose={onSubClose}>
        <ModalContent>
          <ModalHeader>{editingSubscription ? '编辑订阅' : '添加订阅'}</ModalHeader>
          <ModalBody>
            <Input label="名称" placeholder="订阅名称" value={name} onChange={(e) => setName(e.target.value)} />
            <Input label="地址" placeholder="订阅 URL" value={url} onChange={(e) => setUrl(e.target.value)} />
          </ModalBody>
          <ModalFooter>
            <Button variant="flat" onPress={onSubClose}>取消</Button>
            <Button color="primary" onPress={handleSaveSubscription} isLoading={isSubmitting} isDisabled={!name || !url}>
              {editingSubscription ? '保存' : '添加'}
            </Button>
          </ModalFooter>
        </ModalContent>
      </Modal>

      {/* 节点弹窗 */}
      <Modal isOpen={isNodeOpen} onClose={onNodeClose} size="lg">
        <ModalContent>
          <ModalHeader>{editingNode ? '编辑节点' : '添加节点'}</ModalHeader>
          <ModalBody>
            {!editingNode && (
              <div className="space-y-2">
                <div className="flex gap-2">
                  <Input placeholder="粘贴节点链接" value={nodeUrl} onChange={(e) => setNodeUrl(e.target.value)}
                    startContent={<Link className="w-4 h-4 text-gray-400" />} className="flex-1" />
                  <Button color="primary" variant="flat" onPress={handleParseUrl} isLoading={isParsing} isDisabled={!nodeUrl.trim()}>
                    解析
                  </Button>
                </div>
                {parseError && <p className="text-sm text-danger">{parseError}</p>}
              </div>
            )}
            {nodeForm.tag && (
              <Card className="bg-default-100">
                <CardBody className="p-3 flex flex-row items-center gap-3">
                  <span className="text-2xl">{nodeForm.country_emoji || '🌐'}</span>
                  <div className="flex-1">
                    <h4 className="font-medium">{nodeForm.tag}</h4>
                    <p className="text-sm text-gray-500">{nodeForm.type} · {nodeForm.server}:{nodeForm.server_port}</p>
                  </div>
                </CardBody>
              </Card>
            )}
            <Accordion variant="bordered">
              <AccordionItem key="manual" title="手动编辑">
                <div className="space-y-3 pb-2">
                  <Input label="名称" value={nodeForm.tag} onChange={(e) => setNodeForm({ ...nodeForm, tag: e.target.value })} />
                  <div className="grid grid-cols-2 gap-3">
                    <Select label="类型" selectedKeys={[nodeForm.type]} onChange={(e) => setNodeForm({ ...nodeForm, type: e.target.value })}>
                      {nodeTypeOptions.map((o) => <SelectItem key={o.value}>{o.label}</SelectItem>)}
                    </Select>
                    <Select label="地区" selectedKeys={[nodeForm.country || 'HK']} onChange={(e) => {
                      const c = countryOptions.find(x => x.code === e.target.value);
                      setNodeForm({ ...nodeForm, country: e.target.value, country_emoji: c?.emoji || '🌐' });
                    }}>
                      {countryOptions.map((o) => <SelectItem key={o.code}>{o.emoji} {o.name}</SelectItem>)}
                    </Select>
                  </div>
                  <div className="grid grid-cols-2 gap-3">
                    <Input label="服务器" value={nodeForm.server} onChange={(e) => setNodeForm({ ...nodeForm, server: e.target.value })} />
                    <Input type="number" label="端口" value={String(nodeForm.server_port)} onChange={(e) => setNodeForm({ ...nodeForm, server_port: parseInt(e.target.value) || 443 })} />
                  </div>
                </div>
              </AccordionItem>
            </Accordion>
            <div className="flex justify-between items-center">
              <span className="text-sm">启用</span>
              <Switch isSelected={nodeEnabled} onValueChange={setNodeEnabled} />
            </div>
          </ModalBody>
          <ModalFooter>
            <Button variant="flat" onPress={onNodeClose}>取消</Button>
            <Button color="primary" onPress={handleSaveNode} isLoading={isSubmitting} isDisabled={!nodeForm.tag || !nodeForm.server}>
              {editingNode ? '保存' : '添加'}
            </Button>
          </ModalFooter>
        </ModalContent>
      </Modal>

      {/* 过滤器弹窗 */}
      <Modal isOpen={isFilterOpen} onClose={onFilterClose} size="xl">
        <ModalContent>
          <ModalHeader>{editingFilter ? '编辑过滤器' : '添加过滤器'}</ModalHeader>
          <ModalBody>
            <Input label="名称" placeholder="如：日本高速" value={filterForm.name} onChange={(e) => setFilterForm({ ...filterForm, name: e.target.value })} isRequired />
            <div className="grid grid-cols-2 gap-3">
              <Select label="包含国家" selectionMode="multiple" selectedKeys={filterForm.include_countries}
                onSelectionChange={(keys) => setFilterForm({ ...filterForm, include_countries: Array.from(keys) as string[] })}>
                {countryOptions.map((o) => <SelectItem key={o.code}>{o.emoji} {o.name}</SelectItem>)}
              </Select>
              <Select label="排除国家" selectionMode="multiple" selectedKeys={filterForm.exclude_countries}
                onSelectionChange={(keys) => setFilterForm({ ...filterForm, exclude_countries: Array.from(keys) as string[] })}>
                {countryOptions.map((o) => <SelectItem key={o.code}>{o.emoji} {o.name}</SelectItem>)}
              </Select>
            </div>
            <div className="grid grid-cols-2 gap-3">
              <Input label="包含关键字" placeholder="用 | 分隔" value={filterForm.include.join('|')}
                onChange={(e) => setFilterForm({ ...filterForm, include: e.target.value ? e.target.value.split('|').filter(Boolean) : [] })} />
              <Input label="排除关键字" placeholder="用 | 分隔" value={filterForm.exclude.join('|')}
                onChange={(e) => setFilterForm({ ...filterForm, exclude: e.target.value ? e.target.value.split('|').filter(Boolean) : [] })} />
            </div>
            <Select label="模式" selectedKeys={[filterForm.mode]} onChange={(e) => setFilterForm({ ...filterForm, mode: e.target.value })}>
              <SelectItem key="urltest">自动测速</SelectItem>
              <SelectItem key="selector">手动选择</SelectItem>
            </Select>
            {filterForm.mode === 'urltest' && (
              <div className="grid grid-cols-3 gap-3">
                <Input label="测速URL" size="sm" value={filterForm.urltest_config?.url || ''}
                  onChange={(e) => setFilterForm({ ...filterForm, urltest_config: { ...filterForm.urltest_config!, url: e.target.value } })} />
                <Input label="间隔" size="sm" value={filterForm.urltest_config?.interval || ''}
                  onChange={(e) => setFilterForm({ ...filterForm, urltest_config: { ...filterForm.urltest_config!, interval: e.target.value } })} />
                <Input type="number" label="容差(ms)" size="sm" value={String(filterForm.urltest_config?.tolerance || 50)}
                  onChange={(e) => setFilterForm({ ...filterForm, urltest_config: { ...filterForm.urltest_config!, tolerance: parseInt(e.target.value) || 50 } })} />
              </div>
            )}
            <div className="flex justify-between items-center">
              <span className="text-sm">启用</span>
              <Switch isSelected={filterForm.enabled} onValueChange={(v) => setFilterForm({ ...filterForm, enabled: v })} />
            </div>
          </ModalBody>
          <ModalFooter>
            <Button variant="flat" onPress={onFilterClose}>取消</Button>
            <Button color="primary" onPress={handleSaveFilter} isLoading={isSubmitting} isDisabled={!filterForm.name}>
              {editingFilter ? '保存' : '添加'}
            </Button>
          </ModalFooter>
        </ModalContent>
      </Modal>

      {/* 节点详情抽屉 */}
      {isDetailOpen && (
        <>
          <div className="fixed inset-0 bg-black/50 z-40" onClick={onDetailClose} />
          <div className="fixed top-0 right-0 h-full w-full max-w-lg bg-white dark:bg-gray-900 shadow-xl z-50 flex flex-col animate-slide-in-right">
            <div className="flex items-center justify-between p-4 border-b border-gray-200 dark:border-gray-700">
              <h2 className="text-lg font-semibold">{selectedSub?.name}</h2>
              <Button isIconOnly size="sm" variant="light" onPress={onDetailClose}>
                <span className="text-xl">&times;</span>
              </Button>
            </div>
            <div className="flex-1 overflow-y-auto p-4">
              {selectedSub && (
                <div className="space-y-4">
                  {(() => {
                    const nodes = selectedSub.nodes || [];
                    // 保留原始索引
                    const nodesWithIndex = nodes.map((node, index) => ({ node, index }));
                    const nodesByCountry = nodesWithIndex.reduce((acc, item) => {
                      const country = item.node.country || 'OTHER';
                      if (!acc[country]) acc[country] = { emoji: item.node.country_emoji || '🌐', nodes: [] };
                      acc[country].nodes.push(item);
                      return acc;
                    }, {} as Record<string, { emoji: string; nodes: { node: Node; index: number }[] }>);

                    const handleToggleNode = async (nodeIndex: number) => {
                      try {
                        await subscriptionApi.toggleNodeDisabled(selectedSub.id, nodeIndex);
                        fetchSubscriptions(); // useEffect 会自动同步 selectedSub
                      } catch (e) {
                        toast.error('切换失败');
                      }
                    };

                    return Object.entries(nodesByCountry).map(([country, data]) => (
                      <div key={country}>
                        <div className="flex items-center gap-2 mb-2">
                          <span className="text-xl">{data.emoji}</span>
                          <span className="font-medium">{country}</span>
                          <Chip size="sm" variant="flat">
                            {data.nodes.filter(n => !n.node.disabled).length}/{data.nodes.length}
                          </Chip>
                        </div>
                        <div className="space-y-1">
                          {data.nodes.map(({ node, index }) => (
                            <div 
                              key={index} 
                              className={`flex items-center gap-2 p-2 rounded text-sm transition-colors ${
                                node.disabled 
                                  ? 'bg-gray-100 dark:bg-gray-900 opacity-50' 
                                  : 'bg-gray-50 dark:bg-gray-800'
                              }`}
                            >
                              <Switch 
                                size="sm" 
                                isSelected={!node.disabled}
                                onValueChange={() => handleToggleNode(index)}
                              />
                              <span className={`truncate flex-1 ${node.disabled ? 'line-through' : ''}`}>
                                {node.tag}
                              </span>
                              <Chip size="sm" variant="flat">{node.type}</Chip>
                            </div>
                          ))}
                        </div>
                      </div>
                    ));
                  })()}
                </div>
              )}
            </div>
          </div>
        </>
      )}
    </div>
  );
}

// 节点可用性展示组件
function NodeAvailability({ 
  nodes, 
  testResults,
  onTest,
  testing 
}: { 
  nodes: Node[];
  testResults?: Record<string, { delay: number; available: boolean }>;
  onTest?: () => void;
  testing?: boolean;
}) {
  const total = nodes.length;
  if (total === 0) return null;

  // 计算可用/不可用数量
  const tested = testResults ? Object.keys(testResults).length : 0;
  const available = testResults ? Object.values(testResults).filter(r => r.available).length : 0;
  const unavailable = tested - available;
  const percentage = tested > 0 ? (available / tested) * 100 : 0;
  
  // 生成进度条块
  const blocks = Math.min(20, total);
  const testedBlocks = tested > 0 ? Math.round((tested / total) * blocks) : 0;
  const availableRatio = tested > 0 ? available / tested : 0;
  
  return (
    <div className="mb-4">
      <div className="flex items-center gap-2 mb-2">
        {tested > 0 ? (
          <>
            <Chip size="sm" variant="flat" className="bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-400">
              可用: {available}
            </Chip>
            {unavailable > 0 && (
              <Chip size="sm" variant="flat" className="bg-red-100 text-red-600 dark:bg-red-900/30 dark:text-red-400">
                失败: {unavailable}
              </Chip>
            )}
          </>
        ) : (
          <span className="text-xs text-gray-400">共 {total} 个节点</span>
        )}
        {onTest && (
          <Button 
            size="sm" 
            variant="flat" 
            className="ml-auto h-6 min-w-0 px-2"
            onPress={onTest}
            isLoading={testing}
          >
            {testing ? `${tested}/${total}` : '测速'}
          </Button>
        )}
      </div>
      <div className="flex items-center gap-2">
        <div className="flex gap-0.5 flex-1">
          {Array.from({ length: blocks }).map((_, i) => {
            let colorClass = 'bg-gray-200 dark:bg-gray-700'; // 未测试
            if (i < testedBlocks) {
              // 根据可用比例决定颜色
              const blockAvailableRatio = (i + 1) / testedBlocks;
              if (blockAvailableRatio <= availableRatio) {
                colorClass = 'bg-green-400'; // 可用
              } else {
                colorClass = 'bg-red-400'; // 不可用
              }
            }
            return <div key={i} className={`h-2 flex-1 rounded-sm ${colorClass}`} />;
          })}
        </div>
        {tested > 0 && (
          <Chip 
            size="sm" 
            variant="flat" 
            className={percentage >= 80 
              ? "bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-400"
              : percentage >= 50
              ? "bg-orange-100 text-orange-700 dark:bg-orange-900/30 dark:text-orange-400"
              : "bg-red-100 text-red-600 dark:bg-red-900/30 dark:text-red-400"
            }
          >
            {percentage.toFixed(1)}%
          </Chip>
        )}
      </div>
    </div>
  );
}
