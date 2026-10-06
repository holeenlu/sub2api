# KDAN Codex session recovery

Recover existing local sessions; this does not repair networking or recreate deleted messages. Claude Code users should use the [Claude tool](https://nextcode.buildtoconnect.com/apps/session-recovery-claude).

## Prepare and choose a recovery path

Copy valid settings for your current group from [API Keys](https://nextcode.buildtoconnect.com/keys) and check that a new session works. Authentication, balance, 503, and upstream errors need configuration or server troubleshooting. Keep the original session directory.

Requires Python 3.11+ and an installed Codex CLI. [Download the tool](https://nextcode.buildtoconnect.com/downloads/kdan-codex-session-repair.zip), extract it, and enter kdan-codex-session-repair. It supports macOS, Linux, and Windows.

## Find sessions and prepare a resume command

Start with a read-only scan. It reads sessions and archived_sessions directly without requiring a SQLite index, and never prints message content or keys. Reports include session IDs and local paths; review before sharing.

```bash
bash repair-sessions.sh --list
```


```powershell
.\RepairSessions.ps1 -List
```


Choose an ID from the report. If the project moved, provide its current path. These commands only preview a command; they do not launch a client:

```bash
bash repair-sessions.sh --resume "SESSION_ID" --project-dir "/path/to/project"
```


```powershell
.\RepairSessions.ps1 -Resume "SESSION_ID" -ProjectDir "C:\path\to\project"
```


The preview uses the current config.toml provider and model, including its selected profile, as overrides for this CLI launch. Use --provider for an existing configured provider ID and --model for a model your key can use. Provider IDs are case-sensitive. Fix missing or invalid configuration first.

After reviewing the provider, model, and directory, add --run to open that CLI session. The tool sends no prompt automatically; continuing the conversation creates normal API usage. This does not rewrite desktop provider filters, history, or archive status.

## Repair an invalid index path

If a local JSONL exists but the client index points to an old directory, run the diagnostic below. It uses CODEX_HOME or ~/.codex; select another directory with --codex-home. When multiple state_*.sqlite files exist, explicitly choose the current client database with --database.

```bash
bash repair-sessions.sh --dry-run
```


Review repairable_rollout_paths, then close every Codex desktop, CLI, and IDE process using that directory before applying:

```bash
bash repair-sessions.sh --apply --client-closed
```


```powershell
.\RepairSessions.ps1 -Apply -ClientClosed
```


The tool validates a unique candidate by its JSONL session ID, including renamed transcripts and paths moved between operating systems. It creates a consistent SQLite backup and changes only rollout_path. Duplicate candidates, symlinks, unknown schemas, and corruption are not guessed around.

## Verify and roll back

Check repaired_rollout_paths: 0 means nothing changed, not that recovery succeeded. Rerun diagnostics and open the session to verify. Preview rollback first, then close the clients and apply:

```bash
bash repair-sessions.sh --rollback "/path/from/backup/report"
bash repair-sessions.sh --rollback "/path/from/backup/report" --apply --client-closed
```


PowerShell equivalents are -Database, -CodexHome, -Rollback, -Apply, and -ClientClosed. Resume options are -Resume, -Provider, -Model, -ProjectDir, and -Run. Rollback only reverses matching paths written by that repair and preserves later unrelated changes.

Deleted messages and sessions stored only on another host or in official cloud history require the original host or your own backup. Unknown database versions are reported without forced edits.

[Codex CLI resume](https://developers.openai.com/codex/cli/reference/) · 2026-10-02
