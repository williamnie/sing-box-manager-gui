import { useState, useEffect, useCallback, useMemo, useRef } from 'react';
import { Card, CardBody, CardHeader, Button, Chip, Switch, Tooltip } from '@nextui-org/react';
import { Trash2, ChevronDown, ChevronUp, ArrowUpDown, Info, Clock, Upload, Download } from 'lucide-react';
import byteSize from 'byte-size';
import { useClashConnections } from '../hooks/useClashConnections';

// 数据用量条目
interface DataUsageEntry {
  sourceIP: string;
  upload: number;
  download: number;
  total: number;
  firstSeen: number;
  lastSeen: number;
}

// 排序字段
type SortField = 'ip' | 'upload' | 'download' | 'total' | 'duration';
type SortOrder = 'asc' | 'desc';

// 格式化字节
const formatBytes = (bytes: number) => byteSize(bytes).toString();

// 格式化时长
const formatDuration = (start: number, end: number) => {
  const diff = end - start;
  const seconds = Math.floor(diff / 1000);
  const minutes = Math.floor(seconds / 60);
  const hours = Math.floor(minutes / 60);
  const days = Math.floor(hours / 24);

  if (days > 0) return `${days}天${hours % 24}小时`;
  if (hours > 0) return `${hours}小时${minutes % 60}分`;
  if (minutes > 0) return `${minutes}分${seconds % 60}秒`;
  return `${seconds}秒`;
};

// 从 localStorage 读取数据
const loadDataUsage = (): Record<string, DataUsageEntry> => {
  try {
    const data = localStorage.getItem('dataUsageMap');
    return data ? JSON.parse(data) : {};
  } catch {
    return {};
  }
};

// 保存数据到 localStorage
const saveDataUsage = (data: Record<string, DataUsageEntry>) => {
  try {
    localStorage.setItem('dataUsageMap', JSON.stringify(data));
  } catch (e) {
    console.error('Failed to save data usage:', e);
  }
};

