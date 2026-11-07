import { useI18n } from 'vue-i18n'
import en from './locales/en'
import zh from './locales/zh'
import zhTW from './locales/zh-TW'
import ja from './locales/ja'

export function useManagedModelsI18n() {
  return useI18n({
    useScope: 'local',
    messages: {
      en: { managedModels: en },
      zh: { managedModels: zh },
      'zh-TW': { managedModels: zhTW },
      ja: { managedModels: ja },
    },
  })
}
