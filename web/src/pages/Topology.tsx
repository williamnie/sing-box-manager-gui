import { useCallback, useEffect, useMemo, useState } from 'react';
import { Card, CardBody, Button, Chip } from '@nextui-org/react';
import { ArrowLeft, RefreshCw } from 'lucide-react';
import { useNavigate } from 'react-router-dom';
import { useStore } from '../store';
import { configApi } from '../api';
import ConfigTopologyView from '../components/ConfigTopologyView';
import { buildConfigTopology } from '../features/config-topology/buildTopology';
import type { ConfigPreviewData } from '../features/config-topology/types';
import { toast } from '../components/Toast';

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

export default function Topology() {
  const navigate = useNavigate();
  const { settings, fetchSettings } = useStore();
  const [configData, setConfigData] = useState<ConfigPreviewData | null>(null);
  const [configLoading, setConfigLoading] = useState(false);
  const [configError, setConfigError] = useState<string | null>(null);
  const [configUpdatedAt, setConfigUpdatedAt] = useState<Date | null>(null);

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
      setConfigUpdatedAt(new Date());
    } catch (error) {
      const message = toErrorMessage(error, '获取配置预览失败');
      setConfigError(message);
      setConfigData(null);
    } finally {
      setConfigLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchSettings();
    fetchConfigPreview();
  }, [fetchConfigPreview, fetchSettings]);

  const configTopology = useMemo(
    () => buildConfigTopology(configData, settings?.final_outbound),
    [configData, settings?.final_outbound]
  );

  const handleRefresh = async () => {
    await fetchConfigPreview();
    toast.success('配置预览已刷新');
  };

  return (
    <div className="space-y-6">
      {/* 顶部导航 */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-4">
          <Button
            isIconOnly
            variant="light"
            onPress={() => navigate('/settings')}
          >
            <ArrowLeft className="w-5 h-5" />
          </Button>
          <div>
            <h1 className="text-2xl font-bold">工作拓扑</h1>
            <p className="text-sm text-gray-500">上游接入 → 路由决策 → 出口选择 → 最终出口</p>
          </div>
        </div>
        <Button
          color="primary"
          startContent={<RefreshCw className="w-4 h-4" />}
          onPress={handleRefresh}
          isLoading={configLoading}
        >
          刷新预览
        </Button>
      </div>

      {/* 统计信息 */}
      <div className="flex flex-wrap gap-2">
        <Chip size="sm" variant="flat" color="primary">
          节点 {configTopology.nodes.length}
        </Chip>
        <Chip size="sm" variant="flat" color="secondary">
          链路 {configTopology.edges.length}
        </Chip>
        <Chip
          size="sm"
          variant="flat"
          color={configTopology.warnings.length > 0 ? 'warning' : 'default'}
        >
          提示 {configTopology.warnings.length}
        </Chip>
        {configUpdatedAt && (
          <Chip size="sm" variant="flat">
            更新于 {configUpdatedAt.toLocaleTimeString()}
          </Chip>
        )}
      </div>

      {/* 错误提示 */}
      {configError && (
        <div className="p-4 rounded-lg bg-danger-50 dark:bg-danger-900/20 text-danger-600 dark:text-danger-400 text-sm">
          {configError}
        </div>
      )}

      {/* 拓扑图 */}
      <Card>
        <CardBody className="p-4">
          {configData ? (
            <ConfigTopologyView graph={configTopology} />
          ) : configLoading ? (
            <div className="py-20 text-center text-gray-500">
              <RefreshCw className="w-8 h-8 mx-auto mb-4 animate-spin" />
              <p>加载配置预览中...</p>
            </div>
          ) : (
            <div className="py-20 text-center text-gray-500">
              <p>暂无配置数据</p>
              <Button
                className="mt-4"
                color="primary"
                startContent={<RefreshCw className="w-4 h-4" />}
                onPress={handleRefresh}
              >
                刷新预览
              </Button>
            </div>
          )}
        </CardBody>
      </Card>
    </div>
  );
}
