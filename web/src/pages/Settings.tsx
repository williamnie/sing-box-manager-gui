import { useCallback, useEffect, useMemo, useState, useRef } from 'react';
import {
  Card, CardBody, Input, Button, Switch, Chip, Modal, ModalContent, ModalHeader, ModalBody, ModalFooter,
  Select, SelectItem, Progress, Textarea, useDisclosure, Tabs, Tab, Divider
} from '@nextui-org/react';
import { Save, Download, CheckCircle, AlertCircle, Plus, Pencil, Trash2, Eye, EyeOff, Copy, RefreshCw, ArrowRight } from 'lucide-react';
import { useNavigate } from 'react-router-dom';
import { useStore } from '../store';
import type { Settings as SettingsType, HostEntry } from '../store';
import { configApi, daemonApi, kernelApi, settingsApi } from '../api';
import type { ConfigPreviewData } from '../features/config-topology/types';
import { toast } from '../components/Toast';

interface KernelInfo {
  installed: boolean;
  version: string;
  path: string;
  os: string;
  arch: string;
}

interface DownloadProgress {
  status: 'idle' | 'preparing' | 'downloading' | 'extracting' | 'installing' | 'completed' | 'error';
  progress: number;
  message: string;
  downloaded?: number;
  total?: number;
}

interface GithubRelease {
  tag_name: string;
  name: string;
}

interface ApiErrorLike {
  response?: {
    data?: {
      error?: string;
    };
  };
  message?: string;
}

const toErrorMessage = (error: unknown, fallback: string) => {
  if (error && typeof error === 'object') {
    const maybeError = error as ApiErrorLike;
    return maybeError.response?.data?.error || maybeError.message || fallback;
  }
  return fallback;
};

