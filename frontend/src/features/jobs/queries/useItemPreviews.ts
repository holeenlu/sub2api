import { computed, onMounted, onScopeDispose, reactive, readonly, ref, watch } from 'vue'
import type { Ref } from 'vue'
import { getBatchImageItemContent } from '../api/batchImage'
import type { BatchImageItem } from '../api/batchImage'
import { createPreviewCache, previewCacheKey } from '../model/previewCache'
import { createPreviewThumbnail } from '../model/previewThumbnail'
import { isJobWaitCancelled, useJobsRequest } from './useJobsRequest'

interface PreviewContext {
  scopeKey: Readonly<Ref<string | null>>
  apiKeyId: Readonly<Ref<number>>
  batchId: Readonly<Ref<string>>
  readApiKey(): string | null
  onError(error: unknown): void
}

interface ItemPreviews {
  itemPreviewUrls: Readonly<Record<string, string>>
  previewLoadingIds: Readonly<Ref<ReadonlySet<string>>>
  previewErrorIds: Readonly<Ref<ReadonlySet<string>>>
  previewImageItem: Readonly<Ref<BatchImageItem | null>>
  previewImageUrl: Readonly<Ref<string>>
  itemPreviewKey(item: Pick<BatchImageItem, 'batch_id' | 'custom_id'>): string
  canLoadItemPreview(item: BatchImageItem): boolean
  hydrateCachedItemPreviews(items: BatchImageItem[]): Promise<void>
  loadItemPreview(item: BatchImageItem): Promise<void>
  openImagePreview(item: BatchImageItem): void
  closeImagePreview(): void
  handlePreviewError(key: string, url: string): void
  clearItemPreviews(): void
}

