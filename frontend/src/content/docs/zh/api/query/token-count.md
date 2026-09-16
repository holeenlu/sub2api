## 三种 Token Count 入口

| 协议 | 请求 | 成功字段 | 语义 |
| --- | --- | --- | --- |
| Anthropic | `POST /v1/messages/count_tokens` | `input_tokens` | Anthropic 链可转发；OpenAI 链可桥接；Grok 和部分兼容提供商为本地估算 |
| Responses | `POST /v1/responses/input_tokens` | `object: response.input_tokens`、`input_tokens` | 官方兼容账号可转发；不支持此入口及部分兼容账号会回退本地估算 |
| Gemini | `POST /v1beta/models/{model}:countTokens` | `totalTokens` | 仅 Gemini 分组；普通 Gemini 账号按实现返回，Antigravity OAuth 路径当前固定返回占位 `0` |

计数不是最终账单预测。协议包装、工具 Schema、图像、缓存、推理 token、上游 tokenizer 和模型映射都可能使结果与生成响应的 usage 不同。

## Anthropic Messages 计数

```bash
curl "$KDAN_BASE_URL/v1/messages/count_tokens" \
  -H "x-api-key: $KDAN_API_KEY" \
  -H "anthropic-version: 2023-06-01" \
  -H "Content-Type: application/json" \
  -d '{"model":"YOUR_MODEL_ID","system":"Answer briefly.","messages":[{"role":"user","content":"What is idempotency?"}]}'
```

```json
{"input_tokens":19}
```

请求体沿用 Messages 输入，可包括 `system`、`messages`、`tools` 和 thinking 字段。Grok 路径不选账号、不访问上游，经过协议转换和本地 tokenizer 估算，只适合容量规划。Anthropic 兼容上游明确没有计数接口时可能返回 404。

## Responses input_tokens

```bash
curl "$KDAN_BASE_URL/v1/responses/input_tokens" \
  -H "Authorization: Bearer $KDAN_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model":"YOUR_MODEL_ID","instructions":"Answer briefly.","input":"What is idempotency?"}'
```

```json
{"object":"response.input_tokens","input_tokens":19}
```

`model` 是必填非空字符串，其余字段沿用 Responses 输入，包括字符串或类型化 `input`、`instructions` 和工具定义。本地回退同样返回 HTTP 200 和同一信封，客户端不能仅靠格式判断是否为上游精确计数；最终应读取实际生成 usage。

## Gemini countTokens

```bash
curl "$KDAN_BASE_URL/v1beta/models/YOUR_MODEL_ID:countTokens" \
  -H "x-goog-api-key: $KDAN_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"contents":[{"role":"user","parts":[{"text":"What is idempotency?"}]}]}'
```

```json
{"totalTokens":19}
```

上面的数值仅为响应形状示例。普通 `/v1beta` 仅 Gemini 分组可用；如果该分组选择到 Antigravity OAuth 账号，当前实现恒返回 `{"totalTokens":0}`，不能用于容量规划或精确计费估算。使用同一 Key 查询 `/v1beta/models`，并移除返回 `name` 中的 `models/` 前缀后放入 URL。

## 错误与实现边界

缺少 Key、空请求体、无 `model`、模型未开放、无可调度账号按对应协议返回错误。计数不写入生成 usage 账单，但仍经过认证、分组、白名单、内容审核、额度资格和请求体上限。实现见 `backend/internal/server/routes/gateway.go`、`handler/openai_gateway_count_tokens.go`、`service/openai_gateway_count_tokens.go`、`service/gateway_count_tokens.go` 与 `handler/gemini_v1beta_handler.go`。
