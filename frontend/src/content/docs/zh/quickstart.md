## 准备工作

在控制台创建 API Key，并确认它所属的分组。分组决定可以使用的模型、倍率、限额与上游路由；不要仅凭公开模型页判断一把 Key 是否可用。

从控制台复制接入地址。示例中的 `API_BASE_URL` 表示不含末尾 `/v1` 的根地址；若复制的地址已经包含 `/v1`，先移除它再拼接具体路径。

```bash
export API_BASE_URL="https://你的接入域名"
export API_KEY="你的密钥"
```

## 选择协议

| 现有客户端或需求 | 推荐入口 |
| --- | --- |
| OpenAI Responses SDK 或工具工作流 | `POST /v1/responses` |
| OpenAI Chat Completions 客户端 | `POST /v1/chat/completions` |
| Anthropic SDK、Claude Code 或 Messages 请求体 | `POST /v1/messages` |
| 图像生成或编辑 | `POST /v1/images/generations`、`POST /v1/images/edits` |

## 运行请求

在下方示例中选择当前分组开放的模型。HTTP 200 只表示该次请求成功；响应里的模型、`usage`、缓存字段和费用记录仍应核对。

## 核对结果

1. 保存服务端返回的 request ID，便于排查。
2. 从协议对应字段读取文本、工具调用或图片结果。
3. 在控制台用量记录核对模型 ID、输入、输出、缓存、倍率和实际费用。
4. 收到 404 模型错误时，先检查 Key 分组和模型 ID，再检查端点是否适用于该分组。

## 下一步

先阅读[鉴权与接入地址](/docs/authentication)，再进入对应协议页。生产代码应设置超时、处理 429/5xx，并避免把 API Key 写入日志。
