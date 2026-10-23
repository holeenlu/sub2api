import { reactive } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ContainerUpdateStatus, ReleaseInfo, UpdateResult, VersionInfo } from '@/api/admin/system'

type PortalStatus = ContainerUpdateStatus & {
  portal_version: string
  portal_commit?: string
  backend_version: string
  mode: 'portal' | 'paired'
}

// The shared version badge owns the interaction and polling. This source only
// reads the portal's actual build and routes updates to its local host updater.
export function usePortalVersion() {
  const { t } = useI18n()
  const state = reactive({
    versionLoading: false,
    currentVersion: '',
    latestVersion: '',
    hasUpdate: false,
    releaseInfo: null as ReleaseInfo | null,
    buildType: 'release',
    buildCommit: '',
    updateDisabled: false,
    updateMethod: 'manual' as 'manual' | 'compose',
    releaseChannel: '',
    updateWarning: '',
    containerUpdate: undefined as ContainerUpdateStatus | undefined,
    mode: '' as '' | 'portal' | 'paired',
    clearVersionCache() { /* Portal checks always read the running containers. */ },
    async fetchVersion(_force = false): Promise<VersionInfo | null> {
      if (state.versionLoading) return null
      state.versionLoading = true
      try {
        const { default: api } = await import('@/api/client')
        const [build, backend, status] = await Promise.allSettled([
          api.get<{ version: string; commit?: string }>('/portal-build.json', { baseURL: '/' }),
          api.get<{ version: string }>('/admin/system/version'),
          api.get<PortalStatus>('/portal-updater/status'),
        ])
        if (build.status === 'fulfilled') {
          state.currentVersion = build.value.data.version || state.currentVersion
          state.buildCommit = build.value.data.commit || ''
        }
        if (backend.status === 'fulfilled') state.latestVersion = backend.value.data.version || ''
        if (status.status === 'fulfilled' && ['portal', 'paired'].includes(status.value.data?.mode)) {
          const data = status.value.data
          state.currentVersion = data.portal_version || state.currentVersion
          state.buildCommit = data.portal_commit || state.buildCommit
          state.latestVersion = data.backend_version
          state.containerUpdate = data
          state.mode = data.mode
          state.updateMethod = 'compose'
          state.updateWarning = ''
        } else {
          // Keep an accepted update's running state through container downtime.
          state.updateMethod = 'manual'
          state.updateWarning = t('vendor.portalUpdate.setupRequired')
        }
        const versionsKnown = !!state.currentVersion && /^\d+\.\d+\.\d+(\.\d+)?$/.test(state.latestVersion)
        if (!versionsKnown && !state.updateWarning) {
          state.updateMethod = 'manual'
          state.updateWarning = t('vendor.portalUpdate.versionUnavailable')
        }
        state.hasUpdate = versionsKnown && state.currentVersion !== state.latestVersion
        return {
          current_version: state.currentVersion, latest_version: state.latestVersion,
          has_update: state.hasUpdate, build_type: 'release', cached: false,
          update_method: state.updateMethod, warning: state.updateWarning,
          container_update: state.containerUpdate,
        }
      } finally {
        state.versionLoading = false
      }
    },
    async performUpdate(): Promise<UpdateResult> {
      if (state.updateMethod !== 'compose' || !state.hasUpdate) throw new Error(t('vendor.portalUpdate.setupRequired'))
      const { default: api } = await import('@/api/client')
      await api.post('/portal-updater/update', { version: state.latestVersion })
      return { message: '', need_restart: false }
    },
  })
  return state
}
