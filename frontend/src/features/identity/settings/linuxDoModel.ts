import { toRaw } from 'vue'
import type { SettingsDraft } from '@/features/system/api'
export type LinuxDoDraft = Pick<SettingsDraft, 'linuxdo_connect_enabled' | 'linuxdo_connect_client_id' | 'linuxdo_connect_client_secret' | 'linuxdo_connect_client_secret_configured' | 'linuxdo_connect_redirect_url'>
export const linuxDoFields = ['linuxdo_connect_enabled', 'linuxdo_connect_client_id', 'linuxdo_connect_client_secret', 'linuxdo_connect_client_secret_configured', 'linuxdo_connect_redirect_url', 'api_base_url'] as const
export function projectLinuxDo(settings: SettingsDraft): LinuxDoDraft {
  return {
    linuxdo_connect_enabled: structuredClone(toRaw(settings.linuxdo_connect_enabled ?? (false))),
    linuxdo_connect_client_id: structuredClone(toRaw(settings.linuxdo_connect_client_id ?? (""))),
    linuxdo_connect_client_secret: structuredClone(toRaw(settings.linuxdo_connect_client_secret ?? (""))),
    linuxdo_connect_client_secret_configured: structuredClone(toRaw(settings.linuxdo_connect_client_secret_configured ?? (false))),
    linuxdo_connect_redirect_url: structuredClone(toRaw(settings.linuxdo_connect_redirect_url ?? (""))),
  }
}
