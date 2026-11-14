// JSON contract: backend/internal/service/passkey.go and go-webauthn protocol
// options. Keep conversion local; older browsers need BufferSource options.
function invalid(field: string): never {
  throw new Error(`Invalid Passkey options: ${field}`)
}

function record(value: unknown, field: string): Record<string, unknown> {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) return invalid(field)
  return Object.fromEntries(Object.entries(value))
}

function text(value: unknown, field: string): string {
  if (typeof value !== 'string') return invalid(field)
  return value
}

function boolean(value: unknown, field: string): boolean {
  if (typeof value !== 'boolean') return invalid(field)
  return value
}

function integer(value: unknown, field: string, min: number, max: number): number {
  if (typeof value !== 'number' || !Number.isInteger(value) || value < min || value > max) return invalid(field)
  return value
}

function choice<T extends string>(value: unknown, choices: readonly T[], field: string): T {
  for (const candidate of choices) if (value === candidate) return candidate
  return invalid(field)
}

function array<T>(value: unknown, field: string, parse: (entry: unknown, field: string) => T): T[] {
  if (!Array.isArray(value)) return invalid(field)
  return value.map((entry: unknown, index: number) => parse(entry, `${field}[${index}]`))
}

function buffer(value: unknown, field: string): ArrayBuffer {
  const encoded = text(value, field)
  if (!/^[A-Za-z0-9_-]+={0,2}$/.test(encoded)) return invalid(field)
  const unpadded = encoded.replace(/=+$/, '')
  const padding = '='.repeat((4 - unpadded.length % 4) % 4)
  if (unpadded.length % 4 === 1 || (encoded !== unpadded && encoded !== unpadded + padding)) return invalid(field)
  const binary = atob(unpadded.replace(/-/g, '+').replace(/_/g, '/') + padding)
  if (btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '') !== unpadded) return invalid(field)
  return Uint8Array.from(binary, character => character.charCodeAt(0)).buffer
}

function descriptor(value: unknown, field: string): PublicKeyCredentialDescriptor {
  const source = record(value, field)
  const transports = source.transports === undefined ? undefined : transportHints(source.transports, `${field}.transports`)
  return {
    type: choice(source.type, ['public-key'], `${field}.type`),
    id: buffer(source.id, `${field}.id`),
    ...(transports === undefined ? {} : { transports })
  }
}

function transportHints(value: unknown, field: string): AuthenticatorTransport[] | undefined {
  const hints = array(value, field, text)
  const supported: AuthenticatorTransport[] = []
  for (const hint of hints) {
    switch (hint) {
      case 'ble': case 'hybrid': case 'internal': case 'nfc': case 'usb':
        supported.push(hint)
        break
      default:
        // go-webauthn also supports smart-card, absent from our DOM typings.
        // Transports are optional hints (WebAuthn 5.8.3): omit the entire hint
        // list so newer transports remain discoverable, keeping the credential
        // ID/type and allow/exclude constraints intact. Never coerce non-strings.
        return undefined
    }
  }
  return supported
}

function parameters(value: unknown, field: string): PublicKeyCredentialParameters {
  const source = record(value, field)
  return {
    type: choice(source.type, ['public-key'], `${field}.type`),
    alg: integer(source.alg, `${field}.alg`, -2147483648, 2147483647)
  }
}

function selection(value: unknown): AuthenticatorSelectionCriteria {
  const source = record(value, 'authenticatorSelection')
  const result: AuthenticatorSelectionCriteria = {}
  if (source.authenticatorAttachment !== undefined) {
    result.authenticatorAttachment = choice<AuthenticatorAttachment>(source.authenticatorAttachment, ['platform', 'cross-platform'], 'authenticatorAttachment')
  }
  if (source.requireResidentKey !== undefined) result.requireResidentKey = boolean(source.requireResidentKey, 'requireResidentKey')
  if (source.residentKey !== undefined) result.residentKey = choice<ResidentKeyRequirement>(source.residentKey, ['required', 'preferred', 'discouraged'], 'residentKey')
  if (source.userVerification !== undefined) result.userVerification = verification(source.userVerification)
  return result
}

function verification(value: unknown): UserVerificationRequirement {
  return choice(value, ['required', 'preferred', 'discouraged'], 'userVerification')
}

function extensions(value: unknown): AuthenticationExtensionsClientInputs {
  const source = record(value, 'extensions')
  const result: AuthenticationExtensionsClientInputs = {}
  // Registration currently requests credProps; retain the other inputs supported
  // by the installed DOM contract without forwarding unvalidated JSON fields.
  if (source.appid !== undefined) result.appid = text(source.appid, 'extensions.appid')
  if (source.credProps !== undefined) result.credProps = boolean(source.credProps, 'extensions.credProps')
  if (source.hmacCreateSecret !== undefined) result.hmacCreateSecret = boolean(source.hmacCreateSecret, 'extensions.hmacCreateSecret')
  if (source.minPinLength !== undefined) result.minPinLength = boolean(source.minPinLength, 'extensions.minPinLength')
  return result
}

export function ceremonyOptionsFromJSON(value: unknown): { session_token: string; publicKey: unknown } {
  const source = record(value, 'ceremony')
  const sessionToken = text(source.session_token, 'session_token')
  if (!sessionToken) return invalid('session_token')
  return { session_token: sessionToken, publicKey: record(source.options, 'options').publicKey }
}

export function creationOptionsFromJSON(value: unknown): PublicKeyCredentialCreationOptions {
  const source = record(value, 'publicKey')
  const rp = record(source.rp, 'rp')
  const user = record(source.user, 'user')
  const result: PublicKeyCredentialCreationOptions = {
    challenge: buffer(source.challenge, 'challenge'),
    rp: { name: text(rp.name, 'rp.name'), ...(rp.id === undefined ? {} : { id: text(rp.id, 'rp.id') }) },
    user: { id: buffer(user.id, 'user.id'), name: text(user.name, 'user.name'), displayName: text(user.displayName, 'user.displayName') },
    pubKeyCredParams: array(source.pubKeyCredParams, 'pubKeyCredParams', parameters)
  }
  if (result.user.id.byteLength > 64) return invalid('user.id')
  if (result.pubKeyCredParams.length === 0) return invalid('pubKeyCredParams')
  if (source.timeout !== undefined) result.timeout = integer(source.timeout, 'timeout', 0, 4294967295)
  if (source.excludeCredentials !== undefined) result.excludeCredentials = array(source.excludeCredentials, 'excludeCredentials', descriptor)
  if (source.authenticatorSelection !== undefined) result.authenticatorSelection = selection(source.authenticatorSelection)
  if (source.attestation !== undefined) result.attestation = choice<AttestationConveyancePreference>(source.attestation, ['none', 'direct', 'indirect', 'enterprise'], 'attestation')
  if (source.extensions !== undefined) result.extensions = extensions(source.extensions)
  return result
}

export function requestOptionsFromJSON(value: unknown): PublicKeyCredentialRequestOptions {
  const source = record(value, 'publicKey')
  const result: PublicKeyCredentialRequestOptions = { challenge: buffer(source.challenge, 'challenge') }
  if (source.timeout !== undefined) result.timeout = integer(source.timeout, 'timeout', 0, 4294967295)
  if (source.rpId !== undefined) result.rpId = text(source.rpId, 'rpId')
  if (source.allowCredentials !== undefined) result.allowCredentials = array(source.allowCredentials, 'allowCredentials', descriptor)
  if (source.userVerification !== undefined) result.userVerification = verification(source.userVerification)
  if (source.extensions !== undefined) result.extensions = extensions(source.extensions)
  return result
}
