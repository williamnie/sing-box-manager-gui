import type {
  ConfigObject,
  ConfigPreviewData,
  ConfigTopologyGraph,
  TopologyEdge,
  TopologyEdgeKind,
  TopologyLayer,
  TopologyNode,
  TopologyWarning,
} from './types';

const POLICY_TYPES = new Set(['selector', 'urltest']);
const BUILTIN_OUTBOUNDS = new Set(['DIRECT', 'REJECT']);

const MATCH_FIELDS: Array<[key: string, title: string]> = [
  ['domain', '域名'],
  ['domain_suffix', '域名后缀'],
  ['domain_keyword', '域名关键字'],
  ['ip_cidr', 'IP 段'],
  ['port', '端口'],
  ['protocol', '协议'],
  ['network', '网络'],
  ['process_name', '进程'],
  ['package_name', '包名'],
  ['geosite', 'GeoSite'],
  ['geoip', 'GeoIP'],
  ['inbound', '入站'],
  ['inbound_tag', '入站标签'],
];

interface BuildContext {
  nodes: Map<string, TopologyNode>;
  edges: Map<string, TopologyEdge>;
  warnings: TopologyWarning[];
  layers: Record<TopologyLayer, string[]>;
  tagToNodeId: Map<string, string>;
}

const asString = (value: unknown): string | null => {
  if (typeof value === 'string') {
    const trimmed = value.trim();
    return trimmed ? trimmed : null;
  }
  return null;
};

const asStringArray = (value: unknown): string[] => {
  if (!Array.isArray(value)) return [];
  return value
    .map(asString)
    .filter((item): item is string => item !== null);
};

const asObjectArray = (value: unknown): ConfigObject[] => {
  if (!Array.isArray(value)) return [];
  return value.filter((item): item is ConfigObject => !!item && typeof item === 'object' && !Array.isArray(item));
};

const getStringField = (record: ConfigObject, key: string) => asString(record[key]);

const pushLayerNode = (context: BuildContext, layer: TopologyLayer, nodeId: string) => {
  if (!context.layers[layer].includes(nodeId)) {
    context.layers[layer].push(nodeId);
  }
};

const addNode = (
  context: BuildContext,
  node: TopologyNode,
) => {
  if (context.nodes.has(node.id)) return;
  context.nodes.set(node.id, node);
  pushLayerNode(context, node.layer, node.id);
  if (node.tag) {
    context.tagToNodeId.set(node.tag, node.id);
  }
};

const addEdge = (
  context: BuildContext,
  from: string,
  to: string,
  label: string,
  kind: TopologyEdgeKind,
) => {
  const edgeId = `${from}->${to}:${label}:${kind}`;
  if (!context.edges.has(edgeId)) {
    context.edges.set(edgeId, { id: edgeId, from, to, label, kind });
  }
};

const addWarning = (context: BuildContext, message: string, nodeId?: string, level: 'warning' | 'error' = 'warning') => {
  context.warnings.push({
    id: `warning-${context.warnings.length + 1}`,
    level,
    message,
    nodeId,
  });
};

const summarizeRule = (rule: ConfigObject, index: number): string => {
  for (const [field, label] of MATCH_FIELDS) {
    const raw = rule[field];
    if (raw === undefined || raw === null) continue;

    const values = Array.isArray(raw) ? asStringArray(raw) : [asString(raw)].filter((item): item is string => !!item);
    if (values.length === 0) continue;

    if (values.length === 1) {
      return `${label}: ${values[0]}`;
    }
    return `${label}: ${values[0]} +${values.length - 1}`;
  }

  return `规则 ${index + 1}`;
};

const ensureBuiltinOutboundNode = (context: BuildContext, tag: string) => {
  const existing = context.tagToNodeId.get(tag);
  if (existing) return existing;

  const nodeId = `outbound-builtin-${tag.toLowerCase()}`;
  addNode(context, {
    id: nodeId,
    name: tag,
    layer: 'outbound',
    kind: 'outbound',
    tag,
    detail: {
      类型: '内置出站',
      标签: tag,
    },
  });
  return nodeId;
};