export default function DataUsage() {
  const { connections, downloadTotal, uploadTotal, isConnected } = useClashConnections();
  const [showTable, setShowTable] = useState(() => {
    return localStorage.getItem('showDataUsageTable') === 'true';
  });
  const [dataUsageMap, setDataUsageMap] = useState<Record<string, DataUsageEntry>>(loadDataUsage);
  const [sortField, setSortField] = useState<SortField>('total');
  const [sortOrder, setSortOrder] = useState<SortOrder>('desc');

  // 跟踪每个连接的上次数据
  const connectionLastData = useRef<Map<string, { upload: number; download: number }>>(new Map());
  const lastTotals = useRef<{ upload: number; download: number }>({ upload: 0, download: 0 });

  // 更新数据用量
  useEffect(() => {
    if (!connections || connections.length === 0) return;

    // 检测服务重启（总量减少）
    if (uploadTotal < lastTotals.current.upload || downloadTotal < lastTotals.current.download) {
      connectionLastData.current.clear();
      setDataUsageMap({});
      saveDataUsage({});
    }
    lastTotals.current = { upload: uploadTotal, download: downloadTotal };

    const now = Date.now();
    const updates: Record<string, DataUsageEntry> = { ...dataUsageMap };

    // 按 IP 汇总增量
    const ipDeltaMap = new Map<string, { upload: number; download: number }>();

    connections.forEach((conn) => {
      const sourceIP = conn.metadata.sourceIP;
      if (!sourceIP) return;

      const currentUpload = conn.upload || 0;
      const currentDownload = conn.download || 0;

      if (!ipDeltaMap.has(sourceIP)) {
        ipDeltaMap.set(sourceIP, { upload: 0, download: 0 });
      }

      const ipData = ipDeltaMap.get(sourceIP)!;
      const lastData = connectionLastData.current.get(conn.id);

      if (lastData) {
        const uploadDelta = Math.max(0, currentUpload - lastData.upload);
        const downloadDelta = Math.max(0, currentDownload - lastData.download);
        ipData.upload += uploadDelta;
        ipData.download += downloadDelta;
      } else {
        // 新连接，计入全部流量
        ipData.upload += currentUpload;
        ipData.download += currentDownload;
      }

      connectionLastData.current.set(conn.id, {
        upload: currentUpload,
        download: currentDownload,
      });
    });

    // 清理不活跃的连接
    const activeIds = new Set(connections.map((c) => c.id));
    connectionLastData.current.forEach((_, connId) => {
      if (!activeIds.has(connId)) {
        connectionLastData.current.delete(connId);
      }
    });

    // 更新数据用量表
    ipDeltaMap.forEach((data, sourceIP) => {
      const existing = updates[sourceIP];
      if (existing) {
        if (data.upload > 0 || data.download > 0) {
          updates[sourceIP] = {
            ...existing,
            upload: existing.upload + data.upload,
            download: existing.download + data.download,
            total: existing.upload + data.upload + existing.download + data.download,
            lastSeen: now,
          };
        } else {
          updates[sourceIP] = { ...existing, lastSeen: now };
        }
      } else if (data.upload > 0 || data.download > 0) {
        updates[sourceIP] = {
          sourceIP,
          upload: data.upload,
          download: data.download,
          total: data.upload + data.download,
          firstSeen: now,
          lastSeen: now,
        };
      }
    });

    setDataUsageMap(updates);
    saveDataUsage(updates);
  }, [connections, uploadTotal, downloadTotal]);

  // 保存显示状态
  useEffect(() => {
    localStorage.setItem('showDataUsageTable', String(showTable));
  }, [showTable]);

  // 排序后的条目
  const sortedEntries = useMemo(() => {
    const entries = Object.values(dataUsageMap);

    return entries.sort((a, b) => {
      let comparison = 0;

      switch (sortField) {
        case 'ip':
          comparison = a.sourceIP.localeCompare(b.sourceIP);
          break;
        case 'upload':
          comparison = a.upload - b.upload;
          break;
        case 'download':
          comparison = a.download - b.download;
          break;
        case 'total':
          comparison = a.total - b.total;
          break;
        case 'duration':
          comparison = (a.lastSeen - a.firstSeen) - (b.lastSeen - b.firstSeen);
          break;
      }

      return sortOrder === 'asc' ? comparison : -comparison;
    });
  }, [dataUsageMap, sortField, sortOrder]);

  // 统计数据
  const totalStats = useMemo(() => {
    const entries = sortedEntries;
    const totalUpload = entries.reduce((sum, e) => sum + e.upload, 0);
    const totalDownload = entries.reduce((sum, e) => sum + e.download, 0);

    let earliestFirst = Number.MAX_SAFE_INTEGER;
    let latestLast = 0;

    entries.forEach((e) => {
      if (e.firstSeen < earliestFirst) earliestFirst = e.firstSeen;
      if (e.lastSeen > latestLast) latestLast = e.lastSeen;
    });

    const hasTimeRange = earliestFirst !== Number.MAX_SAFE_INTEGER && latestLast > 0;

    return {
      count: entries.length,
      upload: totalUpload,
      download: totalDownload,
      total: totalUpload + totalDownload,
      firstSeen: hasTimeRange ? earliestFirst : undefined,
      lastSeen: hasTimeRange ? latestLast : undefined,
    };
  }, [sortedEntries]);

  // 切换排序
  const handleSort = useCallback((field: SortField) => {
    if (sortField === field) {
      setSortOrder((prev) => (prev === 'asc' ? 'desc' : 'asc'));
    } else {
      setSortField(field);
      setSortOrder('desc');
    }
  }, [sortField]);

  // 清除所有
  const handleClearAll = useCallback(() => {
    if (confirm('确定要清除所有数据用量记录吗？')) {
      setDataUsageMap({});
      saveDataUsage({});
      connectionLastData.current.clear();
    }
  }, []);

  // 删除单条记录
  const handleRemoveEntry = useCallback((sourceIP: string) => {
    setDataUsageMap((prev) => {
      const updates = { ...prev };
      delete updates[sourceIP];
      saveDataUsage(updates);
      return updates;
    });
  }, []);

  // 排序图标
  const SortIcon = ({ field }: { field: SortField }) => {
    if (sortField !== field) return <ArrowUpDown className="w-3 h-3 opacity-50" />;
    return sortOrder === 'asc' ? (
      <ChevronUp className="w-3 h-3" />
    ) : (
      <ChevronDown className="w-3 h-3" />
    );
  };

  return (
    <Card>
      <CardHeader className="flex justify-between items-center">
        <div className="flex items-center gap-3">
          <h2 className="text-lg font-semibold">数据用量</h2>
          <Tooltip content="统计各设备通过代理的流量使用情况，数据保存在本地浏览器中">
            <Info className="w-4 h-4 text-gray-400 cursor-help" />
          </Tooltip>
          <Switch
            size="sm"
            isSelected={showTable}
            onValueChange={setShowTable}
          />
          {!isConnected && (
            <Chip size="sm" color="warning" variant="flat">
              未连接
            </Chip>
          )}
        </div>
        {showTable && sortedEntries.length > 0 && (
          <Button
            size="sm"
            color="danger"
            variant="flat"
            startContent={<Trash2 className="w-4 h-4" />}
            onPress={handleClearAll}
          >
            清除
          </Button>
        )}
      </CardHeader>

      {showTable && (
        <CardBody className="pt-0">
          {/* 统计概览 */}
          {totalStats.count > 0 && (
            <div className="grid grid-cols-2 sm:grid-cols-5 gap-3 mb-4 p-3 bg-gray-50 dark:bg-gray-800 rounded-lg">
              <div>
                <p className="text-xs text-gray-500">设备数</p>
                <p className="text-lg font-bold text-primary">{totalStats.count}</p>
              </div>
              {totalStats.firstSeen && totalStats.lastSeen && (
                <div>
                  <p className="text-xs text-gray-500">时间范围</p>
                  <p className="text-sm font-medium flex items-center gap-1">
                    <Clock className="w-3 h-3" />
                    {formatDuration(totalStats.firstSeen, totalStats.lastSeen)}
                  </p>
                </div>
              )}
              <div>
                <p className="text-xs text-gray-500">总上传</p>
                <p className="text-lg font-bold">{formatBytes(totalStats.upload)}</p>
              </div>
              <div>
                <p className="text-xs text-gray-500">总下载</p>
                <p className="text-lg font-bold">{formatBytes(totalStats.download)}</p>
              </div>
              <div>
                <p className="text-xs text-gray-500">总计</p>
                <p className="text-lg font-bold text-secondary">{formatBytes(totalStats.total)}</p>
              </div>
            </div>
          )}

          {/* 表格 */}
          {sortedEntries.length === 0 ? (
            <p className="text-center text-gray-500 py-4">暂无数据用量记录</p>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-gray-200 dark:border-gray-700">
                    <th className="text-left py-2 px-2">
                      <button
                        className="flex items-center gap-1 hover:text-primary"
                        onClick={() => handleSort('ip')}
                      >
                        IP 地址 <SortIcon field="ip" />
                      </button>
                    </th>
                    <th className="text-left py-2 px-2">
                      <button
                        className="flex items-center gap-1 hover:text-primary"
                        onClick={() => handleSort('duration')}
                      >
                        时长 <SortIcon field="duration" />
                      </button>
                    </th>
                    <th className="text-right py-2 px-2">
                      <button
                        className="flex items-center gap-1 hover:text-primary ml-auto"
                        onClick={() => handleSort('upload')}
                      >
                        上传 <SortIcon field="upload" />
                      </button>
                    </th>
                    <th className="text-right py-2 px-2">
                      <button
                        className="flex items-center gap-1 hover:text-primary ml-auto"
                        onClick={() => handleSort('download')}
                      >
                        下载 <SortIcon field="download" />
                      </button>
                    </th>
                    <th className="text-right py-2 px-2">
                      <button
                        className="flex items-center gap-1 hover:text-primary ml-auto"
                        onClick={() => handleSort('total')}
                      >
                        总计 <SortIcon field="total" />
                      </button>
                    </th>
                    <th className="w-10"></th>
                  </tr>
                </thead>
                <tbody>
                  {sortedEntries.map((entry) => (
                    <tr
                      key={entry.sourceIP}
                      className="border-b border-gray-100 dark:border-gray-800 hover:bg-gray-50 dark:hover:bg-gray-800/50"
                    >
                      <td className="py-2 px-2 font-mono text-xs">{entry.sourceIP}</td>
                      <td className="py-2 px-2 text-gray-500 text-xs">
                        <span className="flex items-center gap-1">
                          <Clock className="w-3 h-3" />
                          {formatDuration(entry.firstSeen, entry.lastSeen)}
                        </span>
                      </td>
                      <td className="py-2 px-2 text-right">
                        <span className="flex items-center justify-end gap-1 text-green-600">
                          <Upload className="w-3 h-3" />
                          {formatBytes(entry.upload)}
                        </span>
                      </td>
                      <td className="py-2 px-2 text-right">
                        <span className="flex items-center justify-end gap-1 text-blue-600">
                          <Download className="w-3 h-3" />
                          {formatBytes(entry.download)}
                        </span>
                      </td>
                      <td className="py-2 px-2 text-right font-bold text-primary">
                        {formatBytes(entry.total)}
                      </td>
                      <td className="py-2 px-2">
                        <Button
                          isIconOnly
                          size="sm"
                          variant="light"
                          color="danger"
                          onPress={() => handleRemoveEntry(entry.sourceIP)}
                        >
                          <Trash2 className="w-3 h-3" />
                        </Button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </CardBody>
      )}
    </Card>
  );
}
