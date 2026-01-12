import { useEffect, useState } from 'react';
import { Card, CardBody, CardHeader, Button, Chip, Modal, ModalContent, ModalHeader, ModalBody, ModalFooter, Tooltip } from '@nextui-org/react';
import { Play, Square, RefreshCw, Cpu, HardDrive, Wifi, Info, Activity, ChevronDown, ArrowUp, ArrowDown } from 'lucide-react';
import { useStore } from '../store';
import { serviceApi, configApi } from '../api';
import { toast } from '../components/Toast';
import NetworkTopology from '../components/NetworkTopology';
import DataUsage from '../components/DataUsage';
import { useClashTraffic, useClashMemory, formatSpeed, formatMemory } from '../hooks/useClashTraffic';

export default function Dashboard() {
  // 使用选择器优化渲染性能
  const serviceStatus = useStore(state => state.serviceStatus);
  const subscriptions = useStore(state => state.subscriptions);
  const settings = useStore(state => state.settings);
  const fetchServiceStatus = useStore(state => state.fetchServiceStatus);
  const fetchSubscriptions = useStore(state => state.fetchSubscriptions);
  const fetchSettings = useStore(state => state.fetchSettings);

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

  // 网络拓扑展开状态
  const [showTopology, setShowTopology] = useState(true);

  // 操作中状态（防止连续点击）
  const [isOperating, setIsOperating] = useState(false);

  // 实时流量和内存监控
  const traffic = useClashTraffic();
  const memory = useClashMemory();

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
    // 初始加载
    fetchServiceStatus();
    fetchSubscriptions();
    fetchSettings();

    // 轮询函数 - 仅在页面可见时执行
    const poll = () => {
      if (!document.hidden) {
        fetchServiceStatus();
      }
    };

    // 每 5 秒轮询
    const interval = setInterval(poll, 5000);

    // 页面可见性变化时立即刷新
    const handleVisibilityChange = () => {
      if (!document.hidden) {
        poll();
      }
    };

    document.addEventListener('visibilitychange', handleVisibilityChange);

    return () => {
      clearInterval(interval);
      document.removeEventListener('visibilitychange', handleVisibilityChange);
    };
  }, [fetchServiceStatus, fetchSubscriptions, fetchSettings]);

  // 等待 Clash API 就绪
  const waitForClashApi = async (maxWait = 15000) => {
    const port = settings?.clash_api_port || 9091;
    const startTime = Date.now();
    
    while (Date.now() - startTime < maxWait) {
      try {
        const res = await fetch(`http://${window.location.hostname}:${port}/version`, { 
          signal: AbortSignal.timeout(2000) 
        });
        if (res.ok) return true;
      } catch {}
      await new Promise(r => setTimeout(r, 500));
    }
    return false;
  };

  // 最少等待时间
  const minDelay = (ms: number) => new Promise(r => setTimeout(r, ms));

  const handleStart = async () => {
    if (isOperating) return;
    setIsOperating(true);
    try {
      const [,] = await Promise.all([
        serviceApi.start(),
        minDelay(2000), // 最少 2 秒动画
      ]);
      await waitForClashApi();
      await fetchServiceStatus();
      toast.success('服务已启动');
    } catch (error) {
      showError('启动失败', error);
    } finally {
      setIsOperating(false);
    }
  };

  const handleStop = async () => {
    if (isOperating) return;
    setIsOperating(true);
    try {
      const [,] = await Promise.all([
        serviceApi.stop(),
        minDelay(2000),
      ]);
      await fetchServiceStatus();
      toast.success('服务已停止');
    } catch (error) {
      showError('停止失败', error);
    } finally {
      setIsOperating(false);
    }
  };

  const handleRestart = async () => {
    if (isOperating) return;
    setIsOperating(true);
    try {
      const [,] = await Promise.all([
        serviceApi.restart(),
        minDelay(2000),
      ]);
      await waitForClashApi();
      await fetchServiceStatus();
      toast.success('服务已重启');
    } catch (error) {
      showError('重启失败', error);
    } finally {
      setIsOperating(false);
    }
  };

  const handleApplyConfig = async () => {
    if (isOperating) return;
    setIsOperating(true);
    try {
      const [,] = await Promise.all([
        configApi.apply(),
        minDelay(2000),
      ]);
      await waitForClashApi();
      await fetchServiceStatus();
      toast.success('配置已应用');
    } catch (error) {
      showError('应用配置失败', error);
    } finally {
      setIsOperating(false);
    }
  };

  const totalNodes = subscriptions.reduce((sum, sub) => sum + sub.node_count, 0);
  const enabledSubs = subscriptions.filter(sub => sub.enabled).length;

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold text-gray-800 dark:text-white">仪表盘</h1>

      {/* 服务状态卡片 */}
      <Card>
        <CardHeader className="flex justify-between items-center">
          <div className="flex items-center gap-3">
            <h2 className="text-lg font-semibold">sing-box 服务</h2>
            <Chip
              color={serviceStatus?.running ? 'success' : 'danger'}
              variant="flat"
              size="sm"
            >
              {serviceStatus?.running ? '运行中' : '已停止'}
            </Chip>
          </div>
          <div className="flex gap-2">
            {serviceStatus?.running ? (
              <>
                <Button
                  size="sm"
                  color="danger"
                  variant="flat"
                  startContent={!isOperating && <Square className="w-4 h-4" />}
                  onPress={handleStop}
                  isLoading={isOperating}
                  isDisabled={isOperating}
                >
                  停止
                </Button>
                <Button
                  size="sm"
                  color="primary"
                  variant="flat"
                  startContent={!isOperating && <RefreshCw className="w-4 h-4" />}
                  onPress={handleRestart}
                  isLoading={isOperating}
                  isDisabled={isOperating}
                >
                  重启
                </Button>
              </>
            ) : (
              <Button
                size="sm"
                color="success"
                startContent={!isOperating && <Play className="w-4 h-4" />}
                onPress={handleStart}
                isLoading={isOperating}
                isDisabled={isOperating}
              >
                启动
              </Button>
            )}
            <Button
              size="sm"
              color="primary"
              onPress={handleApplyConfig}
              isLoading={isOperating}
              isDisabled={isOperating}
            >
              应用配置
            </Button>
          </div>
        </CardHeader>
        <CardBody>
          <div className="grid grid-cols-3 gap-4">
            <div className="overflow-hidden">
              <p className="text-sm text-gray-500">版本</p>
              <div className="flex items-center gap-1">
                <p className="font-medium truncate">
                  {serviceStatus?.version?.match(/version\s+([\d.]+)/)?.[1] || '-'}
                </p>
                {serviceStatus?.version && (
                  <Tooltip
                    content={
                      <pre className="max-w-sm text-xs p-1 whitespace-pre-wrap break-all">
                        {serviceStatus.version}
                      </pre>
                    }
                    placement="bottom"
                  >
                    <Info className="w-3.5 h-3.5 text-gray-400 cursor-help flex-shrink-0" />
                  </Tooltip>
                )}
              </div>
            </div>
            <div>
              <p className="text-sm text-gray-500">进程 ID</p>
              <p className="font-medium">{serviceStatus?.pid || '-'}</p>
            </div>
            <div>
              <p className="text-sm text-gray-500">状态</p>
              <p className="font-medium">
                {serviceStatus?.running ? '正常运行' : '未运行'}
              </p>
            </div>
          </div>
        </CardBody>
      </Card>

      {/* 统计卡片 */}
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
        <Card>
          <CardBody className="flex flex-row items-center gap-4">
            <div className="p-3 bg-blue-100 dark:bg-blue-900 rounded-lg">
              <Wifi className="w-6 h-6 text-blue-600 dark:text-blue-300" />
            </div>
            <div>
              <p className="text-sm text-gray-500">订阅数量</p>
              <p className="text-2xl font-bold">{enabledSubs} / {subscriptions.length}</p>
            </div>
          </CardBody>
        </Card>

        <Card>
          <CardBody className="flex flex-row items-center gap-4">
            <div className="p-3 bg-green-100 dark:bg-green-900 rounded-lg">
              <HardDrive className="w-6 h-6 text-green-600 dark:text-green-300" />
            </div>
            <div>
              <p className="text-sm text-gray-500">节点总数</p>
              <p className="text-2xl font-bold">{totalNodes}</p>
            </div>
          </CardBody>
        </Card>

        <Card>
          <CardBody className="flex flex-row items-center gap-4">
            <div className="p-3 bg-purple-100 dark:bg-purple-900 rounded-lg">
              <Cpu className="w-6 h-6 text-purple-600 dark:text-purple-300" />
            </div>
            <div>
              <p className="text-sm text-gray-500">内存占用</p>
              {memory.connected ? (
                <p className="text-2xl font-bold">{formatMemory(memory.inuse)}</p>
              ) : (
                <p className="text-2xl font-bold text-gray-400">-</p>
              )}
            </div>
          </CardBody>
        </Card>

        <Card>
          <CardBody className="flex flex-row items-center gap-4">
            <div className="p-3 bg-orange-100 dark:bg-orange-900 rounded-lg">
              <Activity className="w-6 h-6 text-orange-600 dark:text-orange-300" />
            </div>
            <div>
              <p className="text-sm text-gray-500">实时流量</p>
              {traffic.connected ? (
                <div className="flex items-center gap-3 text-lg font-bold">
                  <span className="flex items-center gap-1 text-green-600 dark:text-green-400">
                    <ArrowUp className="w-4 h-4" />
                    {formatSpeed(traffic.up)}
                  </span>
                  <span className="flex items-center gap-1 text-blue-600 dark:text-blue-400">
                    <ArrowDown className="w-4 h-4" />
                    {formatSpeed(traffic.down)}
                  </span>
                </div>
              ) : (
                <p className="text-lg font-bold text-gray-400">未连接</p>
              )}
            </div>
          </CardBody>
        </Card>
      </div>

      {/* 网络拓扑 */}
      {serviceStatus?.running && (
        <Card>
          <CardHeader
            className="flex justify-between items-center cursor-pointer"
            onClick={() => setShowTopology(!showTopology)}
          >
            <h2 className="text-lg font-semibold">网络拓扑</h2>
            <Button isIconOnly size="sm" variant="light">
              <ChevronDown
                className={`w-5 h-5 transition-transform ${showTopology ? 'rotate-180' : ''}`}
              />
            </Button>
          </CardHeader>
          {showTopology && (
            <CardBody>
              <NetworkTopology />
            </CardBody>
          )}
        </Card>
      )}

      {/* 数据用量 */}
      {serviceStatus?.running && <DataUsage />}

      {/* 订阅列表预览 */}
      <Card>
        <CardHeader>
          <h2 className="text-lg font-semibold">订阅概览</h2>
        </CardHeader>
        <CardBody>
          {subscriptions.length === 0 ? (
            <p className="text-gray-500 text-center py-4">暂无订阅，请前往节点页面添加</p>
          ) : (
            <div className="space-y-3">
              {subscriptions.map((sub) => (
                <div
                  key={sub.id}
                  className="flex items-center justify-between p-3 bg-gray-50 dark:bg-gray-800 rounded-lg"
                >
                  <div className="flex items-center gap-3">
                    <Chip
                      size="sm"
                      color={sub.enabled ? 'success' : 'default'}
                      variant="dot"
                    >
                      {sub.name}
                    </Chip>
                    <span className="text-sm text-gray-500">
                      {sub.node_count} 个节点
                    </span>
                  </div>
                  <span className="text-sm text-gray-400">
                    更新于 {new Date(sub.updated_at).toLocaleString()}
                  </span>
                </div>
              ))}
            </div>
          )}
        </CardBody>
      </Card>

      {/* 错误提示模态框 */}
      <Modal isOpen={errorModal.isOpen} onClose={() => setErrorModal({ ...errorModal, isOpen: false })}>
        <ModalContent>
          <ModalHeader className="text-danger">{errorModal.title}</ModalHeader>
          <ModalBody>
            <p className="whitespace-pre-wrap text-sm">{errorModal.message}</p>
          </ModalBody>
          <ModalFooter>
            <Button color="primary" onPress={() => setErrorModal({ ...errorModal, isOpen: false })}>
              确定
            </Button>
          </ModalFooter>
        </ModalContent>
      </Modal>
    </div>
  );
}
