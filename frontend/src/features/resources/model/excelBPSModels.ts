/** Matches account.go: only nonempty strings in an explicit array select BPS. */
export function readExcelBPSModels(value: unknown): string[] {
  if (!Array.isArray(value)) return []
  return [...new Set(value.filter((model): model is string => typeof model === 'string').map(model => model.trim()).filter(Boolean))]
}
