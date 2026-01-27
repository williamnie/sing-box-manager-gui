import { useEffect, useState, useRef } from 'react';
import {
  Card, CardHeader, CardBody, Input, Button, Switch, Chip, Modal, ModalContent, ModalHeader, ModalBody, ModalFooter,
  Select, SelectItem, Progress, Textarea, useDisclosure, Tabs, Tab, Divider,
} from '@nextui-org/react';
import { Save, Download, CheckCircle, AlertCircle, Plus, Pencil, Trash2, Eye, EyeOff, Copy, RefreshCw } from 'lucide-react';
import { useStore } from '../store';
import type { Settings as SettingsType, HostEntry } from '../store';
import { daemonApi, kernelApi, settingsApi, subscribeApi } from '../api';
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

export default function Settings() {
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

  // 订阅下发状态
  const [subscribeInfo, setSubscribeInfo] = useState<{ enabled: boolean; token: string; links: { singbox?: string; clash?: string } } | null>(null);
  const [showSubscribeToken, setShowSubscribeToken] = useState(false);
  const [interfaces, setInterfaces] = useState<{ name: string; ip: string }[]>([]);

  useEffect(() => {
    fetchSettings();
    fetchDaemonStatus();
    fetchKernelInfo();
    fetchSystemHosts();
    fetchSubscribeInfo();
    fetchInterfaces();
  }, []);

  useEffect(() => {
    if (settings) setFormData(settings);
  }, [settings]);

  useEffect(() => {
    return () => { if (pollIntervalRef.current) clearInterval(pollIntervalRef.current); };
  }, []);

  const fetchKernelInfo = async () => {
    try {
      const res = await kernelApi.getInfo();
      setKernelInfo(res.data.data);
    } catch (e) { console.error(e); }
  };

  const fetchSystemHosts = async () => {
    try {
      const res = await settingsApi.getSystemHosts();
      setSystemHosts(res.data.data || []);
    } catch (e) { console.error(e); }
  };

  const fetchSubscribeInfo = async () => {
    try {
      const res = await subscribeApi.getInfo();
      setSubscribeInfo(res.data);
    } catch (error) {
      console.error('获取订阅信息失败:', error);
    }
  };

  const fetchInterfaces = async () => {
    try {
      const res = await settingsApi.getInterfaces();
      setInterfaces(res.data.data || []);
    } catch (error) {
      console.error('获取网络接口失败:', error);
    }
  };

  const handleGenerateSubscribeToken = async () => {
    try {
      await subscribeApi.generateToken();
      await fetchSubscribeInfo();
      toast.success('已生成新令牌');
    } catch (error: any) {
      toast.error(error.response?.data?.error || '生成令牌失败');
    }
  };

  const fallbackCopy = (text: string) => {
    const textArea = document.createElement("textarea");
    textArea.value = text;

    // Avoid scrolling to bottom
    textArea.style.top = "0";
    textArea.style.left = "0";
    textArea.style.position = "fixed";

    document.body.appendChild(textArea);
    textArea.focus();
    textArea.select();

    try {
      const successful = document.execCommand('copy');
      if (successful) {
        toast.success('链接已复制');
      } else {
        toast.error('复制失败，请手动复制');
      }
    } catch (err) {
      console.error('Fallback: Unable to copy', err);
      toast.error('复制失败，请手动复制');
    }

    document.body.removeChild(textArea);
  };

  const handleCopySubscribeLink = (link: string) => {
    if (navigator.clipboard && window.isSecureContext) {
      navigator.clipboard.writeText(link).then(() => {
        toast.success('链接已复制');
      }).catch(() => fallbackCopy(link));
    } else {
      fallbackCopy(link);
    }
  };

  // Hosts 处理函数
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
    } catch (e: any) {
      toast.error(e.response?.data?.error || '保存失败');
    }
  };

  const fetchDaemonStatus = async () => {
    try {
      const res = await daemonApi.status();
      setDaemonStatus(res.data.data);
    } catch (e) { console.error(e); }
  };

  const handleInstallDaemon = async () => {
    try {
      await daemonApi.install();
      toast.success('后台服务已安装');
      await fetchDaemonStatus();
    } catch (e: any) {
      toast.error(e.response?.data?.error || '安装失败');
    }
  };

  const handleUninstallDaemon = async () => {
    try {
      await daemonApi.uninstall();
      toast.success('后台服务已卸载');
      await fetchDaemonStatus();
    } catch (e: any) {
      toast.error(e.response?.data?.error || '卸载失败');
    }
  };

  const handleRestartDaemon = async () => {
    try {
      await daemonApi.restart();
      toast.success('服务已重启');
      await fetchDaemonStatus();
    } catch (e: any) {
      toast.error(e.response?.data?.error || '重启失败');
    }
  };

  const fetchReleases = async () => {
    try {
      const res = await kernelApi.getReleases();
      setReleases(res.data.data || []);
      if (res.data.data?.length > 0) setSelectedVersion(res.data.data[0].tag_name);
    } catch (e) { console.error(e); }
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
    } catch (e: any) {
      setDownloading(false);
      setDownloadProgress({ status: 'error', progress: 0, message: e.response?.data?.error || '下载失败' });
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
      <Tabs aria-label="设置" variant="underlined" classNames={{ tabList: "gap-6", panel: "pt-4 flex flex-col gap-4" }}>
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

            </CardBody>
          </Card>


          {/* 自动化设置 */}
          <Card>
            <CardHeader>
              <h2 className="text-lg font-semibold">自动化</h2>
            </CardHeader>
            <CardBody className="space-y-4">
              <div className="flex items-center justify-between">
                <div>
                  <p className="font-medium">配置变更后自动应用</p>
                  <p className="text-sm text-gray-500">订阅刷新或规则变更后自动重启 sing-box</p>
                </div>
                <Switch
                  isSelected={formData.auto_apply}
                  onValueChange={(enabled) => setFormData({ ...formData, auto_apply: enabled })}
                />
              </div>
              <Input
                type="number"
                label="订阅自动更新间隔 (分钟)"
                placeholder="60"
                description="设置为 0 表示禁用自动更新"
                value={String(formData.subscription_interval)}
                onChange={(e) => setFormData({ ...formData, subscription_interval: parseInt(e.target.value) || 0 })}
              />
            </CardBody>
          </Card>

          {/* 订阅下发 */}
          <Card>
            <CardHeader>
              {/* <Upload className="w-5 h-5 mr-2" /> */}
              <h2 className="text-lg font-semibold">订阅下发</h2>
            </CardHeader>
            <CardBody className="space-y-4">
              <div className="flex items-center justify-between">
                <div>
                  <p className="font-medium">启用订阅下发</p>
                  <p className="text-sm text-gray-500">允许其他设备通过链接获取配置</p>
                  <p className="text-sm text-gray-500">注：点击保存后才会生效</p>
                </div>
                <Switch
                  isSelected={formData.subscribe_enabled}
                  onValueChange={(enabled) => setFormData({ ...formData, subscribe_enabled: enabled })}
                />
              </div>

              {formData.subscribe_enabled && (
                <>
                  <div className="mb-4">
                    <Select
                      label="订阅地址 / Subscription URL"
                      placeholder="选择局域网 IP"
                      description="选择本机 IP 自动生成订阅地址，方便局域网设备访问"
                      selectedKeys={formData.subscribe_url ? [formData.subscribe_url] : []}
                      onChange={(e) => {
                        const val = e.target.value;
                        if (val) {
                          // 如果是选择的 IP，自动补全端口
                          let finalUrl = val;
                          // 简单判断是否是 IP，如果是则补全端口
                          if (/^[\d.]+$/.test(val)) {
                            finalUrl = `http://${val}:${formData.web_port || 9090}`;
                          }
                          setFormData({ ...formData, subscribe_url: finalUrl });
                        } else {
                          setFormData({ ...formData, subscribe_url: '' });
                        }
                      }}
                    >
                      {interfaces.map((iface) => (
                        <SelectItem key={`http://${iface.ip}:${formData.web_port || 9090}`} textValue={`${iface.name} - ${iface.ip}`}>
                          <div className="flex flex-col">
                            <span className="text-small">{iface.name}</span>
                            <span className="text-tiny text-default-400">{iface.ip}</span>
                          </div>
                        </SelectItem>
                      ))}
                    </Select>
                    {/* 提供一个手动输入的选项或者 fallback，如果为了简单，可以只提供 Select，
                        或者用 Autocomplete (NextUI 支持自由输入)。
                        用户需求是“做成下拉选择的”，如果需要完全自由输入，Autocomplete 更好。
                        这里先用 Select 展示接口列表。
                        为了灵活性，可以额外加一个Input或者把Select改成Autocomplete允许自定义。
                        NextUI Autocomplete 允许自定义输入。
                    */}
                    <div className="mt-2">
                      <Input
                        size="sm"
                        label="自定义地址 (可选)"
                        placeholder="如果下拉框不满足，可手动输入完整地址"
                        value={formData.subscribe_url || ''}
                        onChange={(e) => setFormData({ ...formData, subscribe_url: e.target.value })}
                      />
                    </div>
                  </div>

                  <div className="p-4 rounded-lg bg-default-100">
                    <div className="flex items-center justify-between mb-2">
                      <p className="font-medium">订阅令牌</p>
                      <div className="flex gap-2">
                        <Button
                          size="sm"
                          variant="flat"
                          startContent={<RefreshCw className="w-4 h-4" />}
                          onPress={handleGenerateSubscribeToken}
                        >
                          生成新令牌
                        </Button>
                      </div>
                    </div>
                    <div className="flex items-center gap-2">
                      <Input
                        type={showSubscribeToken ? "text" : "password"}
                        value={subscribeInfo?.token || formData.subscribe_token || ''}
                        readOnly
                        size="sm"
                        className="flex-1"
                        endContent={
                          <Button
                            isIconOnly
                            size="sm"
                            variant="light"
                            onPress={() => setShowSubscribeToken(!showSubscribeToken)}
                          >
                            {showSubscribeToken ? <EyeOff className="w-4 h-4" /> : <Eye className="w-4 h-4" />}
                          </Button>
                        }
                      />
                    </div>
                  </div>

                  {subscribeInfo?.links?.singbox && (
                    <div className="space-y-3">
                      <p className="text-sm text-gray-500">订阅链接（保存设置后生效）</p>
                      <div className="flex items-center gap-2">
                        <Chip size="sm" color="primary" variant="flat">sing-box</Chip>
                        <Input
                          value={formData.subscribe_url ? `${formData.subscribe_url}/api/subscribe/singbox?token=${subscribeInfo.token}` : subscribeInfo.links.singbox}
                          readOnly
                          size="sm"
                          className="flex-1"
                        />
                        <Button
                          isIconOnly
                          size="sm"
                          variant="flat"
                          onPress={() => handleCopySubscribeLink(formData.subscribe_url ? `${formData.subscribe_url}/api/subscribe/singbox?token=${subscribeInfo.token}` : subscribeInfo.links.singbox!)}
                        >
                          <Copy className="w-4 h-4" />
                        </Button>
                      </div>
                      <div className="flex items-center gap-2">
                        <Chip size="sm" color="secondary" variant="flat">Clash</Chip>
                        <Input
                          value={formData.subscribe_url ? `${formData.subscribe_url}/api/subscribe/clash?token=${subscribeInfo.token}` : (subscribeInfo.links.clash || '')}
                          readOnly
                          size="sm"
                          className="flex-1"
                        />
                        <Button
                          isIconOnly
                          size="sm"
                          variant="flat"
                          onPress={() => handleCopySubscribeLink(formData.subscribe_url ? `${formData.subscribe_url}/api/subscribe/clash?token=${subscribeInfo.token}` : subscribeInfo.links.clash!)}
                        >
                          <Copy className="w-4 h-4" />
                        </Button>
                      </div>
                    </div>
                  )}
                </>
              )}
            </CardBody>
          </Card>

          {/* 后台服务管理 */}
          {daemonStatus?.supported && (
            <Card>
              <CardHeader className="flex justify-between items-center">
                <h2 className="text-lg font-semibold">自动化配置</h2>
              </CardHeader>
              <CardBody>

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


                <SettingItem label="健康检查" desc="定期检查 sing-box 是否正常">
                  <Switch
                    isSelected={formData.health_check_enabled}
                    onValueChange={(v) => setFormData({ ...formData, health_check_enabled: v })}
                  />
                </SettingItem>

                {formData.health_check_enabled && (
                  <>
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

                    <SettingItem label="自动重启" desc="检查失败时自动重启 sing-box">
                      <Switch
                        isSelected={formData.auto_restart}
                        onValueChange={(v) => setFormData({ ...formData, auto_restart: v })}
                      />
                    </SettingItem>
                  </>
                )}
              </CardBody>
            </Card>
          )}
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
                      {systemHosts.map((host) => (
                        <div key={host.id} className="flex items-center gap-2 p-2 text-sm text-gray-500">
                          <span>{host.domain}</span>
                          <span>→</span>
                          <span>{host.ips.join(', ')}</span>
                        </div>
                      ))}
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
                  className="w-24"
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

        {/* 服务管理 */}
        {daemonStatus?.supported && (
          <Tab key="service" title="服务">
            <Card>
              <CardBody className="p-6">
                <div className="flex items-center justify-between mb-4">
                  <div>
                    <p className="font-medium">后台服务</p>
                    <p className="text-sm text-gray-500">开机自启、崩溃自动重启</p>
                  </div>
                  <Chip color={daemonStatus.installed ? 'success' : 'default'} variant="flat">
                    {daemonStatus.installed ? '已安装' : '未安装'}
                  </Chip>
                </div>

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
              </CardBody>
            </Card>
          </Tab>
        )}
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
    </div >
  );
}

// 设置项组件
function SettingItem({ label, desc, children }: { label: string; desc?: string; children: React.ReactNode }) {
  return (
    <div className="flex items-center justify-between gap-4">
      <div className="flex-shrink-0">
        <p className="font-medium">{label}</p>
        {desc && <p className="text-sm text-gray-500">{desc}</p>}
      </div>
      {children}
    </div>
  );
}
