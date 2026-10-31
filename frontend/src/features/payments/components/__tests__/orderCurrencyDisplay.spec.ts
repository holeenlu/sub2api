import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import type { PaymentOrder } from '@/features/payments/api/types';
import AdminRefundDialog from '../AdminRefundDialog.vue'
import OrderTable from '@/features/payments/components/OrderTable.vue'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key,
    }),
  }
})

const BaseDialogStub = {
  props: ['show'],
  template: '<div v-if="show"><slot /><slot name="footer" /></div>',
}

const DataTableStub = {
  props: ['data'],
  template: `
    <div>
      <div v-for="row in data" :key="row.id">
        <slot name="cell-pay_amount" :value="row.pay_amount" :row="row" />
      </div>
    </div>
  `,
}

function orderFactory(overrides: Partial<PaymentOrder> = {}): PaymentOrder {
  return {
    id: 1,
    user_id: 10,
    amount: 100,
    pay_amount: 108,
    currency: 'USD',
    fee_rate: 8,
    payment_type: 'stripe',
    out_trade_no: 'sub2_202606250001',
    status: 'COMPLETED',
    order_type: 'subscription',
    created_at: '2026-06-25T10:00:00Z',
    expires_at: '2026-06-25T10:30:00Z',
    refund_amount: 25,
    ...overrides,
  }
}

describe('admin order currency display', () => {
  it('uses order currency for pay_amount and USD for refundable balance amounts', () => {
    const wrapper = mount(AdminRefundDialog, {
      props: {
        show: true,
        order: orderFactory({
          currency: 'USD',
          status: 'PARTIALLY_REFUNDED',
          refund_amount: 20,
        }),
        userBalance: 200,
      },
      global: {
        stubs: {
          BaseDialog: BaseDialogStub,
        },
      },
    })

    const text = wrapper.text()
    expect(text).toContain('$108.00')
    expect(text).toContain('$100.00')
    expect(text).toContain('$20.00')
    expect(text).toContain('$80.00')
    expect(text).toContain('$200.00')
  })

  it('renders payment currency consistently in the shared order table', () => {
    const wrapper = mount(OrderTable, {
      props: {
        orders: [
          orderFactory({ id: 1, currency: 'USD', amount: 100, pay_amount: 108 }),
          orderFactory({ id: 2, currency: 'CNY', amount: 100, pay_amount: 108 }),
        ],
        loading: false,
        showUser: true,
      },
      global: {
        stubs: {
          DataTable: DataTableStub,
          OrderStatusBadge: true,
        },
      },
    })

    const text = wrapper.text()
    expect(text).toContain('$108.00')
    expect(text).toContain('¥108.00')
    expect(text).toContain('$100.00')
  })

  it('preserves optional administrative annotations through the real typed table', () => {
    const wrapper = mount(OrderTable, {
      props: { orders: [{ ...orderFactory(), user_name: 'Ada', user_notes: 'Admin note' }], loading: false, showUser: true },
      global: { stubs: { OrderStatusBadge: true } }
    })
    expect(wrapper.text()).toContain('Ada')
    expect(wrapper.text()).toContain('(Admin note)')
    wrapper.unmount()
  })

  it('keeps the user ID fallback when optional administrative fields are absent or invalid', () => {
    const wrapper = mount(OrderTable, {
      props: { orders: [{ ...orderFactory(), user_name: 123, user_notes: { unexpected: true } }], loading: false, showUser: true },
      global: { stubs: { OrderStatusBadge: true } }
    })
    expect(wrapper.text()).toContain('#10')
    expect(wrapper.text()).not.toContain('unexpected')
    wrapper.unmount()
  })

  it('does not expose administrative annotations in the user table', () => {
    const wrapper = mount(OrderTable, {
      props: { orders: [{ ...orderFactory(), user_name: 'Hidden name', user_notes: 'Hidden note' }], loading: false },
      global: { stubs: { OrderStatusBadge: true } }
    })
    expect(wrapper.text()).not.toContain('Hidden name')
    expect(wrapper.text()).not.toContain('Hidden note')
    wrapper.unmount()
  })

})
