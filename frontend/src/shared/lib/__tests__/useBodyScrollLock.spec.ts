import { afterEach, describe, expect, it } from 'vitest'
import { effectScope, nextTick, ref, type EffectScope } from 'vue'
import { useBodyScrollLock } from '../useBodyScrollLock'

const scopes: EffectScope[] = []
function owner(initial = true) {
  const active = ref(initial)
  const scope = effectScope()
  scopes.push(scope)
  scope.run(() => useBodyScrollLock(() => active.value))
  return { active, stop: () => scope.stop() }
}

afterEach(() => {
  for (const scope of scopes.splice(0)) scope.stop()
  document.body.removeAttribute('style')
  document.body.classList.remove('modal-open')
})

const locked = () => {
  expect(document.body.style.overflow).toBe('hidden')
  expect(document.body.classList.contains('modal-open')).toBe(true)
}

describe('body scroll ownership', () => {
  it.each(['', 'auto', 'scroll', 'hidden'])('restores the previous inline overflow %j after the last scope closes', async overflow => {
    document.body.style.overflow = overflow
    const first = owner()
    const second = owner()
    locked()
    first.active.value = false
    await nextTick()
    locked()
    first.stop()
    locked()
    second.stop()
    expect(document.body.style.overflow).toBe(overflow)
    expect(document.body.classList.contains('modal-open')).toBe(false)
  })

  it('ignores inactive owners and snapshots again on each new lock cycle', async () => {
    document.body.style.overflow = 'scroll'
    const hidden = owner(false)
    hidden.stop()
    expect(document.body.style.overflow).toBe('scroll')
    expect(document.body.classList.contains('modal-open')).toBe(false)
    const visible = owner()
    owner(false).stop()
    locked()
    visible.active.value = false
    await nextTick()
    expect(document.body.style.overflow).toBe('scroll')
    document.body.style.overflow = 'auto'
    visible.active.value = true
    await nextTick()
    locked()
    visible.stop()
    expect(document.body.style.overflow).toBe('auto')
  })

  it('preserves inline priorities, a pre-existing modal class and unrelated style changes', () => {
    document.body.style.setProperty('overflow', 'scroll', 'important')
    document.body.classList.add('modal-open')
    const visible = owner()
    locked()
    document.body.style.color = 'red'
    visible.stop()
    expect(document.body.style.overflow).toBe('scroll')
    expect(document.body.style.getPropertyPriority('overflow')).toBe('important')
    expect(document.body.style.color).toBe('red')
    expect(document.body.classList.contains('modal-open')).toBe(true)
  })

  it('restores independent overflow axes without leaving an inline shorthand', () => {
    document.body.style.setProperty('overflow-x', 'clip')
    document.body.style.setProperty('overflow-y', 'scroll', 'important')
    const visible = owner()
    locked()
    visible.stop()
    expect(document.body.style.overflow).toBe('')
    expect(document.body.style.overflowX).toBe('clip')
    expect(document.body.style.overflowY).toBe('scroll')
    expect(document.body.style.getPropertyPriority('overflow-y')).toBe('important')
  })
})
