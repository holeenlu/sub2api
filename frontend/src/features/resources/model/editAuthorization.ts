import { ref } from 'vue'
import { OPENAI_WS_MODE_OFF, type OpenAIWSMode } from '@/features/resources/model/openaiWsMode'
import type { Ref } from 'vue'




interface EditAuthorizationInput {

}




export function useEditAuthorizationDraft(_input: EditAuthorizationInput): {
  grokOAuthCustomBaseUrlEnabled: Ref<boolean, boolean>
  grokOAuthBaseUrl: Ref<string, string>
  openaiOAuthResponsesWebSocketV2Mode: Ref<OpenAIWSMode, OpenAIWSMode>
} {


  // Grok OAuth 自定义上游地址（仅转发端点；OAuth 授权/令牌刷新不受影响）
  const grokOAuthCustomBaseUrlEnabled = ref(false)

  const grokOAuthBaseUrl = ref('')

  const openaiOAuthResponsesWebSocketV2Mode = ref<OpenAIWSMode>(OPENAI_WS_MODE_OFF)
  return { grokOAuthCustomBaseUrlEnabled, grokOAuthBaseUrl, openaiOAuthResponsesWebSocketV2Mode }
}
