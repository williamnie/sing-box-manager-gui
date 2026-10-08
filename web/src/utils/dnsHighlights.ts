import type { DNSDomain, DNSRecord } from '../api/dnsQueries';

interface Snapshot {
  scope: string;
  data: { records: DNSRecord[]; domains: DNSDomain[] };
  status: { stats: { last_query: number } };
}
export interface DNSHighlights {
  snapshot: Snapshot;
  domains: Set<string>;
  records: Set<string>;
}

// 同秒相同请求按从旧到新的序号区分，不因新行插入而改变已有行的 key。
export function dnsRecordKeys(records: DNSRecord[]): string[] {
  const bases = records.map(row => JSON.stringify([row.at, row.source, row.domain, row.type]));
  const counts = new Map<string, number>();
  return bases.reverse().map(key => {
    const count = (counts.get(key) ?? 0) + 1;
    counts.set(key, count);
    return `${key}:${count}`;
  }).reverse();
}

export function updateDNSHighlights(previous: DNSHighlights | null, snapshot: Snapshot): DNSHighlights {
  const next: DNSHighlights = { snapshot, domains: new Set(), records: new Set() };
  if (!previous || previous.snapshot.scope !== snapshot.scope) return next;
  const previousDomains = new Map(previous.snapshot.data.domains.map(row => [row.domain, row]));
  const previousRecords = new Set(dnsRecordKeys(previous.snapshot.data.records));
  const watermark = previous.snapshot.status.stats.last_query;
  for (const row of snapshot.data.domains) {
    const old = previousDomains.get(row.domain);
    if (previous.domains.has(row.domain) || (old ? row.count > old.count || row.last_seen > old.last_seen : row.last_seen >= watermark)) next.domains.add(row.domain);
  }
  dnsRecordKeys(snapshot.data.records).forEach((key, index) => {
    if (previous.records.has(key) || (!previousRecords.has(key) && snapshot.data.records[index].at >= watermark)) next.records.add(key);
  });
  return next;
}
