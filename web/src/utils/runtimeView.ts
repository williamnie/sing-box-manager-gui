import type { ConnectionSnapshot, RuntimeConnection, RuntimeProxy } from '../api/runtime';
import type { ObservedGatewayClient } from '../api/gatewayClients';

export function formatBytes(value: number): string {
  if (!Number.isFinite(value) || value <= 0) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  const index = Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1);
  return `${(value / 1024 ** index).toFixed(index ? 1 : 0)} ${units[index]}`;
}

// 内核配置可能引用缺失组或循环组；限制遍历且保留问题位置供排障。
export function selectedChain(tag: string, proxies: Map<string, RuntimeProxy>): string[] {
  const chain: string[] = [];
  const visited = new Set<string>();
  let current = tag;
  while (current) {
    if (visited.has(current)) return [...chain, '循环引用'];
    visited.add(current);
    chain.push(current);
    const proxy = proxies.get(current);
    if (!proxy || !proxy.selected || !proxy.members.length) break;
    current = proxy.selected;
  }
  return chain;
}

export interface ConnectionFilters { search: string; source: string; group: string; network: string }

// 选项取自完整来源集合，不随当前视图、搜索条件或分页收缩。
export function connectionSources(active: RuntimeConnection[], closed: RuntimeConnection[], clients: Pick<ObservedGatewayClient, 'address' | 'name'>[]): { address: string; name: string }[] {
  const sources = new Map<string, string>();
  for (const connection of [...active, ...closed]) {
    const address = connection.source.trim().toLowerCase();
    if (address) sources.set(address, '');
  }
  for (const client of clients) {
    const address = client.address.trim().toLowerCase();
    if (address) sources.set(address, client.name || sources.get(address) || '');
  }
  return [...sources].sort(([left], [right]) => left.localeCompare(right, 'en', { numeric: true }))
    .map(([address, name]) => ({ address, name }));
}

export function filterConnections(connections: RuntimeConnection[], filters: ConnectionFilters): RuntimeConnection[] {
  const query = filters.search.trim().toLocaleLowerCase();
  const source = filters.source.trim().toLocaleLowerCase();
  return connections.filter(connection =>
    (!query || [connection.host, connection.destination, connection.destination_port, connection.source, connection.process, connection.id].some(value => value.toLocaleLowerCase().includes(query))) &&
    (!source || connection.source.trim().toLocaleLowerCase() === source) &&
    (!filters.group || connection.chains.includes(filters.group)) &&
    (!filters.network || connection.network.toLocaleLowerCase() === filters.network.toLocaleLowerCase()),
  );
}

export interface ConnectionRate { upload: number; download: number }
export function connectionRates(current: ConnectionSnapshot | null, previous: ConnectionSnapshot | null): Map<string, ConnectionRate> {
  const rates = new Map<string, ConnectionRate>();
  if (!current || !previous || current.instance !== previous.instance) return rates;
  const seconds = (current.sampled_at - previous.sampled_at) / 1000;
  if (seconds <= 0) return rates;
  const old = new Map(previous.connections.map(connection => [connection.id, connection]));
  for (const connection of current.connections) {
    const baseline = old.get(connection.id);
    // 新连接需要两个样本，计数器回退视为重新开始，避免产生负速率。
    if (!baseline || connection.upload < baseline.upload || connection.download < baseline.download) continue;
    rates.set(connection.id, { upload: (connection.upload - baseline.upload) / seconds, download: (connection.download - baseline.download) / seconds });
  }
  return rates;
}

export type ConnectionSort = 'started_at' | 'source' | 'host' | 'upload' | 'download' | 'uploadRate' | 'downloadRate';
export function sortConnections(connections: RuntimeConnection[], sort: ConnectionSort, descending: boolean, rates: Map<string, ConnectionRate>): RuntimeConnection[] {
  return [...connections].sort((left, right) => {
    let comparison: number;
    if (sort === 'uploadRate' || sort === 'downloadRate') {
      const key = sort === 'uploadRate' ? 'upload' : 'download';
      comparison = (rates.get(left.id)?.[key] ?? -1) - (rates.get(right.id)?.[key] ?? -1);
    } else if (sort === 'upload' || sort === 'download') comparison = left[sort] - right[sort];
    else if (sort === 'started_at') comparison = (Date.parse(left.started_at) || 0) - (Date.parse(right.started_at) || 0);
    else comparison = (left[sort] || left.destination).localeCompare(right[sort] || right.destination, 'zh-CN', { numeric: true });
    return (descending ? -comparison : comparison) || left.id.localeCompare(right.id);
  });
}

export function formatEndpoint(host: string, port: string): string {
  if (!host) return '—';
  return port ? `${host.includes(':') ? `[${host}]` : host}:${port}` : host;
}
