import { toRaw } from 'vue'
import type { SettingsDraft } from '@/features/system/api'
export type NavigationDraft = Pick<SettingsDraft, 'custom_menu_items'>
export const navigationFields = ['custom_menu_items'] as const
export function projectNavigation(settings: SettingsDraft): NavigationDraft {
  return {
    custom_menu_items: structuredClone(toRaw(settings.custom_menu_items ?? ([] as Array<{
    id: string;
    label: string;
    icon_svg: string;
    url: string;
    visibility: "user" | "admin";
    sort_order: number;
    hide_open_button?: boolean;
  }>))),
  }
}
