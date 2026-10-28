import { beforeEach, describe, expect, it, vi } from 'vitest'

const { post, get, put } = vi.hoisted(() => ({
  post: vi.fn(),
  get: vi.fn(),
  put: vi.fn(),
}))

vi.mock('@/shared/http', () => ({
  apiClient: {
    post,
    get,
    put,
  },
}))

import {
  getById,
  update,
  batchUpdateLimits,
  bindUserAuthIdentity,
  type AdminBindAuthIdentityRequest,
  type AdminBoundAuthIdentity,
  type BatchUpdateUserLimitsRequest,
  type BatchUpdateUserLimitsResponse,
} from '@/features/identity/api/users'

type Assert<T extends true> = T
type IsExact<T, U> = (
  (<G>() => G extends T ? 1 : 2) extends (<G>() => G extends U ? 1 : 2)
    ? ((<G>() => G extends U ? 1 : 2) extends (<G>() => G extends T ? 1 : 2) ? true : false)
    : false
)

type ExpectedAdminBindAuthIdentityRequest = {
  provider_type: string
  provider_key: string
  provider_subject: string
  issuer?: string
  metadata?: Record<string, unknown>
  channel?: {
    channel: string
    channel_app_id: string
    channel_subject: string
    metadata?: Record<string, unknown>
  }
}

type ExpectedAdminBoundAuthIdentity = {
  user_id: number
  provider_type: string
  provider_key: string
  provider_subject: string
  verified_at?: string | null
  issuer?: string | null
  metadata: Record<string, unknown> | null
  created_at: string
  updated_at: string
  channel?: {
    channel: string
    channel_app_id: string
    channel_subject: string
    metadata: Record<string, unknown> | null
    created_at: string
    updated_at: string
  } | null
}

const requestContractExact: Assert<
  IsExact<AdminBindAuthIdentityRequest, ExpectedAdminBindAuthIdentityRequest>
> = true
const responseContractExact: Assert<
  IsExact<AdminBoundAuthIdentity, ExpectedAdminBoundAuthIdentity>
> = true
const batchRequestContractExact: Assert<
  IsExact<
    BatchUpdateUserLimitsRequest,
    {
      user_ids: number[]
      all?: boolean
      concurrency?: number
      rpm_limit?: number
    }
  >
> = true
const batchResponseContractExact: Assert<
  IsExact<BatchUpdateUserLimitsResponse, { affected: number }>
> = true

describe('admin users api auth identity binding', () => {
  beforeEach(() => {
    post.mockReset()
  })

  it('posts the backend-compatible auth identity bind payload and returns the backend response shape', async () => {
    const payload: AdminBindAuthIdentityRequest = {
      provider_type: 'wechat',
      provider_key: 'wechat-main',
      provider_subject: 'union-123',
      metadata: { source: 'admin-repair' },
      channel: {
        channel: 'open',
        channel_app_id: 'wx-open',
        channel_subject: 'openid-123',
        metadata: { scene: 'migration' },
      },
    }

    const response: AdminBoundAuthIdentity = {
      user_id: 9,
      provider_type: 'wechat',
      provider_key: 'wechat-main',
      provider_subject: 'union-123',
      verified_at: '2026-04-22T00:00:00Z',
      issuer: null,
      metadata: { source: 'admin-repair' },
      created_at: '2026-04-22T00:00:00Z',
      updated_at: '2026-04-22T00:00:00Z',
      channel: {
        channel: 'open',
        channel_app_id: 'wx-open',
        channel_subject: 'openid-123',
        metadata: { scene: 'migration' },
        created_at: '2026-04-22T00:00:00Z',
        updated_at: '2026-04-22T00:00:00Z',
      },
    }
    post.mockResolvedValue({ data: response })

    const result = await bindUserAuthIdentity(9, payload)

    expect(post).toHaveBeenCalledWith('/admin/users/9/auth-identities', payload)
    expect(result).toEqual(response)
  })

  it('keeps bind auth identity request and response types aligned with the backend contract', () => {
    expect(requestContractExact).toBe(true)
    expect(responseContractExact).toBe(true)
  })

  it('posts batch limit updates once with only the supplied limit fields', async () => {
    const request: BatchUpdateUserLimitsRequest = {
      user_ids: [4, 7],
      all: false,
      rpm_limit: 0,
    }
    post.mockResolvedValue({ data: { affected: 2 } satisfies BatchUpdateUserLimitsResponse })

    const result = await batchUpdateLimits(request)

    expect(post).toHaveBeenCalledWith('/admin/users/batch-limits', request)
    expect(result).toEqual({ affected: 2 })
    expect(batchRequestContractExact).toBe(true)
    expect(batchResponseContractExact).toBe(true)
  })
})


describe('historical user lookup cancellation', () => {
  beforeEach(() => get.mockReset())
  it('preserves include_deleted while forwarding the supplied AbortSignal', async () => {
    const controller = new AbortController()
    const historicalUser = { id: 27, deleted_at: '2026-10-01T00:00:00Z' }
    get.mockResolvedValueOnce({ data: historicalUser })
    await expect(getById(27, true, { signal: controller.signal })).resolves.toEqual(historicalUser)
    expect(get).toHaveBeenCalledWith('/admin/users/27?include_deleted=true', { signal: controller.signal })
  })
  it('keeps the default active-user lookup without an added request configuration', async () => {
    get.mockResolvedValueOnce({ data: { id: 27 } })
    await getById(27)
    expect(get).toHaveBeenCalledWith('/admin/users/27')
  })
})

describe('user permission update cancellation', () => {
  beforeEach(() => put.mockReset())
  it.each([false, true])('preserves the exact grants/rates payload with options=%s', async withOptions => {
    const payload = { allowed_groups: [7], restrict_public_groups: true, group_rates: { 7: 0, 9: null } }
    const response = { id: 27, ...payload }
    const options = { signal: new AbortController().signal }
    put.mockResolvedValueOnce({ data: response })
    await expect(withOptions ? update(27, payload, options) : update(27, payload)).resolves.toBe(response)
    expect(put).toHaveBeenCalledTimes(1)
    if (withOptions) expect(put).toHaveBeenCalledWith('/admin/users/27', payload, options)
    else expect(put).toHaveBeenCalledWith('/admin/users/27', payload)
    expect(put.mock.calls[0][1]).toBe(payload)
  })
})
