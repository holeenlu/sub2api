/* Design-only prototype. No business API, storage, analytics or provider requests. */
'use strict';
const DEMO_TODAY = '2026-10-15';
const INITIAL_PROXIES = [
  { id: 1, name: '专属出口 A', host: 'proxy-a.example.invalid', port: 8080, protocol: 'HTTP', state: '可用' },
  { id: 2, name: '专属出口 B', host: 'proxy-b.example.invalid', port: 1080, protocol: 'SOCKS5', state: '可用' },
  { id: 3, name: '备用出口 C', host: 'proxy-c.example.invalid', port: 8080, protocol: 'HTTP', state: '未测试' }
];
const AUTH_METHODS = {
  anthropic: [{ id: 'oauth', label: 'OAuth 授权', link: true }, { id: 'setup-token', label: 'OAuth Token', link: true }, { id: 'setup-token-manual', label: 'Setup Token', link: false }],
  openai: [{ id: 'oauth', label: 'OAuth 授权', link: true }, { id: 'refresh_token', label: 'Refresh Token', link: false }]
};
const GROUPS = { anthropic: ['Claude 默认', 'Claude Coding', 'Claude 通用'], openai: ['OpenAI 默认', 'OpenAI Coding'] };
const DEMO_NOW = '2026-10-15T14:20:00+08:00';
const INITIAL_ACCOUNTS = Array.from({ length: 14 }, (_, i) => ({
  id: 101 + i, name: `${i % 3 === 1 ? 'OpenAI' : 'Claude'} ${['主力', '工作', '备用'][i % 3]} ${String(i + 1).padStart(2, '0')}`,
  platform: i % 3 === 1 ? 'openai' : 'anthropic', method: i === 5 ? 'setup-token' : i === 8 ? 'setup-token-manual' : i === 10 ? 'refresh_token' : 'oauth',
  notes: ['常规供给', '团队工作账号', '备用资源', '等待重新授权', '固定出口'][i % 5],
  health: i === 3 ? '凭证异常' : i === 6 ? '配额冷却' : i === 11 ? '代理异常' : '正常',
  paused: i === 9, platformDisabled: i === 12, archived: false, proxy: 1 + i % 2,
  groups: i % 3 === 1 ? ['OpenAI 默认'] : i % 2 ? ['Claude 默认', 'Claude Coding'] : ['Claude 默认'], credential: 'DEMO_CREDENTIAL_' + (101 + i), concurrency: 5, priority: 1,
  current: [3, 1, 2, 0, 2, 1, 0, 2, 1, 0, 1, 0, 0, 1][i],
  rpm: 4 + i * 2, input: 2810 + i * 343, output: 1360 + i * 127, cache: 8120 + i * 501,
  quota: { five: i === 6 ? 100 : (18 + i * 13) % 98, seven: (29 + i * 7) % 99, fable: i % 3 === 1 ? null : (12 + i * 9) % 99 },
  resets: { five: '2026-10-15T17:00:00+08:00', seven: `2026-10-${19 + i % 4}T12:00:00+08:00`, fable: null },
  created: `2026-09-${String(1 + i).padStart(2, '0')}`
}));
// Integer 1/10,000 USD fixture amounts prevent independently rounded totals.
// These numbers are fictitious, not provider prices or reference-site account data.
let LEDGER = [];
for (let day = '2026-09-01'; day <= DEMO_TODAY; day = addDays(day, 1)) {
  INITIAL_ACCOUNTS.filter(a => a.created <= day).forEach((a, i) => {
    const seed = Number(day.slice(-2)) + a.id;
    if (seed % 11 === 0) return;
    LEDGER.push({ date: day, account: a.id, requests: 30 + seed % 137, input: 17000 + seed * 137, output: 8000 + seed * 59, write: a.platform === 'openai' ? 0 : 9000 + seed * 89, read: 18000 + seed * 271, cost: 1360 + (seed * 941 + i * 157) % 88000 });
  });
}
// Timestamped buckets preserve daily totals and make fixed windows auditable.
const dailyFixtures=LEDGER.splice(0);
for(const r of dailyFixtures) for(let slot=0;slot<4;slot++){
 const bucket={...r,at:`${r.date}T${String(2+slot*4).padStart(2,'0')}:00:00+08:00`};
 for(const key of ['requests','input','output','write','read','cost'])bucket[key]=Math.floor(r[key]/4)+(slot<r[key]%4?1:0);
 const platform=INITIAL_ACCOUNTS.find(a=>a.id===r.account).platform;
 bucket.modelFamily=platform==='anthropic'?['fable','opus','other','opus'][slot]:'other';
 bucket.model=platform==='anthropic'?['Claude Fable','Claude Opus','Claude Sonnet','Claude Opus'][slot]:(slot%2?'OpenAI 通用':'OpenAI Coding');
 bucket.failed=(r.account+slot+Number(r.date.slice(-2)))%17===0?1:0;
 bucket.latencyMs=18000+((r.account+slot)%8)*3100;bucket.ttftMs=1100+((r.account+slot)%7)*530;LEDGER.push(bucket);
}

