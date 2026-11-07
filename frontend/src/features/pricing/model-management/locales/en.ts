export default {
  title: 'Managed model prices',
  readDenied: 'Reading prices requires an active session, models.read and billing.cost.read.', unit: 'USD per 1 million tokens. Image rates are per 1 million image tokens.',
  search: 'Search model ID', refresh: 'Reload prices', sync: 'Sync prices', loading: 'Loading prices…', empty: 'No matching model prices.',
  model: 'Model', source: 'Source', version: 'Pricing version: {version}', synced: 'Last sync: {time}', details: 'Full price details',
  edit: 'Override price', cancel: 'Cancel', save: 'Save price', reset: 'Remove override', saved: 'Command completed and prices reloaded.',
  input: 'Input', output: 'Output', cache_read: 'Cache read', cache_5m: 'Cache write (5 min)', cache_1h: 'Cache write (1 hour)',
  unknown: 'Unknown / absent', unpriced: 'Unpriced (null)', priced: 'Set rate', inherit: 'Inherit / absent', custom: 'Custom rates',
  fallback: 'Server fallback (null)', noImage: 'No separate image rates (null)',
  tiers: 'Context tiers', above: 'Above token count', addTier: 'Add tier', remove: 'Remove', modes: 'Service modes', image: 'Image token rates',
  stale: 'The pricing version changed. Your draft is preserved. Close and reopen the model to edit the latest version.',
  reloadRequired: 'Reload prices before another command. Your draft is preserved; check the server state before retrying.',
  working: 'Applying command…', invalidDraft: 'Each priced base requires input, output and cache read rates from 0 to 1e9. Optional cache writes may be absent/null. Use at most 32 distinct positive integer tier thresholds.',
  errors: {
    conflict: 'Pricing version conflict. Reload and review the latest values; your draft is preserved.',
    denied: 'Permission denied. Check models.read, billing.cost.read, models.manage and billing.rates.update.',
    unavailable: 'Model prices are currently unavailable.', invalid: 'Invalid or missing pricing version or price data.',
    request: 'The request failed. Its outcome may be unknown; reload prices before retrying.',
    refresh: 'The command was accepted, but reloading failed. Your draft is preserved. Reload before another command.'
  }
}
