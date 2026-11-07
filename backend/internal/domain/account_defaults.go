package domain

// Shared by native and supplier authorization. Priority is ascending (not a
// traffic weight). Leave transport/timeout policies to their gateway defaults.
const DefaultAccountConcurrency = 10
const DefaultAccountPriority = 1
