import { ref } from 'vue'
import type { Ref } from 'vue'




interface CreateTelemetryInput {

}




export function useCreateTelemetryDraft(_input: CreateTelemetryInput): {
  upstreamRequestIdHeader: Ref<string, string>
  withAccountExtra: (extra: Record<string, unknown> | undefined) => Record<string, unknown> | undefined
  upstreamBillingAutoProbeEnabled: Ref<boolean, boolean>
} {


  // Platform-specific hints for API Key type
  // 上游ID：直接上游声明请求标识的响应头名，留空不记录。
  const upstreamRequestIdHeader = ref('')

  const withAccountExtra = (extra: Record<string, unknown> | undefined): Record<string, unknown> | undefined => {
    const copied = extra
    const name = upstreamRequestIdHeader.value.trim()
    return name ? { ...copied, upstream_request_id_header: name } : copied
  }

  const upstreamBillingAutoProbeEnabled = ref(true)
  return { upstreamRequestIdHeader, withAccountExtra, upstreamBillingAutoProbeEnabled }
}
