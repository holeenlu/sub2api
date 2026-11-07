import { hasInjectionContext, inject, type InjectionKey } from 'vue'

/** Application-owned notifications used by shared UI helpers. */
export interface UiFeedback {
  success(message: string): void
  error(message: string): void
}

export const uiFeedbackKey: InjectionKey<UiFeedback> = Symbol('ui.feedback')

export function useUiFeedback(): UiFeedback {
  const feedback = hasInjectionContext() ? inject(uiFeedbackKey, null) : null
  if (!feedback) throw new Error('UiFeedback is not provided')
  return feedback
}
