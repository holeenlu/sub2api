const { readFileSync } = require('node:fs')
const { join } = require('node:path')
const { runInNewContext } = require('node:vm')
const { test } = require('node:test')
const assert = require('node:assert/strict')
const sandbox = {}
runInNewContext(readFileSync(join(__dirname, '../prototype-hierarchy-model.js'), 'utf8'), sandbox)
const m = sandbox.VendorHierarchyModel
const root = { role: 'super_admin', id: 0 }
const l1 = { role: 'vendor_level1', id: 1 }
const l2 = { role: 'vendor_level2', id: 2 }
const parent = { id: 1, tier: 1, parentId: null, rootId: 1, status: 'active', groups: ['coding'], platforms: ['anthropic'], accountLimit: 50, proxyLimit: 20, childLimit: 2, email: 'a@example.invalid' }
const child = { ...parent, id: 2, tier: 2, parentId: 1, email: 'b@example.invalid', accountLimit: 20 }
const sibling = { ...child, id: 3, parentId: 4, rootId: 4, email: 'c@example.invalid' }
const vendors = [parent, child, sibling]
const newChild = { ...child, id: 5, email: 'new@example.invalid' }
const row = { id: 'r1', cost: 10000000, leafId: 2, rootId: 1, group: 'coding', date: '2026-10-14' }
const contracts = [
  { id: 'up-v1', version: 1, payer: 0, payee: 1, leg: 'platform_to_l1', price: '1.8', multiplier: '1', rates: {}, effective: '2026-10-01T00:00:00+08:00' },
  { id: 'down-v1', version: 1, payer: 1, payee: 2, leg: 'l1_to_l2', price: '1.6', multiplier: '0.95', rates: {}, effective: '2026-10-01T00:00:00+08:00' }
]
test('same supply fact has independent upstream and downstream prices', () => {
  assert.equal(m.quote([row], contracts, 'platform_to_l1', '6.7').cents, 26866)
  assert.equal(m.quote([row], contracts, 'l1_to_l2', '6.7').cents, 22687)
  assert.equal(m.quote([{ ...row, leafId: 1 }], contracts, 'l1_to_l2', '6.7').cents, 0)
})
test('event time selects contract version and group overrides', () => {
  const next = { ...contracts[1], id: 'down-v2', version: 2, price: '2', rates: { coding: '1' }, effective: '2026-10-15T00:00:00+08:00' }
  const q = m.quote([row, { ...row, id: 'r2', date: '2026-10-16' }], [...contracts, next], 'l1_to_l2', '6.7')
  assert.equal(q.lines.length, 2)
  assert.equal(q.cents, 22687 + 14179)
})
test('round once at group line level and reject missing prices', () => {
  const q = m.quote([{ ...row, cost: 20 }, { ...row, id: 'r2', cost: 20 }], contracts, 'platform_to_l1', '1')
  assert.equal(q.lines.length, 1)
  assert.equal(q.cents, 1)
  assert.throws(() => m.quote([row], [], 'platform_to_l1', '6.7'), /missingContract/)
  assert.throws(() => m.quote([row], [{ ...contracts[1], payer: 9 }], 'l1_to_l2', '6.7'), /missingContract/)
  assert.throws(() => m.quote([row], contracts, 'invalid_leg', '6.7'), /forbidden/)
})
test('zero is valid pricing; invalid decimals and zero FX cannot create amounts', () => {
  assert.equal(m.amount(10000000, '0', '1', '6.7'), 0)
  for (const value of ['-1', 'NaN', 'Infinity', '1.0000001', '1e2', '']) assert.throws(() => m.amount(10000, value, '1', '1'))
  assert.throws(() => m.amount(10000, '1', '1', '0'), /positiveExchange/)
})
test('only root creates tier1; only parent tier1 creates tier2', () => {
  assert.equal(m.validatePolicy(l1, newChild, vendors), '')
  assert.equal(m.validatePolicy(root, newChild, vendors), 'forbidden')
  assert.equal(m.validatePolicy(l2, newChild, vendors), 'forbidden')
  assert.equal(m.validatePolicy({ role: 'user', id: 1 }, newChild, vendors), 'forbidden')
  assert.equal(m.validatePolicy({ role: 'admin', id: 0 }, newChild, vendors), 'forbidden')
  assert.equal(m.validatePolicy(l1, { ...newChild, parentId: 4, rootId: 4 }, vendors), 'forbidden')
  assert.equal(m.validatePolicy(root, { ...parent, id: 6, rootId: 6, email: 'root2@example.invalid' }, vendors), '')
})
test('child cannot expand group scope, quotas or duplicate email identity', () => {
  assert.equal(m.validatePolicy(l1, { ...newChild, groups: ['other'] }, vendors), 'parentSubset')
  assert.equal(m.validatePolicy(l1, { ...newChild, accountLimit: 51 }, vendors), 'parentLimits')
  assert.equal(m.validatePolicy(l1, { ...newChild, email: child.email.toUpperCase() }, vendors), 'duplicateEmail')
})
test('ancestor pause blocks children and does not erase their own pause', () => {
  const disabled = [{ ...parent, status: 'disabled' }, child]
  assert.equal(m.effective(child, disabled), false)
  assert.equal(m.canManage(l1, child, disabled), false)
  assert.equal(m.effective({ ...child, status: 'disabled' }, vendors), false)
  assert.equal(m.effective(child, vendors), true)
  assert.equal(m.effective(child, [{ ...parent, rootId: 9 }, child]), false)
})
test('bill confirmation belongs to payee and payments to its payer', () => {
  const b = { leg: 'l1_to_l2', payer: 1, payee: 2, state: 'review', paid: 0 }
  assert.equal(m.transition(l1, b, 'confirm'), 'forbidden')
  assert.equal(m.transition(l2, b, 'confirm'), 'confirmed')
  assert.equal(m.transition(root, { ...b, state: 'confirmed' }, 'pay'), 'forbidden')
  assert.equal(m.transition(l1, { ...b, state: 'confirmed' }, 'pay'), 'payment')
  assert.equal(m.transition(l1, { ...b, state: 'partial', paid: 1 }, 'void'), 'forbidden')
  assert.equal(m.canReadBill({ role: 'vendor_level2', id: 3 }, b), false)
  const upstream = { ...b, leg: 'platform_to_l1', payer: 0, payee: 1, state: 'confirmed' }
  assert.equal(m.canReadBill(l1, upstream), true)
  assert.equal(m.canReadBill(l2, upstream), false)
  assert.equal(m.canReadBill({ role: 'admin', id: 0 }, upstream), false)
  assert.equal(m.canReadBill({ role: 'user', id: 2 }, b), false)
  assert.equal(m.transition({ role: 'vendor_level2', id: 1 }, { ...b, state: 'confirmed' }, 'pay'), 'forbidden')
  assert.equal(m.transition(root, upstream, 'pay'), 'payment')
})