/** The detail's preview owner: requests, cache connection, decoding and displayed URL lifetimes. */
export function useItemPreviews(context: PreviewContext): ItemPreviews {
  const cache = createPreviewCache()
  const requests = useJobsRequest(context)
  const itemPreviewUrls = reactive<Record<string, string>>({})
  const previewLoadingIds = ref(new Set<string>())
  const previewErrorIds = ref(new Set<string>())
  const previewImageItem = ref<BatchImageItem | null>(null)
  let cleanupTimer: ReturnType<typeof setInterval> | undefined

  function itemPreviewKey(item: Pick<BatchImageItem, 'batch_id' | 'custom_id'>): string {
    return previewCacheKey(context.scopeKey.value, context.apiKeyId.value, item.batch_id || context.batchId.value, item.custom_id)
  }

  function canLoadItemPreview(item: BatchImageItem): boolean {
    return (item.status === 'succeeded' || item.status === 'success') && item.image_count > 0
  }

  function revoke(key: string): void {
    if (itemPreviewUrls[key]) URL.revokeObjectURL(itemPreviewUrls[key])
    delete itemPreviewUrls[key]
  }

  function clearItemPreviews(): void {
    requests.cancel()
    for (const key of Object.keys(itemPreviewUrls)) revoke(key)
    previewLoadingIds.value = new Set()
    previewErrorIds.value = new Set()
    previewImageItem.value = null
  }

  async function hydrateCachedItemPreviews(items: BatchImageItem[]): Promise<void> {
    await Promise.all(items.filter(canLoadItemPreview).map(async item => {
      const key = itemPreviewKey(item)
      if (!(item.batch_id || context.batchId.value) || itemPreviewUrls[key]
        || previewLoadingIds.value.has(key) || previewErrorIds.value.has(key)) return
      // Cache hydration and a click share the item lane. A click supersedes a late cache read.
      const request = requests.begin('preview:' + key)
      if (!request.isCurrent()) return
      try {
        const blob = await request.wait(cache.get(key, request.signal))
        request.check()
        if (blob && !itemPreviewUrls[key]) itemPreviewUrls[key] = URL.createObjectURL(blob)
      } catch (error: unknown) {
        if (request.isCurrent() && !isJobWaitCancelled(error)) context.onError(error)
      }
    }))
  }

  async function loadItemPreview(item: BatchImageItem): Promise<void> {
    const batchId = item.batch_id || context.batchId.value
    const key = itemPreviewKey(item)
    if (!batchId || !canLoadItemPreview(item) || (itemPreviewUrls[key] && !previewErrorIds.value.has(key))) return
    const apiKey = context.readApiKey()
    if (!apiKey) return
    // The complete key keeps equal custom IDs in parent/child batches independent.
    const request = requests.begin('preview:' + key)
    if (!request.isCurrent()) return
    const retry = previewErrorIds.value.has(key)
    previewLoadingIds.value.add(key)
    previewErrorIds.value.delete(key)
    revoke(key)
    try {
      // A displayed image may be undecodable even if IDB returned a valid Blob.
      // An explicit retry must reach the content API if eviction was denied or cancelled.
      const cached = retry ? null : await request.wait(cache.get(key, request.signal))
      request.check()
      if (cached) {
        itemPreviewUrls[key] = URL.createObjectURL(cached)
        return
      }
      const blob = await request.wait(getBatchImageItemContent(apiKey, batchId, item.custom_id, 0, request.signal))
      let thumbnail: Blob
      try {
        thumbnail = await createPreviewThumbnail(blob, request.signal)
      } catch (error) {
        // Decoding failure retains the existing original-image fallback, cancellation does not.
        if (isJobWaitCancelled(error)) throw error
        thumbnail = blob
      }
      request.check()
      itemPreviewUrls[key] = URL.createObjectURL(thumbnail)
      if (thumbnail !== blob || thumbnail.size <= 1024 * 1024) {
        await cache.put(key, thumbnail, request.signal)
      }
    } catch (error: unknown) {
      if (!request.isCurrent() || isJobWaitCancelled(error)) return
      previewErrorIds.value.add(key)
      context.onError(error)
    } finally {
      if (request.isCurrent()) previewLoadingIds.value.delete(key)
    }
  }

  function openImagePreview(item: BatchImageItem): void {
    const key = itemPreviewKey(item)
    if (itemPreviewUrls[key] && !previewErrorIds.value.has(key)) previewImageItem.value = item
  }

  function closeImagePreview(): void { previewImageItem.value = null }

  function handlePreviewError(key: string, url: string): void {
    // DOM error events can arrive after the URL was replaced or its owner was cleared.
    if (!url || itemPreviewUrls[key] !== url) return
    const request = requests.begin('preview:' + key)
    if (!request.isCurrent()) return
    revoke(key)
    previewLoadingIds.value.delete(key)
    previewErrorIds.value.add(key)
    if (previewImageItem.value && itemPreviewKey(previewImageItem.value) === key) closeImagePreview()
    // The optional cache absorbs storage failures; cancellation also aborts this eviction.
    void cache.remove(key, request.signal)
  }

  const previewImageUrl = computed(() => previewImageItem.value ? itemPreviewUrls[itemPreviewKey(previewImageItem.value)] || '' : '')
  watch([context.scopeKey, context.apiKeyId, context.batchId], clearItemPreviews, { flush: 'sync' })
  onMounted(() => {
    void cache.cleanup()
    cleanupTimer = setInterval(() => { void cache.cleanup() }, 60 * 60 * 1000)
  })
  onScopeDispose(() => {
    clearItemPreviews()
    if (cleanupTimer !== undefined) clearInterval(cleanupTimer)
    cache.dispose()
  })

  return {
    itemPreviewUrls: readonly(itemPreviewUrls),
    previewLoadingIds: readonly(previewLoadingIds),
    previewErrorIds: readonly(previewErrorIds),
    previewImageItem: readonly(previewImageItem),
    previewImageUrl,
    itemPreviewKey, canLoadItemPreview, hydrateCachedItemPreviews, loadItemPreview,
    openImagePreview, closeImagePreview, handlePreviewError, clearItemPreviews,
  }
}
