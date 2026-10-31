import { describe, expect, it } from 'vitest'
import { announcementDraftFrom, createAnnouncementDraft, createAnnouncementPayload, updateAnnouncementPayload } from '../announcementDraft'
import type { Announcement } from '../announcement'
const baseline: Announcement = { id: 1, title: 'Title', content: 'Body', status: 'active', notify_mode: 'popup',
  targeting: { any_of: [{ all_of: [{ type: 'subscription', operator: 'in', group_ids: [1] }, { type: 'balance', operator: 'gte', value: 0 }] }] },
  starts_at: '2026-10-25T00:00:00Z', ends_at: '2026-10-26T00:00:00Z', created_at: '', updated_at: '' }
describe('announcement editable projection', () => {
  it('preserves omitted unchanged fields including date ranges and audience', () => {
    expect(updateAnnouncementPayload(announcementDraftFrom(baseline), baseline)).toEqual({})
  })
  it('encodes cleared existing dates as zero and empty new dates as omitted', () => {
    const draft = announcementDraftFrom(baseline)
    draft.starts_at_str = ''; draft.ends_at_str = ''
    expect(updateAnnouncementPayload(draft, baseline)).toEqual({ starts_at: 0, ends_at: 0 })
    expect(JSON.parse(JSON.stringify(createAnnouncementPayload(createAnnouncementDraft())))).not.toHaveProperty('starts_at')
  })
  it('keeps edits detached from the baseline and preserves explicit empty content and balance zero', () => {
    const draft = announcementDraftFrom(baseline)
    draft.content = ''
    draft.targeting.any_of![0].all_of![0].group_ids!.push(2)
    const payload = updateAnnouncementPayload(draft, baseline)
    expect(baseline.targeting.any_of![0].all_of![0].group_ids).toEqual([1])
    expect(payload.content).toBe('')
    expect(payload.targeting?.any_of?.[0].all_of?.[1].value).toBe(0)
    draft.targeting.any_of![0].all_of![0].group_ids!.push(3)
    expect(payload.targeting?.any_of?.[0].all_of?.[0].group_ids).toEqual([1, 2])
  })
  it('retains all-user targeting as an explicit empty group list', () => {
    const draft = announcementDraftFrom(baseline)
    draft.targeting = { any_of: [] }
    expect(updateAnnouncementPayload(draft, baseline)).toEqual({ targeting: { any_of: [] } })
  })
})
