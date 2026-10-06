import type { ConnectionSnapshot, RuntimeConnection } from '../api/runtime';

export interface ClosedConnection extends RuntimeConnection { ended_at: number }
export interface ConnectionHistory { snapshot: ConnectionSnapshot | null; closed: ClosedConnection[] }

const retentionMS = 5 * 60 * 1000;
const maxClosed = 1000;

// 只保留实际采样过的连接；结束时间表示首次观察到其不在活动列表中的时间。
export function updateConnectionHistory(previous: ConnectionHistory, snapshot: ConnectionSnapshot): ConnectionHistory {
  if (!previous.snapshot || previous.snapshot.instance !== snapshot.instance || snapshot.sampled_at < previous.snapshot.sampled_at) {
    return { snapshot, closed: [] };
  }
  const activeIDs = new Set(snapshot.connections.map(connection => connection.id));
  const closed = new Map(previous.closed
    .filter(connection => !activeIDs.has(connection.id) && snapshot.sampled_at - connection.ended_at < retentionMS)
    .map(connection => [connection.id, connection]));
  for (const connection of previous.snapshot.connections) {
    if (!activeIDs.has(connection.id)) closed.set(connection.id, { ...connection, ended_at: snapshot.sampled_at });
  }
  return { snapshot, closed: [...closed.values()].sort((a, b) => b.ended_at - a.ended_at).slice(0, maxClosed) };
}
