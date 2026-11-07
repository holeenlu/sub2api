// Administrative ceilings; keep in sync with basispoints/image_capacity.go.
// Increasing a ceiling does not change a saved setting or allocate resources.
export const excelBPSImageLimits = {
  bodyMiB: 1024,
  minBudgetMiB: 512,
  budgetMiB: 65536,
  requests: 4096,
  imageMiB: 512,
  totalMiB: 512,
  images: 65536,
  storageMiB: 262144,
  storageEntries: 1048576,
  ttlMinutes: 10080,
  // Mirror the admission accounting in openai_excel_bps_image_admission.go.
  bodyMultiplier: 8,
} as const

// A preview of local admission accounting, not upstream capacity or actual RSS.
export function previewExcelBPSImageCapacity(bodyMiB: number, budgetMiB: number, requests: number) {
  if (
    !Number.isSafeInteger(bodyMiB) || bodyMiB < 1 || bodyMiB > excelBPSImageLimits.bodyMiB ||
    !Number.isSafeInteger(budgetMiB) || budgetMiB < excelBPSImageLimits.minBudgetMiB || budgetMiB > excelBPSImageLimits.budgetMiB ||
    !Number.isSafeInteger(requests) || requests < 1 || requests > excelBPSImageLimits.requests
  ) return null

  const multiplier = excelBPSImageLimits.bodyMultiplier
  const slotBudgetMiB = requests * multiplier
  const effectiveBudgetMiB = Math.max(budgetMiB, slotBudgetMiB)
  return {
    minimumBudgetMiB: Math.max(excelBPSImageLimits.minBudgetMiB, bodyMiB * multiplier),
    slotBudgetMiB,
    effectiveBudgetMiB,
    fullBodyRequests: Math.min(requests, Math.floor(effectiveBudgetMiB / (bodyMiB * multiplier))),
  }
}
