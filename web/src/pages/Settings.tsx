import { useEffect, useState, useRef } from 'react';
import { Card, CardBody, CardHeader, Input, Button, Switch, Chip, Modal, ModalContent, ModalHeader, ModalBody, ModalFooter, Select, SelectItem, Progress, Textarea, useDisclosure } from '@nextui-org/react';
import { Save, Download, Upload, Terminal, CheckCircle, AlertCircle, Plus, Pencil, Trash2, Server } from 'lucide-react';
import { useStore } from '../store';
import type { Settings as SettingsType, HostEntry } from '../store';
import { daemonApi, kernelApi, settingsApi } from '../api';
import { toast } from '../components/Toast';

// 内核信息类型
interface KernelInfo {
  installed: boolean;
  version: string;
  path: string;
  os: string;
  arch: string;
}

// 下载进度类型
interface DownloadProgress {
  status: 'idle' | 'preparing' | 'downloading' | 'extracting' | 'installing' | 'completed' | 'error';
  progress: number;
  message: string;
  downloaded?: number;
  total?: number;
}

// GitHub Release 类型
interface GithubRelease {
  tag_name: string;
  name: string;
}

export default function Settings() {
  const { settings, fetchSettings, updateSettings } = useStore();
  const [formData, setFormData] = useState<SettingsType | null>(null);
  const [daemonStatus, setDaemonStatus] = useState<{ installed: boolean; running: boolean; supported: boolean } | null>(null);

  // 内核相关状态
  const [kernelInfo, setKernelInfo] = useState<KernelInfo | null>(null);
  const [releases, setReleases] = useState<GithubRelease[]>([]);
  const [selectedVersion, setSelectedVersion] = useState<string>('');
  const [showDownloadModal, setShowDownloadModal] = useState(false);
  const [downloading, setDownloading] = useState(false);
  const [downloadProgress, setDownloadProgress] = useState<DownloadProgress | null>(null);
  const pollIntervalRef = useRef<ReturnType<typeof setInterval> | null>(null);

  // Hosts 相关状态
  const [systemHosts, setSystemHosts] = useState<HostEntry[]>([]);
  const { isOpen: isHostModalOpen, onOpen: onHostModalOpen, onClose: onHostModalClose } = useDisclosure();
  const [editingHost, setEditingHost] = useState<HostEntry | null>(null);
  const [hostFormData, setHostFormData] = useState({ domain: '', enabled: true });
  const [ipsText, setIpsText] = useState('');

  useEffect(() => {
    fetchSettings();
    fetchDaemonStatus();
    fetchKernelInfo();
    fetchSystemHosts();
  }, []);

  useEffect(() => {
    if (settings) {
      setFormData(settings);
    }
  }, [settings]);

  // 清理轮询定时器
  useEffect(() => {
    return () => {
      if (pollIntervalRef.current) {
        clearInterval(pollIntervalRef.current);
      }
    };
  }, []);

  const fetchKernelInfo = async () => {
    try {
      const res = await kernelApi.getInfo();
      setKernelInfo(res.data.data);
    } catch (error) {
      console.error('获取内核信息失败:', error);
    }
  };

  const fetchSystemHosts = async () => {
    try {
      const res = await settingsApi.getSystemHosts();
      setSystemHosts(res.data.data || []);
    } catch (error) {
      console.error('获取系统 hosts 失败:', error);
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

  const handleDeleteHost = (id: string) => {
    if (!formData?.hosts) return;
    setFormData({
      ...formData,
      hosts: formData.hosts.filter(h => h.id !== id)
    });
  };

  const handleToggleHost = (id: string, enabled: boolean) => {
    if (!formData?.hosts) return;
    setFormData({
      ...formData,
      hosts: formData.hosts.map(h => h.id === id ? { ...h, enabled } : h)
    });
  };

  const handleSubmitHost = () => {
    const ips = ipsText.split('\n').map(ip => ip.trim()).filter(ip => ip);

    // 验证 IP 格式
    const ipv4Regex = /^(\d{1,3}\.){3}\d{1,3}$/;
    const ipv6Regex = /^([a-fA-F0-9:]+)$/;
    const invalidIps = ips.filter(ip => !ipv4Regex.test(ip) && !ipv6Regex.test(ip));
    if (invalidIps.length > 0) {
      toast.error(`无效的 IP 地址: ${invalidIps.join(', ')}`);
      return;
    }

    // 验证域名格式
    const domainRegex = /^[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?)*$/;
    if (!domainRegex.test(hostFormData.domain)) {
      toast.error('无效的域名格式');
      return;
    }

    if (ips.length === 0) {
      toast.error('请输入至少一个 IP 地址');
      return;
    }

    const hosts = formData?.hosts || [];

    if (editingHost) {
      // 编辑模式
      setFormData({
        ...formData!,
        hosts: hosts.map(h => h.id === editingHost.id
          ? { ...h, domain: hostFormData.domain, ips, enabled: hostFormData.enabled }
          : h
        )
      });
    } else {
      // 新增模式
      const newHost: HostEntry = {
        id: crypto.randomUUID(),
        domain: hostFormData.domain,
        ips,
        enabled: hostFormData.enabled,
      };
      setFormData({
        ...formData!,
        hosts: [...hosts, newHost]
      });
    }

    onHostModalClose();
  };

  const fetchDaemonStatus = async () => {
    try {
      const res = await daemonApi.status();
      setDaemonStatus(res.data.data);
    } catch (error) {
      console.error('获取守护进程状态失败:', error);
    }
  };

  const fetchReleases = async () => {
    try {
      const res = await kernelApi.getReleases();
      setReleases(res.data.data || []);
      if (res.data.data && res.data.data.length > 0) {
        setSelectedVersion(res.data.data[0].tag_name);
      }
    } catch (error) {
      console.error('获取版本列表失败:', error);
    }
  };

  const handleSave = async () => {
    if (formData) {
      try {
        await updateSettings(formData);
        toast.success('设置已保存');
      } catch (error: any) {
        toast.error(error.response?.data?.error || '保存设置失败');
      }
    }
  };

  const handleInstallDaemon = async () => {
    try {
      const res = await daemonApi.install();
      const data = res.data;
      if (data.action === 'exit') {
        toast.success(data.message);
      } else if (data.action === 'manual') {
        toast.info(data.message);
      } else {
        toast.success(data.message || '服务已安装');
      }
      await fetchDaemonStatus();
    } catch (error: any) {
      console.error('安装守护进程服务失败:', error);
      toast.error(error.response?.data?.error || '安装服务失败');
    }
  };

  const handleUninstallDaemon = async () => {
    if (confirm('确定要卸载后台服务吗？卸载后 sbm 将不再开机自启。')) {
      try {
        await daemonApi.uninstall();
        toast.success('服务已卸载');
        await fetchDaemonStatus();
      } catch (error: any) {
        console.error('卸载守护进程服务失败:', error);
        toast.error(error.response?.data?.error || '卸载服务失败');
      }
    }
  };

  const handleRestartDaemon = async () => {
    try {
      await daemonApi.restart();
      toast.success('服务已重启');
      await fetchDaemonStatus();
    } catch (error: any) {
      console.error('重启守护进程服务失败:', error);
      toast.error(error.response?.data?.error || '重启服务失败');
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
    setDownloadProgress({ status: 'preparing', progress: 0, message: '正在准备下载...' });

    try {
      await kernelApi.download(selectedVersion);

      // 开始轮询进度
      pollIntervalRef.current = setInterval(async () => {
        try {
          const res = await kernelApi.getProgress();
          const progress = res.data.data;
          setDownloadProgress(progress);

          if (progress.status === 'completed' || progress.status === 'error') {
            if (pollIntervalRef.current) {
              clearInterval(pollIntervalRef.current);
              pollIntervalRef.current = null;
            }
            setDownloading(false);

            if (progress.status === 'completed') {
              await fetchKernelInfo();
              setTimeout(() => setShowDownloadModal(false), 1500);
            }
          }
        } catch (error) {
          console.error('获取进度失败:', error);
        }
      }, 500);
    } catch (error: any) {
      setDownloading(false);
      setDownloadProgress({
        status: 'error',
        progress: 0,
        message: error.response?.data?.error || '下载失败',
      });
    }
  };

  if (!formData) {
    return <div>加载中...</div>;
  }

  return (
    <div className="space-y-6">
      <div className="flex justify-between items-center">
        <h1 className="text-2xl font-bold text-gray-800 dark:text-white">设置</h1>
        <Button
          color="primary"
          startContent={<Save className="w-4 h-4" />}
          onPress={handleSave}
        >
          保存设置
        </Button>
      </div>

      {/* sing-box 配置 */}
      <Card>
        <CardHeader>
          <Terminal className="w-5 h-5 mr-2" />
          <h2 className="text-lg font-semibold">sing-box 配置</h2>
        </CardHeader>
        <CardBody className="space-y-4">
          {/* 内核状态 */}
          <div className="flex items-center justify-between p-4 rounded-lg bg-default-100">
            <div className="flex items-center gap-3">
              {kernelInfo?.installed ? (
                <>
                  <CheckCircle className="w-5 h-5 text-success" />
                  <div>
                    <p className="font-medium">sing-box 已安装</p>
                    <p className="text-sm text-gray-500">
                      版本: {kernelInfo.version || '未知'} | 平台: {kernelInfo.os}/{kernelInfo.arch}
                    </p>
                  </div>
                </>
              ) : (
                <>
                  <AlertCircle className="w-5 h-5 text-warning" />
                  <div>
                    <p className="font-medium text-warning">sing-box 未安装</p>
                    <p className="text-sm text-gray-500">
                      需要下载 sing-box 内核才能使用代理功能
                    </p>
                  </div>
                </>
              )}
            </div>
            <Button
              color={kernelInfo?.installed ? 'default' : 'primary'}
              variant={kernelInfo?.installed ? 'flat' : 'solid'}
              startContent={<Download className="w-4 h-4" />}
              onPress={openDownloadModal}
            >
              {kernelInfo?.installed ? '更新内核' : '下载内核'}
            </Button>
          </div>

          <Input
            label="配置文件路径"
            placeholder="generated/config.json"
            value={formData.config_path}
            onChange={(e) => setFormData({ ...formData, config_path: e.target.value })}
          />
          <Input
            label="GitHub 代理地址"
            placeholder="如 https://ghproxy.com/"
            description="用于加速 GitHub 下载，留空则直连"
            value={formData.github_proxy || ''}
            onChange={(e) => setFormData({ ...formData, github_proxy: e.target.value })}
          />
        </CardBody>
      </Card>

      {/* 入站配置 */}
      <Card>
        <CardHeader>
          <Download className="w-5 h-5 mr-2" />
          <h2 className="text-lg font-semibold">入站配置</h2>
        </CardHeader>
        <CardBody className="space-y-4">
          <Input
            type="number"
            label="混合代理端口"
            placeholder="2080"
            value={String(formData.mixed_port)}
            onChange={(e) => setFormData({ ...formData, mixed_port: parseInt(e.target.value) || 2080 })}
          />
          <div className="flex items-center justify-between">
            <div>
              <p className="font-medium">TUN 模式</p>
              <p className="text-sm text-gray-500">启用 TUN 模式进行透明代理</p>
            </div>
            <Switch
              isSelected={formData.tun_enabled}
              onValueChange={(enabled) => setFormData({ ...formData, tun_enabled: enabled })}
            />
          </div>
        </CardBody>
      </Card>

      {/* DNS 配置 */}
      <Card>
        <CardHeader>
          <Upload className="w-5 h-5 mr-2" />
          <h2 className="text-lg font-semibold">DNS 配置</h2>
        </CardHeader>
        <CardBody className="space-y-4">
          <Input
            label="代理 DNS"
            placeholder="https://1.1.1.1/dns-query"
            value={formData.proxy_dns}
            onChange={(e) => setFormData({ ...formData, proxy_dns: e.target.value })}
          />
          <Input
            label="直连 DNS"
            placeholder="https://dns.alidns.com/dns-query"
            value={formData.direct_dns}
            onChange={(e) => setFormData({ ...formData, direct_dns: e.target.value })}
          />

          {/* Hosts 映射 */}
          <div className="mt-6 pt-4 border-t border-divider">
            <div className="flex justify-between items-center mb-4">
              <div>
                <h3 className="font-medium">Hosts 映射</h3>
                <p className="text-sm text-gray-500">自定义域名解析（仅对 Sing-Box 生效）</p>
              </div>
              <Button
                color="primary"
                size="sm"
                startContent={<Plus className="w-4 h-4" />}
                onPress={handleAddHost}
              >
                添加
              </Button>
            </div>

            {/* 用户自定义 hosts */}
            {formData.hosts && formData.hosts.length > 0 && (
              <div className="mb-4">
                <p className="text-sm text-gray-500 mb-2">自定义映射</p>
                {formData.hosts.map((host) => (
                  <div
                    key={host.id}
                    className="flex items-center justify-between p-3 bg-default-100 rounded-lg mb-2"
                  >
                    <div className="flex-1">
                      <div className="flex items-center gap-2">
                        <Server className="w-4 h-4 text-gray-500" />
                        <span className="font-medium">{host.domain}</span>
                        {!host.enabled && <Chip size="sm" variant="flat">已禁用</Chip>}
                      </div>
                      <div className="flex gap-1 mt-1 flex-wrap">
                        {host.ips.map((ip, idx) => (
                          <Chip key={idx} size="sm" variant="bordered">{ip}</Chip>
                        ))}
                      </div>
                    </div>
                    <div className="flex items-center gap-1">
                      <Button isIconOnly size="sm" variant="light" onPress={() => handleEditHost(host)}>
                        <Pencil className="w-4 h-4" />
                      </Button>
                      <Button isIconOnly size="sm" variant="light" color="danger" onPress={() => handleDeleteHost(host.id)}>
                        <Trash2 className="w-4 h-4" />
                      </Button>
                      <Switch
                        size="sm"
                        isSelected={host.enabled}
                        onValueChange={(enabled) => handleToggleHost(host.id, enabled)}
                      />
                    </div>
                  </div>
                ))}
              </div>
            )}

            {/* 系统 hosts（只读） */}
            {systemHosts.length > 0 && (
              <div>
                <p className="text-sm text-gray-500 mb-2">
                  系统 hosts <Chip size="sm" variant="flat">只读</Chip>
                </p>
                {systemHosts.map((host) => (
                  <div
                    key={host.id}
                    className="flex items-center justify-between p-3 bg-default-100 rounded-lg mb-2"
                  >
                    <div className="flex-1">
                      <div className="flex items-center gap-2">
                        <Server className="w-4 h-4 text-gray-500" />
                        <span className="font-medium">{host.domain}</span>
                        <Chip size="sm" color="secondary" variant="flat">系统</Chip>
                      </div>
                      <div className="flex gap-1 mt-1 flex-wrap">
                        {host.ips.map((ip, idx) => (
                          <Chip key={idx} size="sm" variant="bordered">{ip}</Chip>
                        ))}
                      </div>
                    </div>
                  </div>
                ))}
              </div>
            )}

            {/* 空状态 */}
            {(!formData.hosts || formData.hosts.length === 0) && systemHosts.length === 0 && (
              <p className="text-gray-500 text-center py-4">暂无 hosts 映射</p>
            )}
          </div>
        </CardBody>
      </Card>

      {/* 控制面板配置 */}
      <Card>
        <CardHeader>
          <h2 className="text-lg font-semibold">控制面板</h2>
        </CardHeader>
        <CardBody className="space-y-4">
          <Input
            type="number"
            label="Web 管理端口"
            placeholder="9090"
            disabled
            value={String(formData.web_port)}
            onChange={(e) => setFormData({ ...formData, web_port: parseInt(e.target.value) || 9090 })}
          />
          <Input
            type="number"
            label="Clash API 端口"
            placeholder="9091"
            value={String(formData.clash_api_port)}
            onChange={(e) => setFormData({ ...formData, clash_api_port: parseInt(e.target.value) || 9091 })}
          />
          <Input
            label="漏网规则出站"
            placeholder="Proxy"
            value={formData.final_outbound}
            onChange={(e) => setFormData({ ...formData, final_outbound: e.target.value })}
          />
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

      {/* 后台服务管理 */}
      {daemonStatus?.supported && (
        <Card>
          <CardHeader className="flex justify-between items-center">
            <h2 className="text-lg font-semibold">后台服务</h2>
            {daemonStatus && (
              <Chip
                color={daemonStatus.installed ? 'success' : 'default'}
                variant="flat"
                size="sm"
              >
                {daemonStatus.installed ? '已安装' : '未安装'}
              </Chip>
            )}
          </CardHeader>
          <CardBody>
            <p className="text-sm text-gray-500 mb-4">
              安装后台服务可让 sbm 管理程序在后台运行，关闭终端后仍可访问 Web 管理界面。服务会开机自启并在崩溃后自动重启。
            </p>
            <div className="flex gap-2">
              {daemonStatus?.installed ? (
                <>
                  <Button
                    color="primary"
                    variant="flat"
                    onPress={handleRestartDaemon}
                  >
                    重启服务
                  </Button>
                  <Button
                    color="danger"
                    variant="flat"
                    onPress={handleUninstallDaemon}
                  >
                    卸载服务
                  </Button>
                </>
              ) : (
                <Button
                  color="primary"
                  onPress={handleInstallDaemon}
                >
                  安装后台服务
                </Button>
              )}
            </div>
          </CardBody>
        </Card>
      )}

      {/* 下载内核弹窗 */}
      <Modal isOpen={showDownloadModal} onClose={() => !downloading && setShowDownloadModal(false)}>
        <ModalContent>
          <ModalHeader>下载 sing-box 内核</ModalHeader>
          <ModalBody>
            <Select
              label="选择版本"
              placeholder="选择要下载的版本"
              selectedKeys={selectedVersion ? [selectedVersion] : []}
              onSelectionChange={(keys) => {
                const selected = Array.from(keys)[0] as string;
                if (selected) setSelectedVersion(selected);
              }}
              isDisabled={downloading}
            >
              {releases.map((release) => (
                <SelectItem key={release.tag_name} textValue={release.tag_name}>
                  {release.tag_name} {release.name && `- ${release.name}`}
                </SelectItem>
              ))}
            </Select>

            {kernelInfo && (
              <p className="text-sm text-gray-500">
                将下载适用于 {kernelInfo.os}/{kernelInfo.arch} 的版本
              </p>
            )}

            {downloadProgress && (
              <div className="mt-4 space-y-2">
                <Progress
                  value={downloadProgress.progress}
                  color={downloadProgress.status === 'error' ? 'danger' : downloadProgress.status === 'completed' ? 'success' : 'primary'}
                  showValueLabel
                />
                <p className={`text-sm ${downloadProgress.status === 'error' ? 'text-danger' : downloadProgress.status === 'completed' ? 'text-success' : 'text-gray-600'}`}>
                  {downloadProgress.message}
                </p>
              </div>
            )}
          </ModalBody>
          <ModalFooter>
            <Button
              variant="flat"
              onPress={() => setShowDownloadModal(false)}
              isDisabled={downloading}
            >
              取消
            </Button>
            <Button
              color="primary"
              onPress={startDownload}
              isLoading={downloading}
              isDisabled={!selectedVersion || downloading}
            >
              开始下载
            </Button>
          </ModalFooter>
        </ModalContent>
      </Modal>

      {/* Hosts 编辑弹窗 */}
      <Modal isOpen={isHostModalOpen} onClose={onHostModalClose}>
        <ModalContent>
          <ModalHeader>{editingHost ? '编辑 Host' : '添加 Host'}</ModalHeader>
          <ModalBody className="gap-4">
            <Input
              label="域名"
              placeholder="例如：example.com"
              value={hostFormData.domain}
              onChange={(e) => setHostFormData({ ...hostFormData, domain: e.target.value })}
            />
            <Textarea
              label="IP 地址"
              placeholder={"每行一个 IP 地址\n例如：\n192.168.1.1\n192.168.1.2"}
              value={ipsText}
              onChange={(e) => setIpsText(e.target.value)}
              minRows={3}
            />
            <div className="flex items-center justify-between">
              <span>启用</span>
              <Switch
                isSelected={hostFormData.enabled}
                onValueChange={(enabled) => setHostFormData({ ...hostFormData, enabled })}
              />
            </div>
          </ModalBody>
          <ModalFooter>
            <Button variant="flat" onPress={onHostModalClose}>取消</Button>
            <Button
              color="primary"
              onPress={handleSubmitHost}
              isDisabled={!hostFormData.domain || !ipsText.trim()}
            >
              {editingHost ? '保存' : '添加'}
            </Button>
          </ModalFooter>
        </ModalContent>
      </Modal>
    </div>
  );
}
