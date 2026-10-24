import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'

import ArchitectureDiagram from '../ArchitectureDiagram.vue'

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key, locale: { value: 'en' } }),
  }
})

const BADGES = ['①', '②', '③', '④', '⑤', '⑥', '⑦']

describe('ArchitectureDiagram', () => {
  it('七个能力编号各自只挂在一个节点标题上', () => {
    // ② 在回接线的文字里会再出现一次（重试其他可用上游 → ② 智慧路由），
    // 所以只数节点标题，不数整段文字。
    const headings = mount(ArchitectureDiagram)
      .findAll('h4')
      .map((h) => h.text())

    for (const badge of BADGES) {
      expect(headings.filter((text) => text.startsWith(badge)), `badge ${badge}`).toHaveLength(1)
    }
  })

  it('③ 成本优先路由嵌在 ② 智慧路由节点内，不是独立步骤', () => {
    const wrapper = mount(ArchitectureDiagram)
    const routing = wrapper.get('[data-testid="node-routing"]')

    expect(routing.find('[data-testid="node-cost"]').exists()).toBe(true)
    // 范围注记固定显示在 ③ 下方
    expect(routing.get('[data-testid="node-cost"]').text()).toContain('home.mechanisms.cost.note')
  })

  it('⑦ 使用 Agent 路由的机制标题 key', () => {
    expect(mount(ArchitectureDiagram).text()).toContain('home.mechanisms.agents.title')
  })
})
