import React, { useState, useMemo, useCallback, useEffect } from 'react';
import {
  Card, CardBody, Button, Input, Chip,
  Modal, ModalContent, ModalHeader, ModalBody, ModalFooter, Pagination
} from '@nextui-org/react';
import {
  Search, X, Pause, Play, Trash2, ChevronDown, ChevronUp,
  ArrowUpDown, RefreshCw, Upload, Download, Server, Globe
} from 'lucide-react';
import byteSize from 'byte-size';
import { useClashConnections, type Connection } from '../hooks/useClashConnections';
import { useStore } from '../store';

type SortField = 'time' | 'download' | 'upload' | 'dlSpeed' | 'ulSpeed' | 'host' | 'sourceIP' | 'type' | 'rule';
type SortOrder = 'asc' | 'desc';
type GroupField = 'none' | 'type' | 'sourceIP' | 'host' | 'rule' | 'chains';

const formatBytes = (bytes: number) => byteSize(bytes).toString();

const formatTime = (isoString: string) => {
  const date = new Date(isoString);
  const now = new Date();
  const diff = now.getTime() - date.getTime();
  const seconds = Math.floor(diff / 1000);
  const minutes = Math.floor(seconds / 60);
  const hours = Math.floor(minutes / 60);

  if (hours > 0) return `${hours}h${minutes % 60}m`;
  if (minutes > 0) return `${minutes}m${seconds % 60}s`;
  return `${seconds}s`;
};

const getConnectionType = (conn: Connection) => `${conn.metadata.type}(${conn.metadata.network})`;
const getConnectionChains = (conn: Connection) => conn.chains.slice().reverse().join(' → ');

