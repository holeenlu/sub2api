import { describe, expect, it, vi } from 'vitest'
import { observePeerSession } from '../peerSession'

describe('peer-session lifecycle', () => {
  it('restores only local authentication changes and unregisters on teardown', () => {
    const restore = vi.fn()
    const stop = observePeerSession(restore)
    window.dispatchEvent(new StorageEvent('storage', { key: 'theme', storageArea: localStorage }))
    window.dispatchEvent(new StorageEvent('storage', { key: 'auth_token', storageArea: sessionStorage }))
    expect(restore).not.toHaveBeenCalled()
    window.dispatchEvent(new StorageEvent('storage', { key: 'auth_token', storageArea: localStorage }))
    window.dispatchEvent(new StorageEvent('storage', { key: 'auth_user', storageArea: localStorage }))
    window.dispatchEvent(new StorageEvent('storage', { key: null, storageArea: localStorage }))
    expect(restore).toHaveBeenCalledTimes(3)
    stop()
    window.dispatchEvent(new StorageEvent('storage', { key: 'auth_token', storageArea: localStorage }))
    expect(restore).toHaveBeenCalledTimes(3)
  })
})
