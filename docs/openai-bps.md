# 独立 OpenAI BPS 平台已移除

2026-09-28 起，sub4api 提供的独立平台 `openai_bps` 已停止支持。创建/编辑入口、独立转发协议、Redis 会话状态、凭据诊断、平台分组、配额和监测支持均已移除。

当前唯一 BPS 模式是 **ranxi2001 的 OpenAI OAuth Excel / BPS 协议**。配置路径：**账号管理 → 添加/编辑 OpenAI OAuth 账号 → Excel / BPS 协议**。详见 [使用说明](excel-bps.md)。

升级迁移 `254_retire_standalone_bps.sql` 停用旧独立平台账号、分组、Composite 路由、监测和定时测试；保留账号凭据与历史数据。旧平台请求在本地拒绝，不会自动转发到其他协议。旧 Redis 键不再被读取或写入，按已有 TTL 过期。

独立平台的 Access Token 账号不会自动转换成 OAuth 账号。需要管理员提供有效的 OAuth 凭据，并重新选择可用分组。不要重新启用旧 `openai_bps` 记录。

实现范围、数据库验证及最新 ranxi 源审查见 [范围调整记录](BPS_RANXI_ONLY_2026_09_28.md)。历史出处见 [来源记录](openai-bps-sources.md)，保留来源记录不代表旧实现仍在运行。
