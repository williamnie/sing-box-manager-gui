export type TopologyLayer = 'inbound' | 'core' | 'outbound';

export type TopologyNodeKind =
  | 'inbound'
  | 'route'
  | 'rule'
  | 'policy'
  | 'final'
  | 'outbound'
  | 'missing';

export type TopologyEdgeKind = 'normal' | 'policy' | 'default' | 'warning';

export interface TopologyNode {
  id: string;
  name: string;
  layer: TopologyLayer;
  kind: TopologyNodeKind;
  tag?: string;
  detail: Record<string, string>;
}

export interface TopologyEdge {
  id: string;
  from: string;
  to: string;
  label?: string;
  kind: TopologyEdgeKind;
}

export interface TopologyWarning {
  id: string;
  level: 'warning' | 'error';
  message: string;
  nodeId?: string;
}

export interface ConfigTopologyGraph {
  nodes: TopologyNode[];
  edges: TopologyEdge[];
  warnings: TopologyWarning[];
  layers: Record<TopologyLayer, string[]>;
  finalOutbound?: string;
}

export type ConfigObject = Record<string, unknown>;

export interface ConfigPreviewData {
  inbounds?: ConfigObject[];
  outbounds?: ConfigObject[];
  dns?: {
    servers?: unknown[];
  };
  route?: {
    final?: unknown;
    rules?: ConfigObject[];
    rule_set?: unknown[];
    rule_sets?: unknown[];
  };
}

