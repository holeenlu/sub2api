import { sanitizeUrl } from '@/utils/url'

export function updateFavicon(logoUrl: string): void {
  const sanitizedLogoUrl = sanitizeUrl(logoUrl, {
    allowRelative: true,
    allowDataUrl: true,
  })
  if (logoUrl.trim() && !sanitizedLogoUrl) return

  let link = document.querySelector<HTMLLinkElement>('link[rel="icon"]')
  if (!link) {
    link = document.createElement('link')
    link.rel = 'icon'
    document.head.appendChild(link)
  }

  // The browser determines the image type from the data URL or response.
  // Declaring every non-SVG upload as ICO can leave the bundled icon selected.
  link.removeAttribute('type')
  link.href = sanitizedLogoUrl || '/logo.svg'
}
