import axios from 'axios'
import {
  AUTH_TOKEN_KEY,
  REFRESH_TOKEN_KEY,
  TOKEN_EXPIRES_AT_KEY,
  authSessionStamp,
  readAuthSessionSnapshot
} from '@/shared/session'
import type { AuthSessionSnapshot } from '@/shared/session'
import { getAPIBaseURL } from './url'

const TOKEN_REFRESH_LOCK_NAME = 'sub2api-auth-token-refresh'
const TOKEN_REFRESH_TIMEOUT_MS = 30_000
const PEER_REFRESH_WAIT_MS = 1_000
const PEER_REFRESH_GRACE_MS = 1_000
const PEER_REFRESH_POLL_MS = 25

export interface RefreshTokenResponse {
  access_token: string
  refresh_token: string
  expires_in: number
  token_type: string
}

export interface RefreshAuthTokensOptions {
  /** Access token attached to the request that received a 401 response. */
  failedAccessToken?: string | null
}

interface RefreshTokenEnvelope {
  code: number
  message: string
  data: RefreshTokenResponse
}

interface AuthSnapshot extends AuthSessionSnapshot {
  readonly refreshToken: string
}

let inFlightRefresh: { session: string; promise: Promise<RefreshTokenResponse> } | null = null

function readAuthSnapshot(): AuthSnapshot {
  const snapshot = readAuthSessionSnapshot()
  if (!snapshot.refreshToken) {
    throw new Error('No refresh token available')
  }

  return { ...snapshot, refreshToken: snapshot.refreshToken }
}

function readStoredTokenPair(snapshot: AuthSnapshot): RefreshTokenResponse | null {
  const { accessToken, refreshToken, expiresAt, userID, session } = readAuthSessionSnapshot()

  if (
    !accessToken ||
    !refreshToken ||
    expiresAt === null ||
    expiresAt <= Date.now() ||
    userID !== snapshot.userID ||
    session !== snapshot.session
  ) {
    return null
  }

  return {
    access_token: accessToken,
    refresh_token: refreshToken,
    expires_in: Math.max(1, Math.ceil((expiresAt - Date.now()) / 1000)),
    token_type: 'Bearer'
  }
}

function readPeerRefreshResult(
  snapshot: AuthSnapshot,
  failedAccessToken?: string | null
): RefreshTokenResponse | null {
  const storedPair = readStoredTokenPair(snapshot)
  if (!storedPair) {
    return null
  }

  if (storedPair.refresh_token !== snapshot.refreshToken) {
    return storedPair
  }

  if (
    failedAccessToken &&
    snapshot.accessToken !== failedAccessToken &&
    storedPair.access_token === snapshot.accessToken
  ) {
    return storedPair
  }

  return null
}

async function waitForPeerRefresh(
  snapshot: AuthSnapshot,
  failedAccessToken?: string | null,
  deadline = Date.now() + PEER_REFRESH_WAIT_MS
): Promise<RefreshTokenResponse | null> {
  while (Date.now() < deadline) {
    const peerResult = readPeerRefreshResult(snapshot, failedAccessToken)
    if (peerResult) {
      return peerResult
    }
    await new Promise((resolve) => window.setTimeout(resolve, PEER_REFRESH_POLL_MS))
  }

  return readPeerRefreshResult(snapshot, failedAccessToken)
}

function persistTokenPair(tokens: RefreshTokenResponse): void {
  localStorage.setItem(AUTH_TOKEN_KEY, tokens.access_token)
  localStorage.setItem(TOKEN_EXPIRES_AT_KEY, String(Date.now() + tokens.expires_in * 1000))
  // The rotating refresh token is written last so other tabs can treat its change as a commit marker.
  localStorage.setItem(REFRESH_TOKEN_KEY, tokens.refresh_token)
}

function readResponseStatus(error: unknown): number | undefined {
  if (typeof error !== 'object' || error === null || !('response' in error)) {
    return undefined
  }
  const response = error.response
  if (typeof response !== 'object' || response === null || !('status' in response)) {
    return undefined
  }
  return typeof response.status === 'number' ? response.status : undefined
}

