import { defineComponent } from 'vue'
import { mount } from '@vue/test-utils'
import { paymentsServicesKey, type PaymentsServices } from '../services'
import { createPaymentsServices } from './services'
export function mountPaymentQuery<T>(create: () => T, services: PaymentsServices = createPaymentsServices()) {
  let query!: T
  const wrapper = mount(defineComponent({ setup() { query = create(); return () => null } }), { global: { provide: { [paymentsServicesKey as symbol]: services } } })
  return { query, wrapper, services }
}
export function deferred<T>() {
  let resolve!: (value: T) => void, reject!: (reason: unknown) => void
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}
