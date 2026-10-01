## コンソール設定ビルダー

**APIキー → キーを使用** は、アプリの設定ビルダーです。コピー可能なシェルコマンド、設定ファイル、および OpenAI/Composite グループ向けの Codex モデルカタログを生成します。現在のモーダルに表示されている値を、アドレス、環境変数名、モデル例の正式な値として扱ってください。

## キーを作成してグループを選択する

1. [APIキー](/keys) を開き、専用キーを作成します。
2. リクエストを受け付けるグループを選択します。グループによって、モデルの可視性、プロトコルルーティング、残高、制限が決まります。グループを指定せずにコピーしたキーを使用すると、401 または unknown-model エラーになることがあります。
3. **キーを使用** をクリックし、クライアントタブを 1 つ選択します。生成されたスニペットを複数のクライアントに貼り付けないでください。
4. **コピー** または **ダウンロード** を使用して、モーダルに表示された正確な出力を保存します。既存のファイルをバックアップし、編集前にフィールドを統合してください。

## 対応している生成出力

| グループプラットフォーム | クライアントタブ | 主な出力 |
| --- | --- | --- |
| OpenAI | Codex CLI、Codex WebSocket、Claude Code（Messages ディスパッチが有効な場合）、OpenCode | `config.toml`、`auth.json` または `experimental_bearer_token`、Anthropic 環境変数ファイル、`opencode.json` |
| Anthropic | Claude Code、ルーティング済み Codex、OpenCode | `ANTHROPIC_BASE_URL`、`ANTHROPIC_AUTH_TOKEN`、Codex Responses プロバイダー、OpenCode プロバイダー |
| Gemini | Gemini CLI、ルーティング済み Codex、OpenCode | `GOOGLE_GEMINI_BASE_URL`、`GEMINI_API_KEY`、`GEMINI_MODEL` |
| Antigravity | Claude Code、Gemini CLI、ルーティング済み Codex、OpenCode | `/antigravity` ベースパス、Gemini は `/v1beta` を使用 |
| Grok | Grok CLI、Claude Code、Codex、OpenCode | `GROK_MODELS_BASE_URL`、`XAI_API_KEY`、またはクライアント固有の設定 |
| DeepSeek、MiniMax、Composite、Kimi、Zhipu、OpenCode | Claude Code、ルーティング済み Codex、OpenCode | 生成されたグループ URL を使用します。Codex カタログを提供するのは Composite のみです |

## Codex の認証モード

OpenAI の Codex タブには 2 つのモードがあります。「キーを使用」を開くと、既定で **Codex CLI (WebSocket)**、**API key**、**macOS / Linux** が選択されます。

- **Legacy** は `requires_openai_auth = true` を設定し、`auth.json` を提供します。このログイン形式を必要とする Codex バージョンでのみ使用してください。
- **API key** は `requires_openai_auth = false` を設定し、`experimental_bearer_token` を書き込み、ローカル画像拡張ヘッダーを追加します。これによりシークレットがディスクに保存されるため、アクセス権を制限し、決してコミットしないでください。

ルーティング済み Codex タブでは、デフォルトで `env_key = "TAPMODELS_API_KEY"`、`wire_api = "responses"`、`supports_websockets = false` が設定されます。WebSocket タブでは、OpenAI Responses パスでのみ WebSocket トランスポートが有効になります。

## モデルカタログ

OpenAI/Composite グループと Zhipu API キーグループが専用 Codex カタログに対応します。その他のグループでは `model_catalog_url` と `model_catalog_json` は生成されません。通常の `GET /v1/models` を実行し、正確なモデル ID を手動で入力してください。

対応している Codex タブで生成される `config.toml` のルートには `model_catalog_json = "~/.codex/codex-models.json"` が含まれます。**モデルカタログの取得とダウンロード** で現在のキーのカタログを取得し、そのパスへ保存して Codex を再起動してください。OpenAI/Composite ではリモートカタログも選択できます。Zhipu は常にローカルファイルを使用し、`GLM-4.7 / GLM-5.3` と `config.toml` は 1,000,000 トークンのコンテキストを宣言します。グループポリシーを回避するためにスラッグを編集しないでください。

## プロジェクトの UI に従う

以下のスクリーンショットには、無効なサンプルキーと `api.example.com` を使用した、現在のプロジェクトの「キーを使用」コンポーネントが表示されています。画像ではなく、ご自身のコンソールから値をコピーしてください。

1. OpenAI グループでは既定で **Codex CLI (WebSocket)** と API key 認証が選択されます。ネットワークが WebSocket に対応していない場合は **Codex CLI**、`auth.json` のログイン形式が必要な場合は Legacy を選びます。各認証モードで生成されるファイルは異なります。

![サンプルデータを使用したプロジェクトの Codex Legacy 設定](/docs-assets/client-codex-en.png)

2. API key モードでは、キーが設定ファイルに保存されます。ファイルのアクセス権を制限し、ダウンロード後にクライアントを完全に再起動してください。

![サンプルデータを使用したプロジェクトの Codex API key 設定](/docs-assets/client-codex-en.png)

3. Anthropic グループの場合は **Claude Code** を選択し、オペレーティングシステムのタブを選びます。完全なコマンドをコピーし、同じターミナルでクライアントを起動してください。

![サンプルデータを使用したプロジェクトの Claude Code macOS / Linux 設定](/docs-assets/client-claude-en.png)

![サンプルデータを使用したプロジェクトの Claude Code PowerShell 設定](/docs-assets/client-claude-en.png)

## 確認とトラブルシューティング

対象のクライアントから簡単なメッセージを送信し、コンソールの使用量でタイムスタンプ、モデル、キー、グループを照合します。401 の場合は、実行中のプロセスが生成された環境変数を継承しているか確認してください。404 または unknown-model エラーの場合は、`/v1` が二重に追加されていないか確認します。429 の場合は、グループの制限と同時実行数を確認してください。Dock やスタートメニューから起動したデスクトップアプリは、別のターミナルで実行した `export` を継承しません。設定済みのターミナルから起動するか、クライアント固有の環境変数を設定してください。

クライアントガイド:

- [Codex](/apps/codex)
- [Claude Code](/apps/claude-code)
- [Claude Desktop](/apps/claude-desktop)
- [画像スキル](/apps/image-skills)
- [Codex セッションの復旧](/apps/session-recovery-codex)
- [Claude Code セッションの復旧](/apps/session-recovery-claude)
