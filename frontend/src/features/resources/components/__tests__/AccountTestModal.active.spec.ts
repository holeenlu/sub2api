import { defineComponent, ref } from 'vue'
import { mount, flushPromises, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import AccountTestModal from '../AccountTestModal.vue'
import { resourceEditorServicesKey } from '../../services'
import { editorTestServices } from '../../__tests__/editorTestServices'
import { accountFixture } from '../../__tests__/accountFixture'
import type { Account } from '@/features/resources/api/accountTypes';

const { getAvailableModels } = vi.hoisted(() => ({ getAvailableModels: vi.fn() }))
vi.mock('@/features/resources/accounts', () => ({ default: { getAvailableModels } }))
vi.mock('@/shared/lib/useClipboard', () => ({ useClipboard: () => ({ copyToClipboard: vi.fn() }) }))
vi.mock('vue-i18n', async () => ({ ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'), useI18n: () => ({ t: (key: string, args?: { name?: string }) => args?.name ? key + ':' + args.name : key }) }))
const Select = defineComponent({
  props: { modelValue: String, options: Array, valueKey: { type: String, default: 'value' }, labelKey: { type: String, default: 'label' } },
  emits: ['update:modelValue'],
  template: '<select :value="modelValue" @change="$emit(\'update:modelValue\', $event.target.value)"><option v-for="option in options" :key="option[valueKey]" :value="option[valueKey]">{{ option[labelKey] }}</option></select>'
})
const dialogs: VueWrapper[] = []
function response(events: unknown[] = [{ type: 'test_complete', success: true }]): Response {
  return new Response(new ReadableStream<Uint8Array>({ start(controller) {
    for (const event of events) controller.enqueue(new TextEncoder().encode('data: ' + JSON.stringify(event) + '\n\n'))
    controller.close()
  } }), { status: 200, headers: { 'Content-Type': 'text/event-stream' } })
}
function deferred<T>() {
  let resolve: (value: T) => void = () => { throw new Error('Not initialized') }
  const promise = new Promise<T>(done => { resolve = done })
  return { promise, resolve }
}
function setup(account: Account = accountFixture({ platform: 'grok' })) {
  const services = editorTestServices(), actor = ref('actor-1:admin:scope-0')
  services.scopeKey = () => actor.value
  services.openAccountTest = vi.fn().mockImplementation(async () => response())
  const wrapper = mount(AccountTestModal, { props: { show: true, account }, global: {
    provide: { [resourceEditorServicesKey as symbol]: services },
    stubs: { BaseDialog: { props: ['show'], emits: ['close'], template: '<div v-if="show"><slot /><slot name="footer" /><button data-test="close" @click="$emit(\'close\')">close</button></div>' }, Select,
      TextArea: { props: ['modelValue'], emits: ['update:modelValue'], template: '<textarea :value="modelValue" @input="$emit(\'update:modelValue\', $event.target.value)" />' }, Icon: true }
  } })
  dialogs.push(wrapper)
  return { wrapper, services, actor }
}
async function start(wrapper: ReturnType<typeof setup>['wrapper']) {
  const button = wrapper.findAll('button').find(button => button.text().includes('admin.accounts.startTest'))
  if (!button) throw new Error('Start test action is absent')
  await button.trigger('click')
  await flushPromises()
}
async function upload(wrapper: ReturnType<typeof setup>['wrapper'], kind: 'image' | 'audio') {
  const input = wrapper.get('input[type=file]')
  Object.defineProperty(input.element, 'files', { configurable: true, value: [new File(['fixture'], kind === 'image' ? 'image.png' : 'audio.wav', { type: kind === 'image' ? 'image/png' : 'audio/wav' })] })
  await input.trigger('change')
  await vi.waitFor(() => expect(wrapper.text()).toContain(kind === 'image' ? 'image.png' : 'audio.wav'))
}
beforeEach(() => getAvailableModels.mockReset().mockResolvedValue([
  { id: 'grok-4.5', display_name: 'Grok text' }, { id: 'grok-imagine-image', display_name: 'Grok image' }, { id: 'grok-imagine-video', display_name: 'Grok video' }
]))
afterEach(() => dialogs.splice(0).forEach(wrapper => wrapper.unmount()))

describe('active account connection test protocol', () => {
  it.each(['text', 'image', 'video', 'search', 'tts', 'stt', 'realtime'] as const)('preserves the Grok %s request fields and mode', async mode => {
    const dialog = setup()
    await flushPromises()
    await dialog.wrapper.findAll('select')[0].setValue(mode)
    if (dialog.wrapper.find('textarea').exists()) await dialog.wrapper.get('textarea').setValue('operator prompt')
    if (mode === 'image' || mode === 'video') await upload(dialog.wrapper, 'image')
    if (mode === 'stt') await upload(dialog.wrapper, 'audio')
    await start(dialog.wrapper)
    expect(dialog.services.openAccountTest).toHaveBeenCalledTimes(1)
    const [id, payload, signal] = vi.mocked(dialog.services.openAccountTest).mock.calls[0]
    expect(id).toBe(42)
    expect(payload.mode).toBe(mode)
    expect(signal).toBeInstanceOf(AbortSignal)
    if (['search', 'tts', 'stt', 'realtime'].includes(mode)) expect(payload.model_id).toBe('')
    else expect(payload.model_id).toMatch(/^grok/)
    if (mode === 'image' || mode === 'video') expect(payload.image_data_url).toMatch(/^data:image\/png;base64,/)
    else expect(payload).not.toHaveProperty('image_data_url')
    if (mode === 'stt') expect(payload.audio_data_url).toMatch(/^data:audio\/wav;base64,/)
    else expect(payload).not.toHaveProperty('audio_data_url')
    expect(dialog.wrapper.emitted('completed')).toHaveLength(1)
  })

  it('keeps non-OpenAI/non-Grok mode omitted on the wire', async () => {
    getAvailableModels.mockResolvedValueOnce([{ id: 'claude-sonnet', display_name: 'Sonnet' }])
    const dialog = setup(accountFixture({ platform: 'anthropic' }))
    await flushPromises()
    await start(dialog.wrapper)
    const [, payload] = vi.mocked(dialog.services.openAccountTest).mock.calls[0]
    expect(payload).toEqual({ model_id: 'claude-sonnet', prompt: '' })
  })

  it('renders image, audio and video events from the active admin stream', async () => {
    const dialog = setup()
    vi.mocked(dialog.services.openAccountTest).mockResolvedValueOnce(response([
      { type: 'image', image_url: 'data:image/png;base64,QUJD', mime_type: 'image/png' },
      { type: 'audio', audio_url: 'data:audio/wav;base64,QUJD', mime_type: 'audio/wav' },
      { type: 'video', video_url: 'data:video/mp4;base64,QUJD', mime_type: 'video/mp4' },
      { type: 'test_complete', success: true }
    ]))
    await flushPromises()
    await start(dialog.wrapper)
    expect(dialog.wrapper.find('img').exists()).toBe(true)
    expect(dialog.wrapper.find('audio').exists()).toBe(true)
    expect(dialog.wrapper.find('video').exists()).toBe(true)
    expect(dialog.wrapper.emitted('completed')).toHaveLength(1)
  })

  it.each(['actor', 'account', 'close'] as const)('aborts and discards a late response after %s changes', async transition => {
    const result = deferred<Response>(), cancel = vi.fn()
    const lateResponse = new Response(new ReadableStream<Uint8Array>({ cancel }))
    const dialog = setup()
    vi.mocked(dialog.services.openAccountTest).mockReturnValueOnce(result.promise)
    await flushPromises()
    await start(dialog.wrapper)
    const signal = vi.mocked(dialog.services.openAccountTest).mock.calls[0][2]
    if (transition === 'actor') dialog.actor.value = 'actor-2:supplier:9'
    if (transition === 'account') await dialog.wrapper.setProps({ account: accountFixture({ id: 99, platform: 'grok' }) })
    if (transition === 'close') await dialog.wrapper.get('[data-test="close"]').trigger('click')
    expect(signal.aborted).toBe(true)
    result.resolve(lateResponse)
    await flushPromises()
    expect(cancel).toHaveBeenCalledTimes(1)
    expect(dialog.wrapper.emitted('completed')).toBeUndefined()
  })

  it('cancels a live reader when an old actor receives another terminal event', async () => {
    let controller: ReadableStreamDefaultController<Uint8Array> | undefined
    const cancel = vi.fn()
    const stream = new ReadableStream<Uint8Array>({ start(value) { controller = value }, cancel })
    const dialog = setup()
    vi.mocked(dialog.services.openAccountTest).mockResolvedValueOnce(new Response(stream))
    await flushPromises()
    await start(dialog.wrapper)
    dialog.actor.value = 'actor-2:admin:0'
    if (!controller) throw new Error('Stream did not start')
    controller.enqueue(new TextEncoder().encode('data: {"type":"test_complete","success":true}\n\n'))
    await flushPromises()
    expect(cancel).toHaveBeenCalledTimes(1)
    expect(dialog.wrapper.emitted('completed')).toBeUndefined()
  })
})

