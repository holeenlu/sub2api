Claude Code users should follow [Claude Code session recovery](/apps/session-recovery-claude). The clients use different data formats and their repair tools are not interchangeable.

## Identify what disappeared

| Symptom | First action |
| --- | --- |
| Empty list after changing projects | Return to the project, try `codex resume --all`, and check archives |
| Different OS user, host, or CODEX_HOME | Locate the original data directory; remote and local history differ |
| Old tasks hidden after renaming a provider | Compare old and current configuration and restore the original provider identity |
| JSONL exists but the index points to an old path | Diagnose with the utility; repair only proven paths |
| Deleted files or official account cloud history | A local index repair cannot restore them; use the original account or backups |

Rotating a key is different from changing an account, provider ID, or data directory. Do not delete authentication files, SQLite databases, or `.codex` while troubleshooting.

## Try Codex first

Use `codex resume` in the original project, `codex resume --all` across directories, or `codex resume <session-id>`. In desktop, check the original project and archived tasks. Do not automate sending a follow-up because a new message makes a model request.

## Provider changes

If only `model_provider` changed, inspect the original configuration backup. Restore that identifier, update the corresponding provider endpoint/key to TapModels, restart, and try the old task. Never send a new key to an old provider's unrelated endpoint.

Account/provider filtering varies by client version. The utility reports provider distribution without rewriting provider identities or assigning one official account's data to another.

## Download and diagnose

Python 3.10+ is required. [Download the TapModels Codex session repair utility](/downloads/tapmodels-codex-session-repair.zip), extract it, and enter `tapmodels-codex-session-repair`.

macOS / Linux:

```bash
bash repair-sessions.sh --dry-run
```

Windows PowerShell:

```powershell
.\Repair-TapModelsSessions.ps1 -DryRun
```

The default home is CODEX_HOME, otherwise `~/.codex`. Override it with `--codex-home` or `-CodexHome`. The utility selects a unique `state_*.sqlite`; if several exist, identify the active database and use `--database state_5.sqlite` or `-Database` with the actual filename.

The report contains schema/integrity, thread/archive counts, provider distribution, and missing paths. It excludes message bodies and keys but still contains local paths and session IDs.

## Repair proven paths

Review the report and quit all desktop, CLI, and IDE processes using that directory before applying:

```bash
bash repair-sessions.sh --apply --client-closed
```

```powershell
.\Repair-TapModelsSessions.ps1 -Apply -ClientClosed
```

Only a unique in-tree JSONL whose first `session_meta.payload.id` matches the thread can replace `rollout_path`. The tool obtains a write lock, scans again, creates a consistent SQLite backup, and validates every table and column before commit. Database, rollout, and backup paths reject `..`, symbolic links, and files replaced during the operation. Conversations, credentials, configuration, providers, and archive flags are unchanged. Failed validation rolls back the transaction.

Missing files, unknown schema, corruption, ambiguous candidates, and symlinks are not guessed. The tool cannot recreate deleted messages or repair every client version's account filtering.

## Verify and roll back

Inspect `repaired_rollout_paths`, rerun diagnostics, and open the client. Preview rollback first:

```bash
bash repair-sessions.sh --rollback "/path/from/the/backup/report"
```

After reviewing the preview and closing every client, apply it explicitly:

```bash
bash repair-sessions.sh --rollback "/path/from/the/backup/report" --apply --client-closed
```

PowerShell uses `-Rollback "path" -Apply -ClientClosed`. Compare-and-set checks restore only paths written by that repair; changed paths, replaced backups, or ownership mismatches abort the rollback.

Source: [Codex CLI resume reference](https://developers.openai.com/codex/cli/reference/). The utility was tested on temporary databases, not real local session data.
