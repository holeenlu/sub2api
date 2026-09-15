## 端點與鑑權

```http
GET /v1/models
Authorization: Bearer $TAPMODELS_API_KEY
```

模型列表與 API Key 的分組、平台、帳號/通道對應和模型白名單相關。請始終用實際發起推理的同一把 Key 查詢。

## 最小請求與回應

```bash
curl "$TAPMODELS_BASE_URL/v1/models" \
  -H "Authorization: Bearer $TAPMODELS_API_KEY"
```

OpenAI 或 OpenAI 相容分組通常返回：

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

Anthropic 類分組的列表項可能使用 `type`、`display_name`、ISO 時間 `created_at`；Grok 列表項還可能帶可設定 reasoning effort 後設資料。因此用戶端應以 `data[].id` 作為穩定發現欄位，其他後設資料按存在性讀取，不要強制所有平台使用同一結構。

## 可見性不等於即時可用

處理器優先彙總目前分組帳號或通道的模型對應，再套用分組白名單。沒有即時對應時，部分平台會回退到程式碼內預設列表；複合分組也可能使用預設候選。因此一個 ID 出現在列表中只表示“對該 Key 可見的接入候選”，不證明此刻有可排程帳號，也不證明它支援 Chat、Responses、Messages、Images 的全部參數。

推薦接入流程：先列出模型，選擇精確 ID，再向目標端點發送最小請求；最後根據真實 HTTP 狀態、回應結構和 usage 確認可用性。快取模型列表時設定較短重新整理週期，並在 `model_not_found` 或無可用帳號後立即重新整理。

## 單模型路徑與 Codex 模式

專案註冊了 `GET /v1/models/{model}`，但目前複用同一個 Models handler：它會按路徑參數篩選列表項，不能假設所有平台都返回官方 Retrieve Model 的完整固定欄位。

當 `GET /v1/models` 帶 `client_version` 查詢參數時，路由會選擇 Codex 模型 manifest 處理鏈；該回應不是普通 `{"object":"list","data":[]}`。通用 SDK 和業務模型選擇器不要附加 `client_version`。

## 錯誤與故障排除

401 通常表示 Key 缺失或無效；403/404 可能來自分組策略或模型白名單；5xx 表示閘道器依賴或上游發現失敗。記錄 HTTP 狀態、request ID 和錯誤體，並確認請求 Key、Base URL 與實際推理請求一致。不要把控制台的全域模型廣場列表當成某把 Key 的 `/v1/models` 結果。