export default function Settings() {
  const navigate = useNavigate();
  const { settings, fetchSettings, updateSettings } = useStore();
  const [formData, setFormData] = useState<SettingsType | null>(null);
  const [daemonStatus, setDaemonStatus] = useState<{ installed: boolean; running: boolean; supported: boolean } | null>(null);
  const [kernelInfo, setKernelInfo] = useState<KernelInfo | null>(null);
  const [releases, setReleases] = useState<GithubRelease[]>([]);
  const [selectedVersion, setSelectedVersion] = useState<string>('');
  const [showDownloadModal, setShowDownloadModal] = useState(false);
  const [downloading, setDownloading] = useState(false);
  const [downloadProgress, setDownloadProgress] = useState<DownloadProgress | null>(null);
  const pollIntervalRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const [systemHosts, setSystemHosts] = useState<HostEntry[]>([]);
  const { isOpen: isHostModalOpen, onOpen: onHostModalOpen, onClose: onHostModalClose } = useDisclosure();
  const [editingHost, setEditingHost] = useState<HostEntry | null>(null);
  const [hostFormData, setHostFormData] = useState({ domain: '', enabled: true });
  const [ipsText, setIpsText] = useState('');
  const [showSecret, setShowSecret] = useState(false);
  const [configPreview, setConfigPreview] = useState('');
  const [configData, setConfigData] = useState<ConfigPreviewData | null>(null);
  const [configLoading, setConfigLoading] = useState(false);
  const [configError, setConfigError] = useState<string | null>(null);
  const [configUpdatedAt, setConfigUpdatedAt] = useState<Date | null>(null);

  const fetchKernelInfo = useCallback(async () => {
    try {
      const res = await kernelApi.getInfo();
      setKernelInfo(res.data.data);
    } catch (e) { console.error(e); }
  }, []);

  const fetchSystemHosts = useCallback(async () => {
    try {
      const res = await settingsApi.getSystemHosts();
      setSystemHosts(res.data.data || []);
    } catch (e) { console.error(e); }
  }, []);

  const fetchConfigPreview = useCallback(async () => {
    setConfigLoading(true);
    setConfigError(null);
    try {
      const res = await configApi.preview();
      let parsed: ConfigPreviewData;

      if (typeof res.data === 'string') {
        parsed = JSON.parse(res.data);
      } else {
        parsed = res.data as ConfigPreviewData;
      }

      setConfigData(parsed);
      setConfigPreview(JSON.stringify(parsed, null, 2));
      setConfigUpdatedAt(new Date());
    } catch (error) {
      const message = toErrorMessage(error, '获取配置预览失败');
      setConfigError(message);
      setConfigData(null);
      setConfigPreview('');
    } finally {
      setConfigLoading(false);
    }
  }, []);

  const fetchDaemonStatus = useCallback(async () => {
    try {
      const res = await daemonApi.status();
      setDaemonStatus(res.data.data);
    } catch (e) { console.error(e); }
  }, []);

  const fetchReleases = useCallback(async () => {
    try {
      const res = await kernelApi.getReleases();
      setReleases(res.data.data || []);
      if (res.data.data?.length > 0) setSelectedVersion(res.data.data[0].tag_name);
    } catch (e) { console.error(e); }
  }, []);

  useEffect(() => {
    fetchSettings();
    fetchDaemonStatus();
    fetchKernelInfo();
    fetchSystemHosts();
    fetchConfigPreview();
  }, [fetchConfigPreview, fetchDaemonStatus, fetchKernelInfo, fetchSettings, fetchSystemHosts]);

  useEffect(() => {
    if (settings) setFormData(settings);
  }, [settings]);

  useEffect(() => {
    return () => { if (pollIntervalRef.current) clearInterval(pollIntervalRef.current); };
  }, []);

  const handleAddHost = () => {
    setEditingHost(null);
    setHostFormData({ domain: '', enabled: true });
    setIpsText('');
    onHostModalOpen();
  };

  const handleEditHost = (host: HostEntry) => {
    setEditingHost(host);
    setHostFormData({ domain: host.domain, enabled: host.enabled });
    setIpsText(host.ips.join('\n'));
    onHostModalOpen();
  };

  const handleSaveHost = () => {
    if (!formData || !hostFormData.domain) return;
    const ips = ipsText.split('\n').map(ip => ip.trim()).filter(Boolean);
    if (ips.length === 0) return;
    const newHost: HostEntry = {
      id: editingHost?.id || `custom-${Date.now()}`,
      domain: hostFormData.domain,
      ips,
      enabled: hostFormData.enabled,
    };
    const hosts = formData.hosts || [];
    if (editingHost) {
      setFormData({ ...formData, hosts: hosts.map(h => h.id === editingHost.id ? newHost : h) });
    } else {
      setFormData({ ...formData, hosts: [...hosts, newHost] });
    }
    onHostModalClose();
  };

  const handleDeleteHost = (id: string) => {
    if (!formData) return;
    setFormData({ ...formData, hosts: (formData.hosts || []).filter(h => h.id !== id) });
  };

  const handleToggleHost = (id: string, enabled: boolean) => {
    if (!formData) return;
    setFormData({ ...formData, hosts: (formData.hosts || []).map(h => h.id === id ? { ...h, enabled } : h) });
  };

  const handleSave = async () => {
    if (!formData) return;
    try {
      await updateSettings(formData);
      toast.success('设置已保存');
    } catch (error) {
      toast.error(toErrorMessage(error, '保存失败'));
    }
  };

  const handleInstallDaemon = async () => {
    try {
      await daemonApi.install();
      toast.success('后台服务已安装');
      await fetchDaemonStatus();
    } catch (error) {
      toast.error(toErrorMessage(error, '安装失败'));
    }
  };

  const handleUninstallDaemon = async () => {
    try {
      await daemonApi.uninstall();
      toast.success('后台服务已卸载');
      await fetchDaemonStatus();
    } catch (error) {
      toast.error(toErrorMessage(error, '卸载失败'));
    }
  };

  const handleRestartDaemon = async () => {
    try {
      await daemonApi.restart();
      toast.success('服务已重启');
      await fetchDaemonStatus();
    } catch (error) {
      toast.error(toErrorMessage(error, '重启失败'));
    }
  };

  const openDownloadModal = async () => {
    await fetchReleases();
    setDownloadProgress(null);
    setShowDownloadModal(true);
  };

  const startDownload = async () => {
    if (!selectedVersion) return;
    setDownloading(true);
    setDownloadProgress({ status: 'preparing', progress: 0, message: '准备下载...' });
    try {
      await kernelApi.download(selectedVersion);
      pollIntervalRef.current = setInterval(async () => {
        try {
          const res = await kernelApi.getProgress();
          const progress = res.data.data;
          setDownloadProgress(progress);
          if (progress.status === 'completed' || progress.status === 'error') {
            if (pollIntervalRef.current) clearInterval(pollIntervalRef.current);
            setDownloading(false);
            if (progress.status === 'completed') {
              await fetchKernelInfo();
              setTimeout(() => setShowDownloadModal(false), 1000);
            }
          }
        } catch (e) { console.error(e); }
      }, 500);
    } catch (error) {
      setDownloading(false);
      setDownloadProgress({ status: 'error', progress: 0, message: toErrorMessage(error, '下载失败') });
    }
  };

  const handleCopySecret = () => {
    if (!formData?.clash_api_secret) return;
    navigator.clipboard.writeText(formData.clash_api_secret);
    toast.success('已复制');
  };

  const handleGenerateSecret = () => {
    const charset = 'abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789';
    let secret = '';
    for (let i = 0; i < 16; i++) secret += charset.charAt(Math.floor(Math.random() * charset.length));
    setFormData({ ...formData!, clash_api_secret: secret });
    toast.success('已生成新密钥');
  };

  const handleCopyConfig = async () => {
    if (!configPreview) return;
    try {
      await navigator.clipboard.writeText(configPreview);
      toast.success('配置 JSON 已复制');
    } catch {
      toast.error('复制失败，请检查浏览器权限');
    }
  };

  const configSummary = useMemo(() => {
    const inbounds = Array.isArray(configData?.inbounds) ? configData.inbounds : [];
    const outbounds = Array.isArray(configData?.outbounds) ? configData.outbounds : [];
    const routeRules = Array.isArray(configData?.route?.rules) ? configData.route.rules : [];
    const dnsServers = Array.isArray(configData?.dns?.servers) ? configData.dns.servers : [];
    const ruleSets = Array.isArray(configData?.route?.rule_set)
      ? configData.route.rule_set
      : Array.isArray(configData?.route?.rule_sets)
        ? configData.route.rule_sets
        : [];

    return { inbounds, outbounds, routeRules, dnsServers, ruleSets };
  }, [configData]);

  if (!formData) return <div className="p-8 text-center text-gray-500">加载中...</div>;

  return (
    <div className="max-w-4xl mx-auto space-y-6">
      {/* 顶部 */}
      <div className="flex justify-between items-center">
        <h1 className="text-2xl font-bold">设置</h1>
        <Button color="primary" startContent={<Save className="w-4 h-4" />} onPress={handleSave}>
          保存
        </Button>
      </div>

      {/* 内核状态卡片 */}
      <Card>
        <CardBody className="flex flex-row items-center justify-between gap-4 p-4">
          <div className="flex items-center gap-3">
            {kernelInfo?.installed ? (
              <CheckCircle className="w-6 h-6 text-success" />
            ) : (
              <AlertCircle className="w-6 h-6 text-warning" />
            )}
            <div>
              <p className="font-medium">{kernelInfo?.installed ? 'sing-box 已安装' : 'sing-box 未安装'}</p>
              <p className="text-sm text-gray-500">
                {kernelInfo?.installed 
                  ? `${kernelInfo.version} · ${kernelInfo.os}/${kernelInfo.arch}`
                  : '需要下载内核才能使用'}
              </p>
            </div>
          </div>
          <Button
            variant={kernelInfo?.installed ? 'flat' : 'solid'}
            color={kernelInfo?.installed ? 'default' : 'primary'}
            startContent={<Download className="w-4 h-4" />}
            onPress={openDownloadModal}
          >
            {kernelInfo?.installed ? '更新' : '下载'}
          </Button>
        </CardBody>
      </Card>

      {/* 设置 Tabs */}
      <Tabs aria-label="设置" variant="underlined" classNames={{ tabList: "gap-6", panel: "pt-4" }}>
        {/* 基础设置 */}
        <Tab key="basic" title="基础">
          <Card>
            <CardBody className="space-y-6 p-6">
              <SettingItem label="混合代理端口" desc="HTTP/SOCKS5 混合代理">
                <Input
                  type="number"
                  size="sm"
                  className="w-32"
                  value={String(formData.mixed_port)}
                  onChange={(e) => setFormData({ ...formData, mixed_port: parseInt(e.target.value) || 2080 })}
                />
              </SettingItem>
              
              <Divider />
              
              <SettingItem label="TUN 模式" desc="透明代理，接管全部流量">
                <Switch
                  isSelected={formData.tun_enabled}
                  onValueChange={(v) => setFormData({ ...formData, tun_enabled: v })}
                />
              </SettingItem>
              
              <Divider />
              
              <SettingItem label="允许局域网" desc="其他设备可通过本机代理">
                <Switch
                  isSelected={formData.allow_lan}
                  onValueChange={(enabled) => {
                    const updates: Partial<typeof formData> = { allow_lan: enabled };
                    if (enabled && !formData.clash_api_secret) {
                      const charset = 'abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789';
                      let secret = '';
                      for (let i = 0; i < 16; i++) secret += charset.charAt(Math.floor(Math.random() * charset.length));
                      updates.clash_api_secret = secret;
                    } else if (!enabled) {
                      updates.clash_api_secret = '';
                    }
                    setFormData({ ...formData, ...updates });
                  }}
                />
              </SettingItem>

              {formData.allow_lan && (
                <div className="ml-4 p-4 bg-warning-50 dark:bg-warning-900/20 rounded-lg">
                  <p className="text-sm text-warning-700 dark:text-warning-400 mb-2">API 密钥</p>
                  <div className="flex gap-2">
                    <Input
                      type={showSecret ? "text" : "password"}
                      size="sm"
                      value={formData.clash_api_secret || ''}
                      onChange={(e) => setFormData({ ...formData, clash_api_secret: e.target.value })}
                      className="flex-1"
                    />
                    <Button isIconOnly size="sm" variant="flat" onPress={() => setShowSecret(!showSecret)}>
                      {showSecret ? <EyeOff className="w-4 h-4" /> : <Eye className="w-4 h-4" />}
                    </Button>
                    <Button isIconOnly size="sm" variant="flat" onPress={handleCopySecret}>
                      <Copy className="w-4 h-4" />
                    </Button>
                    <Button isIconOnly size="sm" variant="flat" onPress={handleGenerateSecret}>
                      <RefreshCw className="w-4 h-4" />
                    </Button>
                  </div>
                </div>
              )}
              
              <Divider />
              
              <SettingItem label="自动应用配置" desc="变更后自动重载 sing-box">
                <Switch
                  isSelected={formData.auto_apply}
                  onValueChange={(v) => setFormData({ ...formData, auto_apply: v })}
                />
              </SettingItem>
              
              <SettingItem label="订阅更新间隔" desc="0 表示禁用自动更新">
                <div className="flex items-center gap-2">
                  <Input
                    type="number"
                    size="sm"
                    className="w-24"
                    value={String(formData.subscription_interval)}
                    onChange={(e) => setFormData({ ...formData, subscription_interval: parseInt(e.target.value) || 0 })}
                  />
                  <span className="text-sm text-gray-500">分钟</span>
                </div>
              </SettingItem>
            </CardBody>
          </Card>
        </Tab>

        {/* DNS 设置 */}
        <Tab key="dns" title="DNS">
          <Card>
            <CardBody className="space-y-6 p-6">
              <SettingItem label="代理 DNS" desc="走代理的域名使用">
                <Input
                  size="sm"
                  className="w-72"
                  value={formData.proxy_dns}
                  onChange={(e) => setFormData({ ...formData, proxy_dns: e.target.value })}
                />
              </SettingItem>
              
              <Divider />
              
              <SettingItem label="直连 DNS" desc="直连域名使用">
                <Input
                  size="sm"
                  className="w-72"
                  value={formData.direct_dns}
                  onChange={(e) => setFormData({ ...formData, direct_dns: e.target.value })}
                />
              </SettingItem>
              
              <Divider />
              
              <div>
                <div className="flex justify-between items-center mb-4">
                  <div>
                    <p className="font-medium">Hosts 映射</p>
                    <p className="text-sm text-gray-500">自定义域名解析</p>
                  </div>
                  <Button size="sm" startContent={<Plus className="w-4 h-4" />} onPress={handleAddHost}>
                    添加
                  </Button>
                </div>
                
                <div className="space-y-2">
                  {formData.hosts?.map((host) => (
                    <div key={host.id} className="flex items-center justify-between p-3 bg-default-100 rounded-lg">
                      <div>
                        <span className="font-medium">{host.domain}</span>
                        {!host.enabled && <Chip size="sm" variant="flat" className="ml-2">禁用</Chip>}
                        <div className="flex gap-1 mt-1">
                          {host.ips.slice(0, 3).map((ip, i) => (
                            <Chip key={i} size="sm" variant="bordered">{ip}</Chip>
                          ))}
                          {host.ips.length > 3 && <Chip size="sm" variant="flat">+{host.ips.length - 3}</Chip>}
                        </div>
                      </div>
                      <div className="flex gap-1">
                        <Button isIconOnly size="sm" variant="light" onPress={() => handleEditHost(host)}>
                          <Pencil className="w-4 h-4" />
                        </Button>
                        <Button isIconOnly size="sm" variant="light" color="danger" onPress={() => handleDeleteHost(host.id)}>
                          <Trash2 className="w-4 h-4" />
                        </Button>
                        <Switch size="sm" isSelected={host.enabled} onValueChange={(v) => handleToggleHost(host.id, v)} />
                      </div>
                    </div>
                  ))}
                  
                  {systemHosts.length > 0 && (
                    <div className="pt-4">
                      <p className="text-sm text-gray-500 mb-2">系统 Hosts</p>
                      {systemHosts.slice(0, 5).map((host) => (
                        <div key={host.id} className="flex items-center gap-2 p-2 text-sm text-gray-500">
                          <span>{host.domain}</span>
                          <span>→</span>
                          <span>{host.ips.join(', ')}</span>
                        </div>
                      ))}
                      {systemHosts.length > 5 && (
                        <p className="text-sm text-gray-400">还有 {systemHosts.length - 5} 条...</p>
                      )}
                    </div>
                  )}
                </div>
              </div>
            </CardBody>
          </Card>
        </Tab>

        {/* 高级设置 */}
        <Tab key="advanced" title="高级">
          <Card>
            <CardBody className="space-y-6 p-6">
              <SettingItem label="配置文件路径">
                <Input
                  size="sm"
                  className="w-72"
                  value={formData.config_path}
                  onChange={(e) => setFormData({ ...formData, config_path: e.target.value })}
                />
              </SettingItem>
              
              <Divider />
              
              <SettingItem label="Clash API 端口">
                <Input
                  type="number"
                  size="sm"
                  className="w-32"
                  value={String(formData.clash_api_port)}
                  onChange={(e) => setFormData({ ...formData, clash_api_port: parseInt(e.target.value) || 9091 })}
                />
              </SettingItem>
              
              <Divider />
              
              <SettingItem label="漏网规则出站" desc="未匹配规则的流量">
                <Input
                  size="sm"
                  className="w-32"
                  value={formData.final_outbound}
                  onChange={(e) => setFormData({ ...formData, final_outbound: e.target.value })}
                />
              </SettingItem>
              
              <Divider />
              
              <SettingItem label="GitHub 代理" desc="加速内核下载">
                <Input
                  size="sm"
                  className="w-72"
                  placeholder="如 https://ghproxy.com/"
                  value={formData.github_proxy || ''}
                  onChange={(e) => setFormData({ ...formData, github_proxy: e.target.value })}
                />
              </SettingItem>
              
              <Divider />
              
              <SettingItem label="规则集地址">
                <Input
                  size="sm"
                  className="w-full"
                  value={formData.ruleset_base_url}
                  onChange={(e) => setFormData({ ...formData, ruleset_base_url: e.target.value })}
                />
              </SettingItem>
            </CardBody>
          </Card>
        </Tab>

        <Tab key="config" title="配置">
          <div className="space-y-4">
            <Card>
              <CardBody className="p-4 space-y-4">
                <div className="flex flex-wrap items-center justify-between gap-2">
                  <div>
                    <p className="font-medium">sing-box 配置预览</p>
                    <p className="text-sm text-gray-500">自动读取后端生成结果，可视化展示并支持复制</p>
                  </div>
                  <div className="flex items-center gap-2">
                    <Button
                      size="sm"
                      variant="flat"
                      startContent={<Copy className="w-4 h-4" />}
                      onPress={handleCopyConfig}
                      isDisabled={!configPreview}
                    >
                      复制 JSON
                    </Button>
                    <Button
                      size="sm"
                      color="primary"
                      startContent={<RefreshCw className="w-4 h-4" />}
                      onPress={fetchConfigPreview}
                      isLoading={configLoading}
                    >
                      刷新预览
                    </Button>
                  </div>
                </div>

                {configError && (
                  <div className="p-3 rounded-lg bg-danger-50 dark:bg-danger-900/20 text-danger-600 dark:text-danger-400 text-sm">
                    {configError}
                  </div>
                )}

                <div className="grid grid-cols-2 md:grid-cols-5 gap-3">
                  <MetricCard label="入站" value={configSummary.inbounds.length} tone="blue" />
                  <MetricCard label="路由规则" value={configSummary.routeRules.length} tone="purple" />
                  <MetricCard label="出站" value={configSummary.outbounds.length} tone="green" />
                  <MetricCard label="DNS 服务器" value={configSummary.dnsServers.length} tone="orange" />
                  <MetricCard label="规则集" value={configSummary.ruleSets.length} tone="indigo" />
                </div>

                <div className="rounded-lg border border-default-200 p-3 dark:border-default-100/20">
                  <div className="flex flex-wrap items-center justify-between gap-2">
                    <div>
                      <p className="text-sm font-medium">工作拓扑</p>
                      <p className="text-xs text-gray-500">以业务流程图方式查看流量路径</p>
                    </div>
                    <Button
                      size="sm"
                      color="primary"
                      endContent={<ArrowRight className="w-4 h-4" />}
                      onPress={() => navigate('/topology')}
                    >
                      查看拓扑
                    </Button>
                  </div>
                </div>

                <div className="text-xs text-gray-500">
                  {configUpdatedAt ? `最近刷新：${configUpdatedAt.toLocaleString()}` : '尚未加载配置预览'}
                </div>
              </CardBody>
            </Card>

            <Card>
              <CardBody className="p-4">
                <p className="text-sm font-medium mb-3">JSON 原文</p>
                <Textarea
                  isReadOnly
                  minRows={18}
                  maxRows={28}
                  value={configPreview}
                  placeholder="点击“刷新预览”加载配置"
                  classNames={{ input: 'font-mono text-xs' }}
                />
              </CardBody>
            </Card>
          </div>
        </Tab>

        {/* 服务与健康 */}
        <Tab key="service" title="服务">
          <div className="space-y-4">
            <Card>
              <CardBody className="p-6">
                <div className="flex items-center justify-between mb-4">
                  <div>
                    <p className="font-medium">后台服务</p>
                    <p className="text-sm text-gray-500">系统守护 sbm（开机自启、崩溃拉起）</p>
                  </div>
                  <Chip
                    color={daemonStatus?.supported ? (daemonStatus.installed ? 'success' : 'default') : 'default'}
                    variant="flat"
                  >
                    {daemonStatus?.supported
                      ? (daemonStatus.installed ? '已安装' : '未安装')
                      : '不支持'}
                  </Chip>
                </div>

                {daemonStatus?.supported ? (
                  <div className="flex gap-2">
                    {daemonStatus.installed ? (
                      <>
                        <Button variant="flat" onPress={handleRestartDaemon}>重启服务</Button>
                        <Button variant="flat" color="danger" onPress={handleUninstallDaemon}>卸载</Button>
                      </>
                    ) : (
                      <Button color="primary" onPress={handleInstallDaemon}>安装服务</Button>
                    )}
                  </div>
                ) : (
                  <p className="text-sm text-gray-500">当前系统不支持守护服务，仅可使用下方健康检查能力。</p>
                )}
              </CardBody>
            </Card>

            <Card>
              <CardBody className="space-y-6 p-6">
                <div>
                  <p className="font-medium">健康检查</p>
                  <p className="text-sm text-gray-500">应用内定期检查 sing-box 可用性（非系统服务）</p>
                </div>

                <SettingItem label="启用健康检查" desc="定期请求 Clash API，发现异常触发自愈策略">
                  <Switch
                    isSelected={formData.health_check_enabled}
                    onValueChange={(v) => setFormData({ ...formData, health_check_enabled: v })}
                  />
                </SettingItem>

                {formData.health_check_enabled && (
                  <>
                    <Divider />

                    <SettingItem label="检查间隔">
                      <div className="flex items-center gap-2">
                        <Input
                          type="number"
                          size="sm"
                          className="w-24"
                          value={String(formData.health_check_interval || 30)}
                          onChange={(e) => setFormData({ ...formData, health_check_interval: parseInt(e.target.value) || 30 })}
                        />
                        <span className="text-sm text-gray-500">秒</span>
                      </div>
                    </SettingItem>

                    <SettingItem label="自动重启" desc="健康检查连续失败后自动重启 sing-box">
                      <Switch
                        isSelected={formData.auto_restart}
                        onValueChange={(v) => setFormData({ ...formData, auto_restart: v })}
                      />
                    </SettingItem>
                  </>
                )}

                <p className="text-xs text-gray-500">
                  说明：后台服务负责守护 sbm 进程；健康检查负责守护 sing-box 可用性。
                </p>
              </CardBody>
            </Card>
          </div>
        </Tab>
      </Tabs>

      {/* 下载内核弹窗 */}
      <Modal isOpen={showDownloadModal} onClose={() => !downloading && setShowDownloadModal(false)}>
        <ModalContent>
          <ModalHeader>下载 sing-box</ModalHeader>
          <ModalBody>
            {downloadProgress ? (
              <div className="space-y-4">
                <Progress
                  value={downloadProgress.progress}
                  color={downloadProgress.status === 'error' ? 'danger' : downloadProgress.status === 'completed' ? 'success' : 'primary'}
                  aria-label="下载进度"
                />
                <p className="text-center text-sm">{downloadProgress.message}</p>
              </div>
            ) : (
              <Select
                label="选择版本"
                selectedKeys={selectedVersion ? [selectedVersion] : []}
                onChange={(e) => setSelectedVersion(e.target.value)}
              >
                {releases.map((r) => (
                  <SelectItem key={r.tag_name}>{r.tag_name}</SelectItem>
                ))}
              </Select>
            )}
          </ModalBody>
          <ModalFooter>
            <Button variant="flat" onPress={() => setShowDownloadModal(false)} isDisabled={downloading}>
              取消
            </Button>
            <Button color="primary" onPress={startDownload} isDisabled={!selectedVersion || downloading} isLoading={downloading}>
              下载
            </Button>
          </ModalFooter>
        </ModalContent>
      </Modal>

      {/* Hosts 编辑弹窗 */}
      <Modal isOpen={isHostModalOpen} onClose={onHostModalClose}>
        <ModalContent>
          <ModalHeader>{editingHost ? '编辑' : '添加'} Host</ModalHeader>
          <ModalBody>
            <Input
              label="域名"
              placeholder="example.com"
              value={hostFormData.domain}
              onChange={(e) => setHostFormData({ ...hostFormData, domain: e.target.value })}
            />
            <Textarea
              label="IP 地址"
              placeholder="每行一个 IP"
              value={ipsText}
              onChange={(e) => setIpsText(e.target.value)}
              minRows={3}
            />
            <div className="flex justify-between items-center">
              <span>启用</span>
              <Switch isSelected={hostFormData.enabled} onValueChange={(v) => setHostFormData({ ...hostFormData, enabled: v })} />
            </div>
          </ModalBody>
          <ModalFooter>
            <Button variant="flat" onPress={onHostModalClose}>取消</Button>
            <Button color="primary" onPress={handleSaveHost} isDisabled={!hostFormData.domain || !ipsText.trim()}>
              保存
            </Button>
          </ModalFooter>
        </ModalContent>
      </Modal>
    </div>
  );
}

