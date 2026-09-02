import type { GroupPlatform } from '@/types'

/**
 * 无可用账号兜底分组的表单辅助。
 *
 * 兜底分组必须与来源分组同平台（后端保存时也会校验：选号本身按平台过滤，
 * 异平台分组永远选不出账号）。平台切换时要不要清空当前选择，取决于所选分组
 * 的平台，而不是「平台变了」本身——后者会把编辑弹窗刚回填的值冲掉。
 */

export interface NoAccountFallbackCandidate {
  id: number
  platform: GroupPlatform
  status: string
}

/**
 * 平台切换后，当前选中的兜底分组是否还能保留。
 *
 * 只有确认目标分组属于另一个平台才清空。目标不在已加载的分组列表里时保留原值：
 * 列表分页或尚未加载不构成「平台不同」的证据，清掉等于静默丢配置。
 */
export function reconcileNoAccountFallbackForPlatform(
  fallbackGroupId: number | null | undefined,
  platform: GroupPlatform,
  groups: readonly NoAccountFallbackCandidate[],
): number | null {
  if (fallbackGroupId == null) return null
  const target = groups.find(group => group.id === fallbackGroupId)
  if (target && target.platform !== platform) return null
  return fallbackGroupId
}

/**
 * 把已保存的目标分组补回下拉候选。
 *
 * 候选来自分组列表，而列表是分页 + 筛选后的当前页；目标分组被停用、换了平台或
 * 只是不在当前页时都不在候选里。Select 找不到对应 option 就退回显示占位「不兜底」，
 * 可值仍会随保存原样提交——管理员看到的和实际保存的对不上，也无法在这个状态下
 * 重新选择。selected 在 groups 里能找到就补到末尾，找不到则维持原样。
 */
export function withSelectedGroup<T extends { id: number }>(
  candidates: readonly T[],
  groups: readonly T[],
  selectedId: number | null | undefined,
): T[] {
  if (selectedId == null || candidates.some(group => group.id === selectedId)) {
    return [...candidates]
  }
  const selected = groups.find(group => group.id === selectedId)
  return selected ? [...candidates, selected] : [...candidates]
}

/** 下拉可选项：同平台 + active，编辑时排除自身。 */
export function noAccountFallbackCandidates<T extends NoAccountFallbackCandidate>(
  groups: readonly T[],
  platform: GroupPlatform,
  excludeGroupId?: number | null,
): T[] {
  return groups.filter(
    group =>
      group.platform === platform &&
      group.status === 'active' &&
      group.id !== excludeGroupId,
  )
}
