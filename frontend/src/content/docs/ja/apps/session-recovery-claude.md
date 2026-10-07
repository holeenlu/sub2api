このツールは Claude Code のローカル JSONL 履歴用です。Codex は [専用ツール](/apps/session-recovery-codex) を使用してください。公式クラウド履歴には元のアカウントまたはホストが必要です。

## 先に接続の問題を確認

現在のキーとモデルで新しいセッションを確認します。401、503、モデル利用不可、残高不足は接続・権限・サーバー側の問題です。既存のローカル履歴だけが見つからない場合に、以下の手順を使用してください。

## 履歴ファイルを直接再開

claude --resume でセッションを探すか、既知の ID を指定します。プロジェクトや保存先を移動した場合は、JSONL の絶対パスを指定すると選択範囲による問題を避けられます。

```bash
claude --resume "/absolute/path/to/SESSION_ID.jsonl"
```


作業を続けるプロジェクトディレクトリで実行し、例のパスを実在するファイルに置き換えてください。絶対パスによる再開に対応する Claude Code が必要です。古いクライアントは先に更新してください。

## ダウンロードとスキャン

Python 3.10 以降が必要です。[Claude Code ツールをダウンロード](/downloads/kdan-claude-session-recovery.zip)して展開し、kdan-claude-session-recovery に移動します。

```bash
bash find-claude-sessions.sh
```


```powershell
.\Find-ClaudeSessions.ps1
```


CLAUDE_CONFIG_DIR または ~/.claude を使用します。ファイル名の UUID と記録された sessionId を照合し、絶対パスと再開コマンドを出力します。本文やキーの出力、履歴の変更、Claude の自動起動は行いません。

## 移動したプロジェクトと以前の設定ディレクトリ

--claude-home には projects を含む設定ディレクトリを指定します。プロジェクトを移動した場合は、--project-dir に現在存在するディレクトリを指定してください。

```bash
bash find-claude-sessions.sh --claude-home "/path/to/.claude" --project-dir "/path/to/project"
```


```powershell
.\Find-ClaudeSessions.ps1 -ClaudeHome "C:\path\to\.claude" -ProjectDir "C:\path\to\project"
```


PowerShell は -ClaudeHome と -ProjectDir を使用します。元の作業ディレクトリがなく移動先の指定もない場合、実行できない cd コマンドを出力せず、ディレクトリの指定を案内します。

生成するコマンドは CLAUDE_CONFIG_DIR を設定し、JSONL の絶対パスで再開します。そのディレクトリの settings.json が目的のサービスと認証情報を使うことを確認してください。現在の設定を維持する場合は、設定済みの端末で claude --resume にレポートの絶対パスを指定して実行できます。

## 実行と確認

生成されたコマンドを対応する shell にコピーします。元の会話を確認してから、新しいメッセージを明示的に送信して API 接続を確認してください。続行には API 使用量が発生します。PowerShell はディレクトリ変更に失敗すると停止します。

削除済みのファイルの再作成、他のホストやクラウドからの履歴ダウンロード、公式クラウド履歴のゲートウェイへの移行はできません。結果が空の場合は OS ユーザー、元のホスト、設定ディレクトリ、自身のバックアップを確認してください。

[Claude Code CLI --resume](https://code.claude.com/docs/en/cli-reference) · 2026-10-02
