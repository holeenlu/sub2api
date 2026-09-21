Codex ユーザーは [Codex セッションの復旧](/apps/session-recovery-codex) に従ってください。Claude Code のセッションは Codex の SQLite データではないため、Codex の修復ユーティリティに渡してはなりません。

## 何が見つからないのかを特定する

| 症状 | 最初に行うこと |
| --- | --- |
| 現在のプロジェクトでピッカーが空 | プロジェクトに戻り、`claude --resume` を実行する。`Ctrl+W` または `Ctrl+A` で範囲を広げる |
| OS ユーザー、ホスト、または `CLAUDE_CONFIG_DIR` が異なる | 元の Claude 設定ディレクトリを特定する。ローカルセッションはホスト間で自動的に移行されない |
| プロジェクトまたは worktree のパスが変更された | 元の作業ディレクトリを復元し、セッション ID で再開する |
| TapModels のキーまたはログインを変更した後に見つからない | キーによってローカルファイルが移動することはない。OS ユーザー、設定ディレクトリ、プロジェクトパスを確認する |
| 削除済み、期限切れ、またはクラウドのみの履歴 | ローカルスキャナーでは再作成できない。元のホスト、クライアント、またはバックアップを使用する |

Claude Code CLI はセッションを `~/.claude/projects/<project>/<session-id>.jsonl` に保存します。`CLAUDE_CONFIG_DIR` が設定されている場合は、その配下に保存されます。デフォルトでは、30 日を超えたローカルレコードがクリーンアップされることがあります。

## まず Claude Code を試す

元のプロジェクトディレクトリに戻り、次を実行します。

```bash
claude --resume
```

`claude --continue` は現在のディレクトリで最新のセッションを再開し、`/resume` はセッションからピッカーを開きます。ピッカーは現在のプロジェクトから開始します。リポジトリ内の他の worktree を表示するには `Ctrl+W`、このマシン上のすべてのプロジェクトを表示するには `Ctrl+A` を押します。ID が分かっている場合は、次を使用します。

```bash
claude --resume <session-id>
```

## 読み取り専用の復旧ユーティリティをダウンロードする

[TapModels Claude Code セッション復旧バンドルをダウンロードする](/downloads/tapmodels-claude-session-recovery.zip)。macOS または Linux では、次を実行します。

```bash
curl -fsSLO https://tapmodels.ai/downloads/tapmodels-claude-session-recovery.zip
unzip tapmodels-claude-session-recovery.zip
cd tapmodels-claude-session-recovery
bash find-claude-sessions.sh
```

Windows PowerShell:

```powershell
Invoke-WebRequest https://tapmodels.ai/downloads/tapmodels-claude-session-recovery.zip -OutFile tapmodels-claude-session-recovery.zip
Expand-Archive .\tapmodels-claude-session-recovery.zip -DestinationPath . -Force
Set-Location .\tapmodels-claude-session-recovery
.\Find-ClaudeSessions.ps1
```

スキャナーは、埋め込まれた `sessionId` と一致するトランスクリプトのファイル名 UUID のみを受け付け、元の作業ディレクトリを含む正確な `claude --resume <session-id>` コマンドを出力します。Claude を起動したり、メッセージ本文を読み取ったり出力したり、JSONL、アカウント、キー、設定を変更したりすることはありません。レポートにはローカルパスとセッション ID が含まれるため、共有する前に内容を確認してください。

## 別のディレクトリをスキャンして再開する

以前の設定ディレクトリをスキャンするには、次を実行します。

```bash
bash find-claude-sessions.sh --claude-home "/old/.claude"
```

PowerShell では `-ClaudeHome` を使用します。レポートに記録された作業ディレクトリが見つからないと表示された場合は、生成されたコマンドを手動で実行する前に、そのパスにプロジェクトを復元してください。セッションを開くだけではメッセージは送信されません。フォローアップを送信すると、モデルへのリクエストが発生します。

このユーティリティでは、削除済み、期限切れ、別のホストにある、またはクラウドのみのセッションを再作成できません。また、Claude の保持期間やアカウントの所有権を変更することもありません。

出典: [Claude Code のセッション管理](https://code.claude.com/docs/en/sessions) および [Claude のローカルデータディレクトリ](https://code.claude.com/docs/en/claude-directory)。このユーティリティは一時的なフィクスチャでテストされており、実際のローカル Claude 履歴ではテストされていません。