async function requestTokenPair(
  snapshot: AuthSnapshot,
  failedAccessToken?: string | null,
  mayHaveUncoordinatedPeer = false
): Promise<RefreshTokenResponse> {
  // If this request loses a one-time-token race, the winning peer can legitimately take as long
  // as our own HTTP timeout to publish its replacement token. Keep the recovery window tied to
  // that timeout instead of an arbitrary short delay.
  const peerRefreshDeadline = Date.now() + TOKEN_REFRESH_TIMEOUT_MS + PEER_REFRESH_GRACE_MS

  try {
    const response = await axios.post<RefreshTokenEnvelope>(
      `${getAPIBaseURL()}/auth/refresh`,
      { refresh_token: snapshot.refreshToken },
      { headers: { 'Content-Type': 'application/json' }, timeout: TOKEN_REFRESH_TIMEOUT_MS }
    )
    const payload = response.data
    if (payload.code !== 0 || !payload.data) {
      throw new Error(payload.message || 'Token refresh failed')
    }

    const current = readAuthSessionSnapshot()
    if (
      current.refreshToken !== snapshot.refreshToken ||
      current.userID !== snapshot.userID ||
      current.session !== snapshot.session
    ) {
      const peerResult = readPeerRefreshResult(snapshot, failedAccessToken)
      if (peerResult) {
        return peerResult
      }
      throw new Error('Session changed during token refresh')
    }

    persistTokenPair(payload.data)
    return payload.data
  } catch (error) {
    // A peer tab may have rotated the one-time refresh token while this request was in flight.
    // A 4xx response can arrive quickly while the winning peer's response is still in flight, so
    // wait through the shared request deadline before treating the session as expired. Transient
    // non-4xx failures retain the short reconciliation window.
    const responseStatus = readResponseStatus(error)
    const isTokenRejection =
      typeof responseStatus === 'number' && responseStatus >= 400 && responseStatus < 500
    const peerResult = await waitForPeerRefresh(
      snapshot,
      failedAccessToken,
      isTokenRejection && mayHaveUncoordinatedPeer
        ? peerRefreshDeadline
        : Date.now() + PEER_REFRESH_WAIT_MS
    )
    if (peerResult) {
      return peerResult
    }
    throw error
  }
}

async function runRefresh(snapshot: AuthSnapshot, options: RefreshAuthTokensOptions): Promise<RefreshTokenResponse> {
  const refresh = async (mayHaveUncoordinatedPeer = false): Promise<RefreshTokenResponse> => {
    if (authSessionStamp(localStorage.getItem(AUTH_TOKEN_KEY)) !== snapshot.session) {
      throw new Error('Session changed during token refresh')
    }
    const peerResult = readPeerRefreshResult(snapshot, options.failedAccessToken)
    if (peerResult) {
      return peerResult
    }
    return requestTokenPair(snapshot, options.failedAccessToken, mayHaveUncoordinatedPeer)
  }

  if (typeof navigator !== 'undefined' && navigator.locks) {
    return navigator.locks.request(TOKEN_REFRESH_LOCK_NAME, () => refresh(false))
  }

  return refresh(true)
}

/**
 * Refresh and persist the browser session.
 *
 * Calls for the same logical session share one promise. Web Locks serialize refreshes across tabs, while
 * the token snapshot check adopts a peer's newly rotated token instead of logging the user out.
 */
export function refreshAuthTokens(
  options: RefreshAuthTokensOptions = {}
): Promise<RefreshTokenResponse> {
  let snapshot: AuthSnapshot
  try {
    snapshot = readAuthSnapshot()
  } catch (error) {
    return Promise.reject(error)
  }
  if (inFlightRefresh?.session === snapshot.session) {
    return inFlightRefresh.promise
  }

  const pending = runRefresh(snapshot, options)
  inFlightRefresh = { session: snapshot.session, promise: pending }
  const clearPending = (): void => {
    if (inFlightRefresh?.promise === pending) {
      inFlightRefresh = null
    }
  }
  void pending.then(clearPending, clearPending)
  return pending
}
