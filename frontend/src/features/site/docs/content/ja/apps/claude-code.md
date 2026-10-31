## 適切な Claude の利用面を選択する

| クライアント | 設定 |
| --- | --- |
| Claude Code CLI | 以下の環境変数またはユーザー設定 |
| VS Code Claude Code 拡張機能 | 以下のエディターのユーザー環境設定 |
| サードパーティ推論を使用する Claude デスクトップ | [デスクトップガイド](/apps/claude-desktop) |
| claude.ai ブラウザチャット | このガイドでは、ブラウザのアカウントチャットを sub2api の API キーに切り替えません |

[API キー](/keys) で対象グループ用のキーを作成します。Messages 互換性およびクライアント／モデルの権限は、そのグループによって異なります。

## CLI の設定

### オンラインインストール（macOS / Linux）

```bash
export SUB2API_BASE_URL="{{API_ROOT}}"
export SUB2API_API_KEY="YOUR_SUB2API_API_KEY"
curl -fsSL {{API_ROOT}}/install/claude-code.sh | bash
```

Python 3 が必要です。新しい設定は Claude Code 固有の Messages 環境フィールドと `600` 権限を使用します。既存の `settings.json` は変更せず、確認用の `settings.sub2api.json` を作成します。バックアップ後、必要な env フィールドだけを手動で統合してください。認証情報、ログイン、プライバシー設定は移行せず、既存の候補ファイルも上書きしません。実行前に内容を確認してください。`SUB2API_BASE_URL` は必須です。

これは、無効なサンプル認証情報と例示用 URL を含む、プロジェクトの **キーを使用 → Claude Code** UI です。オペレーティングシステムのタブを選択し、自分のコンソールから値をコピーしてください。[コンソールガイド](/apps/console) には PowerShell のスクリーンショットも含まれています。

![サンプルデータを使用したプロジェクトの Claude Code 設定](/docs-assets/client-claude-en.png)

[公式セットアップガイド](https://code.claude.com/docs/en/setup) を使用してインストールし、`claude --version` を実行して確認します。

macOS / Linux:

```bash
export ANTHROPIC_BASE_URL="{{API_ROOT}}"
export ANTHROPIC_AUTH_TOKEN="your sub2api API key"
claude --model claude-sonnet-5
```

Windows PowerShell:

```powershell
$env:ANTHROPIC_BASE_URL="{{API_ROOT}}"
$env:ANTHROPIC_AUTH_TOKEN="your sub2api API key"
claude --model claude-sonnet-5
```

例示用モデルを、有効化されている ID に置き換えます。ルート URL を使用してください。クライアントが `/v1/messages` を追加します。`Bearer ` プレフィックスを付けずに、生のキーを入力します。変更する前に競合する設定をバックアップしてください。保存済みの公式ログインを削除する必要はありません。

## 永続設定と IDE

この `env` ブロックを `~/.claude/settings.json` にバックアップしてから統合します。

```json
{
  "env": {
    "ANTHROPIC_BASE_URL": "{{API_ROOT}}",
    "ANTHROPIC_AUTH_TOKEN": "your sub2api API key"
  }
}
```

このファイルには認証情報が含まれます。共有プロジェクトの `.claude/settings.json` に配置しないでください。インストーラーはクライアントの既存のプライバシー設定を保持します。GUI エディターは端末の環境変数を継承しない場合があります。VS Code のユーザー Settings JSON：

```json
{
  "claudeCode.environmentVariables": [
    { "name": "ANTHROPIC_BASE_URL", "value": "{{API_ROOT}}" },
    { "name": "ANTHROPIC_AUTH_TOKEN", "value": "your sub2api API key" }
  ]
}
```

## モデルを確認して選択する

`/status` を使用して、ベース URL と認証情報のソースを確認します。`/model claude-sonnet-5` で有効化されている正確な ID を選択し、簡単なメッセージを送信して sub2api の使用量を確認します。モデルピッカーでは、すべての `/v1/models` エントリが自動的に一覧表示されない場合があります。

Claude Code は、タイトル、要約、トークン数を取得する補助リクエストを送信する場合があります。それらのみが失敗する場合は、グループの許可リストまたはマッピングを確認してください。

## 切り替え後に再開する

元のプロジェクトディレクトリに戻り、`claude --resume`、`claude --continue`、または `/resume` を使用します。OS ユーザー、プロジェクトパス、設定ディレクトリを確認してください。それでもセッションが見つからない場合は、[Claude Code セッションの復旧](/apps/session-recovery-claude) に従ってください。Claude のデータに Codex の修復ユーティリティを使用しないでください。

| エラー | 対応 |
| --- | --- |
| 401 | `/status` でキー、設定の上書き、認証情報のソースを確認する |
| 404 / モデルエラー | ルート URL と有効化されているモデルを確認する |
| 429 | 制限を遵守し、残高、同時実行数、クォータを確認する |
| ログインプロンプトが繰り返し表示される | クライアントと環境を確認する。デスクトップは別の設定を使用する |

参照元: [ゲートウェイの設定](https://code.claude.com/docs/en/llm-gateway-connect)、[会話の再開](https://code.claude.com/docs/en/common-workflows#resume-previous-conversations)。プロジェクト設定の確認日: 2026-10-19。

このゲートウェイは Claude Code / Claude Desktop のテレメトリを収集・保存せず、OTLP `/v1/logs` と `/v1/metrics` も提供しません。従来のイベント API は本文を読み取らず、転送せずに空の 200 を返します。クライアントのプライバシー設定とプロバイダーへの直接通信はクライアントが制御します。

アカウントテレメトリは、管理者が Claude OAuth / Setup Token アカウントにインポートしたデバイスパッケージのみを使用します。新しいアカウントはインポートを待機し、サーバーは Claude Code を初期化しません。ローカルツールの JSON には実際のインストール ID、CLI バージョンと環境が含まれ、アカウント認証情報やセッションは含まれません。インポート後、ゲートウェイは現在のアカウント情報とセッションを紐づけ、vm2api のルールで完全なイベントを生成します。トークンは上流へのリクエスト認証にのみ使用し、テレメトリ JSON には含めません。定期送信は既定でオフです。有効時はインポート済みアカウントのみ、10分間のアクティブ期間内で約10秒ごとにイベント、6時間ごとに GrowthBook を送信します。無効時もインポートしたデバイス情報は保持します。
