import { apiClient } from '@/shared/http'
export { renderMarkdownPage } from './model/markdownPage'
export type { TocItem } from './model/markdownPage'

/** The configured page endpoint returns raw Markdown, using the shared session boundary. */
export async function getMarkdownPage(slug: string, options: { signal: AbortSignal }): Promise<string> {
  const { data } = await apiClient.get<string>('/pages/' + encodeURIComponent(slug), { responseType: 'text', signal: options.signal })
  return data
}
