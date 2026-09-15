## 接入地址

TapModels 的实际接入地址来自系统公开设置和控制台。原始 HTTP 请求使用 `{根地址}/v1/...`。OpenAI SDK 的 `base_url` 通常使用 `{根地址}/v1`；Anthropic SDK 使用根地址，由 SDK 添加 `/v1/messages`。

文档会对配置地址做归一化，避免出现 `/v1/v1`。自定义部署域名、反向代理路径和运行配置优先于示例占位域名。

## 推荐鉴权头

| 协议 | 请求头 |
| --- | --- |
| OpenAI 兼容 | `Authorization: Bearer $TAPMODELS_API_KEY` |
| Anthropic Messages | `x-api-key: $TAPMODELS_API_KEY` |
| Gemini 兼容 | `x-goog-api-key: $TAPMODELS_API_KEY` |

一次请求只需要一种 API Key 鉴权方式。网关按有效 Bearer、`x-api-key`、`x-goog-api-key` 的顺序读取；Messages 原始请求还应带适用的 `anthropic-version`。

## Key 与分组

Key 绑定的分组决定模型白名单、平台路由、费率、并发与其他策略。公开页面能看到的模型广场不等于任意 Key 的最终权限；使用 `GET /v1/models` 辅助发现，并以真实请求结果为准。

## 安全要求

- 只在服务端环境变量或密钥管理服务中保存 Key。
- 不要把 Key 放进 URL、浏览器前端、截图、仓库或日志。
- 泄漏后立即吊销并创建新 Key。
- 401 `API_KEY_REQUIRED` 表示未提供 Key；无效 Key、分组无权限与上游鉴权失败需要分别处理。
