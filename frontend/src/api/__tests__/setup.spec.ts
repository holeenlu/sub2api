import { beforeEach, describe, expect, it, vi } from 'vitest'

const client = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn() }))
vi.mock('axios', () => ({ default: { create: () => client } }))

import { getSetupStatus, install, testDatabase, testRedis, type InstallRequest } from '../setup'

const config: InstallRequest = {
  database: { host: 'db', port: 5432, user: 'app', password: 'db-password', dbname: 'app', sslmode: 'require' },
  redis: { host: 'redis', port: 6379, username: '', password: '', db: 0, enable_tls: false },
  admin: { email: 'operator@example.test', password: 'new-password' },
  server: { host: '0.0.0.0', port: 8080, mode: 'release' }
}

describe('setup operator authorization', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    client.post.mockResolvedValue({ data: { data: { message: 'installed', restart: true } } })
    client.get.mockResolvedValue({ data: { data: { needs_setup: true, step: 'welcome' } } })
  })

  it('sends the token only in each mutation header', async () => {
    const token = 'operator-token-for-tests'
    await testDatabase(config.database, ` ${token} `)
    await testRedis(config.redis, token)
    const result = await install(config, token)
    expect(result.restart).toBe(true)
    expect(client.post.mock.calls).toEqual([
      ['/setup/test-db', config.database, { headers: { 'X-Setup-Token': token } }],
      ['/setup/test-redis', config.redis, { headers: { 'X-Setup-Token': token } }],
      ['/setup/install', config, { headers: { 'X-Setup-Token': token } }]
    ])
  })

  it('keeps the read-only setup status public without retaining a mutation token', async () => {
    await testDatabase(config.database, 'temporary-token')
    expect(await getSetupStatus()).toEqual({ needs_setup: true, step: 'welcome' })
    expect(client.get).toHaveBeenCalledWith('/setup/status')
  })
})
