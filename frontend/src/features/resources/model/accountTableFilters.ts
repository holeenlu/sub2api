/** List-filter UI values. Empty string and null both mean no query constraint. */
export interface AccountTableFilterValues {
  platform: string | null
  type: string | null
  status: string | null
  privacy_mode: string | null
  /** Group IDs stay strings; "ungrouped" is the existing explicit sentinel. */
  group: string | null
}
