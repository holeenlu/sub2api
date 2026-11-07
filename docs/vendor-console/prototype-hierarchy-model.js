/* Design-only rules. No business API, credentials, payments or persistent tenant data. */
'use strict';
globalThis.VendorHierarchyModel = (() => {
  const SCALE = 1000000n;
  function decimal(value, positive = false) {
    if (!/^(0|[1-9]\d{0,8})(\.\d{1,6})?$/.test(String(value))) throw Error('invalidDecimal');
    const [whole, fraction = ''] = String(value).split('.');
    const result = BigInt(whole) * SCALE + BigInt(fraction.padEnd(6, '0'));
    if (positive && result === 0n) throw Error('positiveExchange');
    return result;
  }
  function amount(cost, price, multiplier, fx) {
    if (!Number.isSafeInteger(cost) || cost < 0) throw Error('invalidCost');
    const numerator = BigInt(cost) * decimal(price) * decimal(multiplier) * 100n;
    const denominator = 10000n * SCALE * decimal(fx, true);
    const rounded = (numerator + denominator / 2n) / denominator;
    if (rounded > BigInt(Number.MAX_SAFE_INTEGER)) throw Error('amountOverflow');
    return Number(rounded);
  }
  function effective(vendor, vendors) {
    if (!vendor || vendor.status !== 'active') return false;
    if (vendor.tier === 1) return vendor.parentId === null && vendor.rootId === vendor.id;
    const parent = vendors.find(v => v.id === vendor.parentId);
    return vendor.tier === 2 && parent?.tier === 1 && vendor.rootId === parent.id && effective(parent, vendors);
  }
  function canManage(actor, target, vendors) {
    return actor.role === 'super_admin' || (actor.role === 'vendor_level1' && target.tier === 2 && target.parentId === actor.id && effective(vendors.find(v => v.id === actor.id), vendors));
  }
  function resourceAccess(actor, resource, vendors) {
    const denied = { read: false, manage: false };
    const owner = resource && vendors.find(v => v.id === resource.vendorId);
    if (!owner) return denied;
    if (actor.role === 'super_admin') return { read: true, manage: !resource.archived };
    const current = vendors.find(v => v.id === actor.id);
    if (!current || actor.role !== 'vendor_level' + current.tier || !effective(current, vendors)) return denied;
    if (owner.id === actor.id) return { read: true, manage: !resource.archived };
    return actor.role === 'vendor_level1' && owner.tier === 2 && owner.parentId === actor.id && owner.rootId === actor.id
      ? { read: true, manage: false } : denied;
  }
  function archiveAccount(actor, account, vendors, at) {
    const access = resourceAccess(actor, account, vendors);
    if (!access.manage && !(access.read && account?.archived && (actor.role === 'super_admin' || account.vendorId === actor.id))) throw Error('forbidden');
    if (account.archived) return;
    account.archived = true;
    account.archivedAt = at;
    account.paused = true;
    account.current = 0;
  }
  function usageTotals(rows) {
    const sum = { requests: 0, input: 0, output: 0, write: 0, read: 0, cost: 0, opus: 0, fable: 0, other: 0 };
    for (const row of rows) {
      for (const key of ['requests', 'input', 'output', 'write', 'read', 'cost']) sum[key] += row[key];
      // Fixtures explicitly declare a family; production classification stays on the backend.
      sum[['opus', 'fable'].includes(row.modelFamily) ? row.modelFamily : 'other'] += row.cost;
    }
    sum.ioTokens = sum.input + sum.output;
    sum.totalTokens = sum.ioTokens + sum.write + sum.read;
    const input = sum.input + sum.write + sum.read;
    sum.cacheHitRate = input ? sum.read / input : null;
    return sum;
  }
  function usageGroups(rows, dimension, sort = 'cost', order = 'desc') {
    const groups = new Map();
    for (const row of rows) {
      const key = dimension === 'supplier' ? row.leafId : dimension === 'account' ? row.account : dimension === 'date' ? row.date : dimension === 'model' ? row.model : dimension === 'account-date' ? row.account + ' · ' + row.date : row.group;
      if (!groups.has(key)) groups.set(key, []);
      groups.get(key).push(row);
    }
    return [...groups].map(([key, items]) => ({ key, items, stats: usageTotals(items) }))
      .sort((a, b) => (a.stats[sort] - b.stats[sort]) * (order === 'asc' ? 1 : -1) || String(a.key).localeCompare(String(b.key), 'en', { numeric: true }));
  }
  function validatePolicy(actor, target, vendors) {
    if (!canManage(actor, target, vendors)) return 'forbidden';
    if (!vendors.some(v => v.id === target.id) && (actor.role === 'super_admin' ? target.tier !== 1 : actor.role !== 'vendor_level1' || target.tier !== 2)) return 'forbidden';
    if (!target.groups.length || !target.platforms.length) return 'selectGroups';
    if (![target.accountLimit, target.proxyLimit].every(n => Number.isSafeInteger(n) && n > 0 && n <= 100000)) return 'invalidLimits';
    if (vendors.some(v => v.id !== target.id && v.email.toLowerCase() === target.email.toLowerCase())) return 'duplicateEmail';
    if (target.tier === 2) {
      const parent = vendors.find(v => v.id === target.parentId);
      if (!parent || parent.tier !== 1 || target.rootId !== parent.id) return 'forbidden';
      if (target.groups.some(g => !parent.groups.includes(g)) || target.platforms.some(p => !parent.platforms.includes(p))) return 'parentSubset';
      if (target.accountLimit > parent.accountLimit || target.proxyLimit > parent.proxyLimit) return 'parentLimits';
      if (!vendors.some(v => v.id === target.id) && vendors.filter(v => v.parentId === parent.id).length >= parent.childLimit) return 'childLimit';
    } else if (target.tier !== 1 || target.parentId !== null || target.rootId !== target.id || actor.role !== 'super_admin') return 'forbidden';
    return '';
  }
  function quote(rows, contracts, leg, fx) {
    if (!['platform_to_l1', 'l1_to_l2'].includes(leg)) throw Error('forbidden');
    decimal(fx, true);
    const groups = new Map();
    for (const row of rows) {
      if (leg === 'l1_to_l2' && row.leafId === row.rootId) continue;
      const payee = leg === 'platform_to_l1' ? row.rootId : row.leafId;
      const payer = leg === 'platform_to_l1' ? 0 : row.rootId;
      const time = row.at || row.date + 'T12:00:00+08:00';
      const contract = contracts.filter(c => c.leg === leg && c.payer === payer && c.payee === payee && new Date(c.effective) <= new Date(time))
        .sort((a, b) => new Date(b.effective) - new Date(a.effective))[0];
      if (!contract) throw Error('missingContract');
      const key = [payee, contract.id, row.group].join('|');
      if (!groups.has(key)) groups.set(key, { payee, contract: contract.id, version: contract.version, group: row.group, price: contract.rates[row.group] ?? contract.price, multiplier: contract.multiplier, cost: 0, rowIds: [] });
      const line = groups.get(key);
      line.cost += row.cost;
      line.rowIds.push(row.id);
    }
    const lines = [...groups.values()].map(line => ({ ...line, cents: amount(line.cost, line.price, line.multiplier, fx) }));
    return { lines, cents: lines.reduce((sum, line) => sum + line.cents, 0) };
  }
  function canReadBill(actor, bill) {
    if (actor.role === 'super_admin') return true;
    if (bill.leg === 'platform_to_l1') return actor.role === 'vendor_level1' && actor.id === bill.payee;
    return bill.leg === 'l1_to_l2' && (
      actor.role === 'vendor_level1' && actor.id === bill.payer ||
      actor.role === 'vendor_level2' && actor.id === bill.payee
    );
  }
  function canPay(actor, bill) {
    return bill.leg === 'platform_to_l1'
      ? actor.role === 'super_admin' && bill.payer === 0
      : bill.leg === 'l1_to_l2' && actor.role === 'vendor_level1' && bill.payer === actor.id;
  }
  function transition(actor, bill, action) {
    if (!canReadBill(actor, bill)) return 'forbidden';
    if (action === 'send' && bill.state === 'draft' && canPay(actor, bill)) return 'review';
    if (action === 'confirm' && bill.state === 'review' && actor.id === bill.payee) return 'confirmed';
    if (action === 'dispute' && bill.state === 'review' && actor.id === bill.payee) return 'disputed';
    if (action === 'void' && ['draft', 'review', 'disputed', 'confirmed'].includes(bill.state) && !bill.paid && canPay(actor, bill)) return 'void';
    if (action === 'pay' && ['confirmed', 'partial'].includes(bill.state) && canPay(actor, bill)) return 'payment';
    return 'forbidden';
  }
  return { decimal, amount, effective, canManage, resourceAccess, archiveAccount, usageTotals, usageGroups, validatePolicy, quote, canReadBill, canPay, transition };
})();
