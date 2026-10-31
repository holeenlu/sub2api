import { apiClient, buildGatewayUrl, type HttpRequestConfig } from '@/shared/http'
export interface PublicKeyUsageTotals {
  requests?: number; input_tokens?: number; output_tokens?: number; cache_creation_tokens?: number;
  cache_read_tokens?: number; total_tokens?: number; cost?: number; actual_cost?: number
}
export interface PublicKeyDailyUsage extends PublicKeyUsageTotals { date: string; cache_write_tokens?: number }
export interface PublicKeyModelUsage extends PublicKeyUsageTotals { model: string }
export interface PublicKeyUsageResponse {
  mode: 'quota_limited' | 'unrestricted'; isValid?: boolean; status?: string; planName?: string; unit?: string;
  remaining?: number; balance?: number; expires_at?: string | null; days_until_expiry?: number;
  quota?: { limit: number; used: number; remaining: number; unit: string };
  rate_limits?: Array<{ window: string; limit: number; used: number; remaining: number; window_start?: string | null; reset_at?: string | null }>;
  subscription?: { daily_usage_usd: number; weekly_usage_usd: number; monthly_usage_usd: number;
    daily_limit_usd: number | null; weekly_limit_usd: number | null; monthly_limit_usd: number | null;
    weekly_window_start?: string | null; expires_at: string };
  usage?: { today?: PublicKeyUsageTotals; total?: PublicKeyUsageTotals; average_duration_ms?: number; rpm?: number; tpm?: number };
  daily_usage?: PublicKeyDailyUsage[]; model_stats?: PublicKeyModelUsage[]
}
export interface PublicKeyUsageParams { start_date?: string; end_date?: string; days: 7 | 30 | 90; timezone: string }
/** /v1/usage accepts x-api-key. Anonymous policy strips the management session, not this explicit API key. */
export async function getPublicKeyUsage(key: string, params: PublicKeyUsageParams, options?: { signal?: AbortSignal }): Promise<PublicKeyUsageResponse> {
  const config: HttpRequestConfig = { sessionPolicy: 'anonymous', signal: options?.signal, params, headers: { 'x-api-key': key } }
  try { return (await apiClient.get<PublicKeyUsageResponse>(buildGatewayUrl('/v1/usage'), config)).data }
  catch (error) {
    if (error && typeof error === 'object' && 'error' in error && error.error && typeof error.error === 'object'
      && 'message' in error.error && typeof error.error.message === 'string') throw new Error(error.error.message)
    throw error
  }
}
