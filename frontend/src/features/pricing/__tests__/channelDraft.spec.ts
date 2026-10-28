import { describe, expect, it } from 'vitest'
import { channelDraftToAPI, channelToSections } from '../model/channelDraft'
import type { Channel } from '../api/types'
function channel(patch: Partial<Channel> = {}): Channel {
 return { id: 1, name: 'sample', description: '', status: 'active', billing_model_source: 'channel_mapped', restrict_models: false, group_ids: [], model_pricing: [], model_mapping: {}, apply_pricing_to_account_stats: false, account_stats_pricing_rules: [], created_at: '', updated_at: '', ...patch }
}
describe('channel draft projection', () => {
 it('preserves unknown directory entries and unrelated server configuration', () => {
  const source = channel({ model_mapping: { new_provider: { requested: 'upstream' } }, features_config: { server_owned: { enabled: false } } })
  const sections = channelToSections(source, [], ['openai'])
  expect(sections.map(section => section.platform)).toContain('new_provider')
  const payload = channelDraftToAPI(sections, source)
  expect(payload.model_mapping).toEqual(source.model_mapping)
  expect(payload.features_config.server_owned).toEqual({ enabled: false })
  expect(source.model_mapping.new_provider).toEqual({ requested: 'upstream' })
 })
 it('round trips per-token display units, null prices, explicit zero and feature toggles', () => {
  const source = channel({ features_config: { bedrock_cc_compat: { anthropic: true } }, model_pricing: [{ platform: 'anthropic', models: ['model'], billing_mode: 'token', input_price: 0, output_price: 0.000003, cache_write_price: null, cache_read_price: null, image_input_price: null, image_output_price: null, per_request_price: null, intervals: [], time_pricing: null }] })
  const sections = channelToSections(source, [], ['anthropic'])
  expect(sections[0].model_pricing[0].output_price).toBe(3)
  expect(sections[0].bedrock_cc_compat).toBe(true)
  const payload = channelDraftToAPI(sections, source)
  expect(payload.model_pricing[0]).toMatchObject({ input_price: 0, output_price: 0.000003, cache_read_price: null })
  expect(payload.features_config.bedrock_cc_compat).toEqual({ anthropic: true })
 })
})
