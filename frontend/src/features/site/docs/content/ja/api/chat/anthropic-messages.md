## エンドポイントと認証

```http
POST /v1/messages
x-api-key: $API_KEY
anthropic-version: 2023-06-01
Content-Type: application/json
```

Bearer 認証も利用できますが、Anthropic SDK と Claude Code の統合では `x-api-key` を維持してください。Anthropic グループでは通常、Messages 互換パスを使用します。OpenAI、Grok、Kimi、Zhipu、DeepSeek、MiniMax、OpenCodeGo グループでは、OpenAI ゲートウェイ経由でブリッジできます。ブロック、エラー、停止理由、使用量は、プロトコル間で同一に保たれるとは限りません。

`POST /v1/messages/count_tokens` で入力トークン数を見積もれます。このページの Messages ボディを受け付け、`{"input_tokens":...}` を返します。Grok および一部の OpenAI 互換パスではローカルのトークナイザーを使用できるため、この値は最終的な使用量や請求額ではありません。

## 主要なリクエストフィールド

| フィールド | 型 | 必須 | 制約と動作 |
| --- | --- | --- | --- |
| `model` | string | yes | グループで有効化されている正確なモデル ID |
| `max_tokens` | integer | yes | モデルまたはグループの制限を上限とする最大出力トークン数 |
| `messages` | array | yes | `user` と `assistant` のシーケンス。システム指示はトップレベルの `system` に指定 |
| `messages[].content` | string / array | yes | テキスト、および実装済みの画像、思考、ツール使用、ツール結果ブロック |
| `system` | string / array | no | トップレベルの指示。配列ブロックではサポート対象の `cache_control` を使用可能 |
| `temperature` / `top_p` | number | no | 範囲とサポート状況はモデルによって異なる |
| `stop_sequences` | string[] | no | カスタム停止シーケンス |
| `thinking` | object | no | 解釈される型は `enabled`、`adaptive`、`disabled`。`enabled` では `budget_tokens` を設定できる場合がある |
| `output_config.effort` | string | no | `low/medium/high/max` として解釈される。モデルのサポートが適用される |
| `tools` | array | no | 通常、`name`、`description`、`input_schema` を含む |
| `tool_choice` | object | no | 自動、任意、または名前付きのツール。互換性は異なる |
| `stream` | boolean | no | Anthropic 形式の SSE を送出 |

画像ブロックは `{"type":"image","source":{"type":"base64","media_type":"image/png","data":"..."}}` を使用します。キャッシュ制御は `{"type":"ephemeral","ttl":"5m"}` または `1h` として解釈されます。キャッシュ対象となる条件、最小プレフィックス、料金はモデルごとに異なります。

## 最小リクエストと完全なレスポンス

```bash
curl "$API_BASE_URL/v1/messages" \
  -H "x-api-key: $API_KEY" \
  -H "anthropic-version: 2023-06-01" \
  -H "Content-Type: application/json" \
  -d '{"model":"YOUR_MODEL_ID","max_tokens":256,"messages":[{"role":"user","content":"Explain idempotency in one sentence."}]}'
```

```json
{
  "id":"msg_example","type":"message","role":"assistant",
  "content":[{"type":"text","text":"Idempotency means repeating an operation has the same final effect as performing it once."}],
  "model":"YOUR_MODEL_ID","stop_reason":"end_turn","stop_sequence":null,
  "usage":{"input_tokens":18,"output_tokens":24,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}
}
```

すべての `content` ブロックを反復処理してください。一般的な停止理由は `end_turn`、`max_tokens`、`stop_sequence`、`tool_use` です。翻訳によって正確な値が変わる場合があります。

## SSE イベントと終了

通常のシーケンスは、`message_start`、1 つ以上の `content_block_start` / `content_block_delta` / `content_block_stop` のライフサイクル、続いて `message_delta` と `message_stop` です。デルタの種類には `text_delta`、`thinking_delta`、`signature_delta`、ツールの `input_json_delta` があります。

```text
event: message_start
data: {"type":"message_start","message":{"id":"msg_example","type":"message","role":"assistant","content":[],"model":"YOUR_MODEL_ID","stop_reason":null,"usage":{"input_tokens":18,"output_tokens":0}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Idempotency"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":24}}

event: message_stop
data: {"type":"message_stop"}
```

`index` ごとに蓄積してください。`input_json_delta.partial_json` は断片にすぎないため、`content_block_stop` の後に解析してください。`message_stop` は正常完了として扱います。それ以前に切断または `event: error` が発生した場合、テキスト、思考、引数は不完全なままです。

## ツール呼び出しのラウンドトリップ

```json
{
  "model":"YOUR_MODEL_ID","max_tokens":256,
  "messages":[
    {"role":"user","content":"What time is it in Shanghai?"},
    {"role":"assistant","content":[{"type":"tool_use","id":"toolu_1","name":"get_time","input":{"timezone":"Asia/Shanghai"}}]},
    {"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"{\"time\":\"10:30\"}"}]}
  ],
  "tools":[{"name":"get_time","description":"Return local time for an IANA timezone","input_schema":{"type":"object","properties":{"timezone":{"type":"string"}},"required":["timezone"],"additionalProperties":false}}]
}
```

`tool_use_id` は一致している必要があります。実行前に、モデルが生成した入力とアプリケーションの権限を検証してください。ツールの失敗は `is_error: true` を付けて返せます。

## 構造化出力と互換性

構造化された引数に対応する Messages の中核機構は、ツールの `input_schema` です。正確なモデルのドキュメントに記載されている場合に限り、ネイティブの構造化出力オプションを使用してください。このエンドポイントに Chat の `response_format` や Responses の `text.format` をコピーしないでください。プロトコルブリッジは関数、推論、停止理由の保持を試みますが、プロバイダーのサーバーツール、キャッシュのセマンティクス、暗号化された推論のラウンドトリップには、対象グループでのテストが必要です。

## エラーとトラブルシューティング

Messages のエラーは `{"type":"error","error":{"type":"invalid_request_error","message":"..."}}` 形式です。HTTP ステータス、リクエスト ID、型、メッセージを確認してください。401 は通常キーの問題、400 はボディ、モデル、ブロックの問題、403/404 はグループまたはモデルのポリシー、429 はクォータまたは同時実行数、502/503 はアップストリームのキャパシティを示します。同じキーでモデルを照会し、最小限のリクエストに絞り込んでください。ストリームの場合は、キー、base64 画像、思考、機密性の高いツール入力を除外したうえで、最後に完全なイベント型とブロックインデックスを保持してください。
