import { formatDateTimeLocalInput, parseDateTimeLocalInput } from '@/shared/lib/format'
import type { Announcement, AnnouncementNotifyMode, AnnouncementStatus, AnnouncementTargeting,
  CreateAnnouncementRequest, UpdateAnnouncementRequest } from './announcement'

export interface AnnouncementDraft {
  title: string
  content: string
  status: AnnouncementStatus
  notify_mode: AnnouncementNotifyMode
  starts_at_str: string
  ends_at_str: string
  targeting: AnnouncementTargeting
}
const cloneTargeting = (value: AnnouncementTargeting): AnnouncementTargeting => JSON.parse(JSON.stringify(value))
const timestamp = (value?: string) => value ? Math.floor(new Date(value).getTime() / 1000) : null

export function createAnnouncementDraft(): AnnouncementDraft {
  return { title: '', content: '', status: 'draft', notify_mode: 'silent',
    starts_at_str: '', ends_at_str: '', targeting: { any_of: [] } }
}
export function announcementDraftFrom(value: Announcement): AnnouncementDraft {
  return { title: value.title, content: value.content, status: value.status, notify_mode: value.notify_mode || 'silent',
    starts_at_str: value.starts_at ? formatDateTimeLocalInput(timestamp(value.starts_at)!) : '',
    ends_at_str: value.ends_at ? formatDateTimeLocalInput(timestamp(value.ends_at)!) : '',
    targeting: cloneTargeting(value.targeting ?? { any_of: [] }) }
}
export function createAnnouncementPayload(draft: AnnouncementDraft): CreateAnnouncementRequest {
  return { title: draft.title, content: draft.content, status: draft.status, notify_mode: draft.notify_mode,
    targeting: cloneTargeting(draft.targeting), starts_at: parseDateTimeLocalInput(draft.starts_at_str) ?? undefined,
    ends_at: parseDateTimeLocalInput(draft.ends_at_str) ?? undefined }
}
export function updateAnnouncementPayload(draft: AnnouncementDraft, baseline: Announcement): UpdateAnnouncementRequest {
  const payload: UpdateAnnouncementRequest = {}
  if (draft.title !== baseline.title) payload.title = draft.title
  if (draft.content !== baseline.content) payload.content = draft.content
  if (draft.status !== baseline.status) payload.status = draft.status
  if (draft.notify_mode !== (baseline.notify_mode || 'silent')) payload.notify_mode = draft.notify_mode
  const starts = parseDateTimeLocalInput(draft.starts_at_str)
  const ends = parseDateTimeLocalInput(draft.ends_at_str)
  if (starts !== timestamp(baseline.starts_at)) payload.starts_at = starts === null ? 0 : starts
  if (ends !== timestamp(baseline.ends_at)) payload.ends_at = ends === null ? 0 : ends
  if (JSON.stringify(draft.targeting ?? {}) !== JSON.stringify(baseline.targeting ?? {})) payload.targeting = cloneTargeting(draft.targeting)
  return payload
}
