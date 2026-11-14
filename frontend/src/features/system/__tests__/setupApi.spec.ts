import { describe, expect, it, vi } from 'vitest'
import { apiClient } from '@/shared/http'
import { getSetupStatus, testDatabase, install } from '../api/setup'
vi.mock('@/shared/http', () => ({ apiClient: { get: vi.fn(), post: vi.fn() }, buildGatewayUrl: (path: string) => 'https://console.example.invalid' + path }))
describe('setup transport', () => {
  it('uses the shared client with the gateway-root status endpoint and cancellation', async () => {
    vi.mocked(apiClient.get).mockResolvedValue({ data: { needs_setup: true, step: 'database' } })
    const options = { signal: new AbortController().signal }
    expect(await getSetupStatus(options)).toEqual({ needs_setup: true, step: 'database' })
    expect(apiClient.get).toHaveBeenCalledWith('https://console.example.invalid/setup/status', options)
  })
  it('keeps the setup token on the same trusted target and forwards unwrapped responses', async () => {
    const database = { host: 'localhost', port: 5432, user: 'test', password: 'fixture', dbname: 'sub2api', sslmode: 'disable' }
    const options = { signal: new AbortController().signal }
    await testDatabase(database, ' fixture-bootstrap ', options)
    expect(apiClient.post).toHaveBeenCalledWith('https://console.example.invalid/setup/test-db', database, { ...options, headers: { 'X-Setup-Token': 'fixture-bootstrap' } })
    vi.mocked(apiClient.post).mockResolvedValue({ data: { message: 'Installed', restart: true } })
    const request = { database, redis: { host: 'localhost', port: 6379, username: '', password: '', db: 0, enable_tls: false }, admin: { email: 'test@example.invalid', password: 'fixture' }, server: { host: '127.0.0.1', port: 8080, mode: 'release' } }
    expect(await install(request, 'fixture-bootstrap', options)).toEqual({ message: 'Installed', restart: true })
  })
})
