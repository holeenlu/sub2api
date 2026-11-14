import { projectDefaultSubscriptions } from '@/features/subscriptions/settings/defaultSubscriptionsModel'
import { projectConsole } from '@/features/system/settings/consoleModel'
import type { SettingsDraft } from '@/features/system/api'
import { describe, expect, it } from 'vitest'
import { prepareSettingsPatch } from '../preparePatch'
import { settingsFixture } from './fixture'
import { projectBpsImages } from '@/features/resources/settings/bpsImagesModel'
describe('domain settings projections', () => {
  it('does not add defaults or secrets to a site-only update', () => {
    expect(prepareSettingsPatch({ site_name: 'Updated' }, settingsFixture)).toEqual({ site_name: 'Updated' })
  })
  it('retains explicit false, zero and empty fields', () => {
    const patch = { payment_enabled: false, default_balance: 0, site_name: '', login_agreement_documents: [] }
    expect(prepareSettingsPatch(patch, { ...settingsFixture, ...patch })).toEqual(patch)
  })
  it('converts only an explicitly edited empty OAuth rate to null', () => {
    expect(prepareSettingsPatch({ openai_oauth_scheduling_rate_multiplier: null }, { ...settingsFixture, openai_oauth_scheduling_rate_multiplier: '' })).toEqual({ openai_oauth_scheduling_rate_multiplier: null })
  })
  it('rejects malicious website URLs before any request', () => {
    expect(() => prepareSettingsPatch({ doc_url: 'javascript:alert(1)' }, settingsFixture)).toThrow()
  })
  it('retains native image limits independently from relay storage and validates a relay switch', () => {
    const draft = { ...settingsFixture, ...projectBpsImages(settingsFixture), excel_bps_image_mode: 'native' as const, excel_bps_image_max_images: 500, excel_bps_image_storage_entries: 10 }
    expect(() => prepareSettingsPatch({ excel_bps_image_max_images: 500 }, draft)).not.toThrow()
    expect(() => prepareSettingsPatch({ excel_bps_image_mode: 'relay' }, { ...draft, excel_bps_image_mode: 'relay', excel_bps_image_base_url: 'https://relay.example.invalid' })).toThrow()
  })
  it('rejects insufficient BPS budget without rewriting it to defaults', () => {
    const draft = { ...settingsFixture, ...projectBpsImages(settingsFixture), excel_bps_image_budget_mib: 1 }
    expect(() => prepareSettingsPatch({ excel_bps_image_budget_mib: 1 }, draft)).toThrow()
    expect(draft.excel_bps_image_budget_mib).toBe(1)
  })
  it('projects unset collections and console defaults without adding them to a site patch', () => {
    const raw = { ...settingsFixture, default_subscriptions: undefined, table_page_size_options: undefined, vendor_portal_origin: undefined }
    // @ts-expect-error Deliberately incomplete server data exercises the boundary defaults.
    const fields: SettingsDraft = raw
    expect(projectDefaultSubscriptions(fields).default_subscriptions).toEqual([])
    expect(projectConsole(fields).table_page_size_options).toEqual([10, 20, 50, 100])
    expect(prepareSettingsPatch({ site_name: 'Only change' }, fields)).toEqual({ site_name: 'Only change' })
  })

})
