import { beforeEach, describe, expect, it } from 'vitest'
import { i18n } from '@/i18n'
import { BRAND_NAME } from '@/config/brand'
import { applyRouteMetaDescription, resolveDocumentTitle, resolveRouteDocumentTitle } from '@/router/title'

// 语言包是懒加载的，测试里自己塞一份最小消息，避免依赖真实文案
i18n.global.setLocaleMessage('en', {
  page: {
    standaloneTitle: 'Standalone Title — Example',
    description: 'Example page description'
  }
})
i18n.global.locale.value = 'en'

function metaRoute(meta: Record<string, unknown>) {
  return { name: 'Home', params: {}, meta } as never
}

function descriptionNodes(): HTMLMetaElement[] {
  return Array.from(document.head.querySelectorAll<HTMLMetaElement>('meta[name="description"]'))
}

describe('resolveDocumentTitle', () => {
  it('路由存在标题时，使用“路由标题 - 站点名”格式', () => {
    expect(resolveDocumentTitle('Usage Records', 'My Site')).toBe('Usage Records - My Site')
  })

  it('路由无标题时，回退到站点名', () => {
    expect(resolveDocumentTitle(undefined, 'My Site')).toBe('My Site')
  })

  it('站点名为空时，回退默认站点名', () => {
    expect(resolveDocumentTitle('Dashboard', '')).toBe(`Dashboard - ${BRAND_NAME}`)
    expect(resolveDocumentTitle(undefined, '   ')).toBe(BRAND_NAME)
  })

  it('站点名变更时仅影响后续路由标题计算', () => {
    const before = resolveDocumentTitle('Admin Dashboard', 'Alpha')
    const after = resolveDocumentTitle('Admin Dashboard', 'Beta')

    expect(before).toBe('Admin Dashboard - Alpha')
    expect(after).toBe('Admin Dashboard - Beta')
  })
})

describe('resolveRouteDocumentTitle', () => {
  it('自定义页面菜单加载后，使用菜单名称作为标题', () => {
    const route = {
      name: 'CustomPage',
      params: { id: 'scheduler' },
      meta: {
        title: 'Custom Page'
      }
    }

    expect(resolveRouteDocumentTitle(route, 'EzouAPI')).toBe('Custom Page - EzouAPI')
    expect(resolveRouteDocumentTitle(route, 'EzouAPI', [
      {
        id: 'scheduler',
        label: '账号调度器',
        icon_svg: '',
        url: 'https://example.com',
        visibility: 'admin',
        sort_order: 0
      }
    ])).toBe('账号调度器 - EzouAPI')
  })
})

describe('titleStandalone', () => {
  it('标记为 standalone 时，翻译结果就是完整标题，不再附加站点名', () => {
    expect(
      resolveRouteDocumentTitle(
        metaRoute({ titleKey: 'page.standaloneTitle', titleStandalone: true }),
        'My Site'
      )
    ).toBe('Standalone Title — Example')
  })

  it('未标记 standalone 时，仍然附加站点名', () => {
    expect(
      resolveRouteDocumentTitle(metaRoute({ titleKey: 'page.standaloneTitle' }), 'My Site')
    ).toBe('Standalone Title — Example - My Site')
  })

  it('自定义菜单标题优先，standalone 不生效', () => {
    const route = {
      name: 'CustomPage',
      params: { id: 'scheduler' },
      meta: { titleKey: 'page.standaloneTitle', titleStandalone: true }
    } as never

    expect(
      resolveRouteDocumentTitle(route, 'My Site', [
        {
          id: 'scheduler',
          label: '账号调度器',
          icon_svg: '',
          url: 'https://example.com',
          visibility: 'admin',
          sort_order: 0
        }
      ])
    ).toBe('账号调度器 - My Site')
  })
})

describe('applyRouteMetaDescription', () => {
  beforeEach(() => {
    for (const node of descriptionNodes()) {
      node.remove()
    }
  })

  it('有 metaDescriptionKey 时写入 description', () => {
    applyRouteMetaDescription(metaRoute({ metaDescriptionKey: 'page.description' }))

    expect(descriptionNodes()).toHaveLength(1)
    expect(descriptionNodes()[0].getAttribute('content')).toBe('Example page description')
  })

  it('重复调用只复用同一个节点', () => {
    applyRouteMetaDescription(metaRoute({ metaDescriptionKey: 'page.description' }))
    applyRouteMetaDescription(metaRoute({ metaDescriptionKey: 'page.description' }))

    expect(descriptionNodes()).toHaveLength(1)
  })

  it('离开带 description 的路由后移除节点', () => {
    applyRouteMetaDescription(metaRoute({ metaDescriptionKey: 'page.description' }))
    applyRouteMetaDescription(metaRoute({}))

    expect(descriptionNodes()).toHaveLength(0)
  })

  it('key 无法翻译时不写入残缺的 description', () => {
    applyRouteMetaDescription(metaRoute({ metaDescriptionKey: 'page.missingDescription' }))

    expect(descriptionNodes()).toHaveLength(0)
  })
})
