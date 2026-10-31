import { accountFixture, translate } from './boundaryFixtures'
import type { AccountPlatform } from '@/features/resources/api/platformTypes';
import { beforeEach, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import { createResourceTaskScope } from '../../queries/resourceTaskScope'
import { useCreateAssociationDraft } from '../createAssociation'
import { useEditAssociationDraft } from '../editAssociation'

const check = vi.hoisted(() => vi.fn())
vi.mock('@/features/resources/accounts', () => ({ default: { checkMixedChannelRisk: check } }))
beforeEach(() => { vi.resetAllMocks(); check.mockResolvedValue({ has_risk: true, message: 'fixture risk' }) })

for (const kind of ['create', 'edit'] as const) {
  it.each(['actor', 'groups', 'platform', 'closed', 'unchanged'] as const)(kind + ' keeps preflight confirmation bound after %s changes', async transition => {
    let actor = 'admin-a'
    const scope = createResourceTaskScope(() => actor)
    const form = { platform: 'antigravity' as AccountPlatform, group_ids: [3] }
    const account = accountFixture()
    const input = { resourceTasks: scope, form, props: { account, supplierScope: false },
      mixedChannelWarningDetails: ref(null), mixedChannelWarningRawMessage: ref(''),
      mixedChannelWarningAction: ref<(() => Promise<void>) | null>(null),
      appStore: { showError: vi.fn() }, t: translate, submitting: ref(false),
    }
    const draft = kind === 'create'
      ? useCreateAssociationDraft(input as Parameters<typeof useCreateAssociationDraft>[0])
      : useEditAssociationDraft(input as Parameters<typeof useEditAssociationDraft>[0])
    const write = vi.fn().mockResolvedValue(undefined)
    expect(await draft.ensureAntigravityMixedChannelConfirmed(write)).toBe(false)
    expect(draft.showMixedChannelWarning.value).toBe(true)
    if (transition === 'actor') actor = 'supplier-b'
    if (transition === 'groups') form.group_ids = [9]
    if (transition === 'platform') { form.platform = 'anthropic'; account.platform = 'anthropic' }
    if (transition === 'closed') scope.invalidate()
    await draft.handleMixedChannelConfirm()
    expect(write).toHaveBeenCalledTimes(transition === 'unchanged' ? 1 : 0)
    expect(draft.antigravityMixedChannelConfirmed.value).toBe(transition === 'unchanged')
  })
}
