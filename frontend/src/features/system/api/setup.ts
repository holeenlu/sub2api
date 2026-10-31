/**
 * Setup API endpoints
 */
import { apiClient, buildGatewayUrl } from '@/shared/http'

export interface SetupStatus {
  needs_setup: boolean
  step: string
}

export interface DatabaseConfig {
  host: string
  port: number
  user: string
  password: string
  dbname: string
  sslmode: string
}

export interface RedisConfig {
  host: string
  port: number
  username: string
  password: string
  db: number
  enable_tls: boolean
}

export interface AdminConfig {
  email: string
  password: string
}

export interface ServerConfig {
  host: string
  port: number
  mode: string
}

export interface InstallRequest {
  database: DatabaseConfig
  redis: RedisConfig
  admin: AdminConfig
  server: ServerConfig
}

export interface InstallResponse {
  message: string
  restart: boolean
}

/**
 * Get setup status
 */
export async function getSetupStatus(options?: { signal?: AbortSignal }): Promise<SetupStatus> {
  const response = await apiClient.get<SetupStatus>(buildGatewayUrl('/setup/status'), options)
  return response.data
}

/**
 * Test database connection
 */
export async function testDatabase(config: DatabaseConfig, bootstrapToken: string, options?: { signal?: AbortSignal }): Promise<void> {
  await apiClient.post(buildGatewayUrl('/setup/test-db'), config, {
    ...options, headers: { 'X-Setup-Token': bootstrapToken.trim() }
  })
}

/**
 * Test Redis connection
 */
export async function testRedis(config: RedisConfig, bootstrapToken: string, options?: { signal?: AbortSignal }): Promise<void> {
  await apiClient.post(buildGatewayUrl('/setup/test-redis'), config, {
    ...options, headers: { 'X-Setup-Token': bootstrapToken.trim() }
  })
}

/**
 * Perform installation
 */
export async function install(config: InstallRequest, bootstrapToken: string, options?: { signal?: AbortSignal }): Promise<InstallResponse> {
  const response = await apiClient.post<InstallResponse>(buildGatewayUrl('/setup/install'), config, {
    ...options, headers: { 'X-Setup-Token': bootstrapToken.trim() }
  })
  return response.data
}
