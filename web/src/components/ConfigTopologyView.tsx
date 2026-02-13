import { useMemo, useState } from 'react';
import { Chip } from '@nextui-org/react';
import { AlertTriangle, ArrowRight } from 'lucide-react';
import type { ConfigTopologyGraph, TopologyEdge, TopologyNode, TopologyNodeKind } from '../features/config-topology/types';

const NODE_STYLE_MAP: Record<TopologyNodeKind, string> = {
  inbound: 'border-blue-300 bg-blue-50 text-blue-700 dark:border-blue-700 dark:bg-blue-900/20 dark:text-blue-300',
  route: 'border-purple-300 bg-purple-50 text-purple-700 dark:border-purple-700 dark:bg-purple-900/20 dark:text-purple-300',
  rule: 'border-fuchsia-300 bg-fuchsia-50 text-fuchsia-700 dark:border-fuchsia-700 dark:bg-fuchsia-900/20 dark:text-fuchsia-300',
  policy: 'border-violet-300 bg-violet-50 text-violet-700 dark:border-violet-700 dark:bg-violet-900/20 dark:text-violet-300',
  final: 'border-amber-300 bg-amber-50 text-amber-700 dark:border-amber-700 dark:bg-amber-900/20 dark:text-amber-300',
  outbound: 'border-green-300 bg-green-50 text-green-700 dark:border-green-700 dark:bg-green-900/20 dark:text-green-300',
  missing: 'border-danger-300 bg-danger-50 text-danger-600 dark:border-danger-700 dark:bg-danger-900/20 dark:text-danger-300',
};

const KIND_LABEL_MAP: Record<TopologyNodeKind, string> = {
  inbound: '入口',
  route: '路由',
  rule: '规则',
  policy: '策略组',
  final: '默认出口',
  outbound: '出站',
  missing: '缺失引用',
};

interface FlowTarget {
  edge: TopologyEdge;
  node: TopologyNode;
}

interface RuleFlow {
  rule: TopologyNode;
  targets: FlowTarget[];
}

interface PolicyFlow {
  policy: TopologyNode;
  defaultTargets: FlowTarget[];
  candidateTargets: FlowTarget[];
}

interface ConfigTopologyViewProps {
  graph: ConfigTopologyGraph;
}

