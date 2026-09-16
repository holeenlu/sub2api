## 端点与鉴权

```http
GET /v1/models
Authorization: Bearer $KDAN_API_KEY
```

模型列表与 API Key 的分组、平台、账号/渠道映射和模型白名单相关。请始终用实际发起推理的同一把 Key 查询。

## 最小请求与响应

```bash
curl "$KDAN_BASE_URL/v1/models" \
  -H "Authorization: Bearer $KDAN_API_KEY"
```

OpenAI 或 OpenAI 兼容分组通常返回：

```json
{
  "object": "list",
  "data": [{
    "id": "YOUR_MODEL_ID",
    "object": "model",
    "created": 1704067200,
    "owned_by": "openai",
    "type": "model",
    "display_name": "YOUR_MODEL_ID"
  }]
}
```

Anthropic 类分组的列表项可能使用 `type`、`display_name`、ISO 时间 `created_at`；Grok 列表项还可能带可配置 reasoning effort 元数据。因此客户端应以 `data[].id` 作为稳定发现字段，其他元数据按存在性读取，不要强制所有平台使用同一结构。

## 可见性不等于即时可用

处理器优先汇总当前分组账号或渠道的模型映射，再应用分组白名单。没有实时映射时，部分平台会回退到代码内默认列表；复合分组也可能使用默认候选。因此一个 ID 出现在列表中只表示“对该 Key 可见的接入候选”，不证明此刻有可调度账号，也不证明它支持 Chat、Responses、Messages、Images 的全部参数。

推荐接入流程：先列出模型，选择精确 ID，再向目标端点发送最小请求；最后根据真实 HTTP 状态、响应结构和 usage 确认可用性。缓存模型列表时设置较短刷新周期，并在 `model_not_found` 或无可用账号后立即刷新。

## 单模型路径与 Codex 模式

项目注册了 `GET /v1/models/{model}`，但当前复用同一个 Models handler：它会按路径参数筛选列表项，不能假设所有平台都返回官方 Retrieve Model 的完整固定字段。

当 `GET /v1/models` 带 `client_version` 查询参数时，路由会选择 Codex 模型 manifest 处理链；该响应不是普通 `{"object":"list","data":[]}`。通用 SDK 和业务模型选择器不要附加 `client_version`。

Gemini 原生 SDK 使用独立的 `GET /v1beta/models` 和 `GET /v1beta/models/{model}`，返回 `models[]` / `models/...` 风格数据，且普通路径只允许 Gemini 分组。不要把 `/v1/models` 的 OpenAI 风格信封硬套到 Gemini 原生发现接口。

## 错误与排障

401 通常表示 Key 缺失或无效；403/404 可能来自分组策略或模型白名单；5xx 表示网关依赖或上游发现失败。记录 HTTP 状态、request ID 和错误体，并确认请求 Key、Base URL 与实际推理请求一致。不要把控制台的全局模型广场列表当成某把 Key 的 `/v1/models` 结果。
