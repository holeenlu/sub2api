/** Management-only entry; the public catalog barrel does not eagerly load editors. */
export { managedModelsAPI } from './api/managedModels'
export type { ManagedModelPlatform, ManagedModel, ModelPolicy, ModelMappings, ModelSource } from './api/managedModelTypes'
export { useManagedModels } from './queries/managedModels'
export { default as ManagedCatalogPanel } from './components/managed-models/ManagedCatalogPanel.vue'
export { modelConflictRows } from './model/managedModelConflict'
export type { ManagedModelConflict } from './model/managedModelConflict'
