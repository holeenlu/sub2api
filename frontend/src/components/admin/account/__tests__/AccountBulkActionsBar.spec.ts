import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'

import AccountBulkActionsBar from '../AccountBulkActionsBar.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => key
  })
}))

describe('AccountBulkActionsBar', () => {
  it('does not expose filtered editing while specific accounts are selected', async () => {
    const wrapper = mount(AccountBulkActionsBar, {
      props: { selectedIds: [1], totalResults: 45, selectingAll: false, allResultsSelected: false }
    })
    expect(wrapper.text()).not.toContain('admin.accounts.bulkActions.editFiltered')
    const selected = wrapper.findAll('button').find(button => button.text() === 'admin.accounts.bulkActions.edit')
    await selected!.trigger('click')
    expect(wrapper.emitted('edit-selected')).toHaveLength(1)
    await wrapper.setProps({ selectedIds: [] })
    const filtered = wrapper.findAll('button').find(button => button.text() === 'admin.accounts.bulkActions.editFiltered')
    await filtered!.trigger('click')
    expect(wrapper.emitted('edit-filtered')).toHaveLength(1)
  })
  it('allows selecting all results before any row is selected', async () => {
    const wrapper = mount(AccountBulkActionsBar, {
      props: {
        selectedIds: [],
        totalResults: 45,
        selectingAll: false,
        allResultsSelected: false
      }
    })

    const button = wrapper.findAll('button').find(item =>
      item.text().includes('admin.accounts.bulkActions.selectAllResults')
    )

    expect(button).toBeDefined()
    await button!.trigger('click')
    expect(wrapper.emitted('select-all-results')).toHaveLength(1)
  })

  it('preserves the upstream billing probe action from v0.1.166', async () => {
    const wrapper = mount(AccountBulkActionsBar, {
      props: {
        selectedIds: [1],
        totalResults: 45,
        selectingAll: false,
        allResultsSelected: false
      }
    })

    const button = wrapper.findAll('button').find(item =>
      item.text().includes('admin.accounts.bulkActions.probeUpstreamBilling')
    )

    expect(button).toBeDefined()
    await button!.trigger('click')
    expect(wrapper.emitted('probe-upstream-billing')).toHaveLength(1)
  })
})
