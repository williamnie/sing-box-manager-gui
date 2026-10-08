import { toast } from '../components/Toast';
import { errorMessage } from '../api';
import { useEffect, useState } from 'react';
import {
  Card,
  CardBody,
  Button,
  Input,
  Modal,
  ModalContent,
  ModalHeader,
  ModalBody,
  ModalFooter,
  useDisclosure,
  Chip,
  Accordion,
  AccordionItem,
  Spinner,
  Tabs,
  Tab,
  Switch,
} from '@nextui-org/react';
import { AppSelect, SelectItem } from '../components/AppSelect';
import { Plus, RefreshCw, Trash2, Globe, Server, Pencil, Link, Filter as FilterIcon, ChevronDown, ChevronUp } from 'lucide-react';
import { useStore } from '../store';
import { nodeApi } from '../api';
import type { Subscription, ManualNode, Node, Filter } from '../store';
import ConfirmModal from '../components/ConfirmModal';

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
  tag: '',
  type: 'shadowsocks',
  server: '',
  server_port: 443,
  country: 'HK',
  country_emoji: '🇭🇰',
};

export default function Subscriptions() {
  const [deleteTarget, setDeleteTarget] = useState<{ type: 'subscription' | 'node' | 'filter'; id: string } | null>(null);
  const {
    subscriptions,
    manualNodes,
    countryGroups,
    filters,
    settings,
    loading,
    fetchSubscriptions,
    fetchManualNodes,
    fetchCountryGroups,
    fetchFilters,
    addSubscription,
    updateSubscription,
    deleteSubscription,
    refreshSubscription,
    toggleSubscription,
    addManualNode,
    updateManualNode,
    deleteManualNode,
    addFilter,
    updateFilter,
    deleteFilter,
    toggleFilter,
  } = useStore();

  const { isOpen: isSubOpen, onOpen: onSubOpen, onClose: onSubClose } = useDisclosure();
  const { isOpen: isNodeOpen, onOpen: onNodeOpen, onClose: onNodeClose } = useDisclosure();
  const { isOpen: isFilterOpen, onOpen: onFilterOpen, onClose: onFilterClose } = useDisclosure();
  const [name, setName] = useState('');
  const [url, setUrl] = useState('');
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [editingSubscription, setEditingSubscription] = useState<Subscription | null>(null);

  // 手动节点表单
  const [editingNode, setEditingNode] = useState<ManualNode | null>(null);
  const [nodeForm, setNodeForm] = useState<Node>(defaultNode);
  const [nodeEnabled, setNodeEnabled] = useState(true);
  const [nodeUrl, setNodeUrl] = useState('');
  const [isParsing, setIsParsing] = useState(false);
  const [parseError, setParseError] = useState('');

  // 过滤器表单
  const [editingFilter, setEditingFilter] = useState<Filter | null>(null);
  const defaultFilterForm: Omit<Filter, 'id'> = {
    name: '',
    include: [],
    exclude: [],
    include_countries: [],
    exclude_countries: [],
    mode: 'urltest',
    urltest_config: {
      url: 'https://www.gstatic.com/generate_204',
      interval: '5m',
      tolerance: 50,
    },
    subscriptions: [],
    all_nodes: true,
    enabled: true,
  };
  const [filterForm, setFilterForm] = useState<Omit<Filter, 'id'>>(defaultFilterForm);

  useEffect(() => {
    fetchSubscriptions();
    fetchManualNodes();
    fetchCountryGroups();
    fetchFilters();
  }, []);

  const handleOpenAddSubscription = () => {
    setEditingSubscription(null);
    setName('');
    setUrl('');
    onSubOpen();
  };

  const handleOpenEditSubscription = (sub: Subscription) => {
    setEditingSubscription(sub);
    setName(sub.name);
    setUrl(sub.url);
    onSubOpen();
  };

  const handleSaveSubscription = async () => {
    if (!name || !url) return;

    setIsSubmitting(true);
    try {
      if (editingSubscription) {
        await updateSubscription(editingSubscription.id, name, url);
      } else {
        await addSubscription(name, url);
      }
      setName('');
      setUrl('');
      setEditingSubscription(null);
      onSubClose();
    } catch (error) {
      toast.error(errorMessage(error, '请求失败，请检查管理服务'));
    } finally {
      setIsSubmitting(false);
    }
  };

  const handleRefresh = async (id: string) => {
    await refreshSubscription(id);
  };

  const handleDeleteSubscription = async (id: string) => {
    setDeleteTarget({ type: 'subscription', id });
  };

  const handleToggleSubscription = async (sub: Subscription) => {
    await toggleSubscription(sub.id, !sub.enabled);
  };

  // 手动节点操作
  const handleOpenAddNode = () => {
    setEditingNode(null);
    setNodeForm(defaultNode);
    setNodeEnabled(true);
    setNodeUrl('');
    setParseError('');
    onNodeOpen();
  };

  const handleOpenEditNode = (mn: ManualNode) => {
    setEditingNode(mn);
    setNodeForm(mn.node);
    setNodeEnabled(mn.enabled);
    setNodeUrl('');
    setParseError('');
    onNodeOpen();
  };

  // 解析节点链接
  const handleParseUrl = async () => {
    if (!nodeUrl.trim()) return;

    setIsParsing(true);
    setParseError('');

    try {
      const response = await nodeApi.parse(nodeUrl.trim());
      const parsedNode = response.data.data as Node;
      setNodeForm(parsedNode);
    } catch (error: any) {
      const message = error.response?.data?.error || '解析失败，请检查链接格式';
      setParseError(message);
    } finally {
      setIsParsing(false);
    }
  };

  const handleSaveNode = async () => {
    if (!nodeForm.tag || !nodeForm.server) return;

    setIsSubmitting(true);
    try {
      const country = countryOptions.find(c => c.code === nodeForm.country);
      const nodeData = {
        ...nodeForm,
        country_emoji: country?.emoji || '🌐',
      };

      if (editingNode) {
        await updateManualNode(editingNode.id, { node: nodeData, enabled: nodeEnabled });
      } else {
        await addManualNode({ node: nodeData, enabled: nodeEnabled });
      }
      onNodeClose();
    } catch (error) {
      toast.error(errorMessage(error, '请求失败，请检查管理服务'));
    } finally {
      setIsSubmitting(false);
    }
  };

  const handleDeleteNode = async (id: string) => {
    setDeleteTarget({ type: 'node', id });
  };

  const handleToggleNode = async (mn: ManualNode) => {
    await updateManualNode(mn.id, { ...mn, enabled: !mn.enabled });
  };

  // 过滤器操作
  const handleOpenAddFilter = () => {
    setEditingFilter(null);
    setFilterForm(defaultFilterForm);
    onFilterOpen();
  };

  const handleOpenEditFilter = (filter: Filter) => {
    setEditingFilter(filter);
    setFilterForm({
      name: filter.name,
      include: filter.include || [],
      exclude: filter.exclude || [],
      include_countries: filter.include_countries || [],
      exclude_countries: filter.exclude_countries || [],
      mode: filter.mode || 'urltest',
      urltest_config: filter.urltest_config || {
        url: 'https://www.gstatic.com/generate_204',
        interval: '5m',
        tolerance: 50,
      },
      subscriptions: filter.subscriptions || [],
      all_nodes: filter.all_nodes ?? true,
      enabled: filter.enabled,
    });
    onFilterOpen();
  };

  const handleSaveFilter = async () => {
    if (!filterForm.name) return;

    setIsSubmitting(true);
    try {
      if (editingFilter) {
        await updateFilter(editingFilter.id, filterForm);
      } else {
        await addFilter(filterForm);
      }
      onFilterClose();
    } catch (error) {
      toast.error(errorMessage(error, '请求失败，请检查管理服务'));
    } finally {
      setIsSubmitting(false);
    }
  };

  const handleDeleteFilter = async (id: string) => {
    setDeleteTarget({ type: 'filter', id });
  };

  const handleToggleFilter = async (filter: Filter) => {
    await toggleFilter(filter.id, !filter.enabled);
  };

  return (
    <div className="space-y-6">
      <ConfirmModal isOpen={!!deleteTarget} title="确认删除" onClose={() => setDeleteTarget(null)} onConfirm={async () => {
        if (!deleteTarget) return;
        if (deleteTarget.type === 'subscription') await deleteSubscription(deleteTarget.id);
        else if (deleteTarget.type === 'node') await deleteManualNode(deleteTarget.id);
        else await deleteFilter(deleteTarget.id);
        setDeleteTarget(null);
      }} confirmLabel="删除"><p>删除所选{deleteTarget?.type === 'subscription' ? '订阅及其节点' : deleteTarget?.type === 'node' ? '节点' : '过滤器'}？启用自动应用时会触发配置校验与应用。</p></ConfirmModal>
      <div className="flex flex-col md:flex-row md:items-end justify-between gap-4 border-b border-zinc-200/80 dark:border-white/[0.08] pb-5">
        <div>
          <div className="flex items-center gap-2 mb-1.5 font-mono text-[11px] text-[#ff5722] tracking-wider uppercase font-semibold">
            <span>[ NETWORK // PROXIES & NODES ]</span>
            <span className="text-zinc-400 dark:text-zinc-600">--</span>
            <span className="text-zinc-500 dark:text-zinc-400">SUBSCRIPTION POOL</span>
          </div>
          <h1 className="text-2xl font-bold tracking-tight text-zinc-900 dark:text-white font-sans flex items-center gap-3">
            节点与订阅管理
          </h1>
        </div>

        <div className="flex flex-wrap items-center gap-2.5 font-mono text-xs">
          <Button
            size="sm"
            className="rounded-[3px] font-mono text-xs border border-zinc-200 dark:border-white/[0.1] bg-white dark:bg-[#12141d] hover:bg-zinc-100 dark:hover:bg-[#181c28] text-zinc-700 dark:text-zinc-300 shadow-2xs"
            startContent={<FilterIcon className="size-3.5 text-[#ff5722]" />}
            onPress={handleOpenAddFilter}
          >
            添加过滤器
          </Button>
          <Button
            size="sm"
            className="rounded-[3px] font-mono text-xs border border-zinc-200 dark:border-white/[0.1] bg-white dark:bg-[#12141d] hover:bg-zinc-100 dark:hover:bg-[#181c28] text-zinc-700 dark:text-zinc-300 shadow-2xs"
            startContent={<Plus className="size-3.5 text-zinc-400" />}
            onPress={handleOpenAddNode}
          >
            添加节点
          </Button>
          <Button
            size="sm"
            className="rounded-[3px] font-mono text-xs bg-[#ff5722] hover:bg-[#ff6e40] text-black font-semibold uppercase tracking-wider shadow-geek-glow"
            startContent={<Plus className="size-3.5 fill-current" />}
            onPress={handleOpenAddSubscription}
          >
            添加订阅
          </Button>
        </div>
      </div>

      {settings?.imported_policy ? (
        <div className="relative rounded-[3px] border border-cyan-200 dark:border-cyan-500/20 bg-cyan-50 dark:bg-cyan-500/5 p-3.5 text-sm text-cyan-950 dark:text-cyan-200/90 leading-6 overflow-hidden">
          <div className="absolute left-0 top-0 bottom-0 w-1 bg-cyan-400"></div>
          <span className="font-semibold text-cyan-800 dark:text-cyan-300 mr-2">[POLICY_INFO]</span>
          当前保留导入配置的原有选择组及其成员。这里新增或启用的节点可通过 Managed Proxy / Managed Auto 使用，也可在规则页直接指定节点、国家组或过滤器；存在可用原生节点时才生成 Managed 组。同名节点或自定义组冲突会在配置校验时提示。
        </div>
      ) : null}

      <Tabs
        aria-label="节点管理"
        variant="underlined"
        classNames={{
          tabList: "gap-6 border-b border-zinc-200 dark:border-white/[0.08] p-0 font-mono text-xs",
          cursor: "w-full bg-[#ff5722]",
          tab: "max-w-fit px-0 h-10 data-[selected=true]:font-semibold",
          tabContent: "text-slate-600 dark:text-zinc-400 group-data-[selected=true]:text-slate-950 dark:group-data-[selected=true]:text-white font-mono text-xs",
        }}
      >
        <Tab key="subscriptions" title="// 订阅管理 (SUBS)">
          {subscriptions.length === 0 ? (
            <div className="mt-4 p-12 rounded-[4px] border border-white/[0.08] bg-[#0b0c10] text-center font-mono">
              <Globe className="size-10 mx-auto text-zinc-700 mb-3 stroke-[1.5]" />
              <p className="text-zinc-500 text-xs">暂无订阅源，点击右上角「添加订阅」配置</p>
            </div>
          ) : (
            <div className="space-y-4 mt-4">
              {subscriptions.map((sub) => (
                <SubscriptionCard
                  key={sub.id}
                  subscription={sub}
                  onRefresh={() => handleRefresh(sub.id)}
                  onEdit={() => handleOpenEditSubscription(sub)}
                  onDelete={() => handleDeleteSubscription(sub.id)}
                  onToggle={() => handleToggleSubscription(sub)}
                  loading={loading}
                />
              ))}
            </div>
          )}
        </Tab>

        <Tab key="manual" title="// 手动节点 (CUSTOM)">
          {manualNodes.length === 0 ? (
            <div className="mt-4 p-12 rounded-[4px] border border-white/[0.08] bg-[#0b0c10] text-center font-mono">
              <Server className="size-10 mx-auto text-zinc-700 mb-3 stroke-[1.5]" />
              <p className="text-zinc-500 text-xs">暂无手动节点，可从单个分享链接解析添加</p>
            </div>
          ) : (
            <div className="space-y-3 mt-4">
              {manualNodes.map((mn) => (
                <div
                  key={mn.id}
                  className="flex flex-col sm:flex-row sm:items-center justify-between p-3.5 rounded-[3px] border border-white/[0.08] bg-[#0b0c10] hover:border-white/[0.15] transition-all gap-3"
                >
                  <div className="flex items-center gap-3">
                    <span className="text-2xl p-1 bg-white/[0.04] rounded-[2px]">{mn.node.country_emoji || '🌐'}</span>
                    <div>
                      <div className="flex items-center gap-2">
                        <h3 className="font-mono text-sm font-semibold text-white">{mn.node.tag}</h3>
                        <span className="font-mono text-[10px] px-1.5 py-0.5 rounded-[2px] bg-[#ff5722]/10 border border-[#ff5722]/30 text-[#ff5722] uppercase">
                          {mn.node.type}
                        </span>
                      </div>
                      <p className="text-xs font-mono text-zinc-500 mt-0.5">
                        {mn.node.server}:{mn.node.server_port}
                      </p>
                    </div>
                  </div>
                  <div className="flex items-center gap-2 self-end sm:self-auto font-mono">
                    <Button
                      isIconOnly
                      size="sm"
                      className="size-7 rounded-[2px] bg-white/[0.05] border border-white/[0.08] text-zinc-300 hover:text-white"
                      onPress={() => handleOpenEditNode(mn)}
                    >
                      <Pencil className="size-3.5" />
                    </Button>
                    <Button
                      isIconOnly
                      size="sm"
                      className="size-7 rounded-[2px] bg-rose-500/10 border border-rose-500/20 text-rose-700 dark:text-rose-400 hover:bg-rose-500/20"
                      onPress={() => handleDeleteNode(mn.id)}
                    >
                      <Trash2 className="size-3.5" />
                    </Button>
                    <Switch
                      size="sm"
                      isSelected={mn.enabled}
                      onValueChange={() => handleToggleNode(mn)}
                      classNames={{ wrapper: "group-data-[selected=true]:bg-[#ff5722]" }}
                    />
                  </div>
                </div>
              ))}
            </div>
          )}
        </Tab>

        <Tab key="filters" title="// 过滤器 (FILTERS)">
          {filters.length === 0 ? (
            <div className="mt-4 p-12 rounded-[4px] border border-white/[0.08] bg-[#0b0c10] text-center font-mono">
              <FilterIcon className="size-10 mx-auto text-zinc-700 mb-3 stroke-[1.5]" />
              <p className="text-zinc-500 text-xs">暂无过滤器，可根据国家或关键字筛选节点创建动态组</p>
            </div>
          ) : (
            <div className="space-y-3 mt-4">
              {filters.map((filter) => (
                <div
                  key={filter.id}
                  className="flex flex-col sm:flex-row sm:items-center justify-between p-3.5 rounded-[3px] border border-white/[0.08] bg-[#0b0c10] hover:border-white/[0.15] transition-all gap-3"
                >
                  <div className="flex items-center gap-3">
                    <div className="size-8 rounded-[2px] bg-purple-500/10 border border-purple-500/20 flex items-center justify-center text-purple-400">
                      <FilterIcon className="size-4" />
                    </div>
                    <div>
                      <h3 className="font-mono text-sm font-semibold text-white">{filter.name}</h3>
                      <div className="flex flex-wrap gap-1.5 mt-1 font-mono text-[10px]">
                        {filter.include_countries?.length > 0 && (
                          <span className="px-1.5 py-0.5 rounded-[2px] bg-emerald-500/10 border border-emerald-500/30 text-emerald-400">
                            + {filter.include_countries.map(code => countryOptions.find(c => c.code === code)?.emoji || code).join(' ')}
                          </span>
                        )}
                        {filter.exclude_countries?.length > 0 && (
                          <span className="px-1.5 py-0.5 rounded-[2px] bg-rose-500/10 border border-rose-500/30 text-rose-400">
                            - {filter.exclude_countries.map(code => countryOptions.find(c => c.code === code)?.emoji || code).join(' ')}
                          </span>
                        )}
                        {filter.include?.length > 0 && (
                          <span className="px-1.5 py-0.5 rounded-[2px] bg-white/[0.05] border border-white/[0.08] text-zinc-300">
                            MATCH: {filter.include.join('|')}
                          </span>
                        )}
                        <span className="px-1.5 py-0.5 rounded-[2px] bg-[#ff5722]/10 border border-[#ff5722]/30 text-[#ff5722] uppercase">
                          MODE: {filter.mode === 'urltest' ? 'URLTEST (自动测速)' : 'SELECTOR (手动)'}
                        </span>
                      </div>
                    </div>
                  </div>
                  <div className="flex items-center gap-2 self-end sm:self-auto font-mono">
                    <Button
                      isIconOnly
                      size="sm"
                      className="size-7 rounded-[2px] bg-white/[0.05] border border-white/[0.08] text-zinc-300 hover:text-white"
                      onPress={() => handleOpenEditFilter(filter)}
                    >
                      <Pencil className="size-3.5" />
                    </Button>
                    <Button
                      isIconOnly
                      size="sm"
                      className="size-7 rounded-[2px] bg-rose-500/10 border border-rose-500/20 text-rose-700 dark:text-rose-400 hover:bg-rose-500/20"
                      onPress={() => handleDeleteFilter(filter.id)}
                    >
                      <Trash2 className="size-3.5" />
                    </Button>
                    <Switch
                      size="sm"
                      isSelected={filter.enabled}
                      onValueChange={() => handleToggleFilter(filter)}
                      classNames={{ wrapper: "group-data-[selected=true]:bg-[#ff5722]" }}
                    />
                  </div>
                </div>
              ))}
            </div>
          )}
        </Tab>

        <Tab key="countries" title="// 按国家分组 (GEO)">
          {countryGroups.length === 0 ? (
            <div className="mt-4 p-12 rounded-[4px] border border-white/[0.08] bg-[#0b0c10] text-center font-mono">
              <Globe className="size-10 mx-auto text-zinc-700 mb-3 stroke-[1.5]" />
              <p className="text-zinc-500 text-xs">暂无可用节点，请先添加订阅</p>
            </div>
          ) : (
            <div className="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-4 gap-4 mt-4 font-mono">
              {countryGroups.map((group) => (
                <div
                  key={group.code}
                  className="p-3.5 rounded-[3px] border border-white/[0.08] bg-[#0b0c10] hover:border-white/[0.15] transition-all flex items-center gap-3"
                >
                  <span className="text-3xl p-1 bg-white/[0.03] rounded-[2px]">{group.emoji}</span>
                  <div>
                    <h3 className="font-sans font-semibold text-sm text-white">{group.name}</h3>
                    <p className="font-mono text-xs text-zinc-500">{group.node_count} 节点</p>
                  </div>
                </div>
              ))}
            </div>
          )}
        </Tab>
      </Tabs>

      {/* 添加/编辑订阅弹窗 */}
      <Modal isOpen={isSubOpen} onClose={onSubClose}>
        <ModalContent>
          <ModalHeader>{editingSubscription ? '编辑订阅' : '添加订阅'}</ModalHeader>
          <ModalBody>
            <Input
              label="订阅名称"
              placeholder="输入订阅名称"
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
            <Input
              label="订阅地址"
              placeholder="输入订阅 URL"
              value={url}
              onChange={(e) => setUrl(e.target.value)}
            />
          </ModalBody>
          <ModalFooter>
            <Button variant="flat" onPress={onSubClose}>
              取消
            </Button>
            <Button
              color="primary"
              onPress={handleSaveSubscription}
              isLoading={isSubmitting}
              isDisabled={!name || !url}
            >
              {editingSubscription ? '保存' : '添加'}
            </Button>
          </ModalFooter>
        </ModalContent>
      </Modal>

      {/* 添加/编辑节点弹窗 */}
      <Modal isOpen={isNodeOpen} onClose={onNodeClose} size="lg">
        <ModalContent>
          <ModalHeader>{editingNode ? '编辑节点' : '添加节点'}</ModalHeader>
          <ModalBody>
            <div className="space-y-4">
              {/* 节点链接输入 - 仅在添加模式显示 */}
              {!editingNode && (
                <div className="space-y-2">
                  <div className="flex gap-2">
                    <Input
                      label="节点链接"
                      placeholder="粘贴节点链接，如 hysteria2://... vmess://... ss://... socks://..."
                      value={nodeUrl}
                      onChange={(e) => setNodeUrl(e.target.value)}
                      startContent={<Link className="w-4 h-4 text-gray-400" />}
                      className="flex-1"
                    />
                    <Button
                      color="primary"
                      variant="flat"
                      onPress={handleParseUrl}
                      isLoading={isParsing}
                      isDisabled={!nodeUrl.trim()}
                      className="self-end"
                    >
                      解析
                    </Button>
                  </div>
                  {parseError && (
                    <p className="text-sm text-danger">{parseError}</p>
                  )}
                  <p className="text-xs text-gray-400">
                    支持的协议: ss://, vmess://, vless://, trojan://, hysteria2://, tuic://, socks://
                  </p>
                </div>
              )}

              {/* 解析后显示节点信息 */}
              {nodeForm.tag && (
                <Card className="bg-default-100">
                  <CardBody className="py-3">
                    <div className="flex items-center gap-3">
                      <span className="text-2xl">{nodeForm.country_emoji || '🌐'}</span>
                      <div className="flex-1">
                        <h4 className="font-medium">{nodeForm.tag}</h4>
                        <p className="text-sm text-gray-500">
                          {nodeForm.type} · {nodeForm.server}:{nodeForm.server_port}
                        </p>
                      </div>
                      <Chip size="sm" variant="flat" color="success">已解析</Chip>
                    </div>
                  </CardBody>
                </Card>
              )}

              {/* 手动编辑区域 - 可折叠 */}
              <Accordion variant="bordered" selectionMode="multiple">
                <AccordionItem key="manual" aria-label="手动编辑" title="手动编辑节点信息">
                  <div className="space-y-4 pb-2">
                    <Input
                      label="节点名称"
                      placeholder="例如：香港-01"
                      value={nodeForm.tag}
                      onChange={(e) => setNodeForm({ ...nodeForm, tag: e.target.value })}
                    />

                    <div className="grid grid-cols-2 gap-4">
                      <AppSelect
                        label="节点类型"
                        selectedKeys={[nodeForm.type]}
                        onChange={(e) => setNodeForm({ ...nodeForm, type: e.target.value })}
                      >
                        {nodeTypeOptions.map((opt) => (
                          <SelectItem key={opt.value} value={opt.value}>
                            {opt.label}
                          </SelectItem>
                        ))}
                      </AppSelect>

                      <AppSelect
                        label="国家/地区"
                        selectedKeys={[nodeForm.country || 'HK']}
                        onChange={(e) => {
                          const country = countryOptions.find(c => c.code === e.target.value);
                          setNodeForm({
                            ...nodeForm,
                            country: e.target.value,
                            country_emoji: country?.emoji || '🌐',
                          });
                        }}
                      >
                        {countryOptions.map((opt) => (
                          <SelectItem key={opt.code} value={opt.code}>
                            {opt.emoji} {opt.name}
                          </SelectItem>
                        ))}
                      </AppSelect>
                    </div>

                    <div className="grid grid-cols-2 gap-4">
                      <Input
                        label="服务器地址"
                        placeholder="example.com"
                        value={nodeForm.server}
                        onChange={(e) => setNodeForm({ ...nodeForm, server: e.target.value })}
                      />

                      <Input
                        type="number"
                        label="端口"
                        placeholder="443"
                        value={String(nodeForm.server_port)}
                        onChange={(e) => setNodeForm({ ...nodeForm, server_port: parseInt(e.target.value) || 443 })}
                      />
                    </div>
                  </div>
                </AccordionItem>
              </Accordion>

              <div className="flex items-center justify-between">
                <span>启用节点</span>
                <Switch
                  isSelected={nodeEnabled}
                  onValueChange={setNodeEnabled}
                  classNames={{ wrapper: "group-data-[selected=true]:bg-[#ff5722]" }}
                />
              </div>
            </div>
          </ModalBody>
          <ModalFooter>
            <Button variant="flat" onPress={onNodeClose}>
              取消
            </Button>
            <Button
              color="primary"
              onPress={handleSaveNode}
              isLoading={isSubmitting}
              isDisabled={!nodeForm.tag || !nodeForm.server}
            >
              {editingNode ? '保存' : '添加'}
            </Button>
          </ModalFooter>
        </ModalContent>
      </Modal>

      {/* 添加/编辑过滤器弹窗 */}
      <Modal isOpen={isFilterOpen} onClose={onFilterClose} size="2xl">
        <ModalContent>
          <ModalHeader>{editingFilter ? '编辑过滤器' : '添加过滤器'}</ModalHeader>
          <ModalBody>
            <div className="space-y-4">
              {/* 过滤器名称 */}
              <Input
                label="过滤器名称"
                placeholder="例如：日本高速节点、TikTok专用"
                value={filterForm.name}
                onChange={(e) => setFilterForm({ ...filterForm, name: e.target.value })}
                isRequired
              />
              {/* 包含国家 */}
              <AppSelect
                label="包含国家"
                placeholder="选择要包含的国家（可多选）"
                selectionMode="multiple"
                selectedKeys={filterForm.include_countries}
                onSelectionChange={(keys) => {
                  setFilterForm({
                    ...filterForm,
                    include_countries: Array.from(keys) as string[]
                  })
                }}
              >
                {countryOptions.map((opt) => (
                  <SelectItem key={opt.code} value={opt.code}>
                    {opt.name}
                  </SelectItem>
                ))}
              </AppSelect>

              {/* 排除国家 */}
              <AppSelect
                label="排除国家"
                placeholder="选择要排除的国家（可多选）"
                selectionMode="multiple"
                selectedKeys={filterForm.exclude_countries}
                onSelectionChange={(keys) => setFilterForm({
                  ...filterForm,
                  exclude_countries: Array.from(keys) as string[]
                })}
              >
                {countryOptions.map((opt) => (
                  <SelectItem key={opt.code} value={opt.code}>
                    {opt.name}
                  </SelectItem>
                ))}
              </AppSelect>

              {/* 包含关键字 */}
              <Input
                label="包含关键字"
                placeholder="用 | 分隔，如：高速|IPLC|专线"
                value={filterForm.include.join('|')}
                onChange={(e) => setFilterForm({
                  ...filterForm,
                  include: e.target.value ? e.target.value.split('|').filter(Boolean) : []
                })}
              />

              {/* 排除关键字 */}
              <Input
                label="排除关键字"
                placeholder="用 | 分隔，如：过期|维护|低速"
                value={filterForm.exclude.join('|')}
                onChange={(e) => setFilterForm({
                  ...filterForm,
                  exclude: e.target.value ? e.target.value.split('|').filter(Boolean) : []
                })}
              />

              {/* 全部节点开关 */}
              <div className="flex items-center justify-between">
                <div>
                  <span className="font-medium">应用于全部节点</span>
                  <p className="text-xs text-gray-400">启用后将匹配所有订阅的节点</p>
                </div>
                <Switch
                  isSelected={filterForm.all_nodes}
                  onValueChange={(checked) => setFilterForm({ ...filterForm, all_nodes: checked })}
                  classNames={{ wrapper: "group-data-[selected=true]:bg-[#ff5722]" }}
                />
              </div>

              {/* 模式选择 */}
              <AppSelect
                label="模式"
                selectedKeys={[filterForm.mode]}
                onChange={(e) => setFilterForm({ ...filterForm, mode: e.target.value })}
              >
                <SelectItem key="urltest" value="urltest">
                  自动测速 (urltest)
                </SelectItem>
                <SelectItem key="selector" value="selector">
                  手动选择 (selector)
                </SelectItem>
              </AppSelect>

              {/* urltest 配置 */}
              {filterForm.mode === 'urltest' && (
                <Card className="bg-default-50">
                  <CardBody className="space-y-3">
                    <h4 className="font-medium text-sm">测速配置</h4>
                    <Input
                      label="测速 URL"
                      placeholder="https://www.gstatic.com/generate_204"
                      value={filterForm.urltest_config?.url || ''}
                      onChange={(e) => setFilterForm({
                        ...filterForm,
                        urltest_config: { ...filterForm.urltest_config!, url: e.target.value }
                      })}
                      size="sm"
                    />
                    <div className="grid grid-cols-2 gap-3">
                      <Input
                        label="测速间隔"
                        placeholder="5m"
                        value={filterForm.urltest_config?.interval || ''}
                        onChange={(e) => setFilterForm({
                          ...filterForm,
                          urltest_config: { ...filterForm.urltest_config!, interval: e.target.value }
                        })}
                        size="sm"
                      />
                      <Input
                        type="number"
                        label="容差阈值 (ms)"
                        placeholder="50"
                        value={String(filterForm.urltest_config?.tolerance || 50)}
                        onChange={(e) => setFilterForm({
                          ...filterForm,
                          urltest_config: { ...filterForm.urltest_config!, tolerance: parseInt(e.target.value) || 50 }
                        })}
                        size="sm"
                      />
                    </div>
                  </CardBody>
                </Card>
              )}

              {/* 启用开关 */}
              <div className="flex items-center justify-between">
                <span>启用过滤器</span>
                <Switch
                  isSelected={filterForm.enabled}
                  onValueChange={(checked) => setFilterForm({ ...filterForm, enabled: checked })}
                  classNames={{ wrapper: "group-data-[selected=true]:bg-[#ff5722]" }}
                />
              </div>
            </div>
          </ModalBody>
          <ModalFooter>
            <Button variant="flat" onPress={onFilterClose}>
              取消
            </Button>
            <Button
              color="primary"
              onPress={handleSaveFilter}
              isLoading={isSubmitting}
              isDisabled={!filterForm.name}
            >
              {editingFilter ? '保存' : '添加'}
            </Button>
          </ModalFooter>
        </ModalContent>
      </Modal>
    </div>
  );
}

interface SubscriptionCardProps {
  subscription: Subscription;
  onRefresh: () => void;
  onEdit: () => void;
  onDelete: () => void;
  onToggle: () => void;
  loading: boolean;
}

function SubscriptionCard({ subscription: sub, onRefresh, onEdit, onDelete, onToggle, loading }: SubscriptionCardProps) {
  const [isExpanded, setIsExpanded] = useState(false);

  // 确保 nodes 是数组，处理 null 或 undefined 情况
  const nodes = sub.nodes || [];

  // 按国家分组节点
  const nodesByCountry = nodes.reduce((acc, node) => {
    const country = node.country || 'OTHER';
    if (!acc[country]) {
      acc[country] = {
        emoji: node.country_emoji || '🌐',
        nodes: [],
      };
    }
    acc[country].nodes.push(node);
    return acc;
  }, {} as Record<string, { emoji: string; nodes: Node[] }>);

  return (
    <div className="rounded-[4px] border border-white/[0.08] bg-[#0b0c10] hover:border-white/[0.15] transition-all overflow-hidden shadow-sm">
      <div
        className="p-4 flex flex-col md:flex-row md:items-center justify-between gap-4 cursor-pointer select-none bg-white dark:bg-[#0e1017]/60"
        onClick={(e) => {
          if ((e.target as HTMLElement).closest('button') || (e.target as HTMLElement).closest('.interactive-control')) return;
          setIsExpanded(!isExpanded);
        }}
      >
        <div className="flex items-center gap-3">
          <span className="relative flex size-2.5">
            {sub.enabled ? (
              <>
                <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-emerald-400 opacity-75"></span>
                <span className="relative inline-flex rounded-full size-2.5 bg-emerald-500"></span>
              </>
            ) : (
              <span className="relative inline-flex rounded-full size-2.5 bg-zinc-600"></span>
            )}
          </span>
          <div>
            <div className="flex items-center gap-2">
              <h3 className="font-mono text-base font-bold text-white tracking-tight">{sub.name}</h3>
              <span className="font-mono text-[10px] px-1.5 py-0.5 rounded-[2px] bg-white/[0.05] border border-white/[0.08] text-zinc-400">
                {sub.node_count} 节点
              </span>
            </div>
            <p className="font-mono text-xs text-slate-600 dark:text-zinc-400 mt-1">
              UPDATED: {new Date(sub.updated_at).toLocaleString()}
            </p>
          </div>
        </div>

        <div className="flex items-center gap-2 font-mono text-xs">
          <Button
            size="sm"
            className="rounded-[2px] font-mono text-xs bg-white/[0.04] border border-white/[0.08] text-zinc-300 hover:text-white"
            startContent={loading ? <Spinner size="sm" /> : <RefreshCw className="size-3 text-[#ff5722]" />}
            onPress={onRefresh}
            isDisabled={loading}
          >
            拉取
          </Button>
          <Button
            size="sm"
            className="rounded-[2px] font-mono text-xs bg-white/[0.04] border border-white/[0.08] text-zinc-300 hover:text-white"
            startContent={<Pencil className="size-3 text-zinc-400" />}
            onPress={onEdit}
          >
            编辑
          </Button>
          <Button
            size="sm"
            className="rounded-[2px] font-mono text-xs bg-rose-500/10 border border-rose-500/20 text-rose-700 dark:text-rose-400 hover:bg-rose-500/20"
            startContent={<Trash2 className="size-3" />}
            onPress={onDelete}
          >
            删除
          </Button>
          <Button
            isIconOnly
            size="sm"
            className="size-7 rounded-[2px] bg-white/[0.04] border border-white/[0.08] text-zinc-400 hover:text-white"
            onPress={() => setIsExpanded(!isExpanded)}
          >
            {isExpanded ? <ChevronUp className="size-3.5" /> : <ChevronDown className="size-3.5" />}
          </Button>
          <div className="interactive-control ml-1">
            <Switch
              size="sm"
              isSelected={sub.enabled}
              onValueChange={onToggle}
              classNames={{ wrapper: "group-data-[selected=true]:bg-[#ff5722]" }}
            />
          </div>
        </div>
      </div>

      {isExpanded && (
        <div className="p-4 border-t border-white/[0.06] bg-[#07080c] space-y-4">
          {/* 流量信息条 */}
          {sub.traffic && (
            <div className="p-3 rounded-[3px] bg-black/40 border border-white/[0.06] font-mono text-xs flex flex-wrap items-center justify-between gap-3 text-zinc-400">
              <div className="flex items-center gap-4">
                <span>USED: <b className="text-zinc-200">{formatBytes(sub.traffic.used)}</b></span>
                <span>LEFT: <b className="text-emerald-700 dark:text-emerald-400">{formatBytes(sub.traffic.remaining)}</b></span>
                <span>TOTAL: <b className="text-zinc-200">{formatBytes(sub.traffic.total)}</b></span>
              </div>
              {sub.expire_at && (
                <span className="text-zinc-500">EXPIRES: {new Date(sub.expire_at).toLocaleDateString()}</span>
              )}
            </div>
          )}

          {/* 按国家分组的节点网格 */}
          <div className="space-y-3">
            {Object.entries(nodesByCountry).map(([country, data]) => (
              <div key={country} className="space-y-2">
                <div className="flex items-center gap-2 font-mono text-xs text-zinc-400 border-b border-white/[0.04] pb-1">
                  <span>{data.emoji}</span>
                  <span className="font-semibold text-zinc-200">{country}</span>
                  <span className="text-[11px] text-slate-600 dark:text-zinc-400">({data.nodes.length} 个节点)</span>
                </div>
                <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-2">
                  {data.nodes.map((node, idx) => (
                    <div
                      key={idx}
                      className="p-2.5 rounded-[2px] bg-[#0c0d12] border border-white/[0.05] hover:border-white/[0.12] transition-colors flex items-center justify-between gap-2 text-xs font-mono"
                    >
                      <span className="truncate text-zinc-300 font-medium">{node.tag}</span>
                      <span className="text-[9px] px-1 py-0.5 rounded-[2px] bg-white/[0.06] text-zinc-400 uppercase tracking-wider shrink-0">
                        {node.type}
                      </span>
                    </div>
                  ))}
                </div>
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}
