/** Read-only selection projection shared by real management and supplier proxy DTOs. */
export interface ResourceProxyOption {
  readonly id: number
  readonly name: string
  readonly protocol: string
  readonly host: string
  readonly port: number
  readonly account_count?: number
}
