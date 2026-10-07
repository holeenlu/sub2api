## 適切な Claude の利用面を選択する

| クライアント | 設定 |
| --- | --- |
| Claude Code CLI | 以下の環境変数またはユーザー設定 |
| VS Code Claude Code 拡張機能 | 以下のエディターのユーザー環境設定 |
| サードパーティ推論を使用する Claude デスクトップ | [デスクトップガイド](/apps/claude-desktop) |
| claude.ai ブラウザチャット | このガイドでは、ブラウザのアカウントチャットを Tokensavy の API キーに切り替えません |

[API キー](/keys) で対象グループ用のキーを作成します。Messages 互換性およびクライアント／モデルの権限は、そのグループによって異なります。

## CLI の設定

### オンラインインストール（macOS / Linux）

```bash
export TOKENSAVY_API_KEY="YOUR_TOKENSAVY_API_KEY"
curl -fsSL https://tokensavy.ai/install/claude-code.sh | bash
```

このスクリプトは `~/.claude/settings.json` をバックアップし、Messages の環境変数を書き込み、ファイル権限を制限します。シェルにパイプで渡す前に内容を確認してください。カスタムゲートウェイを使用する場合は `TOKENSAVY_BASE_URL` を設定します。

これは、無効なサンプル認証情報と例示用 URL を含む、プロジェクトの **キーを使用 → Claude Code** UI です。オペレーティングシステムのタブを選択し、自分のコンソールから値をコピーしてください。[コンソールガイド](/apps/console) には PowerShell のスクリーンショットも含まれています。

![サンプルデータを使用したプロジェクトの Claude Code 設定](/docs-assets/client-claude-en.png)

[公式セットアップガイド](https://code.claude.com/docs/en/setup) を使用してインストールし、`claude --version` を実行して確認します。

macOS / Linux:

```bash
export ANTHROPIC_BASE_URL="{{API_ROOT}}"
export ANTHROPIC_AUTH_TOKEN="your Tokensavy API key"
export CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1
claude --model claude-sonnet-5
```

Windows PowerShell:

```powershell
$env:ANTHROPIC_BASE_URL="{{API_ROOT}}"
$env:ANTHROPIC_AUTH_TOKEN="your Tokensavy API key"
$env:CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC="1"
claude --model claude-sonnet-5
```

例示用モデルを、有効化されている ID に置き換えます。ルート URL を使用してください。クライアントが `/v1/messages` を追加します。`Bearer ` プレフィックスを付けずに、生のキーを入力します。変更する前に競合する設定をバックアップしてください。保存済みの公式ログインを削除する必要はありません。

## 永続設定と IDE

この `env` ブロックを `~/.claude/settings.json` にバックアップしてから統合します。

```json
{
  "env": {
    "ANTHROPIC_BASE_URL": "{{API_ROOT}}",
    "ANTHROPIC_AUTH_TOKEN": "your Tokensavy API key",
    "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1"
  }
}
```

これはプライベートな設定です。認証情報を共有プロジェクトの `.claude/settings.json` に配置しないでください。`CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1` は、サインイン、テレメトリ、その他の不要なトラフィックを削減するために、現在のコンソールビルダーが出力します。これによって Claude の Web、Remote Control、音声サービスが Tokensavy 経由でルーティングされることはありません。GUI エディターは、ターミナルでエクスポートした環境変数を継承しない場合があります。VS Code のユーザー Settings JSON では、次のように設定します。

```json
{
  "claudeCode.environmentVariables": [
    { "name": "ANTHROPIC_BASE_URL", "value": "{{API_ROOT}}" },
    { "name": "ANTHROPIC_AUTH_TOKEN", "value": "your Tokensavy API key" },
    { "name": "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC", "value": "1" }
  ]
}
```

## モデルを確認して選択する

`/status` を使用して、ベース URL と認証情報のソースを確認します。`/model claude-sonnet-5` で有効化されている正確な ID を選択し、簡単なメッセージを送信して Tokensavy の使用量を確認します。モデルピッカーでは、すべての `/v1/models` エントリが自動的に一覧表示されない場合があります。

Claude Code は、タイトル、要約、トークン数を取得する補助リクエストを送信する場合があります。それらのみが失敗する場合は、グループの許可リストまたはマッピングを確認してください。

## 切り替え後に再開する

元のプロジェクトディレクトリに戻り、`claude --resume`、`claude --continue`、または `/resume` を使用します。OS ユーザー、プロジェクトパス、設定ディレクトリを確認してください。それでもセッションが見つからない場合は、[Claude Code セッションの復旧](/apps/session-recovery-claude) に従ってください。Claude のデータに Codex の修復ユーティリティを使用しないでください。

| エラー | 対応 |
| --- | --- |
| 401 | `/status` でキー、設定の上書き、認証情報のソースを確認する |
| 404 / モデルエラー | ルート URL と有効化されているモデルを確認する |
| 429 | 制限を遵守し、残高、同時実行数、クォータを確認する |
| ログインプロンプトが繰り返し表示される | クライアントと環境を確認する。デスクトップは別の設定を使用する |

参照元: [ゲートウェイの設定](https://code.claude.com/docs/en/llm-gateway-connect)、[会話の再開](https://code.claude.com/docs/en/common-workflows#resume-previous-conversations)。プロジェクト設定の確認日: 2026-09-15。