export default function ConfigTopologyView({ graph }: ConfigTopologyViewProps) {
  const [selectedNodeId, setSelectedNodeId] = useState<string | null>(null);

  const nodeMap = useMemo(() => {
    const map = new Map<string, TopologyNode>();
    graph.nodes.forEach((node) => map.set(node.id, node));
    return map;
  }, [graph.nodes]);

  const inboundNodes = useMemo(
    () => graph.layers.inbound.map((id) => nodeMap.get(id)).filter((node): node is TopologyNode => Boolean(node)),
    [graph.layers.inbound, nodeMap]
  );

  const coreNodes = useMemo(
    () => graph.layers.core.map((id) => nodeMap.get(id)).filter((node): node is TopologyNode => Boolean(node)),
    [graph.layers.core, nodeMap]
  );

  const outboundNodes = useMemo(
    () => graph.layers.outbound.map((id) => nodeMap.get(id)).filter((node): node is TopologyNode => Boolean(node)),
    [graph.layers.outbound, nodeMap]
  );

  const routeNode = useMemo(() => coreNodes.find((node) => node.kind === 'route') || null, [coreNodes]);
  const ruleNodes = useMemo(() => coreNodes.filter((node) => node.kind === 'rule'), [coreNodes]);
  const policyNodes = useMemo(() => coreNodes.filter((node) => node.kind === 'policy'), [coreNodes]);
  const finalNode = useMemo(() => coreNodes.find((node) => node.kind === 'final') || null, [coreNodes]);

  const selectedNode = selectedNodeId ? nodeMap.get(selectedNodeId) || null : null;

  const highlightedEdgeIds = useMemo(() => {
    if (!selectedNodeId) return new Set<string>();
    return new Set(
      graph.edges
        .filter((edge) => edge.from === selectedNodeId || edge.to === selectedNodeId)
        .map((edge) => edge.id)
    );
  }, [graph.edges, selectedNodeId]);

  const highlightedNodeIds = useMemo(() => {
    if (!selectedNodeId) return new Set<string>();
    const ids = new Set<string>([selectedNodeId]);
    graph.edges.forEach((edge) => {
      if (edge.from === selectedNodeId || edge.to === selectedNodeId) {
        ids.add(edge.from);
        ids.add(edge.to);
      }
    });
    return ids;
  }, [graph.edges, selectedNodeId]);

  const relationStats = useMemo(() => {
    if (!selectedNodeId) {
      return { incoming: 0, outgoing: 0 };
    }
    return {
      incoming: graph.edges.filter((edge) => edge.to === selectedNodeId).length,
      outgoing: graph.edges.filter((edge) => edge.from === selectedNodeId).length,
    };
  }, [graph.edges, selectedNodeId]);

  const ruleFlows = useMemo<RuleFlow[]>(() => {
    return ruleNodes.map((rule) => {
      const targets = graph.edges
        .filter((edge) => edge.from === rule.id)
        .map((edge) => {
          const node = nodeMap.get(edge.to);
          if (!node) return null;
          return { edge, node };
        })
        .filter((item): item is FlowTarget => Boolean(item));

      return { rule, targets };
    });
  }, [graph.edges, nodeMap, ruleNodes]);

  const policyFlows = useMemo<PolicyFlow[]>(() => {
    return policyNodes.map((policy) => {
      const allTargets = graph.edges
        .filter((edge) => edge.from === policy.id)
        .map((edge) => {
          const node = nodeMap.get(edge.to);
          if (!node) return null;
          return { edge, node };
        })
        .filter((item): item is FlowTarget => Boolean(item));

      return {
        policy,
        defaultTargets: allTargets.filter((target) => target.edge.kind === 'default'),
        candidateTargets: allTargets.filter((target) => target.edge.kind !== 'default'),
      };
    });
  }, [graph.edges, nodeMap, policyNodes]);

  const finalTargets = useMemo(() => {
    if (!finalNode) return [];
    return graph.edges
      .filter((edge) => edge.from === finalNode.id)
      .map((edge) => {
        const node = nodeMap.get(edge.to);
        if (!node) return null;
        return { edge, node };
      })
      .filter((item): item is FlowTarget => Boolean(item));
  }, [finalNode, graph.edges, nodeMap]);

  if (graph.nodes.length === 0) {
    return (
      <div className="rounded-lg border border-dashed border-default-300 p-8 text-center text-sm text-gray-500">
        暂无可展示的拓扑数据，请先刷新配置预览。
      </div>
    );
  }

  return (
    <div className="space-y-4">
      <p className="text-sm text-gray-500">业务流程图视角（静态配置结构，不代表实时命中流量）</p>

      {graph.warnings.length > 0 && (
        <div className="rounded-lg border border-warning-200 bg-warning-50/70 p-3 dark:border-warning-800 dark:bg-warning-900/20">
          <div className="mb-2 flex items-center gap-2 text-sm font-medium text-warning-700 dark:text-warning-300">
            <AlertTriangle className="h-4 w-4" />
            检测到 {graph.warnings.length} 条拓扑提示
          </div>
          <div className="space-y-1 text-xs text-warning-700/90 dark:text-warning-200">
            {graph.warnings.map((warning) => (
              <button
                type="button"
                key={warning.id}
                className="block text-left hover:underline"
                onClick={() => warning.nodeId && setSelectedNodeId(warning.nodeId)}
              >
                • {warning.message}
              </button>
            ))}
          </div>
        </div>
      )}

      <div className="rounded-lg border border-default-200 p-3 dark:border-default-100/20">
        <p className="mb-3 text-sm font-medium">主流程（从入口到出口）</p>
        <div className="overflow-x-auto">
          <div className="grid min-w-[980px] grid-cols-[1fr_auto_1fr_auto_1fr_auto_1fr] gap-3">
            <StepCard title="1. 上游接入" subtitle={`${inboundNodes.length} 个 Inbound`}>
              <NodePills
                nodes={inboundNodes}
                selectedNodeId={selectedNodeId}
                highlightedNodeIds={highlightedNodeIds}
                onSelect={setSelectedNodeId}
              />
            </StepCard>

            <StepArrow label="进入路由" />

            <StepCard
              title="2. 路由决策"
              subtitle={`规则 ${ruleNodes.length} · 策略组 ${policyNodes.length}`}
            >
              <NodePills
                nodes={routeNode ? [routeNode] : []}
                selectedNodeId={selectedNodeId}
                highlightedNodeIds={highlightedNodeIds}
                onSelect={setSelectedNodeId}
              />
              {ruleNodes.length > 0 && (
                <div className="mt-2 text-xs text-gray-500">按规则顺序匹配，命中后转发到目标出站</div>
              )}
            </StepCard>

            <StepArrow label="匹配规则" />

            <StepCard
              title="3. 出口选择"
              subtitle={finalNode ? `默认回退: ${finalNode.detail.标签 || graph.finalOutbound || '-'}` : '无默认回退'}
            >
              <NodePills
                nodes={[...policyNodes, ...(finalNode ? [finalNode] : [])]}
                selectedNodeId={selectedNodeId}
                highlightedNodeIds={highlightedNodeIds}
                onSelect={setSelectedNodeId}
              />
            </StepCard>

            <StepArrow label="最终转发" />

            <StepCard title="4. 最终出口" subtitle={`${outboundNodes.length} 个 Outbound`}>
              <NodePills
                nodes={outboundNodes}
                selectedNodeId={selectedNodeId}
                highlightedNodeIds={highlightedNodeIds}
                onSelect={setSelectedNodeId}
              />
            </StepCard>
          </div>
        </div>
      </div>

      <div className="grid grid-cols-1 gap-4 xl:grid-cols-2">
        <div className="rounded-lg border border-default-200 p-3 dark:border-default-100/20">
          <p className="mb-2 text-sm font-medium">规则命中路径</p>
          <div className="space-y-2">
            {ruleFlows.length === 0 ? (
              <p className="text-xs text-gray-500">当前没有路由规则，流量将主要依赖默认出口。</p>
            ) : (
              ruleFlows.map((flow) => (
                <FlowPathRow
                  key={flow.rule.id}
                  source={flow.rule}
                  sourceHint={flow.rule.detail.摘要}
                  targets={flow.targets}
                  selectedNodeId={selectedNodeId}
                  highlightedEdgeIds={highlightedEdgeIds}
                  highlightedNodeIds={highlightedNodeIds}
                  onSelectNode={setSelectedNodeId}
                />
              ))
            )}
          </div>
        </div>

        <div className="rounded-lg border border-default-200 p-3 dark:border-default-100/20">
          <p className="mb-2 text-sm font-medium">策略组分流</p>
          <div className="space-y-2">
            {policyFlows.length === 0 ? (
              <p className="text-xs text-gray-500">当前没有 selector/urltest 策略组。</p>
            ) : (
              policyFlows.map((flow) => (
                <div key={flow.policy.id} className="rounded-md border border-default-200 p-2 dark:border-default-100/20">
                  <button
                    type="button"
                    className={`text-left text-sm font-medium hover:underline ${
                      selectedNodeId === flow.policy.id ? 'text-primary-600' : ''
                    }`}
                    onClick={() => setSelectedNodeId(flow.policy.id)}
                  >
                    {flow.policy.name}
                  </button>

                  {flow.defaultTargets.length > 0 && (
                    <div className="mt-2 flex flex-wrap items-center gap-1 text-xs">
                      <Chip size="sm" color="warning" variant="flat">默认</Chip>
                      {flow.defaultTargets.map((target) => (
                        <EdgeTargetButton
                          key={target.edge.id}
                          target={target}
                          selectedNodeId={selectedNodeId}
                          highlightedEdgeIds={highlightedEdgeIds}
                          highlightedNodeIds={highlightedNodeIds}
                          onSelectNode={setSelectedNodeId}
                        />
                      ))}
                    </div>
                  )}

                  {flow.candidateTargets.length > 0 && (
                    <div className="mt-2 flex flex-wrap items-center gap-1 text-xs">
                      <Chip size="sm" color="secondary" variant="flat">候选</Chip>
                      {flow.candidateTargets.map((target) => (
                        <EdgeTargetButton
                          key={target.edge.id}
                          target={target}
                          selectedNodeId={selectedNodeId}
                          highlightedEdgeIds={highlightedEdgeIds}
                          highlightedNodeIds={highlightedNodeIds}
                          onSelectNode={setSelectedNodeId}
                        />
                      ))}
                    </div>
                  )}
                </div>
              ))
            )}
          </div>
        </div>
      </div>

      <div className="rounded-lg border border-default-200 p-3 text-sm dark:border-default-100/20">
        <p className="mb-2 text-sm font-medium">默认回退路径</p>
        {finalNode && finalTargets.length > 0 ? (
          <div className="space-y-1">
            {finalTargets.map((target) => (
              <div key={target.edge.id} className="flex items-center gap-2 text-sm">
                <button
                  type="button"
                  className="hover:underline"
                  onClick={() => setSelectedNodeId(finalNode.id)}
                >
                  {finalNode.name}
                </button>
                <ArrowRight className="h-3.5 w-3.5 text-amber-600 dark:text-amber-300" />
                <button
                  type="button"
                  className="hover:underline"
                  onClick={() => setSelectedNodeId(target.node.id)}
                >
                  {target.node.name}
                </button>
                <Chip size="sm" variant="flat" className="h-5 text-[11px]">{target.edge.label || 'final'}</Chip>
              </div>
            ))}
          </div>
        ) : (
          <p className="text-xs text-gray-500">当前没有可识别的默认回退链路。</p>
        )}
      </div>

      <div className="rounded-lg border border-default-200 p-3 text-sm dark:border-default-100/20">
        {selectedNode ? (
          <div className="space-y-2">
            <div className="flex flex-wrap items-center gap-2">
              <p className="font-medium">节点详情：{selectedNode.name}</p>
              <Chip size="sm" variant="flat">{KIND_LABEL_MAP[selectedNode.kind]}</Chip>
              <Chip size="sm" variant="flat">入边 {relationStats.incoming}</Chip>
              <Chip size="sm" variant="flat">出边 {relationStats.outgoing}</Chip>
            </div>
            <div className="grid grid-cols-1 gap-1 text-xs text-gray-600 dark:text-gray-300">
              {Object.entries(selectedNode.detail).map(([key, value]) => (
                <p key={key}>
                  <span className="font-medium">{key}：</span>
                  {value}
                </p>
              ))}
            </div>
          </div>
        ) : (
          <p className="text-gray-500">点击流程中的节点或路径标签，可查看详细信息。</p>
        )}
      </div>
    </div>
  );
}

