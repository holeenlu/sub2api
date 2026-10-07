## エンドポイントと認証

```http
POST /v1/chat/completions
Authorization: Bearer $API_KEY
Content-Type: application/json
```

このエンドポイントは、OpenAI Chat Completions リクエストを受け付けます。モデルは APIキーのグループから参照可能でなければなりません。ゲートウェイはネイティブに転送するか、Chat Completions、Responses、Messages、その他の互換プロトコル間で変換できます。そのため、正確なモデルのサポート状況は `GET /v1/models` と最小限のリクエストで確認する必要があります。

## 基本リクエストフィールド

| フィールド | 型 | 必須 | 制約と動作 |
| --- | --- | --- | --- |
| `model` | string | yes | 正確なモデル ID。空の値を指定すると `invalid_request_error` を返します |
| `messages` | array | yes | 順序付けられた `system`、`developer`、`user`、`assistant`、`tool` メッセージ |
| `messages[].content` | string / array / null | role-dependent | テキストまたはマルチモーダルパート。assistant のツール呼び出しメッセージでは `null` を使用できます |
| `max_completion_tokens` | integer | no | 推奨される出力上限。古いクライアントでは `max_tokens` を使用する場合があります。モデルの上限が適用されます |
| `temperature` / `top_p` | number | no | 受け付けられる範囲と、推論モデルがこれらを無視するかどうかはモデルによって異なります |
| `stream` | boolean | no | `text/event-stream` を返します。JSON の boolean でなければなりません |
| `stream_options.include_usage` | boolean | no | 最終的な使用量チャンクを要求します。ゲートウェイは請求のためにアップストリームへ強制的に指定する場合がありますが、クライアントは使用量が欠落していても処理できなければなりません |
| `tools` | array | no | 関数では `type: "function"` に加えて `function.{name,description,parameters}` を使用します |
| `tool_choice` | string / object | no | 一般的な値は `auto`、`none`、`required`、または名前付き関数です。パスによってサポート状況が異なります |
| `parallel_tool_calls` | boolean | no | 複数の呼び出しを許可します。変換によってセマンティクスが限定される場合があります |
| `reasoning_effort` | string | no | ゲートウェイは `low`、`medium`、`high`、`xhigh` を解析します。すべてのモデルがすべての値をサポートするわけではありません |
| `stop` | string / string[] | no | シーケンス数と長さの上限はアップストリームによって決まります |
| `response_format` | object | no | `json_object` または `json_schema` をサポートするモデル向けの構造化出力 |

ユーザーメッセージには `text` パートと `image_url` パートを含めることができます。互換性のある型ではファイルパートも受け付けますが、URL/data-URI のソース、ファイル型、サイズには引き続き処理パス上の制約があります。

## 最小限のリクエスト

```bash
curl "$API_BASE_URL/v1/chat/completions" \
  -H "Authorization: Bearer $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model":"YOUR_MODEL_ID","messages":[{"role":"user","content":"Explain idempotency in one sentence."}]}'
```

## ストリーミングなしの完全なレスポンス

```json
{
  "id":"chatcmpl_example",
  "object":"chat.completion",
  "created":1789401600,
  "model":"YOUR_MODEL_ID",
  "choices":[{"index":0,"message":{"role":"assistant","content":"Idempotency means repeating an operation has the same final effect as performing it once."},"finish_reason":"stop"}],
  "usage":{"prompt_tokens":18,"completion_tokens":24,"total_tokens":42,"prompt_tokens_details":{"cached_tokens":0},"completion_tokens_details":{"reasoning_tokens":0}}
}
```

`choices[].message` を読み取り、`finish_reason` に応じて処理を分岐します。`stop` は自然な終了、`length` は出力上限への到達、`tool_calls` はツール実行が必要であること、`content_filter` はフィルタリングされたことを示します。プロバイダー固有のフィールドは変換中に失われる場合があります。

## SSE ストリーミング

各フレームは `data: {JSON}\n\n` です。テキストは `choices[].delta.content` に含まれます。ツール名と `function.arguments` はフレーム間で分割される場合があります。choice とツールインデックスごとに蓄積し、呼び出しが完了してから引数を解析してください。

```text
data: {"id":"chatcmpl_example","object":"chat.completion.chunk","created":1789401600,"model":"YOUR_MODEL_ID","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}

data: {"id":"chatcmpl_example","object":"chat.completion.chunk","created":1789401600,"model":"YOUR_MODEL_ID","choices":[{"index":0,"delta":{"content":"Idempotency"},"finish_reason":null}]}

data: {"id":"chatcmpl_example","object":"chat.completion.chunk","created":1789401600,"model":"YOUR_MODEL_ID","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

data: {"id":"chatcmpl_example","object":"chat.completion.chunk","created":1789401600,"model":"YOUR_MODEL_ID","choices":[],"usage":{"prompt_tokens":18,"completion_tokens":24,"total_tokens":42}}

data: [DONE]
```

`[DONE]`、最終的な `finish_reason`、または使用量を含む末尾フレームを、成功して完了したことの証拠として扱います。3 つすべてが揃う前に接続が閉じた場合は、結果が途中で切り詰められた可能性があるとしてマークし、アプリケーションレベルの冪等性ポリシーに従う場合にのみ再試行してください。HTTP ストリーミングの開始後は、失敗がストリーム内で通知される場合と、切断として発生する場合があります。

## ツール呼び出しの往復

最初のリクエストで関数を定義します。assistant が `tool_calls` を返したら、各呼び出しを実行して検証し、元の assistant アイテムと対応する結果を送信します。

```json
{
  "model":"YOUR_MODEL_ID",
  "messages":[
    {"role":"user","content":"What time is it in Shanghai?"},
    {"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"get_time","arguments":"{\"timezone\":\"Asia/Shanghai\"}"}}]},
    {"role":"tool","tool_call_id":"call_1","content":"{\"time\":\"10:30\"}"}
  ],
  "tools":[{"type":"function","function":{"name":"get_time","description":"Return local time for an IANA timezone","parameters":{"type":"object","properties":{"timezone":{"type":"string"}},"required":["timezone"],"additionalProperties":false}}}]
}
```

`function.arguments` は文字列です。JSON として解析し、実行前にアプリケーションの権限を適用してください。すべての結果の `tool_call_id` は元の呼び出しと一致していなければなりません。

## 構造化出力

互換性のあるモデルには、次を送信します。

```json
"response_format":{"type":"json_schema","json_schema":{"name":"answer","strict":true,"schema":{"type":"object","properties":{"answer":{"type":"string"}},"required":["answer"],"additionalProperties":false}}}
```

ゲートウェイは一部の変換パスで、一般的な Chat の `response_format` と Responses の `text.format` の形式をマッピングしますが、すべてのプロバイダーで厳密なスキーマ適用が保証されるわけではありません。出力を解析して検証し、拒否、切り詰め、またはプレーンテキストへのフォールバックを処理してください。

## エラーとトラブルシューティング

通常のエラーは `{"error":{"type":"...","message":"..."}}` の形式です。一般的なケースは、401 が無効なキー、400 が無効なボディ、モデル、またはストリーム型、403 が無効化されたグループ機能、404 が利用できないモデル、429 がクォータまたは同時実行数の超過、502/503 がアップストリームまたはスケジューリングの失敗です。

キー、機密性の高いツール引数、大容量の data URI を除外したうえで、リクエスト ID ヘッダー、HTTP ステータス、完全な `error.type/message` を記録してください。同じキーでモデルを照会し、上記の最小限のリクエストまで縮小します。モデルが一覧に表示されることは、すべての Chat パラメーターをサポートしていることの証明にはなりません。
