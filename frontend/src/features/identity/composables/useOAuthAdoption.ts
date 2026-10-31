import { ref } from 'vue'
import type { OAuthAdoptionDecision } from '../api/auth'

/** Profile adoption remains an explicit user decision for every pending provider. */
export function useOAuthAdoption() {
  const adoptionRequired = ref(false)
  const suggestedDisplayName = ref('')
  const suggestedAvatarUrl = ref('')
  const adoptDisplayName = ref(true)
  const adoptAvatar = ref(true)

  function currentAdoptionDecision(): OAuthAdoptionDecision {
    return {
      adoptDisplayName: adoptDisplayName.value,
      adoptAvatar: adoptAvatar.value
    }
  }

  function applyAdoptionSuggestionState(completion: {
    adoption_required?: boolean
    suggested_display_name?: string
    suggested_avatar_url?: string
  }) {
    adoptionRequired.value = completion.adoption_required === true
    suggestedDisplayName.value = completion.suggested_display_name || ''
    suggestedAvatarUrl.value = completion.suggested_avatar_url || ''

    if (!suggestedDisplayName.value) {
      adoptDisplayName.value = false
    }
    if (!suggestedAvatarUrl.value) {
      adoptAvatar.value = false
    }
  }

  return { adoptionRequired, suggestedDisplayName, suggestedAvatarUrl, adoptDisplayName, adoptAvatar, currentAdoptionDecision, applyAdoptionSuggestionState }
}
