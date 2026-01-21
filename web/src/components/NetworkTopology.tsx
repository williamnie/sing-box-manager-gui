import { useEffect, useRef, useState, useMemo, useCallback } from 'react';
import * as d3 from 'd3';
import byteSize from 'byte-size';
import { useClashConnections, type Connection } from '../hooks/useClashConnections';
import { Chip, Button } from '@nextui-org/react';
import { Play, Pause, RefreshCw, Monitor, Server, Filter, Hash, Network } from 'lucide-react';

// 节点类型
type NodeType = 'root' | 'client' | 'port' | 'rule' | 'group' | 'proxy';

interface TopologyNodeData {
  id: string;
  name: string;
  type: NodeType;
  connections: number;
  traffic: number;
  children?: TopologyNodeData[];
  _children?: TopologyNodeData[];
  collapsed?: boolean;
}

// 主题颜色
const colors = {
  root: { fill: '#6b7280', bg: 'rgba(107, 114, 128, 0.15)' },
  client: { fill: '#3b82f6', bg: 'rgba(59, 130, 246, 0.15)' },
  port: { fill: '#f59e0b', bg: 'rgba(245, 158, 11, 0.15)' },
  rule: { fill: '#8b5cf6', bg: 'rgba(139, 92, 246, 0.15)' },
  group: { fill: '#10b981', bg: 'rgba(16, 185, 129, 0.15)' },
  proxy: { fill: '#06b6d4', bg: 'rgba(6, 182, 212, 0.15)' },
};