interface StepCardProps {
  title: string;
  subtitle: string;
  children: React.ReactNode;
}

function StepCard({ title, subtitle, children }: StepCardProps) {
  return (
    <div className="rounded-lg border border-default-200 p-3 dark:border-default-100/20">
      <p className="text-sm font-medium">{title}</p>
      <p className="text-xs text-gray-500">{subtitle}</p>
      <div className="mt-2">{children}</div>
    </div>
  );
}

function StepArrow({ label }: { label: string }) {
  return (
    <div className="flex items-center justify-center text-xs text-gray-400">
      <span className="hidden 2xl:inline">{label}</span>
      <ArrowRight className="h-4 w-4" />
    </div>
  );
}

interface NodePillsProps {
  nodes: TopologyNode[];
  selectedNodeId: string | null;
  highlightedNodeIds: Set<string>;
  onSelect: (id: string) => void;
}

function NodePills({ nodes, selectedNodeId, highlightedNodeIds, onSelect }: NodePillsProps) {
  if (nodes.length === 0) {
    return <p className="text-xs text-gray-400">暂无</p>;
  }

  return (
    <div className="flex flex-wrap gap-1">
      {nodes.slice(0, 8).map((node) => {
        const isSelected = selectedNodeId === node.id;
        const isRelated = highlightedNodeIds.size === 0 || highlightedNodeIds.has(node.id);
        return (
          <button
            type="button"
            key={node.id}
            onClick={() => onSelect(node.id)}
            className={`rounded border px-2 py-1 text-xs ${NODE_STYLE_MAP[node.kind]} ${
              isSelected ? 'ring-2 ring-primary-400' : ''
            } ${isRelated ? '' : 'opacity-45'}`}
          >
            {node.name}
          </button>
        );
      })}
      {nodes.length > 8 && (
        <Chip size="sm" variant="flat" className="h-5 text-[11px]">
          +{nodes.length - 8}
        </Chip>
      )}
    </div>
  );
}

