import { describe, expect, it } from 'vitest'
import { ceremonyOptionsFromJSON, creationOptionsFromJSON, requestOptionsFromJSON } from '../passkeyOptions'

// Synthetic JSON follows passkey.go's go-webauthn options, including credProps.
const creation = {
  challenge: '-_8',
  rp: { id: 'sub2api.example.com', name: 'Sub2API' },
  user: { id: 'AQID', name: 'user@example.com', displayName: '' },
  pubKeyCredParams: [{ type: 'public-key', alg: -7 }, { type: 'public-key', alg: -257 }]
}

describe('Passkey JSON boundary', () => {
  it('builds independent native registration options with all supported optional fields', () => {
    const source = {
      ...creation, timeout: 0, attestation: 'none',
      excludeCredentials: [{ type: 'public-key', id: 'BAU=', transports: ['internal', 'usb', 'nfc', 'ble', 'hybrid'] }],
      authenticatorSelection: { authenticatorAttachment: 'cross-platform', residentKey: 'required', requireResidentKey: false, userVerification: 'required' },
      extensions: { credProps: true, appid: '', hmacCreateSecret: false, minPinLength: false }
    }
    const result: PublicKeyCredentialCreationOptions = creationOptionsFromJSON(source)
    expect(result).toStrictEqual({
      ...source,
      challenge: Uint8Array.from([251, 255]).buffer,
      user: { ...source.user, id: Uint8Array.from([1, 2, 3]).buffer },
      excludeCredentials: [{ type: 'public-key', id: Uint8Array.from([4, 5]).buffer, transports: ['internal', 'usb', 'nfc', 'ble', 'hybrid'] }]
    })
    expect(result.rp).not.toBe(source.rp)
    expect(result.pubKeyCredParams).not.toBe(source.pubKeyCredParams)
    expect(result.authenticatorSelection).not.toBe(source.authenticatorSelection)
    expect(result.extensions).not.toBe(source.extensions)
    expect(source.user.id).toBe('AQID')
  })

  it('keeps discoverable login optional fields absent and empty descriptor lists valid', () => {
    expect(requestOptionsFromJSON({ challenge: 'AQID' })).toStrictEqual({ challenge: Uint8Array.from([1, 2, 3]).buffer })
    expect(requestOptionsFromJSON({ challenge: 'AQID', allowCredentials: [], timeout: 0, rpId: '', userVerification: 'discouraged', extensions: { credProps: false } })).toStrictEqual({
      challenge: Uint8Array.from([1, 2, 3]).buffer, allowCredentials: [], timeout: 0, rpId: '', userVerification: 'discouraged', extensions: { credProps: false }
    })
    expect(creationOptionsFromJSON({ ...creation, excludeCredentials: [] }).excludeCredentials).toStrictEqual([])
  })

  it('decodes login allowCredentials and preserves verification, RP and timeout', () => {
    expect(requestOptionsFromJSON({ challenge: 'AQID', rpId: 'sub2api.example.com', timeout: 60000, userVerification: 'preferred', allowCredentials: [{ type: 'public-key', id: 'BAU', transports: ['internal'] }] })).toStrictEqual({
      challenge: Uint8Array.from([1, 2, 3]).buffer, rpId: 'sub2api.example.com', timeout: 60000, userVerification: 'preferred',
      allowCredentials: [{ type: 'public-key', id: Uint8Array.from([4, 5]).buffer, transports: ['internal'] }]
    })
  })

  it.each(['smart-card', 'future-transport'])('keeps %s credentials with unrestricted transport discovery', transport => {
    const descriptors = [{ type: 'public-key', id: 'BAU', transports: ['usb', transport] }]
    const expected = [{ type: 'public-key', id: Uint8Array.from([4, 5]).buffer }]
    expect(requestOptionsFromJSON({ challenge: 'AQID', allowCredentials: descriptors }).allowCredentials).toStrictEqual(expected)
    expect(creationOptionsFromJSON({ ...creation, excludeCredentials: descriptors }).excludeCredentials).toStrictEqual(expected)
    expect(descriptors[0]?.transports).toStrictEqual(['usb', transport])
  })

  it('validates every transport before considering compatibility fallback', () => {
    expect(() => requestOptionsFromJSON({ challenge: 'AQID', allowCredentials: [{ type: 'public-key', id: 'BAU', transports: ['smart-card', 42] }] })).toThrow('Invalid Passkey options: allowCredentials[0].transports[1]')
  })

  it.each([undefined, null, 12, true, [], {}, { challenge: 123 }, { challenge: {} }, { challenge: '' }, { challenge: 'A' }, { challenge: 'AB' }, { challenge: 'AQ I' }, { challenge: 'AQID=' }, { challenge: 'AQ+' }])('rejects malformed challenge/options %# without string coercion', value => {
    expect(() => requestOptionsFromJSON(value)).toThrow('Invalid Passkey options:')
  })

  it.each([
    { rp: null }, { rp: { name: 1 } }, { rp: { name: 'rp', id: false } },
    { user: null }, { user: { ...creation.user, id: 42 } }, { user: { ...creation.user, id: btoa('x'.repeat(65)) } },
    { user: { ...creation.user, name: false } }, { user: { ...creation.user, displayName: undefined } },
    { pubKeyCredParams: undefined }, { pubKeyCredParams: [] }, { pubKeyCredParams: {} },
    { pubKeyCredParams: [null] }, { pubKeyCredParams: [{ type: 'password', alg: -7 }] },
    { pubKeyCredParams: [{ type: 'public-key', alg: '-7' }] }, { pubKeyCredParams: [{ type: 'public-key', alg: 1.5 }] },
    { pubKeyCredParams: [{ type: 'public-key', alg: Infinity }] },
    { authenticatorSelection: [] }, { authenticatorSelection: { residentKey: 'yes' } },
    { authenticatorSelection: { requireResidentKey: 'false' } }, { authenticatorSelection: { authenticatorAttachment: 'external' } },
    { authenticatorSelection: { userVerification: null } }, { attestation: false },
    { excludeCredentials: {} }, { excludeCredentials: [{ type: 'public-key', id: {} }] }
  ])('rejects malformed registration field %#', patch => {
    expect(() => creationOptionsFromJSON({ ...creation, ...patch })).toThrow('Invalid Passkey options:')
  })

  it.each([
    { timeout: -1 }, { timeout: 1.5 }, { timeout: NaN }, { timeout: Infinity }, { timeout: '60000' }, { timeout: 4294967296 },
    { rpId: 9 }, { userVerification: 'yes' }, { allowCredentials: null }, { allowCredentials: [null] },
    { allowCredentials: [{ type: 'password', id: 'AQID' }] }, { allowCredentials: [{ type: 'public-key', id: 'AQID', transports: 'usb' }] },
    { allowCredentials: [{ type: 'public-key', id: 'AQID', transports: [42] }] },
    { extensions: null }, { extensions: [] }, { extensions: { credProps: 'true' } },
    { extensions: { appid: 0 } }, { extensions: { hmacCreateSecret: null } }, { extensions: { minPinLength: 1 } }
  ])('rejects malformed request field %#', patch => {
    expect(() => requestOptionsFromJSON({ challenge: 'AQID', ...patch })).toThrow('Invalid Passkey options:')
  })

  it.each([null, [], {}, { session_token: 123, options: {} }, { session_token: '', options: {} }, { session_token: 'session', options: null }])('rejects malformed ceremony envelope %#', value => {
    expect(() => ceremonyOptionsFromJSON(value)).toThrow('Invalid Passkey options:')
  })
})
