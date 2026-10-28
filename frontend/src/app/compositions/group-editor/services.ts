import type { InjectionKey } from 'vue';
/** App advances its active group-create tour only after the server confirms creation. */
export const groupEditorCreatedKey: InjectionKey<() => void> = Symbol('group-editor.created')
