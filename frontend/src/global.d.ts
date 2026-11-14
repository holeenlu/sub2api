import type { PublicSettings } from '@/features/site/api/publicSettingsTypes';

declare global {
  interface Window {
    __APP_CONFIG__?: PublicSettings
  }
}

export {}
