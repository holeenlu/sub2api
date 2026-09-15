## 从 API Key 到应用

TapModels 提供 OpenAI Responses、Chat Completions、Anthropic Messages 与图像兼容接口。第三方应用的字段名称不同，但都需要三项真实配置：控制台显示的 API 基础地址、TapModels API Key、当前 Key 分组开放的模型 ID。

| 应用 | 协议 | 基础地址 | 认证方式 |
| --- | --- | --- | --- |
| Codex 桌面端 / CLI | Responses | 控制台 API 地址，保留 `/v1` | `TAPMODELS_API_KEY` 环境变量 |
| Claude Code | Messages | 控制台 API 根地址，不追加 `/v1/messages` | `ANTHROPIC_AUTH_TOKEN` |
| OpenAI SDK | OpenAI 兼容 | 控制台 API 地址，保留 `/v1` | Bearer API Key |


## 创建专用 Key

在 [API 密钥](/keys) 点击创建，选择你实际要使用的分组。下面为本项目控制台截图，选项以当前版本为准。

![TapModels 创建 API 密钥：选择分组与限额](/docs-assets/create-api-key.png)

## 推荐接入顺序

1. 在控制台创建 Key，并记下它绑定的分组。
2. 使用同一把 Key 请求 `GET /v1/models`，复制返回的精确模型 ID。
3. 按应用教程配置 Base URL、Key 和模型。
4. 完全退出并重启应用，发送一条新会话测试。
5. 在控制台用量记录中确认请求、模型与费用。

切换后列表为空可能与项目、归档、Provider 筛选、数据目录或索引路径有关。遇到这种情况先运行只读诊断，再决定是否应用修复。
