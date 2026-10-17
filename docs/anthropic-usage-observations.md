# Anthropic 用量观测与来源

本轮需求：2026-10-19。共享能力，适用于 KDAN、TapModels、Tokensavy；复用现有账号用量查询与被动响应采样，不新增管理员 HTTP 接口或数据库迁移。

## 数据与来源

现有 UsageInfo 增加 anthropic.profile_access 和 anthropic.observations。每个观测项包含 value、source、sampled_at。空值为未知，不能与数字 0 或布尔 false 混用。

原来的整体 source / updated_at 无法表达“5h 来自新响应头、7d 来自旧查询、费用来自本地统计”的混合状态，因此新增逐字段观测结构。新的详情与来源徽标组件复用现有用量单元格，不另建查询页面或第二套调度/报表逻辑。

| source | 含义 | 时间规则 |
| --- | --- | --- |
| upstream_headers | 模型请求返回的窗口、限制原因、overage 响应头 | 接收到该字段的采样时间 |
| upstream_api | OAuth usage / profile 查询结果 | 对应查询采样时间，缓存命中不刷新 |
| local_statistics | 持久化用量记录的窗口聚合 | 实际统计查询时间，缓存沿用原时间 |
| estimate | 到期归零或预计总费用等推算 | 计算时间；历史缺失时为 null |
| local_credentials | 已保存的 scope、有效期及刷新能力 | 旧凭据未记录签发观测时间时为 null |
| stored_snapshot | 升级前缺少逐字段来源的快照 | 未知时间为 null，不套用其他窗口的采样时间 |

页面在百分比、统计和费用估算旁标明来源，并在“数据来源与采样”详情中展示每项值与时间。详情按需展开，避免账号列表一次渲染全部记录。

## 查询权限与失败

- Anthropic OAuth 只有明确包含完整 user:profile scope 才调用 usage/profile；scope 缺失为 unknown，不能从账号类型推断授权。
- Setup Token 和不含该 scope 的 OAuth 使用已有被动快照，不发送额外推理探测请求。
- Profile 与 usage 共用现有代理、TLS 指纹、Bearer 和 beta 头；Profile 失败不丢弃成功取得的窗口数据，保留上次档位及其原始时间。
- usage 查询失败退回已保存的观测，并保留错误提示和负缓存；缓存成功响应不再次写回旧采样。
- API 对外不返回完整 Profile、上游错误正文、凭据或组织标识；只投影列出的档位字段。

## 可选字段与窗口

- 被动采样包括 5h / 7d / 7d_oi、聚合状态、代表性限制、overage 状态/禁用原因、阈值和回退参数。使用率响应头从小数换算为百分数；回退参数保持原值。
- Profile 保留 organization_type、rate_limit_tier、billing_type、subscription_status、has_extra_usage_enabled、has_claude_max、has_claude_pro。
- extra_usage 保留 is_enabled、monthly_limit、used_credits、utilization。金额与 extra_usage 使用率展示为上游原值；不猜测其美元单位或百分比尺度。
- limits 保留 kind、group、percent、resets_at、severity、is_active 和有限的模型/产品范围描述，最多 32 个窗口。未知 kind 保留展示，不参与既有配额调度。
- 仅无 scope 的 session / weekly_all 映射到总体 5h / 7d。模型周窗口不覆盖总体周窗口；同模型家族存在多个不同窗口时不任意选取一个作为家族配额。
- 明确的新结构空值清除旧字段含义；省略字段保留旧采样及旧时间，不能伪装成刚更新。

## 持久化与边界

使用账号 Extra 中逐字段的 anthropic_observation_ 键。每项保存值、来源、时间和内部凭据归属指纹；稀疏响应只更新实际出现的字段，避免整块覆盖其他来源。已有调度使用的窗口键与新观测一并更新。复制账号时清除观测，读取时过滤不匹配的凭据归属。

5h、7d 的统计仍来自 usage_logs，预计总费用仍由本系统费用和使用率推算。它们不是额外使用账单，也不保证覆盖绕过本系统的其他客户端流量。

## 验证范围

服务与仓储回归覆盖 scope 判断、Profile HTTP/重定向、失败降级、缓存时间、空值/零值/false、分窗口隔离、稀疏采样、重置边界、凭据归属和重启恢复。前端组件与四语言检查覆盖来源标签、时间及未知值展示；执行类型检查和主站/供应商门户构建。

不把模拟仓储恢复或本地 HTTP 服务测试表述为真实 Anthropic 账号联调或生产数据库重启验证。