let accounts = structuredClone(INITIAL_ACCOUNTS), proxies = structuredClone(INITIAL_PROXIES);
let page = 'accounts', collapsed = false, refreshedAt = '演示快照 · 10/15 14:20', sequence = 0;
const defaultFilters = () => ({ q: '', platform: 'all', state: 'all', group: 'all', method: 'all', fault: 'all', sort: 'created', cooldown: 'all', schedulable: 'all', createdFrom: '', createdTo: '' });
let filters = defaultFilters();
const ACCOUNT_COLUMNS = [
  ['name', '名称'], ['status', '状态'], ['groups', '账号分组'], ['quota', '配额窗口'],
  ['concurrency', '并发'], ['priority', '权重'], ['today', '今日消耗'], ['cost', '总消耗'],
  ['notes', '备注'], ['cache', '缓存命中率'], ['created', '创建时间']
];
let accountColumnVisibility = Object.fromEntries(ACCOUNT_COLUMNS.map(([key]) => [key, true]));
let usage = { account: 'all', platform: 'all', start: addDays(DEMO_TODAY, -6), end: DEMO_TODAY, label: '近 7 天' };
let calendar = null, accountForm = null, toastTimer, profileSettingsTab = 'info';
const paging = { accounts: { page: 1, size: 10 }, daily: { page: 1, size: 7 }, usageAccounts: { page: 1, size: 10 } };
const $ = id => document.getElementById(id);
const esc = value => String(value ?? '').replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
const number = n => new Intl.NumberFormat('en-US').format(n);
const money = n => '$' + (n / 10000).toLocaleString('en-US', { minimumFractionDigits: 4, maximumFractionDigits: 4 });
const platformName = p => p === 'anthropic' ? 'Anthropic' : 'OpenAI';
const methodName = a => AUTH_METHODS[a.platform].find(m => m.id === a.method)?.label || 'OAuth 授权';
const proxyName = id => id === 0 ? (ownerPolicy().direct?'直连（策略允许）':'直连（未授权）') : allProxies().find(p => p.id === id)?.name || '尚未选择代理';
function addDays(iso, days) { const d = new Date(iso + 'T12:00:00Z'); d.setUTCDate(d.getUTCDate() + days); return d.toISOString().slice(0, 10); }
function accountAccess(a) { return window.vendorHierarchy ? VendorHierarchyModel.resourceAccess(window.vendorHierarchy.actor,a,vendors) : {read:!!a,manage:!!a&&!a.archived}; }
function status(a) { return a.archived ? a.health : vendorScopeBlock(a) ? vendorScopeBlock(a) : a.platformDisabled ? '平台禁用' : a.paused ? '供应商暂停' : a.health; }
function badge(s) { const color = ['正常', '可用', '已开通'].includes(s) ? 'ok' : s === '配额冷却' ? 'warn' : s.includes('异常') ? 'error' : ''; return `<span class="badge ${color}">${esc(s)}</span>`; }
function total(rows) { return VendorHierarchyModel.usageTotals(rows); }
function hitRate(s) { const input = s.input + s.write + s.read; return input ? (100 * s.read / input).toFixed(1) + '%' : '—'; }
function accountTotals(id, today = false) { return total(allLedger().filter(r => r.account === id && (!today || r.date === DEMO_TODAY))); }
function notify(message) { clearTimeout(toastTimer); $('toast').textContent = message; $('toast').hidden = false; toastTimer = setTimeout(() => { $('toast').hidden = true; }, 6000); }
function heading(title, subtitle, buttons = '') { return `<div class="heading"><div><h1>${title}</h1><p>${subtitle}</p></div><div class="row">${buttons}</div></div>`; }
function metric(label, value, caption = '', color = '') { return `<div class="metric ${color}"><span>${label}</span><strong>${value}</strong>${caption ? `<small>${caption}</small>` : ''}</div>`; }
function pagination(key, count) {
  const p = paging[key], max = Math.max(1, Math.ceil(count / p.size)); p.page = Math.min(p.page, max);
  const label = { accounts: '账号池', daily: '按日趋势', usageAccounts: '按账号明细' }[key];
  return `<div class="pagination"><div class="row">共 ${count} 条 <label>每页 <select data-size="${key}" aria-label="${label}每页条数">${[5, 7, 10, 20, 50].map(n => `<option ${n === p.size ? 'selected' : ''}>${n}</option>`).join('')}</select> 条</label></div><div class="row"><span>第 ${p.page} / ${max} 页</span><button data-action="paginate" data-key="${key}" data-to="1" aria-label="${label}第一页" ${p.page === 1 ? 'disabled' : ''}>«</button><button data-action="paginate" data-key="${key}" data-to="${p.page - 1}" aria-label="${label}上一页" ${p.page === 1 ? 'disabled' : ''}>‹</button><span class="current">${p.page}</span><button data-action="paginate" data-key="${key}" data-to="${p.page + 1}" aria-label="${label}下一页" ${p.page === max ? 'disabled' : ''}>›</button><button data-action="paginate" data-key="${key}" data-to="${max}" aria-label="${label}最后一页" ${p.page === max ? 'disabled' : ''}>»</button></div></div>`;
}
function paged(rows, key) { const p = paging[key]; p.page = Math.min(p.page, Math.max(1, Math.ceil(rows.length / p.size))); return rows.slice((p.page - 1) * p.size, p.page * p.size); }
function distribution(label, key, list) {
  const values = list.map(a => a.quota[key]), counts = [values.filter(v => v !== null && v < 90).length, values.filter(v => v >= 90 && v < 99).length, values.filter(v => v >= 99).length, values.filter(v => v === null).length];
  return `<div><h3>${label} <span class="muted">· ${list.length} 个适用账号</span></h3><div class="dist-bar">${counts.map((n, i) => `<span class="${['normal', 'near', 'full', 'unknown'][i]}" style="width:${list.length ? n / list.length * 100 : 0}%"></span>`).join('')}</div><div class="legend">${counts.map((n, i) => `<span><i class="${['normal', 'near', 'full', 'unknown'][i]}"></i>${['正常', '接近上限', '已满', '未知'][i]} ${n}</span>`).join('')}</div></div>`;
}
function renderOverview() {
  const live = accounts.filter(a => !a.archived), ready = live.filter(a => status(a) === '正常');
  const sum = key => ready.reduce((n, a) => n + a[key], 0);
  return `<section class="panel overview-panel"><div class="panel-head"><div class="row"><h2>账号概览（我的账号）</h2><button class="text-button small" data-action="cumulative-help">累计 ⓘ</button></div><div class="row"><span class="muted small">${refreshedAt}</span><button class="text-button small" data-action="metrics-help">统计口径说明</button><button class="quiet small" data-action="collapse" aria-expanded="${!collapsed}">${collapsed ? '展开' : '收起'} ${collapsed ? '⌄' : '⌃'}</button></div></div><div class="panel-body" ${collapsed ? 'hidden' : ''}><div class="overview-top"><div class="overview-summary">${metric('总账号数', accounts.length, accounts.some(a=>a.archived)?'含已归档 '+accounts.filter(a=>a.archived).length+' 个':'')}${metric('可用账号数', ready.length, '', 'green')}${metric('冷却账号数', live.filter(a => status(a) === '配额冷却').length, '', 'amber')}${metric('异常账号数', live.filter(a => status(a).includes('异常')).length, '', 'red')}${metric('停用账号数', live.filter(a => ['供应商暂停', '平台禁用'].includes(status(a))).length)}</div><div class="live-metrics">${metric('RPM', number(sum('rpm')), '最近 1 分钟 · 演示')}${metric('TPM', number(sum('input') + sum('output') + sum('cache')), '含缓存')}${metric('TPM（不含缓存）', number(sum('input') + sum('output')), '输入 + 输出')}${metric('实时并发', `${sum('current')} <small>/ ${live.reduce((n, a) => n + a.concurrency, 0)}</small>`, '当前 / 配置上限')}${metric('配额高负载', live.filter(a => Object.values(a.quota).some(v => v !== null && v >= 90)).length, '演示阈值 ≥ 90%')}</div></div><div class="quota-distributions">${distribution('5 小时窗口', 'five', live)}${distribution('7 天窗口', 'seven', live)}${distribution('Fable 窗口', 'fable', live.filter(a => a.platform === 'anthropic'))}</div></div></section>`;
}
function filterSelect(key, label, options) { return `<select data-filter="${key}" aria-label="${label}">${options.map(([value, text]) => `<option value="${esc(value)}" ${filters[key] === value ? 'selected' : ''}>${esc(text)}</option>`).join('')}</select>`; }
function accountCooldown(a) {
  if (status(a) === '配额冷却') return 'account';
  if (a.quota.seven !== null && a.quota.seven >= 99) return 'seven_day';
  if (a.quota.five !== null && a.quota.five >= 99) return 'five_hour';
  if (a.quota.fable !== null && a.quota.fable >= 99) return 'fable';
  return 'available';
}
function accountMatchesCooldown(a, value) {
  if (value === 'all') return true;
  if (value === 'account') return status(a) === '配额冷却';
  if (value === 'seven_day') return a.quota.seven !== null && a.quota.seven >= 99;
  if (value === 'five_hour') return a.quota.five !== null && a.quota.five >= 99;
  if (value === 'fable') return a.quota.fable !== null && a.quota.fable >= 99;
  return accountCooldown(a) === value;
}
function accountSchedulable(a) { return !a.archived && status(a) === '正常' && !a.paused && !a.platformDisabled; }
function accountVisible(key) { return accountColumnVisibility[key] !== false; }
function accountColumnChooser() {
  return `<details class="column-menu"><summary>☷ 列显示</summary><div><strong>显示的列</strong>${ACCOUNT_COLUMNS.map(([key, label]) => `<label class="checkline"><input type="checkbox" data-account-column="${key}" ${accountVisible(key) ? 'checked' : ''}>${label}</label>`).join('')}<button data-action="reset-account-columns">恢复默认列</button></div></details>`;
}
function renderAccounts() {
  const faultOptions = [['all', '不限类型'], ['凭证异常', '认证失败'], ['平台禁用', '平台禁用'], ['代理异常', '代理连接'], ['配额冷却', '配额冷却']];
  $('view').innerHTML = heading('账号池', '管理自己的 Claude / OpenAI 账号与供给状态', '<button data-action="refresh">↻ 刷新</button><details class="toolbar-menu"><summary>更多操作 ⌄</summary><div><button data-action="import-accounts">↑ 导入账号授权</button><button data-action="export-accounts">↓ 导出账号授权</button></div></details><button class="primary" data-action="add-account">＋ 添加账号</button>') + renderOverview() + `<section class="panel"><div class="filterbar"><input class="search" id="account-search" aria-label="搜索账号" placeholder="搜索名称 / 账号 ID" value="${esc(filters.q)}">${filterSelect('platform', '平台筛选', [['all', '全部平台'], ['anthropic', 'Claude'], ['openai', 'OpenAI']])}${filterSelect('state', '状态筛选', [['all', '全部状态'], ...['正常', '配额冷却', '凭证异常', '代理异常', '供应商暂停', '平台禁用', '已归档'].map(s => [s, s])])}${filterSelect('group', '分组筛选', [['all', '全部分组'], ...Object.values(GROUPS).flat().map(g => [g,g])])}${filterSelect('method', '授权方式筛选', [['all', '全部授权方式'], ['oauth', 'OAuth 授权'], ['setup-token', 'OAuth Token'], ['setup-token-manual', 'Setup Token'], ['refresh_token', 'Refresh Token']])}${filterSelect('sort', '账号排序', [['created', '创建时间 ↓'], ['today', '今日消耗 ↓'], ['cost', '累计消耗 ↓']])}<details class="account-more-filters"><summary>更多筛选</summary><div>${filterSelect('cooldown', '账号冷却', [['all', '全部冷却状态'], ['available', '账号可用'], ['account', '账号冷却'], ['seven_day', '7 天冷却'], ['five_hour', '5 小时冷却'], ['fable', 'Fable 冷却']])}${filterSelect('schedulable', '调度状态', [['all', '全部调度状态'], ['schedulable', '可调度'], ['unschedulable', '不可调度']])}<label>创建时间（起）<input id="account-created-from" type="date" max="${DEMO_TODAY}" value="${filters.createdFrom}"></label><label>至<input id="account-created-to" type="date" max="${DEMO_TODAY}" value="${filters.createdTo}"></label><button data-action="account-created-apply">应用日期</button><button data-action="account-created-clear">清空日期</button></div></details><button class="quiet" data-action="reset-filters">重置</button>${accountColumnChooser()}</div><div class="faults"><span>故障分布</span>${faultOptions.map(([value, label]) => `<button type="button" class="chip ${filters.fault === value ? 'active' : ''}" aria-pressed="${filters.fault === value}" data-action="fault" data-fault="${value}">${label}<b>${accounts.filter(a => !a.archived && (value === 'all' || status(a) === value)).length}</b></button>`).join('')}<span>全量自有账号</span></div><div class="table-wrap"><table class="accounts-table" aria-label="账号池明细"><thead><tr>${ACCOUNT_COLUMNS.filter(([key]) => accountVisible(key)).map(([, label]) => `<th>${label}</th>`).join('')}<th>操作</th></tr></thead><tbody id="account-rows"></tbody></table></div><div id="account-pagination"></div></section><p class="muted small">分组可在平台授权范围内多选；并发、优先级与倍率使用平台默认。配额窗口遵循上游数据；未知不等于零。</p>`;
  renderAccountRows();
}
function renderAccountRows() {
  const rows = accounts.filter(a => (filters.platform === 'all' || filters.platform === a.platform) && (filters.state === 'all' || (filters.state === '已归档' ? a.archived : filters.state === status(a))) && (filters.group === 'all' || a.groups.includes(filters.group)) && (filters.method === 'all' || filters.method === a.method) && (filters.fault === 'all' || filters.fault === status(a)) && accountMatchesCooldown(a, filters.cooldown) && (filters.schedulable === 'all' || (filters.schedulable === 'schedulable') === accountSchedulable(a)) && (!filters.createdFrom || a.created >= filters.createdFrom) && (!filters.createdTo || a.created <= filters.createdTo) && `${a.name} ${a.id}`.toLowerCase().includes(filters.q.toLowerCase()));
  rows.sort((a, b) => filters.sort === 'created' ? b.id - a.id : accountTotals(b.id, filters.sort === 'today').cost - accountTotals(a.id, filters.sort === 'today').cost);
  const visibleCount = ACCOUNT_COLUMNS.filter(([key]) => accountVisible(key)).length + 1;
  $('account-rows').innerHTML = paged(rows, 'accounts').map(a => {
    const totalCost = accountTotals(a.id), todayCost = accountTotals(a.id, true);
    return `<tr>${accountVisible('name') ? `<td><div class="account-name"><span class="provider-icon ${a.platform}">${a.platform === 'anthropic' ? '✳' : '◎'}</span><div><strong>${esc(a.name)}</strong>${a.archived?'<span class="badge archived">已归档</span>':''}<small>#${a.id} · ${methodName(a)}</small><small>${esc(proxyName(a.proxy))}</small></div></div></td>` : ''}${accountVisible('status') ? `<td><div class="status-cell">${badge(status(a))}<button class="switch" role="switch" aria-checked="${!a.paused}" aria-label="${a.paused ? '恢复' : '暂停'} ${esc(a.name)} 供给" data-action="toggle" data-id="${a.id}" ${a.archived || a.platformDisabled ? 'disabled' : ''}></button></div></td>` : ''}${accountVisible('groups') ? `<td>${a.groups.map(g => `<span class="badge">${esc(g)}</span>`).join(' ')}</td>` : ''}${accountVisible('quota') ? `<td class="quota-cell">${quotaCell(a)}</td>` : ''}${accountVisible('concurrency') ? `<td>${accountSchedulable(a) ? a.current : 0} <span class="muted">/ ${a.concurrency}</span></td>` : ''}${accountVisible('priority') ? `<td>${a.priority}</td>` : ''}${accountVisible('today') ? `<td class="number">${money(todayCost.cost)}</td>` : ''}${accountVisible('cost') ? `<td class="number">${money(totalCost.cost)}</td>` : ''}${accountVisible('notes') ? `<td class="note-cell" title="${esc(a.notes)}">${esc(a.notes || '—')}</td>` : ''}${accountVisible('cache') ? `<td class="number">${hitRate(totalCost)}</td>` : ''}${accountVisible('created') ? `<td>${a.created}<small>Asia/Shanghai</small></td>` : ''}<td>${accountActions(a)}</td></tr>`;
  }).join('') || `<tr><td colspan="${visibleCount}" class="empty">没有符合条件的账号，请调整筛选。</td></tr>`;
  $('account-pagination').innerHTML = pagination('accounts', rows.length);
}
function renderUsage() {
  $('view').innerHTML = heading('用量统计', '查看归属账号的请求、Token 与消耗') + `<section class="panel usage-filters"><label>账号<select id="usage-account" aria-label="用量账号"><option value="all">全部账号</option>${accounts.map(a => `<option value="${a.id}" ${usage.account === String(a.id) ? 'selected' : ''}>${esc(a.name)}${a.archived ? '（已归档）' : ''}</option>`).join('')}</select></label><div class="date-wrapper"><label>时间范围<button id="range-toggle" class="range-button" data-action="open-calendar" aria-haspopup="dialog" aria-expanded="false">▦ ${usage.start} — ${usage.end}<span>⌄</span></button></label><div id="calendar-host"></div></div><details class="usage-extra-filters"><summary>更多筛选</summary><div><label>平台<select id="usage-platform" aria-label="用量平台"><option value="all">全部平台</option><option value="anthropic" ${usage.platform === 'anthropic' ? 'selected' : ''}>Claude</option><option value="openai" ${usage.platform === 'openai' ? 'selected' : ''}>OpenAI</option></select></label><span class="tz">Asia/Shanghai · 演示今日 ${DEMO_TODAY}</span></div></details></section><div id="usage-data"></div><p class="muted small">USD · 账号统计消耗，非分成或应结算金额。当前状态仅表示账号现在的状态。示例数据覆盖 2026/09/01–10/15。</p>`;
  renderUsageData();
}
function filteredLedger() { return LEDGER.filter(r => r.date >= usage.start && r.date <= usage.end && (usage.account === 'all' || r.account === Number(usage.account)) && (usage.platform === 'all' || accounts.find(a => a.id === r.account)?.platform === usage.platform)); }
function usagePair(r, cache = false) { return cache ? `<div class="pair"><span>写</span> ${number(r.write)}<br><span>读</span> ${number(r.read)}</div>` : `<div class="pair"><span>入</span> ${number(r.input)}<br><span>出</span> ${number(r.output)}</div>`; }
function aggregate(rows, key) { const grouped = new Map(); rows.forEach(r => { if (!grouped.has(r[key])) grouped.set(r[key], []); grouped.get(r[key]).push(r); }); return [...grouped].map(([id, group]) => ({ id, ...total(group) })); }
function renderUsageData() {
  const rows = filteredLedger(), s = total(rows), daily = aggregate(rows, 'date').sort((a, b) => a.id.localeCompare(b.id));
  const byAccount = aggregate(rows, 'account').sort((a, b) => b.cost - a.cost || a.id - b.id);
  const summary = (label, value, hint) => `<div class="summary-item"><span>${label}</span><strong>${value}</strong>${hint ? `<small>${hint}</small>` : ''}</div>`;
  $('usage-data').innerHTML = `<section class="panel summary-strip" aria-label="用量汇总">${summary('消耗总额', money(s.cost), 'USD · 统计消耗')}${summary('总请求数', number(s.requests), usage.label || '自定义时间范围')}${summary('Token 消耗', number(s.input + s.output), `入 ${number(s.input)} / 出 ${number(s.output)}`)}${summary('缓存', number(s.write + s.read), `写 ${number(s.write)} / 读 ${number(s.read)}`)}</section><section class="panel"><div class="panel-head"><h2>按日趋势</h2><span class="muted small">${usage.start} 至 ${usage.end}</span></div><div class="table-wrap"><table class="usage-table" aria-label="按日趋势"><thead><tr><th>日期</th><th class="number">请求数</th><th class="number">Token 消耗</th><th class="number">缓存</th><th class="number">消耗</th></tr></thead><tbody>${paged(daily, 'daily').map(r => `<tr><td>${r.id.replaceAll('-', '/')}</td><td class="number">${number(r.requests)}</td><td class="number">${usagePair(r)}</td><td class="number">${usagePair(r, true)}</td><td class="number">${money(r.cost)}</td></tr>`).join('') || '<tr><td colspan="5" class="empty">此范围没有用量记录</td></tr>'}</tbody></table></div>${pagination('daily', daily.length)}</section><section class="panel"><div class="panel-head"><div><h2>按账号明细</h2><p class="muted small">归属账号按消耗从高到低排列</p></div><span class="muted small">缓存命中率 = 读取 /（输入 + 写入 + 读取）</span></div><div class="table-wrap"><table class="usage-table" aria-label="按账号明细"><thead><tr><th>账号</th><th>当前状态</th><th class="number">请求数</th><th class="number">Token 消耗</th><th class="number">缓存</th><th class="number">缓存命中率</th><th class="number">消耗</th></tr></thead><tbody>${paged(byAccount, 'usageAccounts').map(r => { const a = accounts.find(a => a.id === r.id); return `<tr><td><button class="text-button" data-action="usage-detail" data-id="${a.id}">${esc(a.name)}</button><small>#${a.id} · ${platformName(a.platform)}</small></td><td>${badge(status(a))}</td><td class="number">${number(r.requests)}</td><td class="number">${usagePair(r)}</td><td class="number">${usagePair(r, true)}</td><td class="number">${hitRate(r)}</td><td class="number">${money(r.cost)}</td></tr>`; }).join('') || '<tr><td colspan="7" class="empty">此范围没有账号用量</td></tr>'}</tbody></table></div>${pagination('usageAccounts', byAccount.length)}</section>`;
}
const PRESETS = ['今天', '昨天', '近 7 天', '近 30 天', '本月', '上月'];
function presetRange(label) {
  if (label === '今天') return [DEMO_TODAY, DEMO_TODAY];
  if (label === '昨天') return [addDays(DEMO_TODAY, -1), addDays(DEMO_TODAY, -1)];
  if (label === '近 7 天') return [addDays(DEMO_TODAY, -6), DEMO_TODAY];
  if (label === '近 30 天') return [addDays(DEMO_TODAY, -29), DEMO_TODAY];
  if (label === '本月') return [DEMO_TODAY.slice(0, 7) + '-01', DEMO_TODAY];
  const end = addDays(DEMO_TODAY.slice(0, 7) + '-01', -1); return [end.slice(0, 7) + '-01', end];
}
function monthMarkup(offset) {
  const date = new Date(Date.UTC(calendar.year, calendar.month + offset, 1)), year = date.getUTCFullYear(), month = date.getUTCMonth();
  const first = (date.getUTCDay() + 6) % 7, count = new Date(Date.UTC(year, month + 1, 0)).getUTCDate();
  return `<section class="calendar-month"><h3>${year}年${month + 1}月</h3><div class="calendar-grid">${['一', '二', '三', '四', '五', '六', '日'].map(x => `<span class="weekday">${x}</span>`).join('')}${'<span></span>'.repeat(first)}${Array.from({ length: count }, (_, i) => { const day = `${year}-${String(month + 1).padStart(2, '0')}-${String(i + 1).padStart(2, '0')}`; return `<button data-action="select-date" data-date="${day}" aria-label="${day}" aria-pressed="${day === calendar.start || day === calendar.end}" class="${day === calendar.start || day === calendar.end ? 'selected' : day > calendar.start && day < calendar.end ? 'between' : ''} ${day === DEMO_TODAY ? 'today' : ''}" ${day > DEMO_TODAY ? 'disabled' : ''}>${i + 1}</button>`; }).join('')}</div></section>`;
}
function renderCalendar() {
  const tooLong = calendar?.end && (new Date(calendar.end + 'T00:00:00Z') - new Date(calendar.start + 'T00:00:00Z')) / 86400000 >= 31;
  $('range-toggle').setAttribute('aria-expanded', String(!!calendar));
  if (!calendar) { $('calendar-host').innerHTML = ''; return; }
  $('calendar-host').innerHTML = `<div class="date-popover" role="dialog" aria-label="选择时间范围"><div class="date-presets">${PRESETS.map(p => `<button data-action="date-preset" data-preset="${p}" class="${calendar.label === p ? 'active' : ''}">${p}</button>`).join('')}</div><div class="calendar-main"><div class="calendar-nav"><button data-action="calendar-month" data-delta="-1" aria-label="前一个月">‹</button><span class="muted small">${calendar.choosingEnd ? '请选择结束日期' : '请选择开始日期'}</span><button data-action="calendar-month" data-delta="1" aria-label="后一个月">›</button></div><div class="calendar-months">${monthMarkup(0)}${monthMarkup(1)}</div><div class="calendar-footer"><span>${calendar.start || '起'} — ${calendar.end || '止'}${tooLong ? '<br><span class="form-error">一次最多查询 31 天，请调整范围</span>' : ''}</span><div class="row"><button class="quiet" data-action="date-clear">清空</button><button class="quiet" data-action="date-cancel">取消</button><button class="primary" data-action="date-confirm" ${!calendar.start || !calendar.end || tooLong ? 'disabled' : ''}>确定</button></div></div></div></div>`;
}
function applyUsageRange(start, end, label) { usage = { ...usage, start, end, label }; calendar = null; paging.daily.page = paging.usageAccounts.page = 1; renderUsage(); $('range-toggle').focus(); }