const ensureMissingOutboundNode = (context: BuildContext, tag: string) => {
  const missingTag = `missing:${tag}`;
  const existing = context.tagToNodeId.get(missingTag);
  if (existing) return existing;

  const nodeId = `outbound-missing-${context.layers.outbound.length + 1}`;
  addNode(context, {
    id: nodeId,
    name: `${tag} (缺失)`,
    layer: 'outbound',
    kind: 'missing',
    tag: missingTag,
    detail: {
      类型: '缺失引用',
      标签: tag,
    },
  });

  addWarning(context, `引用了不存在的出站: ${tag}`, nodeId, 'error');
  return nodeId;
};

const resolveOutboundNode = (context: BuildContext, tag: string): string => {
  const existing = context.tagToNodeId.get(tag);
  if (existing) return existing;
  if (BUILTIN_OUTBOUNDS.has(tag)) {
    return ensureBuiltinOutboundNode(context, tag);
  }
  return ensureMissingOutboundNode(context, tag);
};

const collectRuleTargets = (rule: ConfigObject): string[] => {
  const tags: string[] = [];

  const singleFields = ['outbound', 'outbound_tag'];
  for (const field of singleFields) {
    const value = asString(rule[field]);
    if (value) tags.push(value);
  }

  const arrayFields = ['outbounds', 'outbound_tags'];
  for (const field of arrayFields) {
    tags.push(...asStringArray(rule[field]));
  }

  return Array.from(new Set(tags));
};

const createContext = (): BuildContext => ({
  nodes: new Map(),
  edges: new Map(),
  warnings: [],
  layers: {
    inbound: [],
    core: [],
    outbound: [],
  },
  tagToNodeId: new Map(),
});

const parseFinalOutbound = (config: ConfigPreviewData, fallbackFinal?: string): string | undefined => {
  const byConfig = asString(config.route?.final);
  if (byConfig) return byConfig;
  const byFallback = asString(fallbackFinal);
  return byFallback || undefined;
};

