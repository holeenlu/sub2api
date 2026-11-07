# Usage DTO ownership

The usage DTO owner is `@/features/usage/api/types`; `@/features/usage` explicitly exports the types below. The retired root `frontend/src/types/index.ts` is not an import target. API query/response types that are specific to one endpoint remain in `api/usage.ts` and `api/adminUsage.ts` and are published explicitly by the feature.

- `UsageRequestType`
- `ImageSizeSource`
- `ImageSizeBreakdown`
- `UsageAuthorizationMode`
- `UsageLatencyMetrics`
- `UsageLog`
- `UsageLogAccountSummary`
- `AdminUsageLog`
- `UsageCleanupFilters`
- `UsageCleanupTask`
- `UsageStatsResponse`
- `TrendDataPoint`
- `ModelStat`
- `EndpointStat`
- `GroupStat`
- `UserBreakdownItem`
- `APIKeyBreakdownItem`
- `UserUsageTrendPoint`
- `UserSpendingRankingItem`
- `UserSpendingRankingResponse`
- `ApiKeyUsageTrendPoint`
- `UserErrorRequest`
- `UserErrorRequestDetail`
- `UserErrorListParams`
- `UsageQueryParams`

Administrative dashboard DTOs belong to `@/features/usage/api/dashboardTypes`, with `AdminDashboardStats` also exported by `@/features/usage/adminDashboard`. Configured platform-quota DTOs belong to `@/features/access-policy/api/platformQuotaTypes`; usage consumes that read-only contract rather than maintaining a duplicate.

Ownership and language rules remain in [FRONTEND_ARCHITECTURE](../../../../docs/architecture-rebuild/FRONTEND_ARCHITECTURE.md), [CODING_STANDARDS](../../../../docs/architecture-rebuild/CODING_STANDARDS.md) and [CT-11](../../../../docs/architecture-rebuild/CONTRACTS.md#ct-11-控制面与前端). This ownership record is not a validation receipt: Docker and page-by-page acceptance are `not_run`; full frontend acceptance remains incomplete.