interface FlowPathRowProps {
  source: TopologyNode;
  sourceHint?: string;
  targets: FlowTarget[];
  selectedNodeId: string | null;
  highlightedEdgeIds: Set<string>;
  highlightedNodeIds: Set<string>;
  onSelectNode: (id: string) => void;
}

function FlowPathRow({
  source,
  sourceHint,
  targets,
  selectedNodeId,
  highlightedEdgeIds,
  highlightedNodeIds,
  onSelectNode,
}: FlowPathRowProps) {
  return (
    <div className="rounded-md border border-default-200 p-2 dark:border-default-100/20">
      <button
        type="button"
        className={`text-left text-sm font-medium hover:underline ${selectedNodeId === source.id ? 'text-primary-600' : ''}`}
        onClick={() => onSelectNode(source.id)}
      >
        {source.name}
      </button>
      {sourceHint && <p className="mt-1 text-xs text-gray-500">{sourceHint}</p>}

      <div className="mt-2 flex flex-wrap items-center gap-1">
        {targets.length === 0 ? (
          <p className="text-xs text-gray-400">无目标出站</p>
        ) : (
          targets.map((target) => (
            <EdgeTargetButton
              key={target.edge.id}
              target={target}
              selectedNodeId={selectedNodeId}
              highlightedEdgeIds={highlightedEdgeIds}
              highlightedNodeIds={highlightedNodeIds}
              onSelectNode={onSelectNode}
            />
          ))
        )}
      </div>
    </div>
  );
}