function openModal(title, subtitle, content, buttons) {
  $('modal').classList.remove('wide', 'detail-drawer'); $('modal-body').scrollTop = 0;
  $('modal-title').textContent = title; $('modal-subtitle').textContent = subtitle;
  $('modal-body').innerHTML = content; $('modal-actions').innerHTML = buttons;
  if (!$('modal').open) $('modal').showModal();
}
function profileIdentity() {
  if (previewRole === 'vendor') return { name: ownerPolicy().name, email: ownerPolicy().email, role: '二级供应商' };
  if (previewRole === 'super_admin') return { name: '超级管理员', email: 'super-admin@example.invalid', role: '超级管理员' };
  return { name: '普通管理员', email: 'admin@example.invalid', role: '普通管理员' };
}
function profileSettingsBody() {
  const identity = profileIdentity();
  if (profileSettingsTab === 'password') return `<div class="account-settings-tabs"><button data-action="profile-settings-tab" data-tab="info">账户信息</button><button class="active" data-action="profile-settings-tab" data-tab="password">修改密码</button></div><div class="settings-form"><label>当前密码<input id="profile-current-password" type="password" autocomplete="current-password"></label><label>新密码<input id="profile-new-password" type="password" autocomplete="new-password"></label><label>确认新密码<input id="profile-confirm-password" type="password" autocomplete="new-password"></label><p class="muted small">密码仅用于本地原型演示，不会保存或发送到任何服务。</p><p id="profile-password-error" class="form-error"></p></div>`;
  return `<div class="account-settings-tabs"><button class="active" data-action="profile-settings-tab" data-tab="info">账户信息</button><button data-action="profile-settings-tab" data-tab="password">修改密码</button></div><div class="profile-card"><span class="avatar large">${esc(identity.name.slice(0, 2))}</span><div><strong>${esc(identity.email)}</strong><span class="profile-role-badge">${esc(identity.role)}</span><small>${esc(identity.name)}</small></div></div><dl class="settings-list"><div><dt>邮箱</dt><dd>${esc(identity.email)}</dd></div><div><dt>用户名</dt><dd>${esc(identity.name)}</dd></div><div><dt>角色</dt><dd>${esc(identity.role)}</dd></div><div><dt>注册时间</dt><dd>2026/10/12 20:22</dd></div><div><dt>登录密码</dt><dd>已设置</dd></div></dl>`;
}
function openProfileSettings(tab = 'info') {
  profileSettingsTab = tab;
  const actions = profileSettingsTab === 'password' ? '<div class="buttons"><button data-action="dismiss">取消</button><button class="primary" data-action="profile-password-save">保存密码</button></div>' : '<div class="buttons"><button data-action="dismiss">关闭</button></div>';
  openModal('账号设置', '查看账户信息并管理登录密码', profileSettingsBody(), actions);
}
function renderProfileCenter() {
  const identity = profileIdentity();
  $('profile-avatar').textContent = identity.name.slice(0, 2);
  $('profile-display-name').textContent = identity.name;
  $('profile-role').textContent = identity.role;
  const menu = $('profile-menu');
  menu.innerHTML = `<div class="profile-menu-head"><span class="avatar large">${esc(identity.name.slice(0, 2))}</span><div><strong>${esc(identity.name)}</strong><small>${esc(identity.email)}</small><span class="profile-role-badge">${esc(identity.role)}</span></div></div><button data-action="profile-settings" role="menuitem">⚙ <span>账号设置</span></button><button class="danger" data-action="profile-logout" role="menuitem">⇧ <span>退出登录</span><kbd>⇧⌘Q</kbd></button>`;
  menu.hidden = true;
  $('profile-trigger').setAttribute('aria-expanded', 'false');
}
function startAccountForm(id = null, editing = false) {
  const a = accounts.find(a => a.id === id);
  accountForm = { target: id, editing, groups: [...(a?.groups || allowedGroups(ownerPolicy().platforms[0]).slice(0, 1))], proxyTab: 'standard', platform: a?.platform || ownerPolicy().platforms[0], name: a?.name || '', notes: a?.notes || '', proxy: a ? a.proxy : proxies[0]?.id || (ownerPolicy().direct ? 0 : ''), method: a?.method || 'oauth', result: '', session: '', url: '', inline: proxies.length === 0 };
  renderAccountForm();
}
function invalidateAuthorization() { accountForm.session = ''; accountForm.url = ''; accountForm.result = ''; accountForm.expires = 0; }

