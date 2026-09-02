import { describe, expect, it } from 'vitest'
import {
  noAccountFallbackCandidates,
  reconcileNoAccountFallbackForPlatform,
  withSelectedGroup,
} from '../groupsNoAccountFallback'
import type { NoAccountFallbackCandidate } from '../groupsNoAccountFallback'

const groups: NoAccountFallbackCandidate[] = [
  { id: 1, platform: 'anthropic', status: 'active' },
  { id: 2, platform: 'anthropic', status: 'inactive' },
  { id: 3, platform: 'openai', status: 'active' },
]

// 平台 watcher 若无条件清空兜底分组，编辑弹窗回填的值就会被冲掉：handleEdit 是
// 同步赋值、watcher 是 pre-flush，只要新旧平台不同，回填进来的值在回填之后被清
// 掉，管理员不动兜底项直接保存也会把配置删掉（提交时 null 会序列化成 0 = 清除）。
describe('reconcileNoAccountFallbackForPlatform', () => {
  it('目标分组与新平台一致时保留', () => {
    expect(reconcileNoAccountFallbackForPlatform(1, 'anthropic', groups)).toBe(1)
  })

  it('目标分组属于另一个平台时清空', () => {
    expect(reconcileNoAccountFallbackForPlatform(3, 'anthropic', groups)).toBeNull()
  })

  it('目标不在已加载列表里时保留——分页/未加载不构成证据', () => {
    expect(reconcileNoAccountFallbackForPlatform(99, 'anthropic', groups)).toBe(99)
  })

  it('未选择兜底分组时保持为空', () => {
    expect(reconcileNoAccountFallbackForPlatform(null, 'anthropic', groups)).toBeNull()
    expect(reconcileNoAccountFallbackForPlatform(undefined, 'anthropic', groups)).toBeNull()
  })

  it('停用的同平台分组仍然保留：停用不等于换了平台', () => {
    expect(reconcileNoAccountFallbackForPlatform(2, 'anthropic', groups)).toBe(2)
  })
})

describe('noAccountFallbackCandidates', () => {
  it('只列出同平台且启用中的分组', () => {
    expect(noAccountFallbackCandidates(groups, 'anthropic').map(g => g.id)).toEqual([1])
  })

  it('编辑时排除自身', () => {
    const anthropicGroups: NoAccountFallbackCandidate[] = [
      { id: 1, platform: 'anthropic', status: 'active' },
      { id: 4, platform: 'anthropic', status: 'active' },
    ]
    expect(noAccountFallbackCandidates(anthropicGroups, 'anthropic', 1).map(g => g.id)).toEqual([4])
  })

  it('创建时不传 excludeGroupId，不排除任何分组', () => {
    const anthropicGroups: NoAccountFallbackCandidate[] = [
      { id: 1, platform: 'anthropic', status: 'active' },
      { id: 4, platform: 'anthropic', status: 'active' },
    ]
    expect(noAccountFallbackCandidates(anthropicGroups, 'anthropic').map(g => g.id)).toEqual([1, 4])
  })
})

// Select 找不到对应 option 时显示占位「不兜底」，可值仍随保存提交：管理员看到的
// 和实际保存的对不上，也无法在这个状态下重新选择目标。
describe('withSelectedGroup', () => {
  const pool: NoAccountFallbackCandidate[] = [
    { id: 1, platform: 'anthropic', status: 'active' },
    { id: 2, platform: 'anthropic', status: 'inactive' },
    { id: 3, platform: 'openai', status: 'active' },
  ]

  it('把已保存但不合资格的目标补回候选', () => {
    const candidates = noAccountFallbackCandidates(pool, 'anthropic')
    expect(withSelectedGroup(candidates, pool, 2).map(g => g.id)).toEqual([1, 2])
  })

  it('目标已在候选里时不重复追加', () => {
    const candidates = noAccountFallbackCandidates(pool, 'anthropic')
    expect(withSelectedGroup(candidates, pool, 1).map(g => g.id)).toEqual([1])
  })

  it('未选择目标时原样返回', () => {
    const candidates = noAccountFallbackCandidates(pool, 'anthropic')
    expect(withSelectedGroup(candidates, pool, null).map(g => g.id)).toEqual([1])
    expect(withSelectedGroup(candidates, pool, undefined).map(g => g.id)).toEqual([1])
  })

  it('连全量来源里都没有的目标只能维持原样', () => {
    const candidates = noAccountFallbackCandidates(pool, 'anthropic')
    expect(withSelectedGroup(candidates, pool, 99).map(g => g.id)).toEqual([1])
  })
})
