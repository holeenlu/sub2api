/** Editable resource labels. Credentials never enter preferences or shared state. */
export interface ResourceIdentityDraft {
  name: string
  notes: string
}


export function createIdentityDraft(source?: { name: string; notes?: string | null }): ResourceIdentityDraft {
  return { name: source?.name ?? '', notes: source?.notes ?? '' }
}
