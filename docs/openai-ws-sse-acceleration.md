# HTTP 流式 WS 加速已移除

自 2026-10-04 起，本项目移除普通 OAuth 账号的 HTTP→上游 WS→SSE 加速及其单账号、批量配置入口。普通 OpenAI 请求默认遵循 HTTP→HTTP、WS→WS；BPS 保留 WS→HTTP/SSE 适配，管理员显式选择的手动 `http_bridge` 模式也保留。

历史 `openai_oauth_ws_sse_acceleration=true` 不再生效，无需数据库迁移。按请求大小自动转 HTTP 已移除；手动 `http_bridge` 及历史配置保持有效，与源实现一致。

当前行为、旧配置处理和 BPS 模型切换规则见 [OpenAI 客户端与上游协议](openai-transports.md)。

## 历史来源

该功能最初适配自 ranxi2001 的 `83f9ff9e83b0e35bb3ea2c66a93e02f05ce83f80`，于 2026-09-29 的本地提交 `1f4d65ab1f81e80e3d775268f24680469fba977a` 引入，2026-10-03 的 `cdd3253938640c3a7a72f0b77982d5f1e99be9b3` 增加批量控制。

需求参考为 `openai-ws-sse-accelerator.s2plugin` 0.2.2 的 manifest 和配置页面，本项目使用已有 WS 转发器实现，没有导入插件二进制。引入时默认关闭，验证范围为离线模拟，未验证真实上游延迟。

移除原因是可选转换增加了客户端不可见的传输分支，并使 HTTP 请求进入采用长时间单次读取超时的 WS 等待路径。历史引入记录保留用于来源追溯，不再作为启用指南。