test('root sees all resources while tier1 reads only its own direct children', () => {
  const resource = { id: 2001, vendorId: 2 }
  assert.equal(m.resourceAccess(root, resource, vendors).manage, true)
  assert.equal(m.resourceAccess(l1, resource, vendors).read, true)
  assert.equal(m.resourceAccess(l1, resource, vendors).manage, false)
  assert.equal(m.resourceAccess(l2, resource, vendors).manage, true)
  assert.equal(m.resourceAccess(l1, { vendorId: 3 }, vendors).read, false)
  assert.equal(m.resourceAccess(root, { vendorId: 3 }, vendors).read, true)
  assert.equal(m.resourceAccess(l2, { vendorId: 1 }, vendors).read, false)
  assert.equal(m.resourceAccess({ role: 'admin', id: 2 }, resource, vendors).read, false)
  assert.equal(m.resourceAccess(l1, resource, [{ ...parent, status: 'disabled' }, child]).read, false)
  assert.equal(m.resourceAccess(l1, resource, [parent, { ...child, status: 'disabled' }]).read, true)
})

test('archiving retains identity, credentials, references and quota while stopping supply', () => {
  const account = { id: 2001, vendorId: 2, health: '正常', current: 3, paused: false,
    credential: 'DEMO_KEEP', proxy: 2000, groups: ['coding'], quota: { five: 42 }, notes: 'retained' }
  assert.throws(() => m.archiveAccount(l1, account, vendors, '2026-10-17'), /forbidden/)
  m.archiveAccount(l2, account, vendors, '2026-10-17')
  assert.equal(account.archived, true)
  assert.equal(account.archivedAt, '2026-10-17')
  assert.equal(account.paused, true)
  assert.equal(account.current, 0)
  assert.equal(account.credential, 'DEMO_KEEP')
  assert.equal(account.proxy, 2000)
  assert.deepEqual(account.groups, ['coding'])
  assert.equal(account.quota.five, 42)
  assert.equal(account.health, '正常')
  assert.equal(account.notes, 'retained')
  assert.equal(m.resourceAccess(l2, account, vendors).read, true)
  assert.equal(m.resourceAccess(l2, account, vendors).manage, false)
  assert.equal(m.resourceAccess(root, account, vendors).manage, false)
  m.archiveAccount(l2, account, vendors, '2026-10-18')
  assert.equal(account.archivedAt, '2026-10-17')
})

test('operational totals use weighted cache hits and reconcile all model costs', () => {
  const usage = [
    { account: 1, requests: 1, input: 10, output: 20, write: 0, read: 90, cost: 101, modelFamily: 'opus' },
    { account: 2, requests: 9, input: 800, output: 200, write: 100, read: 0, cost: 205, modelFamily: 'fable' },
    { account: 2, requests: 0, input: 0, output: 0, write: 0, read: 0, cost: 3, modelFamily: null },
  ]
  const sum = m.usageTotals(usage)
  assert.equal(sum.cacheHitRate, 0.09)
  assert.equal(sum.ioTokens, 1030)
  assert.equal(sum.totalTokens, 1220)
  assert.equal(sum.opus + sum.fable + sum.other, sum.cost)
  assert.equal(sum.other, 3)
  assert.equal(m.usageTotals([]).cacheHitRate, null)
  const groups = m.usageGroups(usage, 'account')
  assert.equal(groups[0].key, 2)
  assert.equal(groups[0].stats.cost, 208)
  assert.equal(groups.reduce((sum, g) => sum + g.stats.cost, 0), sum.cost)
})

test('report sorting is stable for equal costs and account/date groups count each fact once', () => {
  const base = { requests: 1, input: 0, output: 1, write: 0, read: 0, cost: 100, date: '2026-10-14' }
  const rows = [{ ...base, account: 12 }, { ...base, account: 2 }, { ...base, account: 2, date: '2026-10-15' }]
  const groups = m.usageGroups(rows, 'account-date')
  assert.equal(groups[0].key, '2 · 2026-10-14')
  assert.equal(groups[1].key, '2 · 2026-10-15')
  assert.equal(groups.length, 3)
  assert.equal(groups.reduce((sum, g) => sum + g.stats.requests, 0), 3)
  assert.equal(m.usageGroups(rows, 'account', 'requests', 'asc')[0].key, 12)
})
