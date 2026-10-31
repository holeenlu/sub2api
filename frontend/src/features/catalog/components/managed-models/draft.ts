export interface MappingDraftState {
  dirty: boolean
  valid: boolean
}

// Policy DTOs contain JSON values. Clone both incoming drafts and outgoing events
// so a parent or sibling editor can never mutate another editor's snapshot.
export function cloneDraft<T>(value: T): T {
  return JSON.parse(JSON.stringify(value)) as T
}

export function commaList(value: string): string[] {
  return value.split(',').map(item => item.trim()).filter(Boolean)
}