export function buildConfigTopology(config: ConfigPreviewData | null, fallbackFinalOutbound?: string): ConfigTopologyGraph {
  if (!config) {
    return {
      nodes: [],
      edges: [],
      warnings: [],
      layers: { inbound: [], core: [], outbound: [] },
    };
  }

  const context = createContext();

  const inbounds = asObjectArray(config.inbounds);
  const outbounds = asObjectArray(config.outbounds);
  const routeRules = asObjectArray(config.route?.rules);
  const finalOutbound = parseFinalOutbound(config, fallbackFinalOutbound);

  if (inbounds.length === 0 && outbounds.length === 0 && routeRules.length === 0 && !finalOutbound) {
    return {
      nodes: [],
      edges: [],
      warnings: [],
      layers: {
        inbound: [],
        core: [],
        outbound: [],
      },
    };
  }

  addNode(context, {
    id: 'route-engine',
    name: '路由引擎',
    layer: 'core',
    kind: 'route',
    detail: {
      类型: 'route',
      规则数: String(routeRules.length),
    },
  });

  inbounds.forEach((inbound, index) => {
    const tag = getStringField(inbound, 'tag') || `inbound-${index + 1}`;
    const nodeId = `inbound-${index + 1}`;
    const inboundType = getStringField(inbound, 'type') || 'unknown';

    addNode(context, {
      id: nodeId,
      name: tag,
      layer: 'inbound',
      kind: 'inbound',
      tag,
      detail: {
        类型: inboundType,
        标签: tag,
      },
    });

    addEdge(context, nodeId, 'route-engine', '进入路由', 'normal');
  });

  if (inbounds.length === 0) {
    addWarning(context, '未检测到入站配置，拓扑可能不完整', 'route-engine');
  }

  const policyNodeIds = new Set<string>();

  outbounds.forEach((outbound, index) => {
    const tag = getStringField(outbound, 'tag') || `outbound-${index + 1}`;
    const outboundType = getStringField(outbound, 'type') || 'unknown';
    const isPolicy = POLICY_TYPES.has(outboundType);
    const nodeId = `outbound-${index + 1}`;

    addNode(context, {
      id: nodeId,
      name: tag,
      layer: isPolicy ? 'core' : 'outbound',
      kind: isPolicy ? 'policy' : 'outbound',
      tag,
      detail: {
        类型: outboundType,
        标签: tag,
      },
    });

    if (isPolicy) {
      policyNodeIds.add(nodeId);
      const candidates = asStringArray(outbound.outbounds);
      if (candidates.length === 0) {
        addWarning(context, `策略组 ${tag} 未配置候选出站`, nodeId);
      }

      candidates.forEach((candidateTag) => {
        const targetId = resolveOutboundNode(context, candidateTag);
        addEdge(context, nodeId, targetId, '候选', 'policy');
      });

      const defaultTag = asString(outbound.default);
      if (defaultTag) {
        const defaultTarget = resolveOutboundNode(context, defaultTag);
        addEdge(context, nodeId, defaultTarget, '默认选择', 'default');
      }
    }
  });

  if (outbounds.length === 0) {
    addWarning(context, '未检测到出站配置，拓扑可能不完整', 'route-engine', 'error');
  }

  routeRules.forEach((rule, index) => {
    const ruleId = `route-rule-${index + 1}`;
    const summary = summarizeRule(rule, index);
    addNode(context, {
      id: ruleId,
      name: `规则 ${index + 1}`,
      layer: 'core',
      kind: 'rule',
      detail: {
        摘要: summary,
      },
    });

    addEdge(context, 'route-engine', ruleId, '规则匹配', 'normal');

    const targets = collectRuleTargets(rule);
    if (targets.length === 0) {
      addWarning(context, `规则 ${index + 1} 未配置目标出站`, ruleId);
      return;
    }

    targets.forEach((targetTag) => {
      const targetNodeId = resolveOutboundNode(context, targetTag);
      const targetNode = context.nodes.get(targetNodeId);
      const edgeKind = targetNode?.kind === 'missing' ? 'warning' : 'normal';
      addEdge(context, ruleId, targetNodeId, '命中后转发', edgeKind);
    });
  });

  if (routeRules.length === 0) {
    addWarning(context, '未配置路由规则，将仅依赖默认出口', 'route-engine');
  }

  if (finalOutbound) {
    addNode(context, {
      id: 'route-final',
      name: '默认出口',
      layer: 'core',
      kind: 'final',
      detail: {
        标签: finalOutbound,
      },
    });

    addEdge(context, 'route-engine', 'route-final', '未命中回退', 'default');
    const finalTargetId = resolveOutboundNode(context, finalOutbound);
    const finalTarget = context.nodes.get(finalTargetId);
    addEdge(context, 'route-final', finalTargetId, `final=${finalOutbound}`, finalTarget?.kind === 'missing' ? 'warning' : 'default');
  } else {
    addWarning(context, '未检测到默认出口（final outbound）配置', 'route-engine');
  }

  const incomingByNodeId = new Map<string, number>();
  context.edges.forEach((edge) => {
    incomingByNodeId.set(edge.to, (incomingByNodeId.get(edge.to) || 0) + 1);
  });

  context.nodes.forEach((node) => {
    if (node.layer !== 'outbound' || node.kind === 'missing') {
      return;
    }
    if ((incomingByNodeId.get(node.id) || 0) === 0) {
      addWarning(context, `出站 ${node.name} 未被任何规则或策略引用`, node.id);
    }
  });

  if (policyNodeIds.size === 0) {
    addWarning(context, '未检测到策略组（selector/urltest），仅展示基础路由链路', 'route-engine');
  }

  return {
    nodes: Array.from(context.nodes.values()),
    edges: Array.from(context.edges.values()),
    warnings: context.warnings,
    layers: context.layers,
    finalOutbound,
  };
}
