import { describe, expect, it } from 'vitest'
import { extractApiErrorCode, extractApiErrorMessage, extractApiErrorMetadata, extractI18nErrorMessage, readApiError } from '../apiError'

describe('unknown API error boundaries', () => {
  it('passes domain metadata through without applying another domain translation', () => {
    const error = { reason: 'INVALID_FIELD', metadata: { key: 'certSerial', keys: 'name,email' } }
    const requested: string[] = []
    const translated = extractI18nErrorMessage(error, (key, metadata) => {
      requested.push(key)
      return String(metadata?.key)
    }, 'auth.errors', 'fallback')
    expect(translated).toBe('certSerial')
    expect(requested).toEqual(['auth.errors.INVALID_FIELD'])
    expect(error.metadata).toEqual({ key: 'certSerial', keys: 'name,email' })
  })
  it('retains flat and response-shaped facts without inferring a business decision', () => {
    const error = {
      status: 409, code: 0, error: 'mixed_channel_warning', message: 'Review the change',
      reason: 'REVISION_CONFLICT', metadata: { revision: 'opaque' },
      response: { status: 429, data: { error: 'quota', detail: 'Detailed failure', message: 'Response message', code: 429 } }
    }
    expect(readApiError(error)).toEqual(error)
    expect(extractApiErrorCode(error)).toBe('REVISION_CONFLICT')
    expect(extractApiErrorMessage(error, 'fallback')).toBe('Review the change')
    expect(extractApiErrorMetadata(error)).toEqual({ revision: 'opaque' })
  })

  it.each([undefined, null, false, 0, 'failure', [], ['message']])('does not access fields of non-record input %s', value => {
    expect(readApiError(value)).toEqual({})
  })

  it('rejects invalid optional fields independently without losing a valid response detail', () => {
    const error = { status: '409', code: {}, message: { secret: 'not display text' }, error: [], reason: true,
      metadata: ['invalid'], response: { status: Infinity, data: { detail: 'Valid detail', message: 123, error: false, code: [] } } }
    const details = readApiError(error)
    expect(details.status).toBeUndefined()
    expect(details.code).toBeUndefined()
    expect(details.message).toBeUndefined()
    expect(details.response?.status).toBeUndefined()
    expect(details.response?.data?.detail).toBe('Valid detail')
    expect(extractApiErrorMessage(error, 'fallback')).toBe('Valid detail')
    expect(extractApiErrorMetadata(error)).toBeUndefined()
    expect(extractApiErrorCode(error)).toBeUndefined()
  })

  it('does not project headers, credentials or arbitrary response fields', () => {
    const details = readApiError({ response: { status: 500, headers: { Authorization: 'private' }, data: { detail: 'Failed', token: 'private' } } })
    expect(details.response).not.toHaveProperty('headers')
    expect(details.response?.data).not.toHaveProperty('token')
  })

  it('preserves native Error messages and established message fallback order', () => {
    const error = new Error('Original error')
    expect(extractApiErrorMessage(error)).toBe('Original error')
    expect(extractApiErrorMessage({ message: '', error: 'Flat failure', response: { data: { detail: 'Response detail' } } })).toBe('Flat failure')
    expect(extractApiErrorMessage({ response: null }, 'translated fallback')).toBe('translated fallback')
  })

  it('does not return a non-string message from a modified Error instance', () => {
    const error = new Error('before modification')
    Object.defineProperty(error, 'message', { value: { secret: 'not display text' } })
    expect(extractApiErrorMessage(error, 'translated fallback')).toBe('translated fallback')
  })
})
