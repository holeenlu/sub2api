import { describe, expect, it } from 'vitest'
import { apiNavGroups, appsNavGroups, docsStaticItems, navGroupsForPath } from '../nav'

describe('separate documentation sections', () => {
  it('keeps API and application sidebars separate', () => {
    expect(apiNavGroups.flatMap(group => group.items).every(item => item.path.startsWith('/docs'))).toBe(true)
    expect(appsNavGroups.flatMap(group => group.items).every(item => item.path.startsWith('/apps'))).toBe(true)
    expect(navGroupsForPath('/apps/codex')).toBe(appsNavGroups)
    expect(navGroupsForPath('/docs/api/chat/openai-responses')).toBe(apiNavGroups)
  })

  it('has an actual translated article for every navigation entry', () => {
    const files = import.meta.glob('../**/*.md', { query: '?raw', import: 'default' })
    for (const item of docsStaticItems) {
      if (!item.articleId) continue
      for (const language of ['zh', 'en', 'zh-TW']) {
        expect(Object.keys(files), `${language}: ${item.path}`).toContain(`../${language}/${item.articleId}.md`)
      }
    }
  })
})
