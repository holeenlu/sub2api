export type SelectValue = string | number | boolean | null

export interface SelectOption {
  value: SelectValue
  label: string
  disabled?: boolean
  [key: string]: unknown // Custom fields must be narrowed by their consumer.
}

/** Search-created options use the configured keys and do not carry domain fields. */
export interface CreatableSelectOption {
  _creatable: true
  [key: string]: unknown
}
