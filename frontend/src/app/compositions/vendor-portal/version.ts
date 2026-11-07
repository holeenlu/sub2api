import { onScopeDispose, reactive } from 'vue'
import apiClient, { type HttpRequestConfig } from '@/shared/http'
import { getVersion } from '@/features/system'

function readString(value: unknown): string {
  return typeof value === 'string' && value.trim().toLowerCase() !== 'unknown' ? value.trim() : ''
}

function readBuild(value: unknown): { version: string; commit: string } {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return { version: '', commit: '' }
  const data = value as Record<string, unknown>
  return { version: readString(data.version), commit: readString(data.commit) }
}

/** Independent runtime facts: a backend version must never stand in for the portal build. */
export function usePortalVersion() {
  let generation = 0
  let controller: AbortController | undefined
  let inFlight: Promise<boolean> | null = null
  const state = reactive({
    versionLoaded: false,
    versionLoading: false,
    currentVersion: '',
    backendVersion: '',
    upstreamVersion: '',
    buildCommit: '',
    backendBuildCommit: '',
    fetchVersion,
    clearVersionCache
  })

  function fetchVersion(force = false): Promise<boolean> {
    if (inFlight) return inFlight
    if (state.versionLoaded && !force) return Promise.resolve(!!state.currentVersion && !!state.backendVersion)
    const version = generation
    controller?.abort()
    const requestController = new AbortController()
    controller = requestController
    state.versionLoading = true
    const manifestConfig: HttpRequestConfig = { baseURL: '/', signal: requestController.signal, sessionPolicy: 'anonymous' }
    const request = Promise.allSettled([
      apiClient.get<unknown>('/portal-build.json', manifestConfig),
      getVersion({ signal: requestController.signal })
    ]).then(([build, backend]) => {
      if (version !== generation) return false
      const manifest = build.status === 'fulfilled' ? readBuild(build.value.data) : { version: '', commit: '' }
      state.currentVersion = manifest.version
      state.buildCommit = manifest.commit
      state.backendVersion = backend.status === 'fulfilled' ? readString(backend.value.version) : ''
      state.upstreamVersion = backend.status === 'fulfilled' ? readString(backend.value.upstream_version) : ''
      state.backendBuildCommit = backend.status === 'fulfilled' ? readString(backend.value.build_commit) : ''
      state.versionLoaded = true
      return !!state.currentVersion && !!state.backendVersion
    }).finally(() => {
      if (version === generation) state.versionLoading = false
      if (inFlight === request) inFlight = null
    })
    inFlight = request
    return request
  }

  function clearVersionCache(): void {
    generation++
    controller?.abort()
    controller = undefined
    inFlight = null
    state.versionLoaded = false
    state.versionLoading = false
    state.currentVersion = ''
    state.backendVersion = ''
    state.upstreamVersion = ''
    state.buildCommit = ''
    state.backendBuildCommit = ''
  }

  onScopeDispose(clearVersionCache)
  return state
}