// 设置项组件
function SettingItem({ label, desc, children }: { label: string; desc?: string; children: React.ReactNode }) {
  return (
    <div className="flex items-center justify-between">
      <div>
        <p className="font-medium">{label}</p>
        {desc && <p className="text-sm text-gray-500">{desc}</p>}
      </div>
      {children}
    </div>
  );
}

function MetricCard({ label, value, tone }: { label: string; value: number; tone: 'blue' | 'purple' | 'green' | 'orange' | 'indigo' }) {
  const toneClassMap = {
    blue: 'bg-blue-50 text-blue-700 dark:bg-blue-900/20 dark:text-blue-300',
    purple: 'bg-purple-50 text-purple-700 dark:bg-purple-900/20 dark:text-purple-300',
    green: 'bg-green-50 text-green-700 dark:bg-green-900/20 dark:text-green-300',
    orange: 'bg-orange-50 text-orange-700 dark:bg-orange-900/20 dark:text-orange-300',
    indigo: 'bg-indigo-50 text-indigo-700 dark:bg-indigo-900/20 dark:text-indigo-300',
  };

  return (
    <div className={`rounded-lg px-3 py-2 ${toneClassMap[tone]}`}>
      <p className="text-xs opacity-80">{label}</p>
      <p className="text-lg font-bold leading-none mt-1">{value}</p>
    </div>
  );
}
