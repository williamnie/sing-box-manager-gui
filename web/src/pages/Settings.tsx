import { errorMessage } from '../api';
import { useEffect, useState, useRef } from 'react';
import {
  Input,
  Button,
  Switch,
  Modal,
  ModalContent,
  ModalHeader,
  ModalBody,
  ModalFooter,
  Progress,
  Textarea,
  useDisclosure,
} from '@nextui-org/react';
import { AppSelect, SelectItem } from '../components/AppSelect';
import {
  Save,
  Download,
  Terminal,
  CheckCircle,
  AlertCircle,
  Plus,
  Pencil,
  Trash2,
  Server,
  Eye,
  EyeOff,
  Copy,
  RefreshCw,
  Wifi,
  Shield,
  Layers,
  Globe,
  Radio,
  Cpu,
} from 'lucide-react';
import { useStore } from '../store';
import type { Settings as SettingsType, HostEntry } from '../store';
import { daemonApi, kernelApi, settingsApi } from '../api';
import { toast } from '../components/Toast';
import ConfirmModal from '../components/ConfirmModal';
import PanelPreferences from '../components/PanelPreferences';
import { Link } from 'react-router-dom';

function randomSecret() {
  return Array.from(crypto.getRandomValues(new Uint8Array(24)), (byte) =>
    byte.toString(16).padStart(2, '0')
  ).join('');
}

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

  // 密钥显示状态与卸载确认
  const [showSecret, setShowSecret] = useState(false);
  const [uninstallConfirm, setUninstallConfirm] = useState(false);
  const [saving, setSaving] = useState(false);

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
      toast.error(errorMessage(error, '请求失败，请检查管理服务'));
    }
  };

  const fetchSystemHosts = async () => {
    try {
      const res = await settingsApi.getSystemHosts();
      setSystemHosts(res.data.data || []);
    } catch (error) {
      toast.error(errorMessage(error, '请求失败，请检查管理服务'));
    }
  };

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
      hosts: formData.hosts.filter((h) => h.id !== id),
    });
  };

  const handleToggleHost = (id: string, enabled: boolean) => {
    if (!formData?.hosts) return;
    setFormData({
      ...formData,
      hosts: formData.hosts.map((h) => (h.id === id ? { ...h, enabled } : h)),
    });
  };

  const handleSubmitHost = () => {
    const ips = ipsText
      .split('\n')
      .map((ip) => ip.trim())
      .filter((ip) => ip);

    const ipv4Regex = /^(\d{1,3}\.){3}\d{1,3}$/;
    const ipv6Regex = /^([a-fA-F0-9:]+)$/;
    const invalidIps = ips.filter((ip) => !ipv4Regex.test(ip) && !ipv6Regex.test(ip));
    if (invalidIps.length > 0) {
      toast.error(`无效的 IP 地址: ${invalidIps.join(', ')}`);
      return;
    }

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
      setFormData({
        ...formData!,
        hosts: hosts.map((h) =>
          h.id === editingHost.id
            ? { ...h, domain: hostFormData.domain, ips, enabled: hostFormData.enabled }
            : h
        ),
      });
    } else {
      const newHost: HostEntry = {
        id: crypto.randomUUID(),
        domain: hostFormData.domain,
        ips,
        enabled: hostFormData.enabled,
      };
      setFormData({
        ...formData!,
        hosts: [...hosts, newHost],
      });
    }

    onHostModalClose();
  };

  const fetchDaemonStatus = async () => {
    try {
      const res = await daemonApi.status();
      setDaemonStatus(res.data.data);
    } catch (error) {
      toast.error(errorMessage(error, '请求失败，请检查管理服务'));
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
      toast.error(errorMessage(error, '请求失败，请检查管理服务'));
    }
  };

  const handleCopySecret = () => {
    if (!formData?.clash_api_secret) return;
    const text = formData.clash_api_secret;

    if (navigator.clipboard && window.isSecureContext) {
      navigator.clipboard
        .writeText(text)
        .then(() => toast.success('API 密钥已复制到剪贴板'))
        .catch(() => fallbackCopy(text));
    } else {
      fallbackCopy(text);
    }
  };

  const fallbackCopy = (text: string) => {
    const textarea = document.createElement('textarea');
    textarea.value = text;
    textarea.style.position = 'fixed';
    textarea.style.left = '-9999px';
    textarea.style.top = '-9999px';
    document.body.appendChild(textarea);
    textarea.focus();
    textarea.select();

    try {
      const success = document.execCommand('copy');
      if (success) {
        toast.success('API 密钥已复制到剪贴板');
      } else {
        toast.error('复制失败');
      }
    } catch {
      toast.error('复制失败');
    } finally {
      document.body.removeChild(textarea);
    }
  };

  const handleGenerateSecret = () => {
    setFormData({ ...formData!, clash_api_secret: randomSecret() });
    toast.success('已生成新密钥，请保存设置');
  };

  const handleSave = async () => {
    if (formData) {
      setSaving(true);
      try {
        await updateSettings(formData);
        toast.success('设置已持久化保存');
      } catch (error: any) {
        toast.error(error.response?.data?.error || '保存设置失败');
      } finally {
        setSaving(false);
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
        toast.success(data.message || '系统守护进程已成功安装');
      }
      await fetchDaemonStatus();
    } catch (error: any) {
      toast.error(error.response?.data?.error || '安装服务失败');
    }
  };

  const handleUninstallDaemon = async () => {
    try {
      await daemonApi.uninstall();
      toast.success('系统守护进程已卸载');
      await fetchDaemonStatus();
      setUninstallConfirm(false);
    } catch (error: any) {
      toast.error(error.response?.data?.error || '卸载服务失败');
    }
  };

  const handleRestartDaemon = async () => {
    try {
      await daemonApi.restart();
      toast.success('后台服务正在重启...');
      await fetchDaemonStatus();
    } catch (error: any) {
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
    setDownloadProgress({ status: 'preparing', progress: 0, message: '正在连接 GitHub Releases 校验二进制签名...' });

    try {
      await kernelApi.download(selectedVersion);
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
          toast.error(errorMessage(error, '进度请求中断，请检查服务'));
        }
      }, 500);
    } catch (error: any) {
      setDownloading(false);
      setDownloadProgress({
        status: 'error',
        progress: 0,
        message: error.response?.data?.error || '下载初始化失败',
      });
    }
  };

  if (!formData) {
    return (
      <div className="flex items-center justify-center p-12 text-zinc-500 font-mono text-xs">
        <div className="flex items-center gap-2">
          <div className="w-2 h-2 rounded-full bg-interface-orange animate-ping" />
          <span>INITIALIZING_SETTINGS_STORE...</span>
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      {/* 头部面板 */}
      <div className="flex flex-col md:flex-row md:items-center justify-between gap-4 pb-4 border-b border-zinc-200/80 dark:border-white/[0.08]">
        <div>
          <div className="flex items-center gap-2 mb-1.5">
            <span className="font-mono text-[10px] uppercase tracking-wider text-[#ff5722] bg-orange-500/10 px-2 py-0.5 rounded-[2px] border border-orange-500/20 font-semibold">
              05 // SYSTEM_CONFIG
            </span>
            <span className="font-mono text-xs text-zinc-500">
              HOST_OS: <span className="text-zinc-700 dark:text-zinc-300 font-semibold">{kernelInfo?.os || 'linux'}</span> /{' '}
              <span className="text-zinc-700 dark:text-zinc-300 font-semibold">{kernelInfo?.arch || 'unknown'}</span>
            </span>
          </div>
          <h1 className="text-xl md:text-2xl font-semibold text-zinc-900 dark:text-white tracking-tight font-sans">
            全局运行环境与系统设置
          </h1>
          <p className="text-xs text-zinc-500 dark:text-zinc-400 mt-1 max-w-2xl leading-relaxed">
            当前部署角色：
            <span className="font-mono text-zinc-800 dark:text-zinc-200 font-medium ml-1">
              {formData.deployment_role === 'gateway'
                ? formData.gateway?.access_mode === 'dns'
                  ? 'DNS 分流旁路 (FakeIP TProxy)'
                  : '完整网关接管 (Gateway TUN)'
                : '单机代理 (Standalone)'}
            </span>
            。接入模式与设备策略请在{' '}
            <Link to="/gateway" className="text-interface-orange hover:underline font-mono">
              [部署与设备]
            </Link>{' '}
            管理。
          </p>
        </div>

        <Button
          color="primary"
          size="sm"
          className="font-mono text-xs font-semibold rounded-[3px] bg-interface-orange text-white shadow-geek-glow px-5"
          startContent={<Save className="w-3.5 h-3.5" />}
          isLoading={saving}
          onPress={handleSave}
        >
          保存所有设置
        </Button>
      </div>

      <PanelPreferences />

      {/* 快捷导航定位 */}
      <div className="flex flex-wrap items-center gap-2 p-2 rounded-[3px] border border-white/[0.06] bg-default-100/40 dark:bg-black/30 text-xs font-mono">
        <span className="text-[11px] text-zinc-500">// 快速跳转:</span>
        <button
          type="button"
          onClick={() => document.getElementById('section-kernel')?.scrollIntoView({ behavior: 'smooth' })}
          className="px-2.5 py-1 rounded-[2px] bg-white/[0.04] hover:bg-white/[0.08] text-zinc-300 hover:text-white transition-colors cursor-pointer"
        >
          核心引擎
        </button>
        <button
          type="button"
          onClick={() => document.getElementById('section-network')?.scrollIntoView({ behavior: 'smooth' })}
          className="px-2.5 py-1 rounded-[2px] bg-white/[0.04] hover:bg-white/[0.08] text-zinc-300 hover:text-white transition-colors cursor-pointer"
        >
          端口与入站
        </button>
        <button
          type="button"
          onClick={() => document.getElementById('section-hosts')?.scrollIntoView({ behavior: 'smooth' })}
          className="px-2.5 py-1 rounded-[2px] bg-white/[0.04] hover:bg-white/[0.08] text-zinc-300 hover:text-white transition-colors cursor-pointer"
        >
          DNS 与 Hosts
        </button>
        {daemonStatus?.supported && (
          <button
            type="button"
            onClick={() => document.getElementById('section-daemon')?.scrollIntoView({ behavior: 'smooth' })}
            className="px-2.5 py-1 rounded-[2px] bg-white/[0.04] hover:bg-white/[0.08] text-zinc-300 hover:text-white transition-colors cursor-pointer"
          >
            后台常驻服务
          </button>
        )}
      </div>

      {/* 1. sing-box 内核状态与下载 */}
      <div id="section-kernel" className="rounded-[4px] border border-white/[0.08] bg-[#0a0a0e] overflow-hidden scroll-mt-16">
        <div className="flex items-center justify-between px-4 py-2.5 bg-[#0d0e12] border-b border-white/[0.08]">
          <div className="flex items-center gap-2">
            <Terminal className="w-4 h-4 text-interface-orange" />
            <h2 className="font-mono text-xs font-semibold text-white tracking-wide uppercase">
              SING_BOX_RUNTIME // 核心引擎
            </h2>
          </div>
          <span className="font-mono text-[10px] text-zinc-500">
            BIN_TARGET: {kernelInfo?.path || 'bin/sing-box'}
          </span>
        </div>

        <div className="p-4 space-y-4">
          <div className="flex flex-col sm:flex-row sm:items-center justify-between p-3.5 rounded-[3px] bg-[#0d0e12] border border-white/[0.08] gap-4">
            <div className="flex items-center gap-3">
              {kernelInfo?.installed ? (
                <>
                  <div className="w-8 h-8 rounded-[3px] bg-emerald-500/10 border border-emerald-500/30 flex items-center justify-center text-emerald-400 shrink-0">
                    <CheckCircle className="w-4 h-4" />
                  </div>
                  <div>
                    <div className="flex items-center gap-2">
                      <span className="font-mono text-xs font-semibold text-white">sing-box 二进制就绪</span>
                      <span className="font-mono text-[10px] px-1.5 py-0.5 rounded-[2px] bg-emerald-500/20 text-emerald-300 border border-emerald-500/30">
                        {kernelInfo.version || 'v1.11.x'}
                      </span>
                    </div>
                    <p className="font-mono text-[11px] text-zinc-500 mt-0.5">
                      架构: {kernelInfo.os}/{kernelInfo.arch} · 位于: {kernelInfo.path || '默认搜索路径'}
                    </p>
                  </div>
                </>
              ) : (
                <>
                  <div className="w-8 h-8 rounded-[3px] bg-amber-500/10 border border-amber-500/30 flex items-center justify-center text-amber-800 dark:text-amber-400 shrink-0">
                    <AlertCircle className="w-4 h-4" />
                  </div>
                  <div>
                    <div className="flex items-center gap-2">
                      <span className="font-mono text-xs font-semibold text-amber-800 dark:text-amber-400">内核二进制缺失</span>
                      <span className="font-mono text-[10px] px-1.5 py-0.5 rounded-[2px] bg-amber-500/20 text-amber-800 dark:text-amber-300 border border-amber-500/30">
                        NOT_FOUND
                      </span>
                    </div>
                    <p className="font-mono text-[11px] text-zinc-500 mt-0.5">
                      需要下载 sing-box 官方内核方可启动代理与网关功能
                    </p>
                  </div>
                </>
              )}
            </div>

            <Button
              size="sm"
              variant={kernelInfo?.installed ? 'flat' : 'solid'}
              color={kernelInfo?.installed ? 'default' : 'primary'}
              className={`font-mono text-xs rounded-[3px] ${
                kernelInfo?.installed
                  ? 'bg-white/[0.05] border border-white/[0.08] text-zinc-300 hover:text-white hover:bg-white/[0.08]'
                  : 'bg-interface-orange text-white shadow-geek-glow'
              }`}
              startContent={<Download className="w-3.5 h-3.5" />}
              onPress={openDownloadModal}
            >
              {kernelInfo?.installed ? '更新 / 切换内核版本' : '下载官方内核'}
            </Button>
          </div>

          <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
            <Input
              label="配置文件生成路径 (CONFIG_PATH)"
              placeholder="generated/config.json"
              value={formData.config_path}
              onChange={(e) => setFormData({ ...formData, config_path: e.target.value })}
              classNames={{
                label: 'font-mono text-xs text-zinc-400',
                input: 'font-mono text-xs text-zinc-200',
                inputWrapper: 'bg-[#060608] border border-white/[0.08] rounded-[2px] hover:border-white/[0.16] focus-within:!border-interface-orange/60',
              }}
            />
            <Input
              label="GitHub 下载加速镜像 (GH_PROXY)"
              placeholder="例如 https://ghproxy.net/"
              description="用于国内网络加速从 GitHub 抓取内核二进制与规则包，留空直连"
              value={formData.github_proxy || ''}
              onChange={(e) => setFormData({ ...formData, github_proxy: e.target.value })}
              classNames={{
                label: 'font-mono text-xs text-zinc-400',
                input: 'font-mono text-xs text-zinc-200',
                description: 'font-mono text-[10px] text-zinc-500',
                inputWrapper: 'bg-[#060608] border border-white/[0.08] rounded-[2px] hover:border-white/[0.16] focus-within:!border-interface-orange/60',
              }}
            />
          </div>
        </div>
      </div>

      {/* 2. 入站与网络代理配置 */}
      <div id="section-network" className="rounded-[4px] border border-white/[0.08] bg-[#0a0a0e] overflow-hidden scroll-mt-16">
        <div className="flex items-center justify-between px-4 py-2.5 bg-[#0d0e12] border-b border-white/[0.08]">
          <div className="flex items-center gap-2">
            <Radio className="w-4 h-4 text-interface-orange" />
            <h2 className="font-mono text-xs font-semibold text-white tracking-wide uppercase">
              INBOUND_NETWORK // 端口与入站
            </h2>
          </div>
          <span className="font-mono text-[10px] text-zinc-500">
            ROLE: {formData.deployment_role ? formData.deployment_role.toUpperCase() : 'STANDALONE'}
          </span>
        </div>

        <div className="p-4 space-y-4">
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            <Input
              type="number"
              label="本地混合代理端口 (MIXED_PORT)"
              placeholder="2080"
              description="同时支持 HTTP/HTTPS 与 SOCKS5 代理协议"
              value={String(formData.mixed_port)}
              onChange={(e) => setFormData({ ...formData, mixed_port: parseInt(e.target.value) || 2080 })}
              classNames={{
                label: 'font-mono text-xs text-zinc-400',
                input: 'font-mono text-xs text-zinc-200',
                description: 'font-mono text-[10px] text-zinc-500',
                inputWrapper: 'bg-[#060608] border border-white/[0.08] rounded-[2px] hover:border-white/[0.16] focus-within:!border-interface-orange/60',
              }}
            />

            <div className="p-3 bg-[#0d0e12] border border-white/[0.08] rounded-[3px] flex items-center justify-between">
              <div>
                <p className="font-mono text-xs font-semibold text-zinc-200">TUN 虚拟网卡模式</p>
                <p className="font-mono text-[10px] text-zinc-500 mt-0.5 leading-relaxed">
                  {formData.deployment_role === 'gateway'
                    ? formData.gateway?.access_mode === 'dns'
                      ? 'DNS 旁路模式使用限定 FakeIP 的 TProxy，不在此处激活单机 TUN'
                      : '完整网关接管已激活，由系统网络接管流水线统一调度'
                    : '创建 tun0 虚拟网卡接管操作系统全局 IP 流量'}
                </p>
              </div>
              <Switch
                size="sm"
                isDisabled={formData.deployment_role === 'gateway'}
                isSelected={formData.deployment_role === 'gateway' ? formData.gateway?.access_mode !== 'dns' : formData.tun_enabled}
                onValueChange={(enabled) => setFormData({ ...formData, tun_enabled: enabled })}
                classNames={{
                  wrapper: 'group-data-[selected=true]:bg-interface-orange',
                }}
              />
            </div>
          </div>

          <div className="p-3 bg-[#0d0e12] border border-white/[0.08] rounded-[3px] flex items-center justify-between">
            <div className="flex items-center gap-3">
              <div className="p-2 rounded-[2px] bg-interface-orange/10 border border-interface-orange/20 text-interface-orange">
                <Wifi className="w-4 h-4" />
              </div>
              <div>
                <p className="font-mono text-xs font-semibold text-zinc-200">允许局域网外部设备访问 (ALLOW_LAN)</p>
                <p className="font-mono text-[10px] text-zinc-500 mt-0.5">
                  开启后 mixed 端口将绑定 0.0.0.0，同一局域网设备可将本机设为 HTTP/SOCKS 代理上网
                </p>
              </div>
            </div>
            <Switch
              size="sm"
              isSelected={formData.allow_lan}
              onValueChange={(enabled) => {
                const updates: Partial<typeof formData> = { allow_lan: enabled };
                if (enabled && !formData.clash_api_secret) {
                  updates.clash_api_secret = randomSecret();
                }
                setFormData({ ...formData, ...updates });
              }}
              classNames={{
                wrapper: 'group-data-[selected=true]:bg-interface-orange',
              }}
            />
          </div>

          {/* Clash API 密钥保险箱 */}
          {formData.clash_api_port > 0 && (
            <div className="p-3.5 rounded-[3px] bg-[#0c0d12] border border-amber-500/30 space-y-2">
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-2">
                  <Shield className="w-3.5 h-3.5 text-amber-800 dark:text-amber-400" />
                  <span className="font-mono text-xs font-semibold text-amber-800 dark:text-amber-400">
                    内核控制接口密钥
                  </span>
                </div>
                <span className="font-mono text-[10px] px-1.5 py-0.2 rounded-[2px] bg-amber-500/10 text-amber-800 dark:text-amber-400 border border-amber-500/20">
                  CRITICAL_TOKEN
                </span>
              </div>
              <p className="font-mono text-[11px] text-zinc-400 leading-relaxed">
                内置面板由管理后端连接本机内核，无需在代理页填写密钥。应用新配置后控制接口仅监听本机，与 mixed 的局域网访问开关独立。
              </p>
              <div className="flex items-center gap-2 pt-1">
                <Input
                  type={showSecret ? 'text' : 'password'}
                  value={formData.clash_api_secret || ''}
                  onChange={(e) => setFormData({ ...formData, clash_api_secret: e.target.value })}
                  placeholder="保存设置后将自动生成"
                  size="sm"
                  className="flex-1"
                  classNames={{
                    input: 'font-mono text-xs text-zinc-200',
                    inputWrapper: 'bg-[#060608] border border-white/[0.08] rounded-[2px]',
                  }}
                  endContent={
                    <button
                      type="button"
                      onClick={() => setShowSecret(!showSecret)}
                      className="text-zinc-500 hover:text-zinc-300"
                    >
                      {showSecret ? <EyeOff className="w-3.5 h-3.5" /> : <Eye className="w-3.5 h-3.5" />}
                    </button>
                  }
                />
                <Button
                  isIconOnly
                  size="sm"
                  variant="flat"
                  className="bg-white/[0.06] border border-white/[0.08] text-zinc-300 hover:text-white rounded-[2px]"
                  onPress={handleCopySecret}
                  isDisabled={!formData.clash_api_secret}
                  title="复制密钥"
                >
                  <Copy className="w-3.5 h-3.5" />
                </Button>
                <Button
                  isIconOnly
                  size="sm"
                  variant="flat"
                  className="bg-white/[0.06] border border-white/[0.08] text-zinc-300 hover:text-white rounded-[2px]"
                  onPress={handleGenerateSecret}
                  title="重新随机生成"
                >
                  <RefreshCw className="w-3.5 h-3.5" />
                </Button>
              </div>
            </div>
          )}
        </div>
      </div>

      {/* 3. DNS 与 Hosts 映射矩阵 */}
      <div id="section-hosts" className="rounded-[4px] border border-white/[0.08] bg-[#0a0a0e] overflow-hidden scroll-mt-16">
        <div className="flex items-center justify-between px-4 py-2.5 bg-[#0d0e12] border-b border-white/[0.08]">
          <div className="flex items-center gap-2">
            <Globe className="w-4 h-4 text-interface-orange" />
            <h2 className="font-mono text-xs font-semibold text-white tracking-wide uppercase">
              DNS_ROUTER & HOSTS // 域名解析拓扑
            </h2>
          </div>
          <span className="font-mono text-[10px] text-zinc-500">
            TOTAL_HOSTS: {(formData.hosts?.length || 0) + systemHosts.length}
          </span>
        </div>

        <div className="p-4 space-y-4">
          <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
            <Input
              label="远程代理 DNS 上游 (DoH)"
              placeholder="https://1.1.1.1/dns-query"
              value={formData.proxy_dns}
              onChange={(e) => setFormData({ ...formData, proxy_dns: e.target.value })}
              classNames={{
                label: 'font-mono text-xs text-zinc-400',
                input: 'font-mono text-xs text-zinc-200',
                inputWrapper: 'bg-[#060608] border border-white/[0.08] rounded-[2px] hover:border-white/[0.16] focus-within:!border-interface-orange/60',
              }}
            />
            <Input
              label="国内直连 DNS 上游 (DoH / UDP)"
              placeholder="https://dns.alidns.com/dns-query"
              value={formData.direct_dns}
              onChange={(e) => setFormData({ ...formData, direct_dns: e.target.value })}
              classNames={{
                label: 'font-mono text-xs text-zinc-400',
                input: 'font-mono text-xs text-zinc-200',
                inputWrapper: 'bg-[#060608] border border-white/[0.08] rounded-[2px] hover:border-white/[0.16] focus-within:!border-interface-orange/60',
              }}
            />
          </div>

          {/* Hosts 映射列表与操作 */}
          <div className="pt-3 border-t border-white/[0.06] space-y-3">
            <div className="flex items-center justify-between">
              <div>
                <h3 className="font-mono text-xs font-semibold text-zinc-200">
                  HOSTS_OVERRIDE // 静态域名映射
                </h3>
                <p className="font-mono text-[10px] text-zinc-500 mt-0.5">
                  优先级最高，仅直接作用于 sing-box 内核内联 DNS
                </p>
              </div>
              <Button
                size="sm"
                color="primary"
                className="font-mono text-xs rounded-[2px] bg-interface-orange text-white shadow-geek-glow"
                startContent={<Plus className="w-3.5 h-3.5" />}
                onPress={handleAddHost}
              >
                添加自定义映射
              </Button>
            </div>

            {/* 用户自定义 Hosts */}
            {formData.hosts && formData.hosts.length > 0 && (
              <div className="space-y-2">
                <span className="font-mono text-[10px] text-zinc-500 uppercase tracking-wider">
                  CUSTOM_RULES ({formData.hosts.length})
                </span>
                <div className="space-y-1.5">
                  {formData.hosts.map((host) => (
                    <div
                      key={host.id}
                      className="flex items-center justify-between p-2.5 bg-[#0d0e12] border border-white/[0.06] rounded-[3px] hover:border-white/[0.12] transition-colors"
                    >
                      <div className="flex-1 min-w-0 pr-3">
                        <div className="flex items-center gap-2">
                          <Server className="w-3.5 h-3.5 text-interface-orange shrink-0" />
                          <span className="font-mono text-xs font-semibold text-zinc-200 truncate">
                            {host.domain}
                          </span>
                          {!host.enabled && (
                            <span className="font-mono text-[9px] px-1 py-0.2 rounded-[2px] bg-zinc-800 text-zinc-400">
                              DISABLED
                            </span>
                          )}
                        </div>
                        <div className="flex gap-1.5 mt-1 flex-wrap">
                          {host.ips.map((ip, idx) => (
                            <span
                              key={idx}
                              className="font-mono text-[10px] px-1.5 py-0.2 bg-[#060608] border border-white/[0.08] text-zinc-300 rounded-[2px]"
                            >
                              {ip}
                            </span>
                          ))}
                        </div>
                      </div>

                      <div className="flex items-center gap-1.5 shrink-0">
                        <Button
                          isIconOnly
                          size="sm"
                          variant="light"
                          className="text-zinc-400 hover:text-white"
                          onPress={() => handleEditHost(host)}
                        >
                          <Pencil className="w-3.5 h-3.5" />
                        </Button>
                        <Button
                          isIconOnly
                          size="sm"
                          variant="light"
                          className="text-zinc-400 hover:text-red-400"
                          onPress={() => handleDeleteHost(host.id)}
                        >
                          <Trash2 className="w-3.5 h-3.5" />
                        </Button>
                        <Switch
                          size="sm"
                          isSelected={host.enabled}
                          onValueChange={(enabled) => handleToggleHost(host.id, enabled)}
                          classNames={{
                            wrapper: 'group-data-[selected=true]:bg-interface-orange',
                          }}
                        />
                      </div>
                    </div>
                  ))}
                </div>
              </div>
            )}

            {/* 系统 Host 只读 */}
            {systemHosts.length > 0 && (
              <div className="space-y-2 pt-2">
                <div className="flex items-center gap-2">
                  <span className="font-mono text-[10px] text-zinc-500 uppercase tracking-wider">
                    SYSTEM_DETECTED (/etc/hosts)
                  </span>
                  <span className="font-mono text-[9px] px-1.5 py-0.2 rounded-[2px] bg-zinc-800 text-zinc-400 border border-white/[0.06]">
                    READ_ONLY
                  </span>
                </div>
                <div className="space-y-1.5">
                  {systemHosts.map((host) => (
                    <div
                      key={host.id}
                      className="flex items-center justify-between p-2.5 bg-[#08080b] border border-white/[0.04] rounded-[3px] opacity-75"
                    >
                      <div className="flex-1 min-w-0">
                        <div className="flex items-center gap-2">
                          <Server className="w-3.5 h-3.5 text-zinc-500 shrink-0" />
                          <span className="font-mono text-xs text-zinc-300 truncate">
                            {host.domain}
                          </span>
                        </div>
                        <div className="flex gap-1.5 mt-1 flex-wrap">
                          {host.ips.map((ip, idx) => (
                            <span
                              key={idx}
                              className="font-mono text-[10px] px-1.5 py-0.2 bg-[#060608] border border-white/[0.06] text-zinc-400 rounded-[2px]"
                            >
                              {ip}
                            </span>
                          ))}
                        </div>
                      </div>
                    </div>
                  ))}
                </div>
              </div>
            )}

            {(!formData.hosts || formData.hosts.length === 0) && systemHosts.length === 0 && (
              <p className="font-mono text-xs text-zinc-600 text-center py-4">
                // 暂无自定义 Hosts 映射规则
              </p>
            )}
          </div>
        </div>
      </div>

      {/* 4. 控制面板与自动化流水线 */}
      <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
        {/* 控制面板端口 */}
        <div className="rounded-[4px] border border-white/[0.08] bg-[#0a0a0e] overflow-hidden">
          <div className="flex items-center justify-between px-4 py-2.5 bg-[#0d0e12] border-b border-white/[0.08]">
            <div className="flex items-center gap-2">
              <Layers className="w-4 h-4 text-interface-orange" />
              <h2 className="font-mono text-xs font-semibold text-white tracking-wide uppercase">
                CONTROL_API // 管理端口
              </h2>
            </div>
          </div>
          <div className="p-4 space-y-3">
            <p className="font-mono text-[10px] text-zinc-500 leading-relaxed">
              管理 API 默认仅监听 127.0.0.1 本地回环。远程访问推荐使用 SSH 隧道或内置 TLS 反向代理。
            </p>
            <div className="grid grid-cols-2 gap-3">
              <Input
                type="number"
                label="Web 管理端口"
                placeholder="9090"
                disabled
                value={String(formData.web_port)}
                classNames={{
                  label: 'font-mono text-xs text-zinc-500',
                  input: 'font-mono text-xs text-zinc-400',
                  inputWrapper: 'bg-[#060608] border border-white/[0.04] rounded-[2px] opacity-75',
                }}
              />
              <Input
                type="number"
                label="内核控制 API 端口"
                placeholder="9091"
                value={String(formData.clash_api_port)}
                onChange={(e) => setFormData({ ...formData, clash_api_port: Number(e.target.value) })}
                classNames={{
                  label: 'font-mono text-xs text-zinc-400',
                  input: 'font-mono text-xs text-zinc-200',
                  inputWrapper: 'bg-[#060608] border border-white/[0.08] rounded-[2px]',
                }}
              />
            </div>
            <Input
              label="未命中分流默认出站 (FINAL_OUTBOUND)"
              placeholder="Proxy"
              value={formData.final_outbound}
              onChange={(e) => setFormData({ ...formData, final_outbound: e.target.value })}
              classNames={{
                label: 'font-mono text-xs text-zinc-400',
                input: 'font-mono text-xs text-zinc-200',
                inputWrapper: 'bg-[#060608] border border-white/[0.08] rounded-[2px]',
              }}
            />
            <label className="block space-y-2 text-xs text-zinc-600 dark:text-zinc-400">
              <span>内核日志输出级别</span>
              <select value={formData.log_level || 'info'} onChange={e => setFormData({ ...formData, log_level: e.target.value as SettingsType['log_level'] })} className="w-full rounded border border-zinc-200 bg-white px-3 py-2 text-zinc-900 dark:border-white/10 dark:bg-zinc-950 dark:text-zinc-100">
                {['trace', 'debug', 'info', 'warn', 'error', 'fatal', 'panic'].map(level => <option key={level} value={level}>{level}</option>)}
              </select>
              <span className="block leading-relaxed">随配置保存并按自动应用设置生效；对运行中的内核应用会重启服务。日志页的级别过滤只改变显示。端口设为 0 会停用代理和连接控制。</span>
            </label>
          </div>
        </div>

        {/* 自动化配置 */}
        <div className="rounded-[4px] border border-white/[0.08] bg-[#0a0a0e] overflow-hidden">
          <div className="flex items-center justify-between px-4 py-2.5 bg-[#0d0e12] border-b border-white/[0.08]">
            <div className="flex items-center gap-2">
              <Cpu className="w-4 h-4 text-interface-orange" />
              <h2 className="font-mono text-xs font-semibold text-white tracking-wide uppercase">
                AUTOMATION // 守护流水线
              </h2>
            </div>
          </div>
          <div className="p-4 space-y-4">
            <div className="p-3 bg-[#0d0e12] border border-white/[0.08] rounded-[3px] flex items-center justify-between">
              <div>
                <p className="font-mono text-xs font-semibold text-zinc-200">
                  配置变更后自动原子应用
                </p>
                <p className="font-mono text-[10px] text-zinc-500 mt-0.5 leading-relaxed">
                  策略经候选校验、备份和健康检查后应用，失败时自动秒级回滚
                </p>
              </div>
              <Switch
                size="sm"
                isSelected={formData.auto_apply}
                onValueChange={(enabled) => setFormData({ ...formData, auto_apply: enabled })}
                classNames={{
                  wrapper: 'group-data-[selected=true]:bg-interface-orange',
                }}
              />
            </div>

            <Input
              type="number"
              label="远程订阅自动更新周期 (分钟)"
              placeholder="60"
              description="设置为 0 表示禁用自动轮询后台更新"
              value={String(formData.subscription_interval)}
              onChange={(e) => setFormData({ ...formData, subscription_interval: parseInt(e.target.value) || 0 })}
              classNames={{
                label: 'font-mono text-xs text-zinc-400',
                input: 'font-mono text-xs text-zinc-200',
                description: 'font-mono text-[10px] text-zinc-500',
                inputWrapper: 'bg-[#060608] border border-white/[0.08] rounded-[2px]',
              }}
            />
          </div>
        </div>
      </div>

      {/* 5. 守护进程服务管理 (Daemon) */}
      {daemonStatus?.supported && (
        <div id="section-daemon" className="rounded-[4px] border border-white/[0.08] bg-[#0a0a0e] overflow-hidden scroll-mt-16">
          <div className="flex items-center justify-between px-4 py-2.5 bg-[#0d0e12] border-b border-white/[0.08]">
            <div className="flex items-center gap-2">
              <Server className="w-4 h-4 text-interface-orange" />
              <h2 className="font-mono text-xs font-semibold text-white tracking-wide uppercase">
                SYSTEM_DAEMON // 开机自启与后台守护
              </h2>
            </div>
            {daemonStatus && (
              <span
                className={`font-mono text-[10px] px-2 py-0.5 rounded-[2px] border ${
                  daemonStatus.installed
                    ? 'bg-emerald-500/10 text-emerald-400 border-emerald-500/30'
                    : 'bg-zinc-800 text-zinc-400 border-white/[0.08]'
                }`}
              >
                {daemonStatus.installed ? 'STATUS: INSTALLED' : 'STATUS: UNINSTALLED'}
              </span>
            )}
          </div>
          <div className="p-4 flex flex-col md:flex-row md:items-center justify-between gap-4">
            <p className="font-mono text-xs text-zinc-400 max-w-xl leading-relaxed">
              安装系统级服务（systemd / launchd）可使 sbm 进程常驻系统后台，终端会话关闭后依然维持网关与 Web 控制台工作，并在异常退出时自动自愈拉起。
            </p>
            <div className="flex items-center gap-2 shrink-0">
              {daemonStatus?.installed ? (
                <>
                  <Button
                    size="sm"
                    variant="flat"
                    className="font-mono text-xs bg-white/[0.05] border border-white/[0.08] text-zinc-200 hover:text-white rounded-[2px]"
                    onPress={handleRestartDaemon}
                  >
                    重启守护进程
                  </Button>
                  <Button
                    size="sm"
                    variant="flat"
                    className="font-mono text-xs bg-red-950/20 border border-red-500/30 text-red-400 hover:bg-red-950/40 rounded-[2px]"
                    onPress={() => setUninstallConfirm(true)}
                  >
                    卸载服务
                  </Button>
                </>
              ) : (
                <Button
                  size="sm"
                  color="primary"
                  className="font-mono text-xs rounded-[2px] bg-interface-orange text-white shadow-geek-glow px-4"
                  onPress={handleInstallDaemon}
                >
                  一键安装后台服务
                </Button>
              )}
            </div>
          </div>
        </div>
      )}

      {/* 底部浮动保存栏 */}
      <div className="sticky bottom-4 z-10 flex flex-col sm:flex-row sm:items-center justify-between gap-3 p-4 rounded-[4px] border border-default-200 dark:border-white/[0.1] bg-content1/95 backdrop-blur-md shadow-2xl">
        <div className="flex items-center gap-2.5 font-mono text-xs text-default-500">
          <Terminal className="w-4 h-4 text-interface-orange shrink-0" />
          <span>修改设置后请点击保存，配置将写入后端持久化存储。</span>
        </div>
        <Button
          color="primary"
          size="sm"
          className="font-mono text-xs font-semibold rounded-[3px] bg-interface-orange text-white shadow-geek-glow px-6 shrink-0"
          startContent={<Save className="w-3.5 h-3.5" />}
          isLoading={saving}
          onPress={handleSave}
        >
          保存所有设置
        </Button>
      </div>

      {/* 内核下载模态框 */}
      <Modal
        isOpen={showDownloadModal}
        onClose={() => !downloading && setShowDownloadModal(false)}
        classNames={{
          base: 'bg-[#0b0c10] border border-white/[0.1] rounded-[4px] shadow-2xl text-zinc-100',
          header: 'border-b border-white/[0.08] py-3 px-4 font-mono text-sm uppercase text-white',
          body: 'py-4 px-4 space-y-4',
          footer: 'border-t border-white/[0.08] py-3 px-4',
        }}
      >
        <ModalContent>
          <ModalHeader className="flex items-center gap-2">
            <Download className="w-4 h-4 text-interface-orange" />
            DOWNLOAD_KERNEL // 下载 sing-box 内核
          </ModalHeader>
          <ModalBody>
            <div className="space-y-3">
              <AppSelect
                label="选择发布的 Release 版本"
                placeholder="选择要部署的 tag"
                selectedKeys={selectedVersion ? [selectedVersion] : []}
                onSelectionChange={(keys) => {
                  const selected = Array.from(keys)[0] as string;
                  if (selected) setSelectedVersion(selected);
                }}
                isDisabled={downloading}
              >
                {releases.map((release) => (
                  <SelectItem key={release.tag_name} textValue={release.tag_name}>
                    {release.tag_name} {release.name ? `- ${release.name}` : ''}
                  </SelectItem>
                ))}
              </AppSelect>

              {kernelInfo && (
                <p className="font-mono text-[11px] text-zinc-400">
                  目标平台检测为: <span className="text-interface-orange">{kernelInfo.os}/{kernelInfo.arch}</span>
                </p>
              )}

              {downloadProgress && (
                <div className="p-3 bg-[#060608] border border-white/[0.08] rounded-[3px] space-y-2">
                  <div className="flex justify-between font-mono text-xs">
                    <span className="text-zinc-400 uppercase tracking-wider">
                      STATUS: {downloadProgress.status}
                    </span>
                    <span className="text-interface-orange font-bold">
                      {downloadProgress.progress}%
                    </span>
                  </div>
                  <Progress
                    value={downloadProgress.progress}
                    color={
                      downloadProgress.status === 'error'
                        ? 'danger'
                        : downloadProgress.status === 'completed'
                        ? 'success'
                        : 'primary'
                    }
                    size="sm"
                    classNames={{
                      track: 'bg-[#15161c]',
                      indicator: 'bg-interface-orange',
                    }}
                  />
                  <p
                    className={`font-mono text-[11px] ${
                      downloadProgress.status === 'error'
                        ? 'text-red-400'
                        : downloadProgress.status === 'completed'
                        ? 'text-emerald-400'
                        : 'text-zinc-400'
                    }`}
                  >
                    &gt; {downloadProgress.message}
                  </p>
                </div>
              )}
            </div>
          </ModalBody>
          <ModalFooter>
            <Button
              size="sm"
              variant="flat"
              className="font-mono text-xs bg-white/[0.05] border border-white/[0.08] text-zinc-300 rounded-[2px]"
              onPress={() => setShowDownloadModal(false)}
              isDisabled={downloading}
            >
              取消
            </Button>
            <Button
              size="sm"
              color="primary"
              className="font-mono text-xs rounded-[2px] bg-interface-orange text-white shadow-geek-glow px-4"
              onPress={startDownload}
              isLoading={downloading}
              isDisabled={!selectedVersion || downloading}
            >
              启动下载与安装
            </Button>
          </ModalFooter>
        </ModalContent>
      </Modal>

      {/* Hosts 编辑模态框 */}
      <Modal
        isOpen={isHostModalOpen}
        onClose={onHostModalClose}
        classNames={{
          base: 'bg-[#0b0c10] border border-white/[0.1] rounded-[4px] shadow-2xl text-zinc-100',
          header: 'border-b border-white/[0.08] py-3 px-4 font-mono text-sm uppercase text-white',
          body: 'py-4 px-4 space-y-4',
          footer: 'border-t border-white/[0.08] py-3 px-4',
        }}
      >
        <ModalContent>
          <ModalHeader className="flex items-center gap-2">
            <Server className="w-4 h-4 text-interface-orange" />
            {editingHost ? 'EDIT_HOST // 编辑域名解析' : 'NEW_HOST // 添加域名映射'}
          </ModalHeader>
          <ModalBody className="space-y-3">
            <Input
              label="目标域名 (DOMAIN)"
              placeholder="例如：api.openai.com 或 router.local"
              value={hostFormData.domain}
              onChange={(e) => setHostFormData({ ...hostFormData, domain: e.target.value })}
              classNames={{
                label: 'font-mono text-xs text-zinc-400',
                input: 'font-mono text-xs text-zinc-200',
                inputWrapper: 'bg-[#060608] border border-white/[0.08] rounded-[2px]',
              }}
            />
            <Textarea
              label="解析目标 IP 列表 (IPv4 / IPv6)"
              placeholder={'每行一条 IP 地址：\n192.168.1.1\n10.0.0.1\n2606:4700::6810:85e5'}
              value={ipsText}
              onChange={(e) => setIpsText(e.target.value)}
              minRows={3}
              classNames={{
                label: 'font-mono text-xs text-zinc-400',
                input: 'font-mono text-xs text-zinc-200',
                inputWrapper: 'bg-[#060608] border border-white/[0.08] rounded-[2px]',
              }}
            />
            <div className="flex items-center justify-between p-3 bg-[#060608] border border-white/[0.06] rounded-[2px]">
              <span className="font-mono text-xs text-zinc-300">是否立即激活该映射</span>
              <Switch
                size="sm"
                isSelected={hostFormData.enabled}
                onValueChange={(enabled) => setHostFormData({ ...hostFormData, enabled })}
                classNames={{
                  wrapper: 'group-data-[selected=true]:bg-interface-orange',
                }}
              />
            </div>
          </ModalBody>
          <ModalFooter>
            <Button
              size="sm"
              variant="flat"
              className="font-mono text-xs bg-white/[0.05] border border-white/[0.08] text-zinc-300 rounded-[2px]"
              onPress={onHostModalClose}
            >
              取消
            </Button>
            <Button
              size="sm"
              color="primary"
              className="font-mono text-xs rounded-[2px] bg-interface-orange text-white shadow-geek-glow px-4"
              onPress={handleSubmitHost}
              isDisabled={!hostFormData.domain || !ipsText.trim()}
            >
              {editingHost ? '保存变更' : '创建映射'}
            </Button>
          </ModalFooter>
        </ModalContent>
      </Modal>

      {/* 卸载后台服务二次确认 */}
      <ConfirmModal
        isOpen={uninstallConfirm}
        title="卸载后台服务 (DAEMON_UNINSTALL)"
        onClose={() => setUninstallConfirm(false)}
        onConfirm={handleUninstallDaemon}
        confirmLabel="确认卸载系统服务"
      >
        <p className="font-mono text-xs text-zinc-300 leading-relaxed">
          卸载后，sbm 管理服务将不再于系统启动时自启，当前的后台守护进程将停止运行。当前活动的 Web 管理连接与终端代理可能立即中断。
        </p>
      </ConfirmModal>
    </div>
  );
}
