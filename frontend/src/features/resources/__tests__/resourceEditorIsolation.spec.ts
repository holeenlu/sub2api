import { defineComponent, h, ref, type Ref } from 'vue'
import { mount, flushPromises } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import { resourceEditorServicesKey } from '../services'
import { createResourceTaskScope, StaleResourceTask } from '../queries/resourceTaskScope'
import { useAccountOAuth, useOpenAIOAuth, useGeminiOAuth, useAntigravityOAuth, useGrokOAuth } from '../queries'
import { usePanelField } from '../model/usePanelField'
import ResourceIdentityPanel from '../panels/ResourceIdentityPanel.vue'
import { editorTestServices } from './editorTestServices'

const { generateAccountURL } = vi.hoisted(() => ({ generateAccountURL: vi.fn() }))
vi.mock('@/features/resources/accounts', () => ({ default: { generateAuthUrl: generateAccountURL } }))
vi.mock('vue-i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('vue-i18n')>()),
  useI18n: () => ({ t: (key: string) => key })
}))

function deferred<T>() {
  let resolve: (value: T) => void = () => { throw new Error('Not initialized') }
  const promise = new Promise<T>(done => { resolve = done })
  return { promise, resolve }
}

interface OAuthProbe { start(): Promise<boolean>; url: Ref<string>; loading: Ref<boolean>; reset(): void }

for (const provider of ['anthropic', 'openai', 'gemini', 'antigravity', 'grok'] as const) {
  for (const transition of ['actor', 'close', 'reset'] as const) {
    it(provider + ' discards an authorization link after ' + transition, async () => {
      const scope = ref('session-1:admin:vendor-0')
      const services = editorTestServices()
      services.scopeKey = () => scope.value
      const response = deferred<{ auth_url: string; session_id: string; state: string }>()
      generateAccountURL.mockReturnValue(response.promise)
      services.credentialAuthorization.gemini.generateAuthUrl = vi.fn().mockReturnValue(response.promise)
      services.credentialAuthorization.antigravity.generateAuthUrl = vi.fn().mockReturnValue(response.promise)
      services.credentialAuthorization.grok.generateAuthUrl = vi.fn().mockReturnValue(response.promise)
      let probe: OAuthProbe | undefined
      const wrapper = mount(defineComponent({ setup() {
        if (provider === 'anthropic') {
          const flow = useAccountOAuth()
          probe = { start: () => flow.generateAuthUrl('oauth'), url: flow.authUrl, loading: flow.loading, reset: flow.resetState }
        } else if (provider === 'openai') {
          const flow = useOpenAIOAuth()
          probe = { start: () => flow.generateAuthUrl(null), url: flow.authUrl, loading: flow.loading, reset: flow.resetState }
        } else if (provider === 'gemini') {
          const flow = useGeminiOAuth()
          probe = { start: () => flow.generateAuthUrl(null), url: flow.authUrl, loading: flow.loading, reset: flow.resetState }
        } else if (provider === 'antigravity') {
          const flow = useAntigravityOAuth()
          probe = { start: () => flow.generateAuthUrl(null), url: flow.authUrl, loading: flow.loading, reset: flow.resetState }
        } else {
          const flow = useGrokOAuth()
          probe = { start: () => flow.generateAuthUrl(null), url: flow.authUrl, loading: flow.loading, reset: flow.resetState }
        }
        return () => h('div')
      } }), { global: { provide: { [resourceEditorServicesKey as symbol]: services } } })
      if (!probe) throw new Error('OAuth probe did not mount')
      const pending = probe.start()
      if (transition === 'actor') scope.value = 'session-2:supplier:vendor-9'
      if (transition === 'close') wrapper.unmount()
      if (transition === 'reset') probe.reset()
      response.resolve({ auth_url: 'https://auth.example.test/authorize?state=test', session_id: 'old-session', state: 'test' })
      expect(await pending).toBe(false)
      expect(probe.url.value).toBe('')
      expect(services.notices.showError).not.toHaveBeenCalled()
      if (transition !== 'close') wrapper.unmount()
    })
  }
}

describe('resource command and panel ownership', () => {
  it('checks the captured actor before a second command can be dispatched', async () => {
    let actor = 'actor-a'
    const scope = createResourceTaskScope(() => actor)
    const task = scope.capture()
    actor = 'actor-b'
    const send = vi.fn().mockResolvedValue({ ok: true })
    await expect(task.wait(send)).rejects.toBeInstanceOf(StaleResourceTask)
    expect(send).not.toHaveBeenCalled()
  })

  it('discards a completed remote command after the editor closes and reopens', async () => {
    const scope = createResourceTaskScope(() => 'same-actor')
    const task = scope.capture()
    const result = deferred<{ id: number }>()
    const waiting = task.wait(() => result.promise)
    scope.invalidate()
    result.resolve({ id: 12 })
    await expect(waiting).rejects.toBeInstanceOf(StaleResourceTask)
  })

  it('updates resource labels through events without mutating a frozen input', async () => {
    const model = Object.freeze({ name: 'source', notes: 'unchanged' })
    const wrapper = mount(ResourceIdentityPanel, { props: { modelValue: model } })
    await wrapper.get('input').setValue('edited')
    expect(model).toEqual({ name: 'source', notes: 'unchanged' })
    expect(wrapper.emitted('update:modelValue')?.[0]).toEqual([{ name: 'edited', notes: 'unchanged' }])
    wrapper.unmount()
  })

  it('owns nested draft rows and accepts newer parent snapshots without feedback loops', async () => {
    const original = Object.freeze([{ from: 'client', to: 'upstream' }])
    const parent = ref(original)
    const update = vi.fn()
    let rows: Ref<readonly { from: string; to: string }[]> | undefined
    const wrapper = mount(defineComponent({ setup() {
      rows = usePanelField(() => parent.value, update)
      return () => h('div')
    } }))
    if (!rows) throw new Error('Panel did not mount')
    rows.value[0].to = 'edited'
    expect(original[0].to).toBe('upstream')
    expect(update).toHaveBeenLastCalledWith([{ from: 'client', to: 'edited' }])
    parent.value = [{ from: 'new', to: 'snapshot' }]
    await flushPromises()
    expect(rows.value[0]).toEqual({ from: 'new', to: 'snapshot' })
    expect(update).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })
})

