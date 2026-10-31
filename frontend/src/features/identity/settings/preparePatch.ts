import type { UpdateSettingsRequest, SettingsDraft } from '@/features/system/api'
import type { WeChatDraft } from './weChatModel'
import type { AuthDefaultsDraft } from './authDefaultsModel'
type IdentityPatch = Partial<WeChatDraft & AuthDefaultsDraft>
export function prepareIdentitySettings(patch: IdentityPatch, draft: SettingsDraft): Partial<UpdateSettingsRequest> {
  if (Object.keys(patch).some(key => key.startsWith('wechat_connect_')) && draft.wechat_connect_mp_enabled && draft.wechat_connect_mobile_enabled) throw { field: 'wechat_connect_mp_enabled', messageKey: 'ui.officialAccountAndMobileAppCannotBeEnabledAt' }
  return {}
}
