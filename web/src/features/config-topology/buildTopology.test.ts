import { describe, expect, it } from 'vitest';
import { buildConfigTopology } from './buildTopology';
import type { ConfigPreviewData } from './types';

describe('buildConfigTopology', () => {
  it('应生成三层拓扑并包含默认回退路径', () => {
    const config: ConfigPreviewData = {
      inbounds: [{ tag: 'mixed-in', type: 'mixed' }],
      outbounds: [
        { tag: 'Proxy', type: 'selector', outbounds: ['node-a', 'DIRECT'] },
        { tag: 'node-a', type: 'vless' },
      ],
      route: {
        rules: [{ domain_suffix: ['google.com'], outbound: 'Proxy' }],
        final: 'DIRECT',
      },
    };

    const graph = buildConfigTopology(config);

    expect(graph.layers.inbound.length).toBeGreaterThan(0);
    expect(graph.layers.core.length).toBeGreaterThan(0);
    expect(graph.layers.outbound.length).toBeGreaterThan(0);

    expect(graph.edges.some((edge) => edge.label === '候选')).toBe(true);
    expect(graph.edges.some((edge) => edge.label === '未命中回退')).toBe(true);
    expect(graph.edges.some((edge) => edge.label === 'final=DIRECT')).toBe(true);
  });

  it('应对缺失出站引用给出告警并生成缺失节点', () => {
    const config: ConfigPreviewData = {
      inbounds: [{ tag: 'mixed-in', type: 'mixed' }],
      outbounds: [{ tag: 'node-a', type: 'vless' }],
      route: {
        rules: [{ domain_suffix: ['example.com'], outbound: 'missing-outbound' }],
      },
    };

    const graph = buildConfigTopology(config);
    const missingNode = graph.nodes.find((node) => node.kind === 'missing');

    expect(missingNode).toBeDefined();
    expect(graph.warnings.some((warning) => warning.message.includes('missing-outbound'))).toBe(true);
  });

  it('应识别未被引用的孤立出站', () => {
    const config: ConfigPreviewData = {
      inbounds: [{ tag: 'mixed-in', type: 'mixed' }],
      outbounds: [
        { tag: 'node-a', type: 'vless' },
        { tag: 'node-b', type: 'vless' },
      ],
      route: {
        rules: [{ domain_suffix: ['example.com'], outbound: 'node-a' }],
      },
    };

    const graph = buildConfigTopology(config);

    expect(graph.warnings.some((warning) => warning.message.includes('node-b'))).toBe(true);
  });

  it('空配置应返回空拓扑', () => {
    const graph = buildConfigTopology({});

    expect(graph.nodes).toHaveLength(0);
    expect(graph.edges).toHaveLength(0);
    expect(graph.warnings).toHaveLength(0);
  });
});

