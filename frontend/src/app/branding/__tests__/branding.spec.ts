import { beforeEach, describe, expect, it } from 'vitest'
import { updateFavicon } from '@/app/branding/favicon'
import imageReferences from '@/shared/lib/__tests__/fixtures/imageReferences.json'

describe('updateFavicon', () => {
  beforeEach(() => {
    document.head.innerHTML = '<link rel="icon" type="image/svg+xml" href="/logo.svg">'
  })

  it.each(imageReferences)('uses the common image reference contract: $name', ({ input, expected }) => {
    updateFavicon(input)
    expect(document.querySelector('link[rel="icon"]')?.getAttribute('href')).toBe(expected || '/logo.svg')
  })

  it('replaces the default favicon with the configured logo', () => {
    updateFavicon('https://example.com/custom-logo.png')

    const link = document.querySelector<HTMLLinkElement>('link[rel="icon"]')
    expect(link?.href).toBe('https://example.com/custom-logo.png')
    expect(link?.hasAttribute('type')).toBe(false)
  })

  it('ignores unsafe logo URLs', () => {
    updateFavicon('javascript:alert(1)')

    const link = document.querySelector<HTMLLinkElement>('link[rel="icon"]')
    expect(link?.getAttribute('href')).toBe('/logo.svg')
  })
})


it('supports uploaded image data and clearing the configured logo', () => {
  const logo = 'data:image/png;base64,aGVsbG8='
  updateFavicon(logo)
  const link = document.querySelector<HTMLLinkElement>('link[rel="icon"]')!
  expect(link.href).toBe(logo)
  expect(link.hasAttribute('type')).toBe(false)
  updateFavicon('')
  expect(link.getAttribute('href')).toBe('/logo.svg')
})
