## 3つのトークンカウントエンドポイント

| プロトコル | リクエスト | 成功時のフィールド | セマンティクス |
| --- | --- | --- | --- |
| Anthropic | `POST /v1/messages/count_tokens` | `input_tokens` | Anthropic のパスは転送可能。OpenAI のパスはブリッジされ、Grok および一部の互換プロバイダーはローカルで推定します |
| Responses | `POST /v1/responses/input_tokens` | `object: response.input_tokens`、`input_tokens` | 対応する公式アカウントは転送可能。非対応のアカウントおよび一部の互換アカウントはローカルにフォールバックします |
| Gemini | `POST /v1beta/models/{model}:countTokens` | `totalTokens` | Gemini グループのみ。Antigravity OAuth アカウントのパスは現在、プレースホルダーの `0` を返します |

カウントは最終的な請求額の予測ではありません。プロトコルラッパー、ツールスキーマ、画像、キャッシュ、推論トークン、トークナイザーの選択、およびモデルマッピングによって、実際の生成時の使用量が変わる可能性があります。

## Anthropic Messages のカウント

```bash
curl "$TOKENSAVY_BASE_URL/v1/messages/count_tokens" \
  -H "x-api-key: $TOKENSAVY_API_KEY" \
  -H "anthropic-version: 2023-06-01" \
  -H "Content-Type: application/json" \
  -d '{"model":"YOUR_MODEL_ID","system":"Answer briefly.","messages":[{"role":"user","content":"What is idempotency?"}]}'
```

```json
{"input_tokens":19}
```

ボディは Messages の入力形式に従い、`system`、`messages`、`tools`、および thinking フィールドを含めることができます。Grok のパスではアカウントを選択せず、アップストリームへのリクエストも行いません。プロトコルを変換し、ローカルのトークナイザーで推定するため、キャパシティプランニングに使用してください。カウント機能を明示的に提供していない Anthropic 互換のアップストリームは、404 を返すことがあります。

## Responses input_tokens

```bash
curl "$TOKENSAVY_BASE_URL/v1/responses/input_tokens" \
  -H "Authorization: Bearer $TOKENSAVY_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model":"YOUR_MODEL_ID","instructions":"Answer briefly.","input":"What is idempotency?"}'
```

```json
{"object":"response.input_tokens","input_tokens":19}
```

`model` は必須の空でない文字列です。その他のフィールドは Responses の入力形式に従い、文字列または型付きの `input`、`instructions`、およびツールを含めることができます。ローカルフォールバックも HTTP 200 と同じエンベロープを返すため、この形式だけではアップストリームから取得した正確なカウントであることを証明できません。請求には実際の生成時の使用量を使用してください。

## Gemini countTokens

```bash
curl "$TOKENSAVY_BASE_URL/v1beta/models/YOUR_MODEL_ID:countTokens" \
  -H "x-goog-api-key: $TOKENSAVY_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"contents":[{"role":"user","parts":[{"text":"What is idempotency?"}]}]}'
```

```json
{"totalTokens":19}
```

上記の数値はレスポンス形式を示すための例にすぎません。通常の `/v1beta` パスは Gemini グループに限定されています。ルーティングで Antigravity OAuth アカウントが選択された場合、実装は常に `{"totalTokens":0}` を返します。このプレースホルダーをキャパシティプランニングや正確なコスト見積もりに使用しないでください。同じキーで `/v1beta/models` をクエリし、URL に ID を指定する前に、その `name` から `models/` プレフィックスを削除してください。

## エラーと実装境界

キーの不足、空のボディ、モデルの不足、許可リストの検証失敗、および利用できないアカウントは、プロトコル固有のエラーを返します。カウントでは生成の使用量レコードは作成されませんが、認証、グループポリシー、コンテンツレビュー、請求対象の適格性、該当する場合の同時実行数、およびボディサイズ制限は引き続き適用されます。`backend/internal/server/routes/gateway.go`、`handler/openai_gateway_count_tokens.go`、`service/openai_gateway_count_tokens.go`、`service/gateway_count_tokens.go`、および `handler/gemini_v1beta_handler.go` を参照してください。
