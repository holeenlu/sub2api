## キーとモデルを準備する

[API キー](/keys) で使用するキーを選び、**Use key → Codex** を開きます。グループが利用できるモデルと接続先を決めます。現在のダイアログで生成した設定を優先してください。本文のモデル名は例であり、利用権限を保証しません。既存設定をバックアップしてから必要な項目を統合します。

メインモデルとレビューモデルの初期値は、現在のキーの設定情報から取得します。カタログ取得後は利用可能なモデルに基づいて選択を更新できます。本文の例、フロントエンドの候補、メーカーの全モデル一覧は、そのキーの許可リストではありません。

このガイドでは、Codex の設定を読み取るローカルクライアントについて説明します。リモートホスト、WSL、コンテナでは実行場所に設定が必要です。クラウドタスクではローカルファイルが使用されない場合があります。

## CLI のセットアップ

### オンラインインストール（macOS / Linux）

```bash
export TAPMODELS_BASE_URL="{{API_ROOT}}"
export TAPMODELS_API_KEY="YOUR_TAPMODELS_API_KEY"
curl -fsSL {{API_ROOT}}/install/codex.sh | bash
```

このスクリプトは `config.toml` をバックアップし、Responses プロバイダーを書き込み、モードを `600` に設定します。シェルにパイプする前に内容を確認してください。キーをシェル履歴やソース管理に決して保存しないでください。カスタムゲートウェイを使用する場合は `TAPMODELS_BASE_URL` を設定し、例のモデルを上書きする場合は `MODEL_ID` を設定します。

このスクリーンショットは、無効なサンプル認証情報と例の URL が設定された、プロジェクトの OpenAI グループ API キー設定を示しています。自身のグループ設定をコピーしてください。[コンソールガイド](/apps/console) では Legacy モードと手順も説明しています。画像をクリックするとフルサイズで表示できます。

![サンプルデータを使用したプロジェクトの Codex API キー設定](/docs-assets/client-codex-en.png)

[公式手順](https://developers.openai.com/codex/cli/) に従って CLI をインストールし、`codex --version` で確認します。

macOS / Linux:

```bash
export TAPMODELS_API_KEY="your TAPMODELS API key"
mkdir -p "${CODEX_HOME:-$HOME/.codex}"
```

Windows PowerShell:

```powershell
$env:TAPMODELS_API_KEY="your TAPMODELS API key"
$codexConfigDir = if ($env:CODEX_HOME) { $env:CODEX_HOME } else { Join-Path $env:USERPROFILE '.codex' }
New-Item -ItemType Directory -Force $codexConfigDir | Out-Null
notepad (Join-Path $codexConfigDir 'config.toml')
```

このルーティングされたグループの例を `~/.codex/config.toml`、または `CODEX_HOME` ディレクトリにマージしてください。これは、Responses 経由で Codex にルーティングされる Anthropic、Gemini、Grok、その他のグループに適用されます。トップレベルのフィールドはテーブルより前に記述し、各プロバイダーテーブルは 1 回だけ記述してください。現在のモデルと `base_url` はこの例から推測せず、モーダルからコピーしてください。

```toml
model_provider = "gateway"
model = "gpt-6-astra"

[model_providers.gateway]
name = "TAPMODELS"
base_url = "{{API_ROOT}}/v1"
env_key = "TAPMODELS_API_KEY"
wire_api = "responses"
requires_openai_auth = false
supports_websockets = false
```

同じターミナルで `codex` を実行します。WebSockets を無効にすると HTTP/SSE の開始点になりますが、ゲートウェイに WebSocket のルートがないという意味ではありません。

OpenAI グループのダイアログに表示された Provider ID を使用します。大文字と小文字を区別し、model_providers のテーブル名と一致する必要があります。API key モードは直接トークンを使用し、Legacy は対応する auth.json も必要です。ローカルカタログを選んだ場合は config.toml に加えて JSON も取得してください。設定変更後はクライアントを完全に再起動します。

## デスクトップ: キーを利用可能にする

Dock またはスタートメニューから起動したデスクトップアプリは、別のターミナルでエクスポートした変数を自動的には継承しません。既存のプロセスを終了し、キーを含むターミナルからインストール済みの実行ファイルを直接起動してください。macOS に `/Applications/Codex.app` としてインストールされている場合:

```bash
"/Applications/Codex.app/Contents/MacOS/Codex"
```

最初に実際のインストール先を確認してください。Windows では、`$env:TAPMODELS_API_KEY` を含む PowerShell からインストール済みの Codex `.exe` を呼び出します。

アイコンから起動する場合は、OpenAI グループの Use key モーダルで **API key** を選択し、完全な `config.toml` をダウンロードしてください。これによりファイル内にシークレットが保存されます。アクセスを制限し、決してコミットしないでください。Zhipu Codex 設定は API キーを `experimental_bearer_token` に埋め込むため、ダウンロードした `config.toml` だけで使用できます。その他のグループでは、生成された `env_key` 設定をそのまま使用し、自分で 2 つ目の認証フィールドを追加しないでください。

## グループモデルカタログ

選択した Codex タブにカタログ機能がある場合は、表示された方式に従います。

- **ローカルファイル**：codex-models.json を取得し、生成された model_catalog_json のパスに保存します。ファイル名や CODEX_HOME を変更した場合はパスも合わせてください。
- **リモートカタログ**：クライアントとグループが対応する場合、生成された model_catalog_url からキーのカタログを取得できます。設定内のフィールド位置は変更しないでください。

カタログ非対応のタブでは、グループで利用できる正確なモデル ID を指定します。一覧への表示、利用可能なアカウント、プロトコル対応は別の条件です。すべてのツールやエンドポイントの利用を保証しません。通常のモデル一覧 JSON を Codex 専用カタログとして保存しないでください。

## 確認とトラブルシューティング

新しいタスクで簡単なメッセージを送信し、TAPMODELS の使用量レコードでタイムスタンプ、キー、モデルが一致することを確認します。その後、既存の作業を再開してください。履歴が消えた場合は、[Codex セッションの復旧](/apps/session-recovery-codex) を参照してください。

| 症状 | 確認項目 |
| --- | --- |
| 401 / API_KEY_REQUIRED | 実際のプロセスにキー変数があるか、アクティブなプロバイダーにトークンがあるか |
| 不明なモデル | キーのグループ、完全一致する ID、古いカタログ |
| 設定が無視される | CODEX_HOME、選択したプロファイル、プロジェクトの上書き設定、リモートホスト |
| Speed/Fast コントロールがない | カタログの機能。請求倍率によってクライアントのコントロールが作成されることはありません |
| 古いタスクが見つからない | 元のプロジェクト、アーカイブ、プロバイダー、ローカルデータディレクトリ |

現在の **Use key** ダイアログで設定を生成してください。各項目は [Codex 設定リファレンス](https://developers.openai.com/codex/config-reference/) を参照してください。
