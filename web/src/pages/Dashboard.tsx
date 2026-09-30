import { useEffect, useState } from 'react';
import { Button, Modal, ModalContent, ModalHeader, ModalBody, ModalFooter, Tooltip } from '@nextui-org/react';
import { Play, Square, RefreshCw, HardDrive, Wifi, Info, Activity, Terminal, ArrowUpRight, Zap, Cpu } from 'lucide-react';
import { useStore } from '../store';
import { serviceApi } from '../api';
import { useNavigate } from 'react-router-dom';
import { toast } from '../components/Toast';

export default function Dashboard() {
  const navigate = useNavigate();
  const { serviceStatus, subscriptions, systemInfo, settings, fetchServiceStatus, fetchSubscriptions, fetchSystemInfo, fetchSettings } = useStore();

  // 错误模态框状态
  const [errorModal, setErrorModal] = useState<{
    isOpen: boolean;
    title: string;
    message: string;
  }>({
    isOpen: false,
    title: '',
    message: ''
  });

  // 显示错误的辅助函数
  const showError = (title: string, error: any) => {
    const message = error.response?.data?.error || error.message || '操作失败';
    setErrorModal({
      isOpen: true,
      title,
      message
    });
  };

  useEffect(() => {
    fetchServiceStatus();
    fetchSubscriptions();
    fetchSystemInfo();
    fetchSettings();

    // 每 5 秒刷新状态和系统信息
    const interval = setInterval(() => {
      fetchServiceStatus();
      fetchSystemInfo();
    }, 5000);
    return () => clearInterval(interval);
  }, [fetchServiceStatus, fetchSettings, fetchSubscriptions, fetchSystemInfo]);

  const handleStart = async () => {
    try {
      await serviceApi.start();
      await fetchServiceStatus();
      toast.success('服务已启动');
    } catch (error) {
      showError('启动失败', error);
    }
  };

  const handleStop = async () => {
    try {
      await serviceApi.stop();
      await Promise.all([fetchServiceStatus(), fetchSettings()]);
      toast.success(settings?.deployment_role === 'gateway' && settings.gateway?.access_mode === 'dns' ? 'DNS 旁路已停止并恢复接管资源；再次使用请检查并启用' : '服务已停止');
    } catch (error) {
      showError('停止失败', error);
    }
  };

  const handleRestart = async () => {
    try {
      await serviceApi.restart();
      await fetchServiceStatus();
      toast.success('服务已重启');
    } catch (error) {
      showError('重启失败', error);
    }
  };

  const totalNodes = subscriptions.reduce((sum, sub) => sum + sub.node_count, 0);
  const enabledSubs = subscriptions.filter(sub => sub.enabled).length;
  const isRunning = serviceStatus?.running ?? false;

  return (
    <div className="space-y-6">
      {/* 极客头部与部署状态条 */}
      <div className="flex flex-col md:flex-row md:items-end justify-between gap-4 border-b border-zinc-200/80 dark:border-white/[0.08] pb-5">
        <div>
          <div className="flex items-center gap-2 mb-1.5 font-mono text-[11px] text-[#ff5722] tracking-wider uppercase font-semibold">
            <span>[ SYSTEM // OVERVIEW ]</span>
            <span className="text-zinc-400 dark:text-zinc-600">--</span>
            <span className="text-zinc-500 dark:text-zinc-400">CONTROL CENTER</span>
          </div>
          <h1 className="text-2xl font-bold tracking-tight text-zinc-900 dark:text-white font-sans flex items-center gap-3">
            仪表盘控制台
            <span className="text-xs font-mono font-normal px-2 py-0.5 rounded-[2px] bg-zinc-100 dark:bg-white/[0.05] border border-zinc-200 dark:border-white/[0.08] text-zinc-600 dark:text-zinc-400">
              CLUSTER: LOCAL
            </span>
          </h1>
        </div>

        <div className="flex items-center gap-3">
          <Button
            size="sm"
            className="font-mono text-xs rounded-[3px] border border-zinc-200 dark:border-white/[0.1] bg-white dark:bg-[#0f1118] text-zinc-700 dark:text-zinc-300 hover:text-zinc-950 dark:hover:text-white hover:border-[#ff5722]/50 shadow-2xs transition-all"
            endContent={<ArrowUpRight className="size-3 text-[#ff5722]" />}
            onPress={() => navigate('/gateway')}
          >
            部署与设备策略
          </Button>
        </div>
      </div>

      {/* 部署说明提示 */}
      <div className="relative rounded-[3px] border border-zinc-200/80 dark:border-white/[0.08] bg-white dark:bg-[#0c0d12] p-3.5 flex flex-col sm:flex-row sm:items-center justify-between gap-3 overflow-hidden shadow-2xs">
        <div className="absolute left-0 top-0 bottom-0 w-1 bg-[#ff5722]"></div>
        <div className="flex items-center gap-3 pl-2">
          <Zap className="size-4 text-[#ff5722] shrink-0" />
          <p className="text-xs text-zinc-700 dark:text-zinc-300 font-mono">
            DEPLOYMENT: <span className="text-zinc-900 dark:text-white font-semibold">{settings?.deployment_role === 'gateway' ? (settings.gateway?.access_mode === 'dns' ? 'DNS 分流旁路 · 默认网关保持主路由' : '完整网关接管 · 默认网关指向本机') : '单机独立代理模式'}</span>
          </p>
        </div>
        <span className="text-[11px] text-zinc-400 dark:text-zinc-500 font-mono pl-2 sm:pl-0">草案与实际接管状态请在部署页核对</span>
      </div>

      {/* E2B 沙盒终端视窗风格：核心服务面板 */}
      <div className="rounded-[4px] border border-zinc-200/80 dark:border-white/[0.09] bg-white dark:bg-[#0b0c10] shadow-sm dark:shadow-[0_8px_30px_rgb(0,0,0,0.6)] overflow-hidden">
        {/* 视窗标题栏 */}
        <div className="h-9 px-4 border-b border-zinc-200/80 dark:border-white/[0.08] bg-zinc-50 dark:bg-[#0f1117] flex items-center justify-between select-none">
          <div className="flex items-center gap-2">
            <span className="size-2.5 rounded-full bg-zinc-300 dark:bg-zinc-700/80"></span>
            <span className="size-2.5 rounded-full bg-zinc-300 dark:bg-zinc-700/80"></span>
            <span className="size-2.5 rounded-full bg-zinc-300 dark:bg-zinc-700/80"></span>
            <span className="ml-2 font-mono text-[11px] text-zinc-600 dark:text-zinc-400 flex items-center gap-1.5 font-medium">
              <Terminal className="size-3 text-[#ff5722]" />
              <span>sing-box.service</span>
              <span className="text-zinc-400 dark:text-zinc-600">@</span>
              <span className="text-zinc-500">127.0.0.1</span>
            </span>
          </div>

          <div className="flex items-center gap-2">
            <div className="flex items-center gap-1.5 px-2 py-0.5 rounded-[2px] border border-zinc-200 dark:border-white/[0.06] bg-zinc-100 dark:bg-black/60 font-mono text-[10px]">
              <span className={`size-1.5 rounded-full ${isRunning ? 'bg-emerald-500 animate-ping' : 'bg-rose-500'}`}></span>
              <span className={isRunning ? 'text-emerald-600 dark:text-emerald-400 font-semibold' : 'text-rose-600 dark:text-rose-400 font-semibold'}>
                {isRunning ? 'RUNNING' : 'STOPPED'}
              </span>
            </div>
          </div>
        </div>

        {/* 视窗主体控制区 */}
        <div className="p-6">
          <div className="flex flex-col lg:flex-row lg:items-center justify-between gap-6 pb-6 border-b border-zinc-100 dark:border-white/[0.06]">
            <div className="space-y-1">
              <div className="flex items-center gap-3">
                <h2 className="text-xl font-bold font-sans tracking-tight text-zinc-900 dark:text-white">Sing-Box 内核服务</h2>
                <span className={`font-mono text-[11px] px-2 py-0.5 rounded-[2px] border ${
                  isRunning
                    ? 'border-emerald-500/30 bg-emerald-500/10 text-emerald-600 dark:text-emerald-400'
                    : 'border-rose-500/30 bg-rose-500/10 text-rose-600 dark:text-rose-400'
                }`}>
                  {isRunning ? '● ACTIVE (ONLINE)' : '○ INACTIVE'}
                </span>
              </div>
              <p className="text-xs text-zinc-500 dark:text-zinc-400 font-mono">
                核心网络转发与路由协议处理引擎 · 单实例受控
              </p>
            </div>

            {/* 控制操作按钮组 */}
            <div className="flex flex-wrap items-center gap-2.5">
              {isRunning ? (
                <>
                  <Button
                    size="sm"
                    className="font-mono text-xs rounded-[3px] border border-rose-500/30 bg-rose-500/10 hover:bg-rose-500/20 text-rose-600 dark:text-rose-300 font-medium transition-all"
                    startContent={<Square className="size-3.5 fill-current" />}
                    onPress={handleStop}
                  >
                    停止服务
                  </Button>
                  <Button
                    size="sm"
                    className="font-mono text-xs rounded-[3px] border border-zinc-200 dark:border-white/[0.1] bg-zinc-100 hover:bg-zinc-200 dark:bg-[#141720] dark:hover:bg-[#1c202d] text-zinc-700 dark:text-zinc-200 transition-all"
                    startContent={<RefreshCw className="size-3.5 text-zinc-400" />}
                    onPress={handleRestart}
                  >
                    重启服务
                  </Button>
                </>
              ) : (
                <Button
                  size="sm"
                  className="font-mono text-xs rounded-[3px] bg-[#ff5722] hover:bg-[#ff6e40] text-white font-bold uppercase tracking-wider shadow-geek-glow transition-all"
                  startContent={<Play className="size-3.5 fill-current" />}
                  onPress={settings?.deployment_role === 'gateway' && settings.gateway?.access_mode === 'dns' && !settings.gateway.enabled ? () => navigate('/gateway') : handleStart}
                >
                  {settings?.deployment_role === 'gateway' && settings.gateway?.access_mode === 'dns' && !settings.gateway.enabled ? '检查并启用' : '启动服务'}
                </Button>
              )}

              <Button
                size="sm"
                className="font-mono text-xs rounded-[3px] border border-[#ff5722]/40 bg-[#ff5722]/10 hover:bg-[#ff5722]/20 text-[#ff5722] font-medium transition-all"
                onPress={() => navigate('/configuration')}
              >
                审阅与应用配置
              </Button>
            </div>
          </div>

          {settings?.deployment_role === 'gateway' && settings.gateway?.access_mode === 'dns' && (
            <div className="mt-4 p-3 rounded-[3px] bg-amber-500/10 border border-amber-500/30 text-xs text-amber-700 dark:text-amber-300/90 font-mono">
              [NOTICE] 显式停止会恢复 DNS 旁路接管资源并关闭启用状态；再次使用请前往部署页检查并启用。
            </div>
          )}

          {/* 核心元数据网格 */}
          <div className="grid grid-cols-1 sm:grid-cols-3 gap-4 pt-6">
            <div className="p-3.5 rounded-[3px] bg-zinc-50 dark:bg-[#08090d] border border-zinc-200/80 dark:border-white/[0.05]">
              <span className="font-mono text-[10px] uppercase text-zinc-400 dark:text-zinc-500 tracking-wider">// VERSION</span>
              <div className="flex items-center gap-1.5 mt-1 font-mono text-sm font-semibold text-zinc-900 dark:text-zinc-200">
                <span>{serviceStatus?.version?.match(/version\s+([\d.]+)/)?.[1] || serviceStatus?.version || '-'}</span>
                {serviceStatus?.version && (
                  <Tooltip
                    classNames={{
                      content: 'max-w-[min(24rem,calc(100vw-2rem))] bg-content1 text-foreground p-3',
                    }}
                    content={
                      <div className="min-w-0 max-w-full whitespace-pre-wrap [overflow-wrap:anywhere] text-[11px] leading-relaxed font-mono">
                        {serviceStatus.version}
                      </div>
                    }
                    placement="bottom"
                  >
                    <Info className="size-3.5 text-zinc-400 hover:text-zinc-600 dark:text-zinc-500 dark:hover:text-zinc-300 cursor-pointer" />
                  </Tooltip>
                )}
              </div>
            </div>

            <div className="p-3.5 rounded-[3px] bg-zinc-50 dark:bg-[#08090d] border border-zinc-200/80 dark:border-white/[0.05]">
              <span className="font-mono text-[10px] uppercase text-zinc-400 dark:text-zinc-500 tracking-wider">// PROCESS ID</span>
              <p className="mt-1 font-mono text-sm font-semibold text-zinc-900 dark:text-zinc-200">
                {serviceStatus?.pid ? `#${serviceStatus.pid}` : 'NONE'}
              </p>
            </div>

            <div className="p-3.5 rounded-[3px] bg-zinc-50 dark:bg-[#08090d] border border-zinc-200/80 dark:border-white/[0.05]">
              <span className="font-mono text-[10px] uppercase text-zinc-400 dark:text-zinc-500 tracking-wider">// STATUS CODE</span>
              <p className="mt-1 font-mono text-sm font-semibold text-zinc-900 dark:text-zinc-200 flex items-center gap-2">
                <span className={`size-2 rounded-full ${isRunning ? 'bg-emerald-500' : 'bg-zinc-400 dark:bg-zinc-600'}`}></span>
                {isRunning ? 'OPERATIONAL' : 'OFFLINE'}
              </p>
            </div>
          </div>
        </div>
      </div>

      {/* 4 大核心指标卡片 (HUD Metrics) */}
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
        {/* 指标 1：订阅 */}
        <div className="p-4 rounded-[4px] border border-zinc-200/80 dark:border-white/[0.08] bg-white dark:bg-[#0b0c10] hover:border-zinc-300 dark:hover:border-white/[0.15] shadow-2xs transition-all">
          <div className="flex items-center justify-between mb-3">
            <span className="font-mono text-[11px] text-zinc-500 uppercase tracking-wider">订阅源配置</span>
            <div className="size-7 rounded-[2px] bg-cyan-500/10 border border-cyan-500/20 flex items-center justify-center text-cyan-600 dark:text-cyan-400">
              <Wifi className="size-3.5" />
            </div>
          </div>
          <div className="flex items-baseline gap-2">
            <span className="font-mono text-2xl font-bold text-zinc-900 dark:text-white">{enabledSubs}</span>
            <span className="font-mono text-xs text-zinc-400 dark:text-zinc-500">/ {subscriptions.length} 启用</span>
          </div>
          <div className="mt-3 w-full bg-zinc-100 dark:bg-zinc-800/60 h-1 rounded-full overflow-hidden">
            <div
              className="bg-cyan-500 h-full transition-all duration-500"
              style={{ width: `${subscriptions.length > 0 ? (enabledSubs / subscriptions.length) * 100 : 0}%` }}
            ></div>
          </div>
        </div>

        {/* 指标 2：节点 */}
        <div className="p-4 rounded-[4px] border border-zinc-200/80 dark:border-white/[0.08] bg-white dark:bg-[#0b0c10] hover:border-zinc-300 dark:hover:border-white/[0.15] shadow-2xs transition-all">
          <div className="flex items-center justify-between mb-3">
            <span className="font-mono text-[11px] text-zinc-500 uppercase tracking-wider">节点总数</span>
            <div className="size-7 rounded-[2px] bg-emerald-500/10 border border-emerald-500/20 flex items-center justify-center text-emerald-600 dark:text-emerald-400">
              <HardDrive className="size-3.5" />
            </div>
          </div>
          <div className="flex items-baseline gap-2">
            <span className="font-mono text-2xl font-bold text-zinc-900 dark:text-white">{totalNodes}</span>
            <span className="font-mono text-xs text-zinc-400 dark:text-zinc-500">PROXIES</span>
          </div>
          <p className="mt-3 font-mono text-[10px] text-zinc-400 dark:text-zinc-500">已载入分流路由池</p>
        </div>

        {/* 指标 3：SBM 进程 */}
        <div className="p-4 rounded-[4px] border border-zinc-200/80 dark:border-white/[0.08] bg-white dark:bg-[#0b0c10] hover:border-zinc-300 dark:hover:border-white/[0.15] shadow-2xs transition-all">
          <div className="flex items-center justify-between mb-3">
            <span className="font-mono text-[11px] text-zinc-500 uppercase tracking-wider">SBM 管理器</span>
            <div className="size-7 rounded-[2px] bg-purple-500/10 border border-purple-500/20 flex items-center justify-center text-purple-600 dark:text-purple-400">
              <Cpu className="size-3.5" />
            </div>
          </div>
          <div className="font-mono">
            {systemInfo?.sbm ? (
              <div className="space-y-1">
                <div className="flex items-baseline justify-between text-xs">
                  <span className="text-zinc-400 dark:text-zinc-500">CPU</span>
                  <span className="text-zinc-900 dark:text-white font-semibold">{systemInfo.sbm.cpu_percent.toFixed(1)}%</span>
                </div>
                <div className="flex items-baseline justify-between text-xs">
                  <span className="text-zinc-400 dark:text-zinc-500">MEM</span>
                  <span className="text-zinc-900 dark:text-white font-semibold">{systemInfo.sbm.memory_mb.toFixed(1)} MB</span>
                </div>
              </div>
            ) : (
              <span className="font-mono text-zinc-400 text-xs">-</span>
            )}
          </div>
        </div>

        {/* 指标 4：Sing-Box 资源 */}
        <div className="p-4 rounded-[4px] border border-zinc-200/80 dark:border-white/[0.08] bg-white dark:bg-[#0b0c10] hover:border-zinc-300 dark:hover:border-white/[0.15] shadow-2xs transition-all">
          <div className="flex items-center justify-between mb-3">
            <span className="font-mono text-[11px] text-zinc-500 uppercase tracking-wider">Sing-Box 核心</span>
            <div className="size-7 rounded-[2px] bg-[#ff5722]/10 border border-[#ff5722]/20 flex items-center justify-center text-[#ff5722]">
              <Activity className="size-3.5" />
            </div>
          </div>
          <div className="font-mono">
            {isRunning && systemInfo?.singbox ? (
              <div className="space-y-1">
                <div className="flex items-baseline justify-between text-xs">
                  <span className="text-zinc-400 dark:text-zinc-500">CPU</span>
                  <span className="text-zinc-900 dark:text-white font-semibold">{systemInfo.singbox.cpu_percent.toFixed(1)}%</span>
                </div>
                <div className="flex items-baseline justify-between text-xs">
                  <span className="text-zinc-400 dark:text-zinc-500">MEM</span>
                  <span className="text-zinc-900 dark:text-white font-semibold">{systemInfo.singbox.memory_mb.toFixed(1)} MB</span>
                </div>
              </div>
            ) : (
              <span className="font-mono text-zinc-400 dark:text-zinc-600 text-xs">OFFLINE</span>
            )}
          </div>
        </div>
      </div>

      {/* 订阅概览卡片 */}
      <div className="rounded-[4px] border border-zinc-200/80 dark:border-white/[0.08] bg-white dark:bg-[#0b0c10] shadow-2xs overflow-hidden">
        <div className="h-10 px-4 border-b border-zinc-200/80 dark:border-white/[0.08] bg-zinc-50 dark:bg-[#0e1017] flex items-center justify-between">
          <span className="font-mono text-xs text-zinc-700 dark:text-zinc-300 font-semibold tracking-wider uppercase">
            // 订阅概览 (SUBSCRIPTIONS)
          </span>
          <button
            onClick={() => navigate('/subscriptions')}
            className="font-mono text-[11px] text-[#ff5722] hover:text-[#ff6e40] font-medium transition-colors cursor-pointer"
          >
            MANAGE &rarr;
          </button>
        </div>

        <div className="p-4">
          {subscriptions.length === 0 ? (
            <div className="p-8 text-center font-mono text-xs text-zinc-400 dark:text-zinc-500">
              暂无已配置的订阅源，请前往「节点管理」添加
            </div>
          ) : (
            <div className="space-y-2">
              {subscriptions.map((sub) => (
                <div
                  key={sub.id}
                  className="flex items-center justify-between p-3 rounded-[3px] bg-zinc-50/80 dark:bg-[#08090d] border border-zinc-200/60 dark:border-white/[0.04] hover:border-zinc-300 dark:hover:border-white/[0.1] transition-all"
                >
                  <div className="flex items-center gap-3">
                    <span className={`size-2 rounded-full ${sub.enabled ? 'bg-emerald-500' : 'bg-zinc-300 dark:bg-zinc-600'}`}></span>
                    <span className="font-sans font-medium text-sm text-zinc-800 dark:text-zinc-200">
                      {sub.name}
                    </span>
                    <span className="font-mono text-[11px] px-1.5 py-0.5 rounded-[2px] bg-zinc-100 dark:bg-white/[0.04] text-zinc-600 dark:text-zinc-400 border border-zinc-200 dark:border-white/[0.06]">
                      {sub.node_count} 节点
                    </span>
                  </div>
                  <span className="font-mono text-xs text-zinc-400 dark:text-zinc-500">
                    UPDATED: {new Date(sub.updated_at).toLocaleDateString()} {new Date(sub.updated_at).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}
                  </span>
                </div>
              ))}
            </div>
          )}
        </div>
      </div>

      {/* 错误提示模态框 */}
      <Modal
        isOpen={errorModal.isOpen}
        onClose={() => setErrorModal({ ...errorModal, isOpen: false })}
        classNames={{
          base: "bg-white dark:bg-[#0c0d12] border border-rose-500/30 text-zinc-900 dark:text-white rounded-[4px] shadow-2xl",
          header: "border-b border-zinc-200 dark:border-white/[0.08] font-mono text-sm text-rose-500 dark:text-rose-400",
          footer: "border-t border-zinc-200 dark:border-white/[0.08]",
        }}
      >
        <ModalContent>
          <ModalHeader className="flex items-center gap-2">
            <span className="size-2 rounded-full bg-rose-500"></span>
            {errorModal.title}
          </ModalHeader>
          <ModalBody className="py-4">
            <p className="whitespace-pre-wrap font-mono text-xs text-zinc-700 dark:text-zinc-300 bg-zinc-50 dark:bg-black/60 p-3 rounded border border-rose-500/20">
              {errorModal.message}
            </p>
          </ModalBody>
          <ModalFooter>
            <Button
              size="sm"
              className="rounded-[2px] font-mono text-xs bg-[#ff5722] text-white font-semibold"
              onPress={() => setErrorModal({ ...errorModal, isOpen: false })}
            >
              ACKNOWLEDGE
            </Button>
          </ModalFooter>
        </ModalContent>
      </Modal>
    </div>
  );
}