export default function NetworkTopology() {
  const { connections, isConnected, error, reconnect } = useClashConnections();
  const svgRef = useRef<SVGSVGElement | null>(null);
  const containerRef = useRef<HTMLDivElement | null>(null);
  const [isPaused, setIsPaused] = useState(false);
  const [frozenConnections, setFrozenConnections] = useState<Connection[] | null>(null);
  const [collapsedNodes, setCollapsedNodes] = useState<Set<string>>(new Set());

  // 当前显示的连接数据
  const currentConnections = useMemo(() => {
    if (isPaused && frozenConnections) {
      return frozenConnections;
    }
    return connections;
  }, [isPaused, frozenConnections, connections]);

  // 构建层级数据
  const hierarchyData = useMemo<TopologyNodeData>(() => {
    const groupsMap = new Map<string, {
      data: TopologyNodeData;
      proxies: Map<string, {
        data: TopologyNodeData;
        rules: Map<string, {
          data: TopologyNodeData;
          clients: Map<string, {
            data: TopologyNodeData;
            ports: Map<string, TopologyNodeData>;
          }>;
        }>;
      }>;
    }>();

    currentConnections.forEach((conn) => {
      const clientIP = conn.metadata.sourceIP || 'Unknown';
      const sourcePort = conn.metadata.sourcePort || 'Unknown';
      const ruleType = conn.rule || 'Direct';
      const rulePayload = conn.rulePayload;
      const fullRule = rulePayload ? `${ruleType}: ${rulePayload}` : ruleType;
      const chains = conn.chains || [];
      const proxy = chains[0] ?? 'Direct';
      const group = chains.length > 1 ? (chains[1] ?? 'Direct') : (chains[0] ?? 'Direct');
      const traffic = conn.download + conn.upload;

      // Group
      if (!groupsMap.has(group)) {
        groupsMap.set(group, {
          data: { id: `group-${group}`, name: group, type: 'group', connections: 0, traffic: 0 },
          proxies: new Map(),
        });
      }
      const groupEntry = groupsMap.get(group)!;
      groupEntry.data.connections++;
      groupEntry.data.traffic += traffic;

      // Proxy
      if (!groupEntry.proxies.has(proxy)) {
        groupEntry.proxies.set(proxy, {
          data: { id: `proxy-${group}-${proxy}`, name: proxy, type: 'proxy', connections: 0, traffic: 0 },
          rules: new Map(),
        });
      }
      const proxyEntry = groupEntry.proxies.get(proxy)!;
      proxyEntry.data.connections++;
      proxyEntry.data.traffic += traffic;

      // Rule
      if (!proxyEntry.rules.has(fullRule)) {
        proxyEntry.rules.set(fullRule, {
          data: { id: `rule-${group}-${proxy}-${fullRule}`, name: fullRule, type: 'rule', connections: 0, traffic: 0 },
          clients: new Map(),
        });
      }
      const ruleEntry = proxyEntry.rules.get(fullRule)!;
      ruleEntry.data.connections++;
      ruleEntry.data.traffic += traffic;

      // Client
      if (!ruleEntry.clients.has(clientIP)) {
        ruleEntry.clients.set(clientIP, {
          data: { id: `client-${group}-${proxy}-${fullRule}-${clientIP}`, name: clientIP, type: 'client', connections: 0, traffic: 0 },
          ports: new Map(),
        });
      }
      const clientEntry = ruleEntry.clients.get(clientIP)!;
      clientEntry.data.connections++;
      clientEntry.data.traffic += traffic;

      // Port
      if (!clientEntry.ports.has(sourcePort)) {
        clientEntry.ports.set(sourcePort, {
          id: `port-${group}-${proxy}-${fullRule}-${clientIP}-${sourcePort}`,
          name: sourcePort,
          type: 'port',
          connections: 0,
          traffic: 0,
        });
      }
      const portNode = clientEntry.ports.get(sourcePort)!;
      portNode.connections++;
      portNode.traffic += traffic;
    });

    // 转换为层级结构
    const rootChildren: TopologyNodeData[] = [];

    groupsMap.forEach((groupEntry) => {
      const groupNode: TopologyNodeData = { ...groupEntry.data, children: [] };

      groupEntry.proxies.forEach((proxyEntry) => {
        const proxyNode: TopologyNodeData = { ...proxyEntry.data, children: [] };

        proxyEntry.rules.forEach((ruleEntry) => {
          const ruleNode: TopologyNodeData = { ...ruleEntry.data, children: [] };

          ruleEntry.clients.forEach((clientEntry) => {
            const portChildren = Array.from(clientEntry.ports.values());
            const isClientCollapsed = !collapsedNodes.has(`expanded-${clientEntry.data.id}`);
            const clientNode: TopologyNodeData = isClientCollapsed
              ? { ...clientEntry.data, _children: portChildren, collapsed: true }
              : { ...clientEntry.data, children: portChildren, collapsed: false };
            ruleNode.children!.push(clientNode);
          });

          // Rule 默认折叠
          const isRuleCollapsed = !collapsedNodes.has(`expanded-${ruleEntry.data.id}`);
          if (isRuleCollapsed && ruleNode.children!.length > 0) {
            ruleNode._children = ruleNode.children;
            ruleNode.children = undefined;
            ruleNode.collapsed = true;
          }
          proxyNode.children!.push(ruleNode);
        });

        groupNode.children!.push(proxyNode);
      });

      rootChildren.push(groupNode);
    });

    return {
      id: 'root',
      name: 'Connections',
      type: 'root',
      connections: currentConnections.length,
      traffic: currentConnections.reduce((sum, c) => sum + c.download + c.upload, 0),
      children: rootChildren,
    };
  }, [currentConnections, collapsedNodes]);

  // 统计数据
  const stats = useMemo(() => {
    const clientSet = new Set<string>();
    const ruleSet = new Set<string>();
    const groupSet = new Set<string>();
    const proxySet = new Set<string>();

    currentConnections.forEach((conn) => {
      clientSet.add(conn.metadata.sourceIP || 'Unknown');
      ruleSet.add(conn.rule || 'Direct');
      const chains = conn.chains || [];
      proxySet.add(chains[0] ?? 'Direct');
      groupSet.add(chains.length > 1 ? (chains[1] ?? 'Direct') : (chains[0] ?? 'Direct'));
    });

    return {
      clientCount: clientSet.size,
      ruleCount: ruleSet.size,
      groupCount: groupSet.size,
      proxyCount: proxySet.size,
      totalTraffic: currentConnections.reduce((sum, c) => sum + c.download + c.upload, 0),
    };
  }, [currentConnections]);

  // 切换节点折叠状态
  const toggleCollapse = useCallback((nodeId: string, isCurrentlyCollapsed: boolean) => {
    const expandedKey = `expanded-${nodeId}`;
    setCollapsedNodes(prev => {
      const next = new Set(prev);
      if (isCurrentlyCollapsed) {
        if (nodeId.startsWith('rule-') || nodeId.startsWith('client-')) {
          next.add(expandedKey);
        } else {
          next.delete(nodeId);
        }
      } else {
        if (nodeId.startsWith('rule-') || nodeId.startsWith('client-')) {
          next.delete(expandedKey);
        } else {
          next.add(nodeId);
        }
      }
      return next;
    });
  }, []);

  // 暂停/恢复
  const togglePause = useCallback(() => {
    if (isPaused) {
      setFrozenConnections(null);
    } else {
      setFrozenConnections([...connections]);
    }
    setIsPaused(!isPaused);
  }, [isPaused, connections]);

  // 渲染树
  useEffect(() => {
    if (!svgRef.current || !containerRef.current) return;

    const data = hierarchyData;
    if (!data.children || data.children.length === 0) {
      d3.select(svgRef.current).selectAll('*').remove();
      return;
    }

    d3.select(svgRef.current).selectAll('*').remove();

    const nodeHeight = 30;
    const nodePaddingX = 16;
    const nodeSpacingY = 50;
    const levelGap = 40;

    const root = d3.hierarchy(data);
    const containerWidth = containerRef.current.clientWidth;

    const treeLayout = d3.tree<TopologyNodeData>()
      .nodeSize([nodeSpacingY, 100])
      .separation((a, b) => (a.parent === b.parent ? 1 : 1.5));

    treeLayout(root);

    // 计算节点宽度
    const nodeWidths = new Map<string, number>();
    const measureCanvas = document.createElement('canvas');
    const ctx = measureCanvas.getContext('2d');
    
    root.descendants().forEach((d) => {
      if (d.data.type !== 'root') {
        let textWidth = d.data.name.length * 7;
        if (ctx) {
          ctx.font = '600 11px sans-serif';
          textWidth = ctx.measureText(d.data.name).width;
        }
        const hasChildren = (d.data.children && d.data.children.length > 0) || 
                           (d.data._children && d.data._children.length > 0);
        const indicatorSpace = hasChildren ? 20 : 0;
        nodeWidths.set(d.data.id, textWidth + nodePaddingX * 2 + indicatorSpace);
      }
    });

    const getNodeWidth = (d: d3.HierarchyNode<TopologyNodeData>): number => {
      return nodeWidths.get(d.data.id) ?? 80;
    };

    // 计算每层最大宽度
    const maxWidthPerLevel = new Map<number, number>();
    root.descendants().forEach((d) => {
      if (d.data.type !== 'root') {
        const width = getNodeWidth(d);
        const currentMax = maxWidthPerLevel.get(d.depth) ?? 0;
        maxWidthPerLevel.set(d.depth, Math.max(currentMax, width));
      }
    });

    // 计算层级 X 偏移
    const levelXOffset = new Map<number, number>();
    let cumulativeX = 0;
    for (let depth = 1; depth <= maxWidthPerLevel.size; depth++) {
      const currentLevelWidth = maxWidthPerLevel.get(depth) ?? 100;
      if (depth === 1) {
        cumulativeX = currentLevelWidth / 2;
      } else {
        const prevLevelWidth = maxWidthPerLevel.get(depth - 1) ?? 100;
        cumulativeX += prevLevelWidth / 2 + levelGap + currentLevelWidth / 2;
      }
      levelXOffset.set(depth, cumulativeX);
    }

    // 调整节点位置
    root.descendants().forEach((d) => {
      if (d.data.type !== 'root' && d.depth > 0) {
        d.y = levelXOffset.get(d.depth) ?? d.y;
      }
    });

    // 计算边界
    let minX = Infinity, maxX = -Infinity;
    root.each((d) => {
      const x = d.x ?? 0;
      if (x < minX) minX = x;
      if (x > maxX) maxX = x;
    });

    const topPadding = 25;
    const treeHeight = maxX - minX + nodeSpacingY;
    const actualHeight = Math.max(400, treeHeight + topPadding + 40);

    let maxY = 0;
    root.each((d) => {
      if (d.data.type !== 'root') {
        const nodeWidth = nodeWidths.get(d.data.id) ?? 80;
        const rightEdge = (d.y ?? 0) + nodeWidth / 2;
        if (rightEdge > maxY) maxY = rightEdge;
      }
    });

    const actualWidth = Math.max(containerWidth, maxY + 100);

    const svg = d3.select(svgRef.current)
      .attr('width', actualWidth)
      .attr('height', actualHeight);

    const g = svg.append('g')
      .attr('transform', `translate(60, ${-minX + nodeSpacingY / 2 + topPadding})`);

    // 绘制连线
    const visibleLinks = root.links().filter((link) => link.source.data.type !== 'root');

    g.selectAll('.link')
      .data(visibleLinks)
      .join('path')
      .attr('class', 'link')
      .attr('d', (d) => {
        const source = d.source as d3.HierarchyPointNode<TopologyNodeData>;
        const target = d.target as d3.HierarchyPointNode<TopologyNodeData>;
        const sourceWidth = getNodeWidth(source);
        const targetWidth = getNodeWidth(target);
        const sourceX = source.y + sourceWidth / 2;
        const sourceY = source.x;
        const targetX = target.y - targetWidth / 2;
        const targetY = target.x;
        const midX = (sourceX + targetX) / 2;
        return `M${sourceX},${sourceY} C${midX},${sourceY} ${midX},${targetY} ${targetX},${targetY}`;
      })
      .attr('fill', 'none')
      .attr('stroke', '#6b7280')
      .attr('stroke-opacity', 0.3)
      .attr('stroke-width', (d) => Math.max(1, Math.min(4, d.target.data.connections / 5)));

    // 绘制节点
    const nodes = g.selectAll('.node')
      .data(root.descendants().filter((d) => d.data.type !== 'root'))
      .join('g')
      .attr('class', 'node')
      .attr('transform', (d) => `translate(${d.y ?? 0},${d.x})`)
      .style('cursor', (d) => {
        const hasChildren = (d.data.children && d.data.children.length > 0) ||
                           (d.data._children && d.data._children.length > 0);
        return hasChildren ? 'pointer' : 'default';
      })
      .on('click', (_event, d) => {
        const hasChildren = (d.data.children && d.data.children.length > 0) ||
                           (d.data._children && d.data._children.length > 0);
        if (hasChildren) {
          toggleCollapse(d.data.id, d.data.collapsed ?? false);
        }
      });

    // 连接数徽章
    nodes.append('text')
      .attr('dy', -nodeHeight / 2 - 4)
      .attr('text-anchor', 'middle')
      .attr('fill', (d) => colors[d.data.type].fill)
      .attr('font-size', '10px')
      .attr('font-weight', '500')
      .text((d) => `${d.data.connections}`);

    // 节点矩形
    nodes.append('rect')
      .attr('x', (d) => -getNodeWidth(d) / 2)
      .attr('y', -nodeHeight / 2)
      .attr('width', (d) => getNodeWidth(d))
      .attr('height', nodeHeight)
      .attr('rx', 6)
      .attr('fill', (d) => colors[d.data.type].bg)
      .attr('stroke', (d) => colors[d.data.type].fill)
      .attr('stroke-width', 2);

    // 折叠指示器
    nodes.filter((d) => {
      const hasChildren = (d.data.children && d.data.children.length > 0) ||
                         (d.data._children && d.data._children.length > 0);
      return Boolean(hasChildren);
    })
      .append('text')
      .attr('x', (d) => getNodeWidth(d) / 2 - 12)
      .attr('dy', '0.35em')
      .attr('text-anchor', 'middle')
      .attr('fill', (d) => colors[d.data.type].fill)
      .attr('font-size', '14px')
      .attr('font-weight', '700')
      .text((d) => (d.data.collapsed ? '+' : '−'));

    // 节点标签
    nodes.append('text')
      .attr('dy', '0.31em')
      .attr('text-anchor', 'middle')
      .attr('fill', (d) => colors[d.data.type].fill)
      .attr('font-size', '11px')
      .attr('font-weight', '600')
      .text((d) => d.data.name);

    // 提示
    nodes.append('title')
      .text((d) => `${d.data.name}\n${d.data.connections} 连接\n${byteSize(d.data.traffic).toString()}`);

  }, [hierarchyData, toggleCollapse]);

  // 图例配置
  const legendItems = [
    { type: 'group' as NodeType, label: '代理组', Icon: Network },
    { type: 'proxy' as NodeType, label: '代理节点', Icon: Server },
    { type: 'rule' as NodeType, label: '规则', Icon: Filter },
    { type: 'client' as NodeType, label: '源 IP', Icon: Monitor },
    { type: 'port' as NodeType, label: '源端口', Icon: Hash },
  ];

  if (!isConnected && error) {
    return (
      <div className="flex flex-col items-center justify-center py-8 text-gray-500">
        <RefreshCw className="w-8 h-8 mb-2 animate-pulse" />
        <span>{error}</span>
        <Button size="sm" className="mt-2" onPress={reconnect}>重新连接</Button>
      </div>
    );
  }

  return (
    <div>
      {/* 控制栏 */}
      <div className="mb-4 flex items-center justify-between">
        <div className="flex items-center gap-2">
          <Button
            isIconOnly
            size="sm"
            variant="light"
            className={isPaused ? 'text-warning' : ''}
            onPress={togglePause}
          >
            {isPaused ? <Play className="w-4 h-4" /> : <Pause className="w-4 h-4" />}
          </Button>
          <Chip size="sm" color={isConnected ? 'success' : 'default'} variant="dot">
            {isConnected ? '已连接' : '未连接'}
          </Chip>
        </div>
        <div className="flex flex-wrap gap-2 text-sm text-gray-500">
          <span>{stats.clientCount} 客户端</span>
          <span>·</span>
          <span>{stats.ruleCount} 规则</span>
          <span>·</span>
          <span>{stats.groupCount} 代理组</span>
          <span>·</span>
          <span>{stats.proxyCount} 节点</span>
          <span>·</span>
          <span>{byteSize(stats.totalTraffic).toString()}</span>
        </div>
      </div>

      {/* 图例 */}
      <div className="mb-4 flex flex-wrap gap-4 text-xs">
        {legendItems.map(({ type, label, Icon }) => (
          <div key={type} className="flex items-center gap-1">
            <Icon className="w-4 h-4" style={{ color: colors[type].fill }} />
            <span>{label}</span>
          </div>
        ))}
      </div>

      {/* 无连接提示 */}
      {currentConnections.length === 0 ? (
        <div className="flex flex-col items-center justify-center py-8 text-gray-500">
          <RefreshCw className="w-8 h-8 mb-2 animate-pulse" />
          <span>等待连接数据...</span>
        </div>
      ) : (
        <div ref={containerRef} className="overflow-x-auto touch-pan-x touch-pan-y">
          <svg ref={svgRef} className="min-h-[400px]" />
        </div>
      )}
    </div>
  );
}
