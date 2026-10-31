import { expect, it, vi } from 'vitest'
import { defineComponent, h, reactive } from 'vue'
import { mount } from '@vue/test-utils'
import { resourceEditorServicesKey } from '../../services'
import { editorTestServices } from '../../__tests__/editorTestServices'
import { useResourceTaskScope, createResourceTaskScope, StaleResourceTask } from '../resourceTaskScope'

it.each(['canAuthorize', 'isSuperAdmin'] as const)('rejects the next write after %s is revoked in the same session', async capability => {
  const policy = reactive({ canAuthorize: true, isSuperAdmin: true })
  const services = editorTestServices()
  services.access = { isSimpleMode: true, get canAuthorize() { return policy.canAuthorize }, get isSuperAdmin() { return policy.isSuperAdmin } }
  let tasks!: ReturnType<typeof useResourceTaskScope>
  const wrapper = mount(defineComponent({ setup() { tasks = useResourceTaskScope(() => 7); return () => h('div') } }), { global: { provide: { [resourceEditorServicesKey as symbol]: services } } })
  let complete!: () => void
  const authorized = new Promise<void>(resolve => { complete = resolve })
  const write = vi.fn().mockResolvedValue(undefined)
  try {
    const task = tasks.capture()
    const waiting = (async () => { await task.wait(() => authorized); await task.wait(write) })()
    policy[capability] = false
    complete()
    await expect(waiting).rejects.toBeInstanceOf(StaleResourceTask)
    expect(write).not.toHaveBeenCalled()
  } finally { wrapper.unmount() }
})


it('invalidates an authorization target without invalidating independent editor loading', async () => {
  let proxy = 11
  const scope = createResourceTaskScope(() => 'same-admin')
  const authorization = scope.capture(() => ['openai', proxy])
  const reference = scope.capture()
  proxy = 22
  const write = vi.fn().mockResolvedValue(undefined)
  await expect(authorization.wait(write)).rejects.toBeInstanceOf(StaleResourceTask)
  expect(write).not.toHaveBeenCalled()
  await expect(reference.wait(async () => 'reference models')).resolves.toBe('reference models')
})
