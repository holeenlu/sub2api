import { marked } from 'marked'
import DOMPurify from 'dompurify'
import { buildApiUrl } from '@/shared/http'
export interface TocItem { id: string; text: string; level: number }

function generateHeadingId(text: string, index: number): string {
  const base = text
    .toLowerCase()
    .replace(/[^\w一-鿿]+/g, '-')
    .replace(/^-+|-+$/g, '')
  return base ? `${base}-${index}` : `heading-${index}`
}

function isRelativeMarkdownAsset(src: string): boolean {
  const trimmed = src.trim()
  if (!trimmed || /^[a-z][a-z0-9+.-]*:/i.test(trimmed) || trimmed.startsWith('//') || trimmed.startsWith('/')) {
    return false
  }
  const [pathPart] = trimmed.split(/([?#].*)/, 2)
  return pathPart
    .split('/')
    .filter((part) => part && part !== '.')
    .every((part) => part !== '..' && !part.includes('\\'))
}

function buildPageImageUrl(slug: string, src: string): string {
  const trimmed = src.trim()
  const [pathPart, suffix = ''] = trimmed.split(/([?#].*)/, 2)
  const encodedPath = pathPart
    .split('/')
    .filter((part) => part && part !== '.')
    .map((part) => encodeURIComponent(part))
    .join('/')
  return buildApiUrl(`/pages/${encodeURIComponent(slug)}/images/${encodedPath}${suffix}`)
}

export async function renderMarkdownPage(raw: string, slug: string): Promise<{ html: string; toc: TocItem[] }> {
    raw = raw.replace(
      /!\[([^\]]*)\]\(([^)]+)\)/g,
      (match, alt, src) => isRelativeMarkdownAsset(src) ? `![${alt}](${buildPageImageUrl(slug, src)})` : match
    )

    const html = await marked.parse(raw)
    const sanitized = DOMPurify.sanitize(html, {
      ADD_TAGS: ['iframe'],
      ADD_ATTR: ['allowfullscreen', 'frameborder', 'src'],
    })

    // Inject IDs into headings and build TOC
    const toc: TocItem[] = []
    let headingIndex = 0
    const withIds = sanitized.replace(
      /<(h[1-4])[^>]*>(.*?)<\/h[1-4]>/gi,
      (_, tag: string, content: string) => {
        const level = parseInt(tag[1])
        const text = content.replace(/<[^>]+>/g, '').trim()
        const id = generateHeadingId(text, headingIndex++)
        toc.push({ id, text, level })
        return `<${tag} id="${id}">${content}</${tag}>`
      }
    )

  return { html: withIds, toc }
}