export default function Connections() {
  const { connections, downloadTotal, uploadTotal, isConnected, reconnect } = useClashConnections();
  const settings = useStore(state => state.settings);
  const fetchSettings = useStore(state => state.fetchSettings);

  // 确保 settings 已加载
  useEffect(() => {
    if (!settings) {
      fetchSettings();
    }
  }, [settings, fetchSettings]);

  const [activeTab, setActiveTab] = useState<'active' | 'closed'>('active');
  const [searchQuery, setSearchQuery] = useState('');
  const [sortField, setSortField] = useState<SortField>('time');
  const [sortOrder, setSortOrder] = useState<SortOrder>('desc');
  const [isPaused, setIsPaused] = useState(false);
  const [pausedConnections, setPausedConnections] = useState<Connection[]>([]);
  const [selectedConnection, setSelectedConnection] = useState<Connection | null>(null);
  const [closedConnections, setClosedConnections] = useState<Connection[]>([]);
  
  // 新增筛选状态
  const [sourceIPFilter, setSourceIPFilter] = useState<string>('');
  const [groupField, setGroupField] = useState<GroupField>('none');
  const [expandedGroups, setExpandedGroups] = useState<Set<string>>(new Set());
  const [currentPage, setCurrentPage] = useState(1);
  const [pageSize] = useState(50);

  // 当连接消失时，添加到已关闭列表
  useMemo(() => {
    if (isPaused) return;
    
    const currentIds = new Set(connections.map(c => c.id));
    const closed = pausedConnections.filter(c => !currentIds.has(c.id));
    
    if (closed.length > 0) {
      setClosedConnections(prev => [...closed, ...prev].slice(0, 100));
    }
    setPausedConnections(connections);
  }, [connections, isPaused]);

  // 当前显示的连接
  const currentConnections = useMemo(() => {
    if (isPaused) return pausedConnections;
    return connections;
  }, [isPaused, pausedConnections, connections]);

  // 获取唯一的来源 IP 列表
  const uniqueSourceIPs = useMemo(() => {
    const allConns = [...currentConnections, ...closedConnections];
    const ips = new Set(allConns.map(c => c.metadata.sourceIP).filter(Boolean));
    return Array.from(ips).sort();
  }, [currentConnections, closedConnections]);

  // 过滤和排序
  const filteredConnections = useMemo(() => {
    let result = activeTab === 'active' ? currentConnections : closedConnections;

    // 来源 IP 筛选
    if (sourceIPFilter) {
      result = result.filter(conn => conn.metadata.sourceIP === sourceIPFilter);
    }

    // 搜索筛选
    if (searchQuery) {
      const query = searchQuery.toLowerCase();
      result = result.filter(conn =>
        conn.metadata.host?.toLowerCase().includes(query) ||
        conn.metadata.sourceIP?.toLowerCase().includes(query) ||
        conn.metadata.destinationIP?.toLowerCase().includes(query) ||
        conn.chains.some(c => c.toLowerCase().includes(query)) ||
        conn.rule?.toLowerCase().includes(query) ||
        conn.metadata.processPath?.toLowerCase().includes(query)
      );
    }

    // 排序
    return [...result].sort((a, b) => {
      let comparison = 0;
      switch (sortField) {
        case 'time':
          comparison = new Date(a.start).getTime() - new Date(b.start).getTime();
          break;
        case 'download':
          comparison = a.download - b.download;
          break;
        case 'upload':
          comparison = a.upload - b.upload;
          break;
        case 'dlSpeed':
          comparison = ((a as any).downloadSpeed || 0) - ((b as any).downloadSpeed || 0);
          break;
        case 'ulSpeed':
          comparison = ((a as any).uploadSpeed || 0) - ((b as any).uploadSpeed || 0);
          break;
        case 'host':
          comparison = (a.metadata.host || '').localeCompare(b.metadata.host || '');
          break;
        case 'sourceIP':
          comparison = (a.metadata.sourceIP || '').localeCompare(b.metadata.sourceIP || '');
          break;
        case 'type':
          comparison = getConnectionType(a).localeCompare(getConnectionType(b));
          break;
        case 'rule':
          comparison = (a.rule || '').localeCompare(b.rule || '');
          break;
      }
      return sortOrder === 'asc' ? comparison : -comparison;
    });
  }, [currentConnections, closedConnections, activeTab, searchQuery, sortField, sortOrder, sourceIPFilter]);

  // 分组数据
  const groupedData = useMemo(() => {
    if (groupField === 'none') return null;

    const groups = new Map<string, Connection[]>();
    
    filteredConnections.forEach(conn => {
      let key: string;
      switch (groupField) {
        case 'type':
          key = getConnectionType(conn);
          break;
        case 'sourceIP':
          key = conn.metadata.sourceIP || 'Unknown';
          break;
        case 'host':
          key = conn.metadata.host || conn.metadata.destinationIP || 'Unknown';
          break;
        case 'rule':
          key = conn.rule || 'Unknown';
          break;
        case 'chains':
          key = getConnectionChains(conn);
          break;
        default:
          key = 'Unknown';
      }
      
      if (!groups.has(key)) {
        groups.set(key, []);
      }
      groups.get(key)!.push(conn);
    });

    return Array.from(groups.entries()).sort((a, b) => b[1].length - a[1].length);
  }, [filteredConnections, groupField]);

  // 分页数据
  const paginatedConnections = useMemo(() => {
    if (groupField !== 'none') return filteredConnections;
    const start = (currentPage - 1) * pageSize;
    return filteredConnections.slice(start, start + pageSize);
  }, [filteredConnections, currentPage, pageSize, groupField]);

  const totalPages = Math.ceil(filteredConnections.length / pageSize);

  // 重置页码当筛选条件变化时
  useMemo(() => {
    setCurrentPage(1);
  }, [activeTab, searchQuery, sourceIPFilter]);

  // 切换排序
  const handleSort = useCallback((field: SortField) => {
    if (sortField === field) {
      setSortOrder(prev => prev === 'asc' ? 'desc' : 'asc');
    } else {
      setSortField(field);
      setSortOrder('desc');
    }
  }, [sortField]);

  // 暂停/继续
  const togglePause = useCallback(() => {
    if (!isPaused) {
      setPausedConnections(connections);
    }
    setIsPaused(prev => !prev);
  }, [isPaused, connections]);

  // 切换分组展开
  const toggleGroupExpand = useCallback((key: string) => {
    setExpandedGroups(prev => {
      const next = new Set(prev);
      if (next.has(key)) {
        next.delete(key);
      } else {
        next.add(key);
      }
      return next;
    });
  }, []);

  // 关闭连接
  const closeConnection = useCallback(async (id: string) => {
    if (!settings) return;
    
    const port = settings.clash_api_port || 9091;
    const secret = settings.clash_api_secret || '';
    
    try {
      const headers: Record<string, string> = { 'Content-Type': 'application/json' };
      if (secret) headers['Authorization'] = `Bearer ${secret}`;
      
      await fetch(`http://127.0.0.1:${port}/connections/${id}`, {
        method: 'DELETE',
        headers,
      });
    } catch (e) {
      console.error('Failed to close connection:', e);
    }
  }, [settings]);

  // 关闭所有连接
  const closeAllConnections = useCallback(async () => {
    if (!settings) return;
    if (!confirm('确定要关闭所有连接吗？')) return;
    
    const port = settings.clash_api_port || 9091;
    const secret = settings.clash_api_secret || '';
    
    try {
      const headers: Record<string, string> = { 'Content-Type': 'application/json' };
      if (secret) headers['Authorization'] = `Bearer ${secret}`;
      
      await fetch(`http://127.0.0.1:${port}/connections`, {
        method: 'DELETE',
        headers,
      });
    } catch (e) {
      console.error('Failed to close all connections:', e);
    }
  }, [settings]);

  const clearClosedConnections = useCallback(() => {
    setClosedConnections([]);
  }, []);

  const SortIcon = ({ field }: { field: SortField }) => {
    if (sortField !== field) return <ArrowUpDown className="w-3 h-3 opacity-50" />;
    return sortOrder === 'asc' ? <ChevronUp className="w-3 h-3" /> : <ChevronDown className="w-3 h-3" />;
  };

  // 渲染连接行
  const renderConnectionRow = (conn: Connection, showClose = true) => (
    <tr
      key={conn.id}
      className="border-b border-gray-100 dark:border-gray-800 hover:bg-gray-50 dark:hover:bg-gray-800/50 cursor-pointer"
      onClick={() => setSelectedConnection(conn)}
    >
      <td className="py-2 px-3">
        <div className="flex flex-col">
          <span className="font-medium truncate max-w-[180px]" title={conn.metadata.host || conn.metadata.destinationIP}>
            {conn.metadata.host || conn.metadata.destinationIP}
          </span>
          <span className="text-xs text-gray-500">
            {conn.metadata.sourceIP}:{conn.metadata.sourcePort}
          </span>
        </div>
      </td>
      <td className="py-2 px-3 hidden md:table-cell">
        <Chip size="sm" variant="flat" className="text-xs">
          {conn.metadata.type}({conn.metadata.network})
        </Chip>
      </td>
      <td className="py-2 px-3 hidden lg:table-cell">
        <span className="text-gray-600 dark:text-gray-400 truncate max-w-[120px] block text-xs" title={conn.rule}>
          {conn.rule}
        </span>
      </td>
      <td className="py-2 px-3 hidden xl:table-cell">
        <span className="text-xs text-gray-500 truncate max-w-[150px] block" title={getConnectionChains(conn)}>
          {getConnectionChains(conn)}
        </span>
      </td>
      <td className="py-2 px-3 text-right">
        <span className="text-blue-600 text-sm">{formatBytes(conn.download)}</span>
      </td>
      <td className="py-2 px-3 text-right">
        <span className="text-green-600 text-sm">{formatBytes(conn.upload)}</span>
      </td>
      <td className="py-2 px-3 text-right hidden sm:table-cell text-gray-500 text-xs">
        {formatTime(conn.start)}
      </td>
      {showClose && activeTab === 'active' && (
        <td className="py-2 px-3">
          <Button
            isIconOnly
            size="sm"
            variant="light"
            color="danger"
            onPress={() => closeConnection(conn.id)}
          >
            <X className="w-3 h-3" />
          </Button>
        </td>
      )}
    </tr>
  );

  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold text-gray-800 dark:text-white">连接</h1>
        <div className="flex items-center gap-2">
          {!isConnected && (
            <Button size="sm" color="warning" variant="flat" startContent={<RefreshCw className="w-4 h-4" />} onPress={reconnect}>
              重连
            </Button>
          )}
          <Chip size="sm" color={isConnected ? 'success' : 'danger'} variant="flat">
            {isConnected ? '已连接' : '未连接'}
          </Chip>
        </div>
      </div>

      {/* 流量统计 */}
      <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
        <Card>
          <CardBody className="flex flex-row items-center gap-2 py-2 px-3">
            <div className="p-1.5 bg-blue-100 dark:bg-blue-900 rounded">
              <Download className="w-4 h-4 text-blue-600 dark:text-blue-300" />
            </div>
            <div>
              <p className="text-xs text-gray-500">下载</p>
              <p className="font-bold">{formatBytes(downloadTotal)}</p>
            </div>
          </CardBody>
        </Card>
        <Card>
          <CardBody className="flex flex-row items-center gap-2 py-2 px-3">
            <div className="p-1.5 bg-green-100 dark:bg-green-900 rounded">
              <Upload className="w-4 h-4 text-green-600 dark:text-green-300" />
            </div>
            <div>
              <p className="text-xs text-gray-500">上传</p>
              <p className="font-bold">{formatBytes(uploadTotal)}</p>
            </div>
          </CardBody>
        </Card>
        <Card>
          <CardBody className="flex flex-row items-center gap-2 py-2 px-3">
            <div className="p-1.5 bg-purple-100 dark:bg-purple-900 rounded">
              <Globe className="w-4 h-4 text-purple-600 dark:text-purple-300" />
            </div>
            <div>
              <p className="text-xs text-gray-500">活跃</p>
              <p className="font-bold">{connections.length}</p>
            </div>
          </CardBody>
        </Card>
        <Card>
          <CardBody className="flex flex-row items-center gap-2 py-2 px-3">
            <div className="p-1.5 bg-orange-100 dark:bg-orange-900 rounded">
              <Server className="w-4 h-4 text-orange-600 dark:text-orange-300" />
            </div>
            <div>
              <p className="text-xs text-gray-500">已关闭</p>
              <p className="font-bold">{closedConnections.length}</p>
            </div>
          </CardBody>
        </Card>
      </div>

      {/* 工具栏 */}
      <Card>
        <CardBody className="py-3 px-4">
          <div className="flex items-center gap-3 flex-wrap">
            {/* Tab 切换 */}
            <div className="inline-flex bg-default-100 rounded-lg p-1">
              <button
                className={`px-3 py-1.5 text-xs rounded-md transition-all ${activeTab === 'active' ? 'bg-white dark:bg-default-50 shadow-small font-medium' : 'text-default-500 hover:text-default-700'}`}
                onClick={() => setActiveTab('active')}
              >
                活跃 {currentConnections.length}
              </button>
              <button
                className={`px-3 py-1.5 text-xs rounded-md transition-all ${activeTab === 'closed' ? 'bg-white dark:bg-default-50 shadow-small font-medium' : 'text-default-500 hover:text-default-700'}`}
                onClick={() => setActiveTab('closed')}
              >
                已关闭 {closedConnections.length}
              </button>
            </div>

            {/* 来源 IP 筛选 */}
            {uniqueSourceIPs.length > 0 && (
              <select
                value={sourceIPFilter}
                onChange={(e) => setSourceIPFilter(e.target.value)}
                className="h-8 px-2 text-xs bg-default-100 border-0 rounded-lg outline-none cursor-pointer"
              >
                <option value="">全部来源</option>
                {uniqueSourceIPs.map(ip => (
                  <option key={ip} value={ip}>{ip}</option>
                ))}
              </select>
            )}

            {/* 分组 */}
            <select
              value={groupField}
              onChange={(e) => {
                setGroupField(e.target.value as GroupField);
                setExpandedGroups(new Set());
              }}
              className="h-8 px-2 text-xs bg-default-100 border-0 rounded-lg outline-none cursor-pointer"
            >
              <option value="none">不分组</option>
              <option value="type">按类型</option>
              <option value="sourceIP">按来源</option>
              <option value="host">按主机</option>
              <option value="rule">按规则</option>
              <option value="chains">按链路</option>
            </select>

            {/* 搜索 */}
            <Input
              placeholder="搜索主机、IP、规则..."
              value={searchQuery}
              onValueChange={setSearchQuery}
              startContent={<Search className="w-4 h-4 text-default-400" />}
              isClearable
              onClear={() => setSearchQuery('')}
              size="sm"
              className="w-48 md:w-64"
              classNames={{ inputWrapper: "h-8" }}
            />

            {/* 操作按钮 */}
            <div className="flex gap-1 ml-auto">
              <Button
                size="sm"
                variant={isPaused ? 'solid' : 'flat'}
                color={isPaused ? 'warning' : 'default'}
                isIconOnly
                onPress={togglePause}
              >
                {isPaused ? <Play className="w-4 h-4" /> : <Pause className="w-4 h-4" />}
              </Button>
              <Button
                size="sm"
                color="danger"
                variant="flat"
                isIconOnly
                onPress={activeTab === 'active' ? closeAllConnections : clearClosedConnections}
              >
                <Trash2 className="w-4 h-4" />
              </Button>
            </div>
          </div>
        </CardBody>
      </Card>

      {/* 连接列表 */}
      <Card>
        <CardBody className="p-0">
          <div className="overflow-x-auto max-h-[calc(100vh-380px)]">
            <table className="w-full text-sm">
              <thead className="sticky top-0 z-10">
                <tr className="border-b border-gray-200 dark:border-gray-700 bg-gray-50 dark:bg-gray-800">
                  <th className="text-left py-2 px-3">
                    <button className="flex items-center gap-1 hover:text-primary text-xs" onClick={() => handleSort('host')}>
                      主机 <SortIcon field="host" />
                    </button>
                  </th>
                  <th className="text-left py-2 px-3 hidden md:table-cell text-xs">类型</th>
                  <th className="text-left py-2 px-3 hidden lg:table-cell text-xs">规则</th>
                  <th className="text-left py-2 px-3 hidden xl:table-cell text-xs">链路</th>
                  <th className="text-right py-2 px-3">
                    <button className="flex items-center gap-1 hover:text-primary ml-auto text-xs" onClick={() => handleSort('download')}>
                      下载 <SortIcon field="download" />
                    </button>
                  </th>
                  <th className="text-right py-2 px-3">
                    <button className="flex items-center gap-1 hover:text-primary ml-auto text-xs" onClick={() => handleSort('upload')}>
                      上传 <SortIcon field="upload" />
                    </button>
                  </th>
                  <th className="text-right py-2 px-3 hidden sm:table-cell">
                    <button className="flex items-center gap-1 hover:text-primary ml-auto text-xs" onClick={() => handleSort('time')}>
                      时间 <SortIcon field="time" />
                    </button>
                  </th>
                  {activeTab === 'active' && <th className="w-8"></th>}
                </tr>
              </thead>
              <tbody>
                {filteredConnections.length === 0 ? (
                  <tr>
                    <td colSpan={8} className="text-center py-8 text-gray-500">
                      暂无连接
                    </td>
                  </tr>
                ) : groupField !== 'none' && groupedData ? (
                  // 分组显示
                  groupedData.map(([groupKey, conns]) => (
                    <React.Fragment key={groupKey}>
                      <tr
                        className="bg-gray-100 dark:bg-gray-800 cursor-pointer hover:bg-gray-200 dark:hover:bg-gray-700"
                        onClick={() => toggleGroupExpand(groupKey)}
                      >
                        <td colSpan={activeTab === 'active' ? 8 : 7} className="py-2 px-3">
                          <div className="flex items-center gap-2">
                            {expandedGroups.has(groupKey) ? (
                              <ChevronDown className="w-4 h-4" />
                            ) : (
                              <ChevronUp className="w-4 h-4 rotate-90" />
                            )}
                            <span className="font-medium text-primary">{groupKey}</span>
                            <Chip size="sm" variant="flat">{conns.length}</Chip>
                          </div>
                        </td>
                      </tr>
                      {expandedGroups.has(groupKey) && conns.map(conn => renderConnectionRow(conn))}
                    </React.Fragment>
                  ))
                ) : (
                  // 普通列表显示
                  paginatedConnections.map(conn => renderConnectionRow(conn))
                )}
              </tbody>
            </table>
          </div>
        </CardBody>
      </Card>

      {/* 分页 */}
      {groupField === 'none' && totalPages > 1 && (
        <div className="flex justify-center">
          <Pagination
            total={totalPages}
            page={currentPage}
            onChange={setCurrentPage}
            size="sm"
            showControls
          />
        </div>
      )}

      {/* 连接详情模态框 */}
      <Modal isOpen={!!selectedConnection} onClose={() => setSelectedConnection(null)} size="2xl">
        <ModalContent>
          {selectedConnection && (
            <>
              <ModalHeader className="text-base">连接详情</ModalHeader>
              <ModalBody>
                <div className="grid grid-cols-2 gap-3 text-sm">
                  <div>
                    <p className="text-xs text-gray-500 mb-0.5">主机</p>
                    <p className="font-medium break-all">{selectedConnection.metadata.host || '-'}</p>
                  </div>
                  <div>
                    <p className="text-xs text-gray-500 mb-0.5">目标 IP</p>
                    <p className="font-medium">{selectedConnection.metadata.destinationIP}:{selectedConnection.metadata.destinationPort}</p>
                  </div>
                  <div>
                    <p className="text-xs text-gray-500 mb-0.5">来源 IP</p>
                    <p className="font-medium">{selectedConnection.metadata.sourceIP}:{selectedConnection.metadata.sourcePort}</p>
                  </div>
                  <div>
                    <p className="text-xs text-gray-500 mb-0.5">类型</p>
                    <p className="font-medium">{selectedConnection.metadata.type} ({selectedConnection.metadata.network})</p>
                  </div>
                  <div>
                    <p className="text-xs text-gray-500 mb-0.5">规则</p>
                    <p className="font-medium">{selectedConnection.rule}</p>
                  </div>
                  <div>
                    <p className="text-xs text-gray-500 mb-0.5">规则载荷</p>
                    <p className="font-medium">{selectedConnection.rulePayload || '-'}</p>
                  </div>
                  <div>
                    <p className="text-xs text-gray-500 mb-0.5">下载量</p>
                    <p className="font-medium text-blue-600">{formatBytes(selectedConnection.download)}</p>
                  </div>
                  <div>
                    <p className="text-xs text-gray-500 mb-0.5">上传量</p>
                    <p className="font-medium text-green-600">{formatBytes(selectedConnection.upload)}</p>
                  </div>
                  {selectedConnection.metadata.processPath && (
                    <div className="col-span-2">
                      <p className="text-xs text-gray-500 mb-0.5">进程</p>
                      <p className="font-medium">{selectedConnection.metadata.processPath}</p>
                    </div>
                  )}
                  <div className="col-span-2">
                    <p className="text-xs text-gray-500 mb-0.5">代理链路</p>
                    <p className="font-medium">{getConnectionChains(selectedConnection)}</p>
                  </div>
                  <div className="col-span-2">
                    <p className="text-xs text-gray-500 mb-0.5">连接时间</p>
                    <p className="font-medium">{new Date(selectedConnection.start).toLocaleString()}</p>
                  </div>
                </div>
              </ModalBody>
              <ModalFooter>
                {activeTab === 'active' && (
                  <Button
                    color="danger"
                    variant="flat"
                    size="sm"
                    onPress={() => {
                      closeConnection(selectedConnection.id);
                      setSelectedConnection(null);
                    }}
                  >
                    关闭连接
                  </Button>
                )}
                <Button color="primary" size="sm" onPress={() => setSelectedConnection(null)}>
                  确定
                </Button>
              </ModalFooter>
            </>
          )}
        </ModalContent>
      </Modal>
    </div>
  );
}
