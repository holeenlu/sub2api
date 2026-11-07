import type { OpsNodeMetricsSnapshot } from '../api/ops'

/** Preserve the server's observation age; opening a detail panel does not resample a node. */
export function isOpsNodeStale(node: OpsNodeMetricsSnapshot, receivedAt: number, now: number): boolean {
  return node.stale || !Number.isFinite(node.age_seconds) || node.age_seconds < 0
    || node.age_seconds + Math.max(0, now - receivedAt) / 1000 > 30
}
