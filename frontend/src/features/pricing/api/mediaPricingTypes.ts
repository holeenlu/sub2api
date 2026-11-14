export type VideoModelPrices = Record<string, Record<string, number>>

/** Pricing-owned fields composed into the existing Group read/write DTOs. */
export interface GroupImageCountBilling {
  image_count_billing: boolean
}