interface EdgeTargetButtonProps {
  target: FlowTarget;
  selectedNodeId: string | null;
  highlightedEdgeIds: Set<string>;
  highlightedNodeIds: Set<string>;
  onSelectNode: (id: string) => void;
}

function EdgeTargetButton({
  target,
  selectedNodeId,
  highlightedEdgeIds,
  highlightedNodeIds,
  onSelectNode,
}: EdgeTargetButtonProps) {
  const isSelected = selectedNodeId === target.node.id;
  const isRelatedNode = highlightedNodeIds.size === 0 || highlightedNodeIds.has(target.node.id);
  const isRelatedEdge = highlightedEdgeIds.size === 0 || highlightedEdgeIds.has(target.edge.id);

  return (
    <div className={`flex items-center gap-1 ${isRelatedEdge ? '' : 'opacity-50'}`}>
      <ArrowRight className="h-3.5 w-3.5 text-gray-400" />
      <button
        type="button"
        className={`rounded border px-2 py-0.5 text-xs ${NODE_STYLE_MAP[target.node.kind]} ${
          isSelected ? 'ring-2 ring-primary-400' : ''
        } ${isRelatedNode ? '' : 'opacity-45'}`}
        onClick={() => onSelectNode(target.node.id)}
      >
        {target.node.name}
      </button>
      {target.edge.label && (
        <Chip size="sm" variant="flat" className="h-5 text-[11px]">
          {target.edge.label}
        </Chip>
      )}
    </div>
  );
}

