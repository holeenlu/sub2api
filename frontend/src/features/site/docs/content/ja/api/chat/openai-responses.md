## エンドポイントと認証

```http
POST /v1/responses
Authorization: Bearer $API_KEY
Content-Type: application/json
```

これは OpenAI Responses 形式のエンドポイントです。APIキーのグループによって、ネイティブパスまたは翻訳パスが選択されます。公式APIに存在する組み込みツールは、ここでは自動的に有効になりません。

入力のみをカウントするには `POST /v1/responses/input_tokens` を使用します。空でない `model` が必要で、`{"object":"response.input_tokens","input_tokens":...}` を返します。一部のアカウントパスではローカル推定が行われるため、実際の生成時の使用量を置き換えるものではありません。

## 基本リクエストフィールド

| フィールド | 型 | 必須 | 制約と動作 |
| --- | --- | --- | --- |
| `model` | string | はい | グループで有効化されている正確なモデルID |
| `input` | string / array | はい | プレーンテキスト、または型付きメッセージ、関数呼び出し、関数結果 |
| `instructions` | string | いいえ | このレスポンスに対するトップレベルの指示 |
| `max_output_tokens` | integer | いいえ | 翻訳コードでは安全のための下限値として 128 を使用します。これより小さい値は引き上げられるか、拒否される場合があります |
| `temperature` / `top_p` | number | いいえ | 効果と範囲はモデルによって異なります |
| `reasoning` | object | いいえ | 推論の強度: `low/medium/high/xhigh`; 要約: `auto/concise/detailed`; モデルの対応状況も適用されます |
| `text` | object | いいえ | `format` で構造化出力を設定します。解析済みの詳細度は `low/medium/high` です |
| `tools` | array | いいえ | 型には function、custom、および複数のクライアント/検索ツールが含まれます。利用可否はパスごとに異なります |
| `tool_choice` | string / object | いいえ | 自動、無効、必須、または名前付きツール。翻訳によって選択肢が狭められる場合があります |
| `parallel_tool_calls` | boolean | いいえ | 並列呼び出しを許可します |
| `previous_response_id` | string | いいえ | メッセージIDではなく、アクセス可能な `resp_*` である必要があります |
| `include` | string[] | いいえ | 選択したアップストリームパスが対応する追加フィールドをリクエストします |
| `store` | boolean | いいえ | パススルーの意図を示します。このプロジェクトに取得履歴APIを作成するものではありません |
| `stream` | boolean | いいえ | Responses SSEイベントを出力します。JSONのブール値である必要があります |

一般的な配列パーツには `input_text`、`input_image`、`input_file` があります。画像URLおよびファイルデータ/IDには、アップストリーム側の型およびサイズ制限が引き続き適用されます。

## 最小リクエストと完全なレスポンス

```bash
curl "$API_BASE_URL/v1/responses" \
  -H "Authorization: Bearer $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model":"YOUR_MODEL_ID","input":"Explain idempotency in one sentence."}'
```

```json
{
  "id":"resp_example","object":"response","created_at":1789401600,"model":"YOUR_MODEL_ID","status":"completed",
  "output":[{"id":"msg_example","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"Idempotency means repeating an operation has the same final effect as performing it once."}]}],
  "usage":{"input_tokens":18,"output_tokens":24,"total_tokens":42,"input_tokens_details":{"cached_tokens":0},"output_tokens_details":{"reasoning_tokens":0}}
}
```

`output` は `type` ごとに反復処理してください。`output[0]` がテキストであると仮定してはいけません。対応する形状には `message`、`reasoning`、`function_call`、`custom_tool_call`、`web_search_call` があります。完全な成功を示すのは `completed` のみです。`incomplete` の場合は `incomplete_details.reason` を、`failed` の場合は `error` を確認してください。

## SSEイベントと終了

| イベント | 目的 |
| --- | --- |
| `response.created` / `response.in_progress` | レスポンスIDと状態を初期化します |
| `response.output_item.added` / `.done` | 型付き出力アイテムのライフサイクル |
| `response.content_part.added` / `.done` | コンテンツパートのライフサイクル |
| `response.output_text.delta` / `.done` | テキストを蓄積します |
| `response.function_call_arguments.delta` / `.done` | 引数文字列を蓄積します |
| `response.reasoning_summary_text.delta` / `.done` | 存在する場合に推論要約を蓄積します |
| `response.completed` | 最終レスポンスと使用量を含む成功時の終端イベント |
| `response.incomplete` / `response.failed` / `error` | 成功ではない終端イベント |

```text
event: response.output_text.delta
data: {"type":"response.output_text.delta","item_id":"msg_example","output_index":0,"content_index":0,"delta":"Idempotency"}

event: response.completed
data: {"type":"response.completed","response":{"id":"resp_example","object":"response","created_at":1789401600,"model":"YOUR_MODEL_ID","status":"completed","output":[],"usage":{"input_tokens":18,"output_tokens":24,"total_tokens":42}}}
```

終端イベントの状態を使用してください。Responsesの完了は `response.completed` であり、単なる `[DONE]` ではありません。終端イベントの前に切断された場合、状態は不明または途中で切り詰められている可能性があります。完了していないツール引数は決して実行しないでください。

## 関数ツールのラウンドトリップ

```json
{
  "model":"YOUR_MODEL_ID",
  "input":[
    {"type":"function_call","call_id":"call_1","name":"get_time","arguments":"{\"timezone\":\"Asia/Shanghai\"}"},
    {"type":"function_call_output","call_id":"call_1","output":"{\"time\":\"10:30\"}"}
  ],
  "tools":[{"type":"function","name":"get_time","description":"Return local time for an IANA timezone","parameters":{"type":"object","properties":{"timezone":{"type":"string"}},"required":["timezone"],"additionalProperties":false},"strict":true}]
}
```

HTTPの `function_call_output` には `call_id` が必要です。ゲートウェイは関連付けのない結果を拒否します。`previous_response_id` による継続は、Responses WebSocket v2 の場合に限り異なる処理になります。

## 構造化出力

Responsesでは、Chat Completionsの `response_format` ではなく `text.format` を使用します。

```json
"text":{"format":{"type":"json_schema","name":"answer","strict":true,"schema":{"type":"object","properties":{"answer":{"type":"string"}},"required":["answer"],"additionalProperties":false}}}
```

翻訳によって一般的なJSON形式をマッピングできますが、厳密な適用は引き続きモデルの動作に依存します。解析済みの出力を検証し、拒否、`incomplete`、またはプレーンテキストへのフォールバックを処理してください。

## WebSocketとサブパスの境界

`GET /v1/responses` には `Upgrade: websocket` が必要です。これはRetrieve Responseではなく、通常のGETは426を返します。保護された `POST /v1/responses/*subpath` ルートがあるからといって、公式のCRUDエンドポイントがすべて存在することを意味するわけではありません。一般的な統合にはHTTP/SSEを優先してください。

## エラーとトラブルシューティング

HTTPエラーは `{"error":{"type":"invalid_request_error","message":"..."}}` を使用します。一般的な原因には、無効なAPIキー、モデルの欠落、無効なストリーム型、無効またはアクセスできない `previous_response_id`、モデルポリシー、クォータ/同時実行数、アップストリーム障害などがあります。HTTPステータス、リクエストID、イベントタイプ、最終ステータス、秘匿化した `call_id` のコンテキストを記録してください。モデルが表示されることは、すべての組み込みツール、ストレージ機能、サブパス、または `include` 値に対応していることを保証するものではありません。