function authSection() {
  const f = accountForm, method = AUTH_METHODS[f.platform].find(m => m.id === f.method);
  const selector = `<div class="segmented" role="radiogroup" aria-label="授权方式">${AUTH_METHODS[f.platform].map(m => `<label><input type="radio" name="auth-method" value="${m.id}" ${m.id === f.method ? 'checked' : ''}><span>${m.label}</span></label>`).join('')}</div>`;
  if (!method.link) {
    const name = f.method === 'refresh_token' ? 'Refresh Token' : 'Setup Token';
    return selector + `<div class="auth-box"><p>${name === 'Refresh Token' ? '使用现有 Refresh Token 核验并接入 OpenAI 账号。' : '使用现有 Claude Setup Token 接入账号；此方式不生成授权链接。'}正式提交后凭证不回显。</p><label>${name}<textarea id="auth-result" data-form="result" aria-label="${name}" rows="3" autocomplete="off" spellcheck="false" placeholder="仅填写 DEMO 演示材料，请勿输入真实 Token">${esc(f.result)}</textarea></label><div class="row" style="margin-top:10px"><button data-action="fill-token">填入演示 Token</button><span class="small muted">单账号导入 · 关闭后清空</span></div></div>`;
  }
  return selector + `<div class="auth-box"><p>生成 ${platformName(f.platform)} 授权链接，在新窗口完成授权后回填结果。</p><label>授权链接</label>${f.url ? `<div class="url-row"><input id="generated-url" aria-label="授权链接" readonly value="${esc(f.url)}"><button data-action="copy-auth-link">复制</button><button data-action="open-auth-demo">打开授权页（演示）</button></div><div class="row" style="margin-top:8px"><button class="text-button small" data-action="generate-url">重新生成授权链接</button><span class="small muted">已绑定当前平台 · 代理变更保留授权 · 有效期 10 分钟</span></div>` : '<button class="generate" data-action="generate-url">生成授权链接</button>'}<label>授权结果<textarea id="auth-result" data-form="result" aria-label="授权结果" rows="3" autocomplete="off" spellcheck="false" placeholder="${f.platform === 'openai' ? '完整回调 URL（包含 code 和 state）或授权码' : '完整回调 URL 或 code#state'}" ${!f.session ? 'disabled' : ''}>${esc(f.result)}</textarea></label><p class="help">演示链接不会访问上游。点击“打开授权页（演示）”可模拟回填；请勿输入真实授权结果。</p></div>`;
}
function renderAccountForm() {
  const f = accountForm, title = f.editing ? '编辑账号资料' : f.target ? '重新登录' : '添加账号';
  const platform = f.editing ? '' : `<div class="form-section"><h3>账号平台</h3><div class="segmented" role="radiogroup" aria-label="账号平台">${ownerPolicy().platforms.map(p => `<label><input type="radio" name="account-platform" value="${p}" ${p === f.platform ? 'checked' : ''} ${f.target ? 'disabled' : ''}><span>${platformName(p)}</span></label>`).join('')}</div></div>`;
  const details = `<div class="form-section fieldgrid"><label>账号名称<span class="required" hidden>*</span><input id="new-name" data-form="name" aria-label="账号名称" value="${esc(f.name)}" required maxlength="100" placeholder="为账号填写易识别的名称（必填）"></label><label>备注<input id="new-notes" data-form="notes" aria-label="备注" value="${esc(f.notes)}" maxlength="500" placeholder="可选，仅记录账号维护信息"></label></div>`;
  const proxy = `<div class="form-section"><h3>专属代理</h3><div class="proxy-controls"><select id="new-proxy" data-form="proxy" aria-label="专属代理"><option value="">请选择我的代理</option>${ownerPolicy().direct ? '<option value="0" ' + (f.proxy === 0 ? 'selected' : '') + '>不使用代理（已授权直连）</option>' : ''}${selectableProxies(1, f.proxy).map(p => `<option value="${p.id}" ${f.proxy === p.id ? 'selected' : ''}>${esc(p.name)} · ${p.protocol} · ${esc(p.host)}:${p.port}</option>`).join('')}</select><button data-action="inline-proxy" aria-expanded="${f.inline}">${f.inline ? '收起新增' : '＋ 新增代理'}</button></div><p class="muted small" style="margin-top:6px">仅显示自己的代理；直连选项由超级管理员策略控制。</p>${f.inline ? `<div class="inline-proxy"><h3>新增我的专属代理</h3>${proxyWidget('inline',f.proxyTab)}${f.proxyTab === 'standard' ? '<div class="row"><button data-action="inline-proxy">取消新增</button><button data-action="save-inline-proxy" class="primary">保存并选用</button></div>' : ''}</div>` : ''}</div>`;
  openModal(title, f.target ? `${platformName(f.platform)} · #${f.target} · 保留账号归属与历史` : '填写账号资料并完成授权，即可加入自己的账号池', platform + details + proxy + groupSelector(f) + (f.editing ? '' : `<div class="form-section"><h3>授权方式</h3>${authSection()}</div>`) + '<div class="default-note">并发、优先级、倍率及其他高级参数由平台分配或使用系统默认。更换代理不会清空已有账号授权。</div><p id="account-form-error" class="form-error" role="alert"></p>', `<span class="muted small">${f.editing ? '仅修改自己的资料和代理' : '本地授权演示 · 未连接业务 API'}</span><div class="buttons"><button data-action="dismiss">取消</button><button class="primary" id="complete-account" data-action="complete-account">${f.editing ? '保存修改' : '完成登录'}</button></div>`);
  updateFormSubmit();
}
function updateFormSubmit() {
  const f = accountForm; if (!f || !$('complete-account')) return;
  const method = AUTH_METHODS[f.platform].find(m => m.id === f.method);
  $('complete-account').disabled = !f.name.trim() || (f.proxy === '' || (f.proxy === 0 && !ownerPolicy().direct)) || f.inline || !f.groups.length || (!f.editing && (!f.result.trim() || (method.link && !f.session)));
}
function generateURL() {
  if (accountForm.proxy === '' || (accountForm.proxy === 0 && !ownerPolicy().direct) || accountForm.inline) { $('account-form-error').textContent = '请先选择自己的代理；新增代理需先保存并选用。'; return; }
  invalidateAuthorization();
  const f = accountForm; f.session = 'DEMO_STATE_' + ++sequence; f.expires = Date.now() + 10 * 60 * 1000;
  f.url = `https://auth.example.invalid/${f.platform}/authorize?method=${encodeURIComponent(f.method)}&state=${f.session}`;
  renderAccountForm();
}
function validateDemoResult() {
  const f = accountForm, method = AUTH_METHODS[f.platform].find(m => m.id === f.method);
  if (!method.link) return f.result.trim() === (f.method === 'refresh_token' ? 'DEMO_REFRESH_TOKEN' : 'DEMO_SETUP_TOKEN');
  if (Date.now() > f.expires) return false;
  const raw = f.result.trim(); let code = raw, state = f.session;
  if (raw.includes('code=')) {
    try { const u = new URL(raw); code = u.searchParams.get('code'); state = u.searchParams.get('state'); } catch { return false; }
  } else if (raw.includes('#')) { [code, state] = raw.split('#'); }
  return code === 'DEMO_CODE' && state === f.session;
}
function completeAccount() {
  const f = accountForm; if ($('complete-account').disabled) return;
  if(!ownerPolicy().platforms.includes(f.platform)||f.groups.some(g=>!allowedGroups(f.platform).includes(g))){$('account-form-error').textContent='平台或分组未开放，请联系超级管理员。';return;}
  if(!f.target&&accounts.length>=ownerPolicy().accountLimit){$('account-form-error').textContent='已达到账号数量上限，请联系超级管理员。';return;}
  if (!f.editing && !validateDemoResult()) { $('account-form-error').textContent = '演示授权结果无效、状态不匹配或已过期。请重新生成链接并模拟授权，或使用“填入演示 Token”。'; return; }
  const a = accounts.find(a => a.id === f.target);
  const exit = proxies.find(p=>p.id===f.proxy);
  if(f.proxy!==0&&(!exit||(!proxyOperational(exit)&&f.proxy!==a?.proxy))){$('account-form-error').textContent='请选择仍有效、已启用的本人代理。';return;}
  if (a) {
    a.name = f.name.trim(); a.notes = f.notes.trim(); a.proxy = f.proxy; a.groups = [...f.groups];
    if (!f.editing) { a.method = f.method; a.credential = 'DEMO_CREDENTIAL_' + a.id + '_' + ++sequence; if (a.health === '凭证异常') a.health = '正常'; }

  } else {
    accounts.unshift({ id: Math.max(...allAccounts().map(a => a.id), 100) + 1, vendorId: ownerPolicy().id, creatorRole: window.vendorHierarchy?.actor.role, creatorId: window.vendorHierarchy?.actor.id, name: f.name.trim(), notes: f.notes.trim(), platform: f.platform, method: f.method, proxy: f.proxy, health: '正常', paused: false, platformDisabled: false, archived: false, groups: [...f.groups], credential: 'DEMO_CREDENTIAL_NEW_' + ++sequence, resets: {five:null,seven:null,fable:null}, concurrency: ownerPolicy().concurrency, priority: ownerPolicy().priority, current: 0, rpm: 0, input: 0, output: 0, cache: 0, quota: { five: null, seven: null, fable: null }, created: DEMO_TODAY });
  }
  const name = f.name.trim(), editing = f.editing; $('modal').close(); page = previewRole === 'super_admin' ? 'admin' : 'accounts'; if(!editing){filters = defaultFilters(); paging.accounts.page = 1;} render();
  notify(`${name}：${editing ? '资料已更新' : '演示授权完成'}。未执行真实授权或业务请求。`);
}
function resetTime(value) { return value ? new Intl.DateTimeFormat('zh-CN', {timeZone:'Asia/Shanghai',month:'2-digit',day:'2-digit',hour:'2-digit',minute:'2-digit',hour12:false}).format(new Date(value)) : '重置时间未知'; }
function quotaCell(a) { return [...(a.platform === 'anthropic' ? ['fable'] : []), 'seven','five'].map(key => { const value=a.quota[key],reset=a.resets?.[key]; return `<div class="quota-line" title="${esc(reset || '上游未提供重置时间')} · Asia/Shanghai"><span>${{five:'5 小时',seven:'7 天',fable:'Fable'}[key]}</span><span class="quota-track"><span class="${value>=100?'full':value>=70?'hot':''}" style="width:${value??0}%"></span></span><span class="quota-percent ${value>=100?'form-error':''}">${value===null?'未知':value+'%'}</span><time>${resetTime(reset)}</time></div>`; }).join(''); }
function groupSelector(f) { return `<div class="form-section"><h3>分组 <span class="muted small">可多选</span></h3><details class="group-select"><summary aria-label="选择账号分组"><span id="selected-groups">${f.groups.length?f.groups.map(esc).join('、'):'请选择至少一个分组'}</span><span>⌄</span></summary><div class="group-options">${allowedGroups(f.platform).map(g=>`<label><input type="checkbox" name="account-group" value="${esc(g)}" ${f.groups.includes(g)?'checked':''}>${g}</label>`).join('')}</div></details><p class="muted small">仅可选择超级管理员开放的 ${platformName(f.platform)} 分组，已预选默认分组。</p></div>`; }
// Shared prototype controls use the same SVG paths as the project Icon component.
const WORKBENCH_ICONS = {"infoCircle":"M11.25 11.25l.041-.02a.75.75 0 011.063.852l-.708 2.836a.75.75 0 001.063.853l.041-.021M21 12a9 9 0 11-18 0 9 9 0 0118 0zm-9-3.75h.008v.008H12V8.25z","chart":"M3 13.125C3 12.504 3.504 12 4.125 12h2.25c.621 0 1.125.504 1.125 1.125v6.75C7.5 20.496 6.996 21 6.375 21h-2.25A1.125 1.125 0 013 19.875v-6.75zM9.75 8.625c0-.621.504-1.125 1.125-1.125h2.25c.621 0 1.125.504 1.125 1.125v11.25c0 .621-.504 1.125-1.125 1.125h-2.25a1.125 1.125 0 01-1.125-1.125V8.625zM16.5 4.125c0-.621.504-1.125 1.125-1.125h2.25C20.496 3 21 3.504 21 4.125v15.75c0 .621-.504 1.125-1.125 1.125h-2.25a1.125 1.125 0 01-1.125-1.125V4.125z","edit":"M16.862 4.487l1.687-1.688a1.875 1.875 0 112.652 2.652L10.582 16.07a4.5 4.5 0 01-1.897 1.13L6 18l.8-2.685a4.5 4.5 0 011.13-1.897l8.932-8.931zm0 0L19.5 7.125M18 14v4.75A2.25 2.25 0 0115.75 21H5.25A2.25 2.25 0 013 18.75V8.25A2.25 2.25 0 015.25 6H10","refresh":"M16.023 9.348h4.992v-.001M2.985 19.644v-4.992m0 0h4.992m-4.993 0l3.181 3.183a8.25 8.25 0 0013.803-3.7M4.031 9.865a8.25 8.25 0 0113.803-3.7l3.181 3.182m0-4.991v4.99","shield":"M9 12.75L11.25 15 15 9.75m-3-7.036A11.959 11.959 0 013.598 6 11.99 11.99 0 003 9.749c0 5.592 3.824 10.29 9 11.623 5.176-1.332 9-6.03 9-11.622 0-1.31-.21-2.571-.598-3.751h-.152c-3.196 0-6.1-1.248-8.25-3.285z","trash":"M14.74 9l-.346 9m-4.788 0L9.26 9m9.968-3.21c.342.052.682.107 1.022.166m-1.022-.165L18.16 19.673a2.25 2.25 0 01-2.244 2.077H8.084a2.25 2.25 0 01-2.244-2.077L4.772 5.79m14.456 0a48.108 48.108 0 00-3.478-.397m-12 .562c.34-.059.68-.114 1.022-.165m0 0a48.11 48.11 0 013.478-.397m7.5 0v-.916c0-1.18-.91-2.164-2.09-2.201a51.964 51.964 0 00-3.32 0c-1.18.037-2.09 1.022-2.09 2.201v.916m7.5 0a48.667 48.667 0 00-7.5 0","copy":"M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z","plus":"M12 4.5v15m7.5-7.5h-15","checkCircle":"M9 12.75L11.25 15 15 9.75M21 12a9 9 0 11-18 0 9 9 0 0118 0z"};
function workbenchIcon(name, spinning=false) {
  return `<svg class="workbench-icon${spinning?' spinning':''}" aria-hidden="true" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round">${name==='more'?'<circle cx="5" cy="12" r="1"/><circle cx="12" cy="12" r="1"/><circle cx="19" cy="12" r="1"/>':`<path d="${WORKBENCH_ICONS[name]}"/>`}</svg>`;
}
function prototypeText(source, values={}) {
  const message=window.vendorPrototypeText?.(source)||source;
  return message.replace(/\{(\w+)\}/g,(_,key)=>String(values[key]??'{'+key+'}'));
}
function accountActions(a) {
 const actions=[['detail','infoCircle','账号详情'],['usage-detail','chart','用量详情']];
 if(accountAccess(a).manage)actions.push(['edit','edit','编辑账号'],['account-menu','more','更多操作']);
 return `<div class="actions">${actions.map(([action,icon,label])=>`<button data-action="${action}" data-id="${a.id}" title="${prototypeText(label)}" aria-label="${esc(a.name)} · ${prototypeText(label)}">${workbenchIcon(icon)}</button>`).join('')}</div>`;
}
function openAccountMenu(id,button) { const a=accounts.find(a=>a.id===id),menu=$('account-menu'); menu.innerHTML=`<p class="muted small">${esc(a.name)}</p>${[['reauth','重新登录'],['probe','探测实例'],['sync-account','同步账号信息'],['export-account','导出账号授权'],['toggle',a.paused?'恢复供给':'暂停供给'],['delete-account','删除账号']].map(([action,label])=>`<button role="menuitem" class="${action==='delete-account'?'danger':''}" data-action="${action}" data-id="${id}">${label}</button>`).join('')}`; const r=button.getBoundingClientRect();menu.style.left=Math.max(8,Math.min(r.right-210,innerWidth-220))+'px';menu.style.top=Math.max(8,Math.min(r.bottom+6,innerHeight-300))+'px';menu.showPopover(); }
function runtimeStats(rows) { const s=total(rows),failures=rows.reduce((n,r)=>n+r.failed,0),success=s.requests-failures;return {...s,success,failures,rate:s.requests?(success/s.requests*100).toFixed(2)+'%':'—',latency:success?(rows.reduce((n,r)=>n+r.latencyMs*(r.requests-r.failed),0)/success/1000).toFixed(1)+' s':'—',ttft:success?(rows.reduce((n,r)=>n+r.ttftMs*(r.requests-r.failed),0)/success/1000).toFixed(1)+' s':'—'}; }
function detailRows(rows) { return `<dl class="detail-list">${rows.map(([k,v])=>`<div><dt>${k}</dt><dd>${v}</dd></div>`).join('')}</dl>`; }
function blockingReasons(a) { return [vendorScopeBlock(a),a.archived?'账号已归档':'',a.platformDisabled?'平台禁用':'',a.paused?'供应商暂停':'',a.health!=='正常'?a.health:''].filter(Boolean); }
function showDetail(id) {
 if(!accountAccess(findAccount(id)).read)return;
 const a=findAccount(id),p=allProxies().find(p=>p.id===a.proxy),rows=allLedger().filter(r=>r.account===id),s=runtimeStats(rows),today=runtimeStats(rows.filter(r=>r.date===DEMO_TODAY)),blocked=blockingReasons(a);
 const proxy=p?`<button class="detail-copy" data-action="account-copy-proxy" data-id="${id}" title="${prototypeText('复制代理地址（不含认证）')}" aria-label="${prototypeText('复制代理地址（不含认证）')}"><code>${esc(p.host)}:${p.port}</code>${workbenchIcon('copy')}</button><span id="detail-copy-feedback" class="small green" role="status" hidden></span><small>${p.protocol} · ${esc(prototypeText(regionLabel(p.country))+(p.city?' · '+prototypeText(p.city):''))} · ${esc(p.name)}</small>`:a.proxy===0?esc(proxyName(0)):'无绑定代理';
 openModal(`${a.name} · ${prototypeText('账号详情')}`,a.archived?'账号已归档，保留账号资料、关联与全部历史，停止供给。':'调度状态在打开时获取，不自动刷新。当前为虚构演示快照。',
 `<section class="detail-section"><h3>基本信息</h3>${detailRows([['账号 ID / 平台',`#${id} · ${platformName(a.platform)}`],['代理',proxy],['所属供应商',esc(accountVendor(a).name)+' · ID:'+accountVendor(a).id],['创建来源',a.creatorRole?esc(prototypeText(({super_admin:'超级管理员',vendor_level1:'一级供应商',vendor_level2:'二级供应商'})[a.creatorRole]||'未知'))+' · ID:'+a.creatorId:'未记录创建来源'],['授权方式',methodName(a)],['账号分组',a.groups.map(g=>`<span class="badge">${esc(g)}</span>`).join(' ')],['权重',prototypeText('未知')],['并发 / 优先级',`${a.concurrency} / ${a.priority}`],['总请求数 / Token',`${number(s.requests)} / ${number(s.input+s.output+s.write+s.read)} (${prototypeText('含缓存')})`],['累计统计消耗',money(s.cost)],['创建时间',`${a.created} 12:00:00 · Asia/Shanghai`],['归档时间',a.archived?(a.archivedAt||DEMO_NOW):'未归档'],['备注',esc(a.notes||'—')]])}</section>
 <section class="detail-section"><h3>运行指标</h3>${detailRows([['最近 60 秒速率',`${status(a)==='正常'?a.rpm:0} RPM · ${number(status(a)==='正常'?a.input+a.output+a.cache:0)} TPM<small>${prototypeText('不含缓存 {count} TPM',{count:number(status(a)==='正常'?a.input+a.output:0)})}</small>`],['今日成功率',`${today.rate} <span class="muted">${number(today.success)} / ${number(today.requests)}</span>`],['累计成功率',`${s.rate} <span class="muted">${number(s.success)} / ${number(s.requests)}</span>`]])}</section>
 <section class="detail-section"><h3>当前状态</h3><div class="row">${badge(status(a))}<strong>${blocked.length?'当前不可调度':'可调度'}</strong></div><p class="muted">${blocked.length?blocked.map(esc).join(' · '):'账号、供应商及出口条件均满足演示调度资格。'}</p></section>
 <section class="detail-section"><h3>账号级冷却</h3><p>${a.health==='配额冷却'?prototypeText('5 小时配额已满，预计 {time} 恢复（Asia/Shanghai）。',{time:resetTime(a.resets.five)}):'当前没有冷却'}</p></section>
 <section class="detail-section detail-quota"><h3>${prototypeText('{platform} 配额',{platform:platformName(a.platform)})}</h3>${quotaCell(a)}<p class="muted small">重置时间来自上游快照；未知不代表不重置。时间统一为 Asia/Shanghai。</p></section>
 <section class="detail-section"><h3>模型级限制</h3><p>${a.health==='配额冷却'?'当前 5 小时窗口对应模型暂停接收新请求；重登录不会清除限制。':'无已知模型级限制'}</p></section>`,
 `<span class="muted small">账号详情不包含授权凭证</span><div class="buttons"><button data-action="usage-detail" data-id="${id}">用量详情</button>${accountAccess(a).manage?`<button data-action="scoped-edit" data-id="${id}">编辑账号</button>`:''}<button data-action="dismiss">关闭</button></div>`);
 $('modal').classList.add('detail-drawer');
}
function confirmDelete(id) { const a=findAccount(id);if(!accountAccess(a).manage)return;openModal('删除账号（仅归档）',`#${id} · ${a.name}`,`<div class="notice">此操作仅归档账号并停止供给，不会从数据库或账号池列表中删除。</div><p>账号将显示“已归档”，保留账号资料、授权、分组、代理关联、用量、账单和审计历史。归档后只可查看详情与用量。</p><label class="checkline"><input type="checkbox" id="confirm-delete-check">我确认归档此账号并停止供给</label>`,`<div class="buttons"><button data-action="dismiss">取消</button><button class="danger" id="confirm-delete" data-action="confirm-delete" data-id="${id}" disabled>确认归档</button></div>`); }
function runAccountOperation(id,kind) { const a=accounts.find(a=>a.id===id),probe=kind==='probe',block=vendorScopeBlock(a),failed=!!block||['凭证异常','代理异常'].includes(a.health);openModal(`${a.name} · ${probe?'探测实例':'同步账号信息'}`,'本地模拟结果 · 未向上游发送请求',`<div class="notice">${probe?'验证当前账号授权与代理的可用性；正式操作使用允许的固定探测目标。':'同步账号资料、订阅和配额快照；不会改写分组、授权、暂停或平台禁用状态。'}</div><div class="detail-section">${detailRows([['执行结果',badge(block?'未执行：'+block:failed?a.health:'正常')],['出口代理',esc(proxyName(a.proxy))],['授权材料','保持原授权'],['供给状态',badge(status(a))],['结果说明',failed?'演示检测未通过，请处理异常后重试；原账号授权仍保留。':probe?'演示实例可达，响应成功，延迟 218 ms。':'演示资料与配额快照已同步；未改变账号权限与调度配置。']])}</div>${probe?'':`<div class="detail-quota">${quotaCell(a)}</div>`}`,`<div class="buttons"><button data-action="detail" data-id="${id}">查看账号详情</button><button data-action="dismiss">完成</button></div>`); }

let usageDetail=null;
function showUsageDetail(id){if(!accountAccess(findAccount(id)).read)return;usageDetail={id,start:previewRole==='super_admin'?adminFilters.start:'',end:previewRole==='super_admin'?adminFilters.end:'',metric:'requests',page:1};renderUsageDetail();}
function trendSVG(rows,key){
 if(!rows.length)return '<div class="empty">此范围没有用量记录</div>';
 const val=r=>key==='tokens'?r.input+r.output+r.write+r.read:key==='cost'?r.cost/10000:r.requests;
 const top=Math.max(...rows.map(val),1)*1.12,x=i=>58+i*866/Math.max(1,rows.length-1),y=r=>205-val(r)/top*175,labels=[...new Set([0,Math.floor((rows.length-1)/2),rows.length-1])];
 return `<svg class="trend-chart" viewBox="0 0 960 250" role="img" aria-label="${{requests:'请求',tokens:'Token',cost:'消耗金额'}[key]}趋势图">${[0,1,2,3,4].map(i=>`<line x1="58" x2="924" y1="${205-i*44}" y2="${205-i*44}" stroke="#e5ebf0" stroke-dasharray="4 4"/><text x="48" y="${209-i*44}" text-anchor="end">${new Intl.NumberFormat('en-US',{notation:'compact',maximumFractionDigits:1}).format(top*i/4)}</text>`).join('')}<polyline points="${rows.map((r,i)=>`${x(i)},${y(r)}`).join(' ')}" fill="none" stroke="#087f8c" stroke-width="2.4"/>${rows.map((r,i)=>`<circle cx="${x(i)}" cy="${y(r)}" r="3" fill="#fff" stroke="#087f8c" stroke-width="2"><title>${r.id}: ${key==='cost'?money(r.cost):number(val(r))}</title></circle>`).join('')}${labels.map(i=>`<text x="${x(i)}" y="235" text-anchor="middle">${rows[i].id.slice(5)}</text>`).join('')}</svg>`;
}
function renderUsageDetail(){
 const d=usageDetail,a=findAccount(d.id),all=allLedger().filter(r=>r.account===d.id),rows=all.filter(r=>(!d.start||r.date>=d.start)&&(!d.end||r.date<=d.end)),s=runtimeStats(rows),daily=aggregate(rows,'date').sort((a,b)=>a.id.localeCompare(b.id)),models=aggregate(rows,'model').sort((a,b)=>b.cost-a.cost);
 const pageCount=Math.max(1,Math.ceil(daily.length/7));d.page=Math.min(d.page,pageCount);
 const windows=[['最近 5 小时',5],['最近 7 天',168],['最近 30 天',720]].map(([label,hours])=>({label,...total(all.filter(r=>new Date(r.at)>new Date(DEMO_NOW)-hours*3600000&&new Date(r.at)<=new Date(DEMO_NOW)))}));
 openModal(`${a.name} · 用量详情`,'按请求发生时间统计，默认显示全部历史。金额为账号统计消耗，非分成或结算金额。',
 `<div class="detail-datebar">${a.archived?'<span class="badge">已归档账号 · 历史记录</span>':''}<span class="badge">${d.start?`${d.start} — ${d.end}`:'全部历史'}</span><label>开始日期<input id="detail-start" type="date" value="${d.start}" max="${DEMO_TODAY}"></label><label>结束日期<input id="detail-end" type="date" value="${d.end}" max="${DEMO_TODAY}"></label><button data-action="detail-date-apply">应用</button><button data-action="detail-date-all">全部历史</button><button data-action="detail-date-week">近 7 天</button></div><p id="detail-date-error" class="form-error" role="alert"></p>
 <div class="detail-kpis">${metric('总请求数',number(s.requests),`成功 ${number(s.success)} / 失败 ${number(s.failures)}`)}${metric('金额消耗',money(s.cost),'USD · 账号统计消耗')}${metric('成功率',s.rate,s.requests?'所选范围请求成功比例':'暂无请求','green')}${metric('Token 消耗',number(s.input+s.output),`入 ${number(s.input)} / 出 ${number(s.output)}`)}${metric('缓存命中率',hitRate(s),`写 ${number(s.write)} / 读 ${number(s.read)}`)}${metric('平均延迟',s.latency,'成功请求加权均值')}${metric('首字时间',s.ttft,'成功流式请求加权均值')}</div>
 <section class="panel"><div class="panel-head"><h3>历史额度消耗 <span class="muted small">固定窗口快照，不受日期筛选影响</span></h3></div><div class="table-wrap"><table aria-label="历史额度消耗"><thead><tr><th>窗口</th><th class="number">请求</th><th class="number">Token（${prototypeText('含缓存')}）</th><th class="number">统计消耗（USD）</th></tr></thead><tbody>${windows.map(r=>`<tr><td>${r.label}</td><td class="number">${number(r.requests)}</td><td class="number">${number(r.input+r.output+r.write+r.read)}</td><td class="number">${money(r.cost)}</td></tr>`).join('')}</tbody></table></div><p class="panel-footnote">快照：${DEMO_NOW.replace('T',' ')} · 消耗记录不等于上游配额剩余量。</p></section>
 <section class="panel"><div class="panel-head"><h3>趋势图表</h3><div class="chart-tabs">${[['requests','请求趋势'],['tokens','Token 消耗'],['cost','金额消耗']].map(([k,label])=>`<button class="${d.metric===k?'active':''}" data-action="detail-chart" data-metric="${k}" aria-pressed="${d.metric===k}">${label}</button>`).join('')}</div></div>${trendSVG(daily,d.metric)}</section>
 <div class="distribution-grid"><section class="panel"><div class="panel-head"><h3>模型分布</h3><span class="muted small">演示模型分类</span></div><div class="panel-body">${models.map(m=>`<div class="model-item"><div class="row spread"><strong>${esc(m.id)}</strong><span>${money(m.cost)}</span></div><div class="distribution-track"><i style="width:${s.requests?m.requests/s.requests*100:0}%"></i></div><span class="muted small">${number(m.requests)} 次请求 · ${number(m.input+m.output+m.write+m.read)} Token（${prototypeText('含缓存')}）</span></div>`).join('')||'<div class="empty">暂无模型用量</div>'}</div></section><section class="panel"><div class="panel-head"><h3>请求结果分布</h3></div><div class="panel-body">${detailRows([['成功请求',number(s.success)],['失败请求',number(s.failures)],['覆盖天数',daily.length],['统计时区','Asia/Shanghai']])}<p class="muted small">仅展示当前账号聚合；不提供下游 API Key、用户身份或请求内容。</p></div></section></div>
 <section class="panel"><div class="panel-head"><h3>按日用量明细</h3><span class="muted small">日期同步作用于汇总、趋势与模型分布</span></div><div class="table-wrap"><table aria-label="账号按日用量"><thead><tr><th>日期</th><th class="number">请求数</th><th class="number">输入 / 输出</th><th class="number">缓存写入 / 读取</th><th class="number">统计消耗</th></tr></thead><tbody>${daily.slice((d.page-1)*7,d.page*7).map(r=>`<tr><td>${r.id}</td><td class="number">${number(r.requests)}</td><td class="number">${usagePair(r)}</td><td class="number">${usagePair(r,true)}</td><td class="number">${money(r.cost)}</td></tr>`).join('')||'<tr><td colspan="5" class="empty">此范围没有用量记录</td></tr>'}</tbody></table></div><div class="pagination"><span>共 ${daily.length} 条 · 每页 7 条</span><div class="row"><button data-action="detail-page" data-delta="-1" ${d.page===1?'disabled':''}>上一页</button><span>${d.page} / ${pageCount}</span><button data-action="detail-page" data-delta="1" ${d.page===pageCount?'disabled':''}>下一页</button></div></div></section>`,
 `<span class="muted small">虚构数据 · 未连接业务统计</span><div class="buttons"><button data-action="detail" data-id="${a.id}">账号详情</button><button data-action="dismiss">关闭</button></div>`);$('modal').classList.add('wide');
}

let transfer=null;
function exportRecord(a){const p=proxies.find(p=>p.id===a.proxy);return {subject:a.subject||'DEMO_SUBJECT_'+a.id,name:a.name,notes:a.notes,platform:a.platform,method:a.method,groups:[...a.groups],proxy:p?{protocol:p.protocol,host:p.host,port:p.port}:null,credentials:{token:a.credential}};}
function openExport(id){transfer={mode:'export',ids:accounts.filter(a=>!a.archived&&(!id||a.id===id)).map(a=>a.id)};openModal('导出账号授权','仅导出自己选择的账号；当前文件只含 DEMO 虚构材料。',`<div class="notice">正式导出文件包含敏感授权，应妥善保存；不包含代理密码、历史用量及平台内部配置。正式流程复用项目敏感导出的二次验证。</div><div class="export-choices"><label class="checkline"><input type="checkbox" id="export-all" ${transfer.ids.length===accounts.filter(a=>!a.archived).length?'checked':''}>全选我的账号</label>${accounts.filter(a=>!a.archived).map(a=>`<label class="checkline"><input type="checkbox" name="export-id" value="${a.id}" ${transfer.ids.includes(a.id)?'checked':''}>${esc(a.name)} <span class="muted">${platformName(a.platform)}</span></label>`).join('')}</div><label class="checkline"><input type="checkbox" id="export-confirm">我了解授权文件需要妥善保管</label><div id="export-result"></div>`,`<span class="muted small" id="export-count">已选 ${transfer.ids.length} 个账号</span><div class="buttons"><button data-action="dismiss">取消</button><button class="primary" id="prepare-export" data-action="prepare-export" disabled>生成授权文件</button></div>`);}
function updateExport(){transfer.ids=[...document.querySelectorAll('[name=export-id]:checked')].map(e=>Number(e.value));$('export-count').textContent=`已选 ${transfer.ids.length} 个账号`;$('prepare-export').disabled=!transfer.ids.length||!$('export-confirm').checked;$('export-result').innerHTML='';transfer.payload=null;}
function prepareExport(){transfer.payload={type:'sub2api-vendor-demo',version:1,exported_at:DEMO_NOW,accounts:accounts.filter(a=>!a.archived&&transfer.ids.includes(a.id)).map(exportRecord)};$('export-result').innerHTML=`<label>授权 JSON（仅 DEMO）<textarea id="export-json" rows="9" readonly spellcheck="false">${esc(JSON.stringify(transfer.payload,null,2))}</textarea></label><button class="primary" data-action="download-accounts">下载 JSON 文件</button>`;$('export-result').scrollIntoView({block:'nearest'});}
function openImport(){transfer={mode:'import',rows:[],raw:''};openModal('导入账号授权','导入自己的 Claude / OpenAI 授权。先预览，再确认。',`<div class="notice">原型只接受 type=sub2api-vendor-demo 的演示文件，不会接收或验证真实 Token。生产版本按现有账号数据格式复用解析器。</div><label>选择 JSON 文件<input type="file" id="account-import-file" accept=".json,application/json"></label><label>或粘贴授权 JSON<textarea id="account-import-json" rows="9" spellcheck="false" autocomplete="off" placeholder="粘贴从本原型导出的演示 JSON"></textarea></label><div class="row"><button data-action="fill-import-sample">填入演示示例</button><label>代理无法匹配时选用<select id="import-fallback"><option value="">请选择回退出口</option>${ownerPolicy().direct?'<option value="0">直连（策略允许，需明确选择）</option>':''}${proxies.filter(proxyOperational).map(p=>`<option value="${p.id}">${esc(p.name)}</option>`).join('')}</select></label></div><label class="checkline"><input type="checkbox" id="import-update">更新已存在的同一自有账号授权（保留 ID、历史和供给状态）</label><p id="import-error" class="form-error" role="alert"></p><div id="import-preview"></div>`,`<span class="muted small">新导入账号默认暂停，确认后自行启用</span><div class="buttons"><button data-action="dismiss">取消</button><button data-action="preview-import">解析预览</button><button class="primary" id="commit-import" data-action="commit-import" disabled>确认导入</button></div>`);}
function invalidateImport(){if(transfer?.mode!=='import')return;transfer.rows=[];$('import-error').textContent='';$('commit-import').textContent='确认导入';$('import-preview').innerHTML='';$('commit-import').disabled=true;}
function parseAccountImport(){
 const raw=$('account-import-json').value;transfer.rows=[];$('import-error').textContent='';$('commit-import').disabled=true;$('import-preview').innerHTML='';
 try{if(raw.length>1024*1024)throw Error('文件不能超过 1 MB');const data=JSON.parse(raw);if(data.type!=='sub2api-vendor-demo'||data.version!==1||!Array.isArray(data.accounts)||!data.accounts.length||data.accounts.length>100)throw Error('需要有效的演示格式、版本 1，且包含 1–100 个账号');if(data.vendor_id!==undefined||data.owner!==undefined)throw Error('文件不能指定供应商归属');
 const seen=new Set();transfer.rows=data.accounts.map((item,index)=>{let reason='',target=null,proxy=null;
 if(!item||typeof item!=='object'||!GROUPS[item.platform]||!ownerPolicy().platforms.includes(item.platform)||!AUTH_METHODS[item.platform]?.some(m=>m.id===item.method))reason='平台或授权方式不受支持';
 else if(typeof item.name!=='string'||!item.name.trim()||item.name.length>100||typeof item.subject!=='string'||!/^DEMO_SUBJECT_[A-Za-z0-9_-]{1,80}$/.test(item.subject))reason='缺少合法名称或演示身份';
 else if(typeof item.notes!=='string'||item.notes.length>500)reason='备注必须为 500 字以内文本';
 else if(!Array.isArray(item.groups)||!item.groups.length||item.groups.some(g=>!allowedGroups(item.platform).includes(g)))reason='分组不在当前平台授权范围';
 else if(typeof item.credentials?.token!=='string'||!/^DEMO_[A-Za-z0-9_-]{1,200}$/.test(item.credentials.token))reason='只接受 DEMO 演示授权';
 else if(['id','vendor_id','owner','priority','concurrency','rate_multiplier'].some(k=>Object.hasOwn(item,k)))reason='包含不可导入的归属或系统参数';
 else if(seen.has(item.subject))reason='文件内身份重复';
 else{seen.add(item.subject);target=accounts.find(a=>(a.subject||'DEMO_SUBJECT_'+a.id)===item.subject);if(target?.archived)reason='已归档账号不能被导入覆盖';else if(target&&target.platform!==item.platform)reason='身份平台冲突';else if(target&&!$('import-update').checked)reason='已存在，默认跳过';const fallback=$('import-fallback').value;proxy=proxies.find(p=>proxyOperational(p)&&p.host===item.proxy?.host&&p.port===item.proxy?.port&&p.protocol===item.proxy?.protocol)||proxies.find(p=>proxyOperational(p)&&p.id===Number(fallback))||(fallback==='0'&&ownerPolicy().direct?{id:0}:null);if(!reason&&!proxy)reason='代理未匹配，请明确选择自己的代理或已授权直连';}
 return {index:index+1,item,target,proxy,reason,ok:!reason};});
 const count=transfer.rows.filter(r=>r.ok).length;$('import-preview').innerHTML=`<div class="notice">可导入 ${count} 条 · 跳过 / 无效 ${transfer.rows.length-count} 条</div><div class="table-wrap"><table><thead><tr><th>行</th><th>账号</th><th>操作</th><th>导入出口</th><th>结果</th></tr></thead><tbody>${transfer.rows.map(r=>`<tr><td>${r.index}</td><td>${esc(typeof r.item?.name==='string'?r.item.name:'无效条目')}</td><td>${r.target?'更新原账号':'新增账号'}</td><td>${r.proxy?esc(proxyName(r.proxy.id)):'待选择'}</td><td>${esc(r.reason||'校验通过')}</td></tr>`).join('')}</tbody></table></div>`;$('commit-import').disabled=!count;$('commit-import').textContent=`确认导入 ${count} 个账号`;
 }catch(e){$('import-error').textContent=e instanceof SyntaxError?'JSON 格式无效，请检查括号、逗号和引号。':e instanceof TypeError?'需要包含账号列表的有效 JSON 对象。':'解析失败：'+e.message;}
}
function commitAccountImport(){parseAccountImport();const rows=transfer.rows.filter(r=>r.ok);if(!rows.length)return;if(accounts.length+rows.filter(r=>!r.target).length>ownerPolicy().accountLimit){$('import-error').textContent='导入后会超过账号数量上限，请减少条目或联系超级管理员';return;}for(const r of rows){const v=r.item;if(r.target){Object.assign(r.target,{name:v.name.trim(),notes:v.notes,groups:[...new Set(v.groups)],proxy:r.proxy.id,method:v.method,credential:v.credentials.token});if(r.target.health==='凭证异常')r.target.health='正常';}else{accounts.unshift({id:Math.max(...allAccounts().map(a=>a.id),100)+1,vendorId:ownerPolicy().id,creatorRole:window.vendorHierarchy?.actor.role,creatorId:window.vendorHierarchy?.actor.id,subject:v.subject,name:v.name.trim(),notes:v.notes,platform:v.platform,method:v.method,groups:[...new Set(v.groups)],proxy:r.proxy.id,credential:v.credentials.token,health:'正常',paused:true,platformDisabled:false,archived:false,concurrency:ownerPolicy().concurrency,priority:ownerPolicy().priority,current:0,rpm:0,input:0,output:0,cache:0,quota:{five:null,seven:null,fable:null},resets:{five:null,seven:null,fable:null},created:DEMO_TODAY});}}const count=rows.length;$('modal').close();page='accounts';filters=defaultFilters();render();notify(`已导入 ${count} 个演示账号；重复和无效行未写入，原有账号状态保留。`);}


function render() {
  if(portalMode){previewRole='vendor';if(!portalLoggedIn){renderPortalLogin();return;}}
  document.querySelector('.sidebar').hidden=false;document.querySelector('.topbar').hidden=false;document.querySelector('.app').classList.remove('portal-layout');document.querySelector('.review-nav').hidden=portalMode;$('reset').hidden=portalMode;$('portal-logout').hidden=!portalMode;
  calendar = null;
  document.querySelectorAll('[data-vendor-nav]').forEach(e=>e.hidden=previewRole!=='vendor');
  $('admin-nav').hidden=previewRole!=='super_admin';
  $('profile-scope').textContent=previewRole==='vendor'?'仅我的资源':previewRole==='super_admin'?'供应商管理权限':'无供应商管理权限';
  $('profile-name').textContent=previewRole==='vendor'?ownerPolicy().name:previewRole==='super_admin'?'超级管理员':'普通管理员';
  renderProfileCenter();
  if(previewRole==='vendor'&&ownerPolicy().status!=='active'){$('crumb').textContent='供应商工作区 / 身份已停用';$('view').innerHTML=heading('供应商身份已停用','请联系超级管理员')+'<div class="notice">供给管理和新请求已停止。账号授权、代理与历史用量仍保留。</div>';return;}
  document.querySelectorAll('[data-page]').forEach(b => { if (b.dataset.page === page) b.setAttribute('aria-current', 'page'); else b.removeAttribute('aria-current'); });
  $('crumb').textContent = (page === 'admin' ? '后台工作区 / ' : '供应商工作区 / ') + ({ accounts: '账号池', proxies: '代理管理', usage: '用量统计', settlements:'我的结算', admin: '供应商平台' }[page]);
  ({ accounts: renderAccounts, proxies: renderProxies, usage: renderUsage, settlements:renderVendorSettlements, admin: renderAdmin })[page]();
}
document.addEventListener('input', event => {
  const t = event.target;
  if(t.id==='admin-search'){const position=t.selectionStart;adminQuery=t.value;adminPages.vendors=1;renderAdmin();$('admin-search').focus();$('admin-search').setSelectionRange(position,position);}
  if(t.dataset.quick)previewQuick(t.dataset.quick);
  if(t.id==='account-import-json')invalidateImport();
  if (t.id === 'account-search') { filters.q = t.value; paging.accounts.page = 1; renderAccountRows(); }
  if (t.dataset.form && accountForm && t.dataset.form !== 'proxy') { accountForm[t.dataset.form] = t.value; updateFormSubmit(); }
});
document.addEventListener('change', event => {
  const t = event.target;
  if(t.id==='preview-role'){previewRole=t.value;page=previewRole==='vendor'?'accounts':'admin';render();}
  if(t.id==='admin-status'){adminStatus=t.value;adminPages.vendors=1;renderAdmin();}

  if(t.name==='account-group'&&accountForm){accountForm.groups=[...document.querySelectorAll('[name=account-group]:checked')].map(e=>e.value);$('selected-groups').textContent=accountForm.groups.join('、')||'请选择至少一个分组';updateFormSubmit();}
  if(t.id==='confirm-delete-check')$('confirm-delete').disabled=!t.checked;
  if(t.id==='export-all'){document.querySelectorAll('[name=export-id]').forEach(e=>e.checked=t.checked);updateExport();}
  if(t.name==='export-id'||t.id==='export-confirm')updateExport();
  if(t.id==='import-fallback'||t.id==='import-update')invalidateImport();
  if(t.id==='account-import-file'){const file=t.files?.[0];if(file){if(file.size>1024*1024){$('import-error').textContent='文件不能超过 1 MB';return;}const current=transfer;file.text().then(text=>{if(transfer!==current||!$('account-import-json'))return;$('account-import-json').value=text;invalidateImport();parseAccountImport();}).catch(()=>{if($('import-error'))$('import-error').textContent='文件读取失败，请重试或粘贴 JSON';});}}

  if (t.dataset.filter) { filters[t.dataset.filter] = t.value; paging.accounts.page = 1; renderAccountRows(); }
  if (t.dataset.accountColumn) { accountColumnVisibility[t.dataset.accountColumn] = t.checked; renderAccounts(); }
  if (t.dataset.size) { paging[t.dataset.size].size = Number(t.value); paging[t.dataset.size].page = 1; page === 'accounts' ? renderAccountRows() : renderUsageData(); }
  if (t.id === 'usage-account' || t.id === 'usage-platform') { usage[t.id === 'usage-account' ? 'account' : 'platform'] = t.value; paging.daily.page = paging.usageAccounts.page = 1; renderUsageData(); }
  if (accountForm && ['account-platform', 'auth-method'].includes(t.name)) {
    if (t.name === 'account-platform') { accountForm.platform = t.value; accountForm.method = 'oauth'; accountForm.groups = allowedGroups(t.value).slice(0, 1); } else accountForm.method = t.value;
    invalidateAuthorization(); renderAccountForm();
  }
  if (t.id === 'new-proxy' && accountForm) { accountForm.proxy = t.value === '' ? '' : Number(t.value); renderAccountForm(); }
});
document.addEventListener('click', async event => {
  if (!event.target.closest('.profile-wrap')) { $('profile-menu').hidden = true; $('profile-trigger').setAttribute('aria-expanded', 'false'); }
  const b = event.target.closest('button');
  if (calendar && !event.target.closest('.date-wrapper')) { calendar = null; renderCalendar(); }
  if (!b || b.disabled) return;
  if (b.dataset.page && !b.dataset.action) { page = b.dataset.page; render(); return; }
  const action = b.dataset.action, id = Number(b.dataset.id);
  if(action==='admin-tab'&&requireSuperAdmin()){adminView=b.dataset.tab;adminError='';renderAdmin();}
  if(action==='profile-toggle'){const menu=$('profile-menu');const open=menu.hidden;menu.hidden=!open;$('profile-trigger').setAttribute('aria-expanded',String(open));return;}
  if(action==='profile-settings'){ $('profile-menu').hidden=true;$('profile-trigger').setAttribute('aria-expanded','false');openProfileSettings();return; }
  if(action==='profile-logout'){ $('profile-menu').hidden=true;$('profile-trigger').setAttribute('aria-expanded','false');if(portalMode){portalLoggedIn=false;$('modal').close();render();}else notify('原型不会执行真实退出登录；正式页面将清理会话并返回登录页。');return; }
  if(action==='profile-settings-tab'){openProfileSettings(b.dataset.tab);return;}
  if(action==='profile-password-save'){const current=$('profile-current-password').value.trim(),next=$('profile-new-password').value,confirm=$('profile-confirm-password').value;if(!current||next.length<8||next!==confirm){$('profile-password-error').textContent='请填写当前密码，新密码至少 8 位且两次输入一致。';return;}$('modal').close();notify('演示密码已更新；正式环境将调用账号安全接口。');return;}
  if(action==='vendor-create')vendorPolicyForm();
  if(action==='vendor-edit')vendorPolicyForm(id);
  if(action==='vendor-detail')showVendor(id);
  if(action==='vendor-status')confirmVendorStatus(id);

  if(action!=='account-menu'&&$('account-menu').matches(':popover-open'))$('account-menu').hidePopover();
  if(action==='account-menu')openAccountMenu(id,b);
  if(action==='usage-detail')showUsageDetail(id);
  if(action==='probe'||action==='sync-account')runAccountOperation(id,action);
  if(action==='delete-account')confirmDelete(id);
  if(action==='confirm-delete'){const a=findAccount(id);if(!$('confirm-delete-check').checked||!accountAccess(a).manage)return;VendorHierarchyModel.archiveAccount(window.vendorHierarchy.actor,a,vendors,DEMO_NOW);auditVendor(accountVendor(a),'account.archive','#'+id+' · 账号、授权、关联及历史保留');window.vendorHierarchy.events.unshift({at:DEMO_NOW,actor:previewRole==='super_admin'?'超级管理员':ownerPolicy().name,actorId:window.vendorHierarchy.actor.id,target:a.vendorId,action:'account.archive',result:'#'+id+' · 账号与历史保留'});$('modal').close();render();notify('账号已归档并停止供给，仍保留在账号池列表，全部历史保留。');}
  if(action==='detail-date-apply'){const start=$('detail-start').value,end=$('detail-end').value;if(!start||!end||start>end||end>DEMO_TODAY){$('detail-date-error').textContent='请选择有效起止日期，结束日期不能晚于演示今日。';return;}usageDetail.start=start;usageDetail.end=end;usageDetail.page=1;renderUsageDetail();}
  if(action==='detail-date-all'){usageDetail.start='';usageDetail.end='';usageDetail.page=1;renderUsageDetail();}
  if(action==='detail-date-week'){usageDetail.start=addDays(DEMO_TODAY,-6);usageDetail.end=DEMO_TODAY;usageDetail.page=1;renderUsageDetail();}
  if(action==='detail-chart'){const scroll=$('modal-body').scrollTop;usageDetail.metric=b.dataset.metric;renderUsageDetail();$('modal-body').scrollTop=scroll;}
  if(action==='detail-page'){const scroll=$('modal-body').scrollTop;usageDetail.page+=Number(b.dataset.delta);renderUsageDetail();$('modal-body').scrollTop=scroll;}
  if(action==='export-accounts'||action==='export-account')openExport(action==='export-account'?id:null);
  if(action==='prepare-export')prepareExport();
  if(action==='download-accounts'&&transfer?.payload){const url=URL.createObjectURL(new Blob([JSON.stringify(transfer.payload,null,2)],{type:'application/json'})),a=document.createElement('a');a.href=url;a.download='vendor-accounts-demo-20261015.json';a.click();setTimeout(()=>URL.revokeObjectURL(url),1000);notify('已生成演示授权 JSON，可用于本原型导入验证。');}
  if(action==='import-accounts')openImport();
  if(action==='fill-import-sample'){const a=exportRecord(INITIAL_ACCOUNTS[0]);a.subject='DEMO_SUBJECT_IMPORTED';a.name='Claude 导入示例';a.credentials.token='DEMO_CREDENTIAL_IMPORTED';$('account-import-json').value=JSON.stringify({type:'sub2api-vendor-demo',version:1,accounts:[a]},null,2);invalidateImport();}
  if(action==='preview-import')parseAccountImport();
  if(action==='commit-import')commitAccountImport();
  if(action==='add-proxy')openProxyEditor();
  if(action==='edit-proxy')openProxyEditor(id);
  if(action==='import-proxy')openProxyEditor(null,'quick');
  if(action==='proxy-tab'){if(b.dataset.prefix==='inline'){accountForm.proxyTab=b.dataset.tab;renderAccountForm();}else{proxyEditor.tab=b.dataset.tab;renderProxyEditor();}}
  if(action==='quick-proxies')saveQuickProxies(b.dataset.prefix);

  if (action === 'add-account') startAccountForm();
  if (action === 'reauth') startAccountForm(id);
  if (action === 'edit') startAccountForm(id, true);
  if (action === 'account-copy-proxy') { const a=findAccount(id); if(accountAccess(a).read) await copyProxyAddress(a.proxy); }
  if (action === 'dismiss') $('modal').close();
  if (action === 'inline-proxy') { accountForm.inline = !accountForm.inline; renderAccountForm(); }
  if (action === 'save-inline-proxy') saveProxy('inline', true);
  if (action === 'generate-url') generateURL();
  if (action === 'copy-auth-link') {
    try { await navigator.clipboard.writeText(accountForm.url); notify('演示授权链接已复制。'); } catch { $('generated-url').focus(); $('generated-url').select(); notify('链接已选中，可手动复制。'); }
  }
  if (action === 'open-auth-demo') $('authorization-demo').showModal();
  if (action === 'close-auth-demo') $('authorization-demo').close();
  if (action === 'fill-auth-result') {
    accountForm.result = accountForm.platform === 'openai' ? `https://callback.example.invalid/callback?code=DEMO_CODE&state=${accountForm.session}` : `DEMO_CODE#${accountForm.session}`;
    $('authorization-demo').close(); renderAccountForm();
  }
  if (action === 'fill-token') { accountForm.result = accountForm.method === 'refresh_token' ? 'DEMO_REFRESH_TOKEN' : 'DEMO_SETUP_TOKEN'; renderAccountForm(); }
  if (action === 'complete-account') completeAccount();
  if (action === 'detail') showDetail(id);
  if (action === 'toggle') { const a = accounts.find(a => a.id === id); a.paused = !a.paused; renderAccounts(); notify(a.paused ? '已暂停演示供给，其他阻断状态与历史仍保留。' : `已恢复供给开关；当前状态：${status(a)}。`); }
  if (action === 'reset-filters') { filters = defaultFilters(); paging.accounts.page = 1; renderAccounts(); }
  if (action === 'reset-account-columns') { accountColumnVisibility = Object.fromEntries(ACCOUNT_COLUMNS.map(([key]) => [key, true])); renderAccounts(); }
  if (action === 'account-created-apply') { filters.createdFrom = $('account-created-from').value; filters.createdTo = $('account-created-to').value; paging.accounts.page = 1; renderAccountRows(); }
  if (action === 'account-created-clear') { filters.createdFrom = ''; filters.createdTo = ''; paging.accounts.page = 1; renderAccounts(); }
  if (action === 'fault') { filters.fault = b.dataset.fault; paging.accounts.page = 1; renderAccounts(); }
  if (action === 'paginate') { paging[b.dataset.key].page = Number(b.dataset.to); page === 'accounts' ? renderAccountRows() : renderUsageData(); }
  if (action === 'collapse') { collapsed = !collapsed; renderAccounts(); }
  if (action === 'refresh') { refreshedAt = '已刷新演示快照 · ' + new Date().toLocaleTimeString('zh-CN', { hour12: false }); renderAccounts(); notify('本地演示数据已刷新；未请求生产数据。'); }
  if (action === 'cumulative-help') { const live = accounts; const all = total(allLedger().filter(r => live.some(a => a.id === r.account))); const today = total(allLedger().filter(r => r.date === DEMO_TODAY && live.some(a => a.id === r.account))); const fable = total(allLedger().filter(r => r.model === 'Claude Fable' && live.some(a => a.id === r.account))); const nonFable = total(allLedger().filter(r => r.model !== 'Claude Fable' && live.some(a => a.id === r.account))); const hit = all.input + all.write + all.read ? (100 * all.read / (all.input + all.write + all.read)).toFixed(1) + '%' : '—'; openModal('累计统计', '全部账号历史累计，右侧为今日数据', `<div class="cumulative-grid"><div>总请求数 <strong>${number(all.requests)}</strong><span>今日 ${number(today.requests)}</span></div><div>成功率 <strong>演示 99.2%</strong><span>今日 99.6%</span></div><div>总消耗 <strong>${money(all.cost)}</strong><span>今日 ${money(today.cost)}</span></div><div>Claude Fable <strong>${money(fable.cost)}</strong><span>${all.cost ? (100 * fable.cost / all.cost).toFixed(1) : 0}% · 今日 ${money(fable.cost && today.cost ? today.cost * fable.cost / all.cost : 0)}</span></div><div>非 Fable <strong>${money(nonFable.cost)}</strong><span>${all.cost ? (100 * nonFable.cost / all.cost).toFixed(1) : 0}% · 今日 ${money(today.cost - (fable.cost && today.cost ? today.cost * fable.cost / all.cost : 0))}</span></div><div>缓存命中率 <strong>${hit}</strong><span>今日 54.1%</span></div></div>`, '<div class="buttons"><button data-action="dismiss">了解</button></div>'); }
  if (action === 'metrics-help') openModal('统计口径', '仅本供应商 · 未连接实际监控', '<p>账号概览不受列表筛选或分页影响；已归档账号不计入活跃账号总数。演示中的 RPM/TPM 按可用账号聚合。</p><div class="notice">5 小时和 7 天窗口示例适用于两平台；Fable 仅出现在 Claude。正常低于 90%，接近上限为 90% 至 99% 以下，已满从 99% 起；未知不等于零。</div><p>累计消耗与统计明细共用一份虚构用量记录。消费金额不是供应商分成。</p>', '<div class="buttons"><button data-action="dismiss">了解</button></div>');
  if (action === 'open-calendar') {
    if (calendar) calendar = null; else calendar = { start: usage.start, end: usage.end, label: usage.label, year: Number(usage.start.slice(0, 4)), month: Number(usage.start.slice(5, 7)) - 1, choosingEnd: false };
    renderCalendar();
  }
  if (action === 'date-preset') { const [start, end] = presetRange(b.dataset.preset); calendar = { ...calendar, start, end, label: b.dataset.preset, year: Number(start.slice(0, 4)), month: Number(start.slice(5, 7)) - 1, choosingEnd: false }; renderCalendar(); }
  if (action === 'select-date') {
    const date = b.dataset.date; calendar.label = '自定义时间范围';
    if (!calendar.choosingEnd) { calendar.start = date; calendar.end = ''; calendar.choosingEnd = true; }
    else { const range = [calendar.start, date].sort(); calendar.start = range[0]; calendar.end = range[1]; calendar.choosingEnd = false; }
    renderCalendar();
  }
  if (action === 'calendar-month') { const d = new Date(Date.UTC(calendar.year, calendar.month + Number(b.dataset.delta), 1)); calendar.year = d.getUTCFullYear(); calendar.month = d.getUTCMonth(); renderCalendar(); }
  if (action === 'date-confirm') applyUsageRange(calendar.start, calendar.end, calendar.label);
  if (action === 'date-clear') { const [start, end] = presetRange('近 7 天'); calendar = { ...calendar, start, end, label: '近 7 天', year: 2026, month: 9, choosingEnd: false }; renderCalendar(); }
  if (action === 'date-cancel') { calendar = null; renderCalendar(); $('range-toggle').focus(); }
  if (action === 'save-proxy') saveProxy('proxy', false);
  if (action === 'proxy-detail') showProxyDetail(id);
});
document.addEventListener('keydown', event => {
  if (event.key === 'Escape' && !$('profile-menu').hidden) { $('profile-menu').hidden = true; $('profile-trigger').setAttribute('aria-expanded', 'false'); $('profile-trigger').focus(); return; }
  if (event.key === 'Escape' && calendar) { calendar = null; renderCalendar(); $('range-toggle').focus(); }
});
$('modal').addEventListener('close', () => { if ($('modal').open) return; accountForm = null; transfer = null; proxyEditor = null; usageDetail = null; $('modal-body').innerHTML = ''; $('modal-actions').innerHTML = ''; });
$('reset').addEventListener('click', () => {
  accounts = structuredClone(INITIAL_ACCOUNTS); proxies = structuredClone(INITIAL_PROXIES); vendors=structuredClone(INITIAL_VENDORS);accountColumnVisibility = Object.fromEntries(ACCOUNT_COLUMNS.map(([key]) => [key, true]));adminEvents=[];previewRole='vendor';$('preview-role').value='vendor';adminView='vendors';adminQuery='';adminStatus='all'; filters = defaultFilters();
  usage = { account: 'all', platform: 'all', start: addDays(DEMO_TODAY, -6), end: DEMO_TODAY, label: '近 7 天' };
  for (const [key, size] of Object.entries({ accounts: 10, daily: 7, usageAccounts: 10 })) paging[key] = { page: 1, size }; page = 'accounts'; collapsed = false; resetAdminState(); resetProxyWorkspace();seedStatements();render(); notify('演示数据已重置。');
});


// Preview navigation mirrors the product drawer without changing the demo identity.
const previewNavigationMedia = window.matchMedia('(max-width: 1023px)');
function setPreviewNavigation(open = false) {
  const app = document.querySelector('.app'), sidebar = document.querySelector('.sidebar');
  const toggle = $('navigation-toggle');
  const visible = previewNavigationMedia.matches && open;
  app.classList.toggle('sidebar-open', visible);
  sidebar.inert = previewNavigationMedia.matches && !visible;
  document.querySelector('.main').inert = visible;
  toggle.setAttribute('aria-expanded', String(visible));
  if (visible) sidebar.querySelector('button')?.focus();
}
previewNavigationMedia.addEventListener('change', () => setPreviewNavigation());
document.addEventListener('click', event => {
  const button = event.target.closest('button');
  if (!button) return;
  if (button.dataset.action === 'toggle-navigation') setPreviewNavigation(!document.querySelector('.app').classList.contains('sidebar-open'));
  if (button.dataset.action === 'close-navigation' || button.dataset.page) setPreviewNavigation();
});
document.querySelector('.sidebar').addEventListener('keydown', event => {
  if (event.key === 'Escape') { setPreviewNavigation(); $('navigation-toggle').focus(); }
});
setPreviewNavigation();
