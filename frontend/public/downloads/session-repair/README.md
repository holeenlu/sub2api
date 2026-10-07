# project Codex session repair

This package diagnoses Codex's local thread database. It does not contact
project and never reads, prints, deletes, or rewrites API credentials, session
JSONL content, or archive state.

The default run is read-only:

```bash
./repair-sessions.sh --dry-run
```

The tool selects the only `state_*.sqlite` file directly under the Codex home.
If multiple state databases exist, select one explicitly with
`--database state_5.sqlite` (or the actual filename) after checking which
client version created it. PowerShell accepts the equivalent `-Database`.

The report verifies the database schema and integrity, lists thread/archive
counts and provider metadata, and inspects stale rollout paths. A replacement
candidate is considered repairable only when it is a regular file under
`~/.codex/sessions` or `~/.codex/archived_sessions`, contains no symlink path
components, and its first non-empty JSONL record is a `session_meta` whose
`payload.id` exactly matches the database thread ID.

After reviewing the report, fully quit Codex desktop, CLI, IDE extensions, and
any other process using this Codex home. Then explicitly confirm that state:

```bash
./repair-sessions.sh --apply --client-closed
```

The tool first obtains a SQLite write lock and scans again. While that lock
blocks concurrent writers, a separate read-only connection creates a consistent
SQLite backup. The same transaction changes only proven stale
`threads.rollout_path` values. Before commit, a logical digest of every user
table, column, row, and schema object must match the locked snapshot after only
those approved path values are normalized. Any validation failure rolls the
transaction back. The backup directory is created with mode `0700` under
`~/.codex/backups/sub2api-session-repair-*`; the database copy uses mode
`0600`, preserves the selected database filename, and includes a selective
rollback manifest. Credentials are not copied into the backup.

Database, rollout, backup, and manifest paths must stay under the resolved Codex
home. The tool rejects `..`, symbolic links in checked path components, and
files whose device/inode identity changes during an operation.

On Windows, run the equivalent commands in PowerShell:

```powershell
.\RepairSessions.ps1 -DryRun
.\RepairSessions.ps1 -Apply -ClientClosed
```

Preview a rollback without changing data:

```bash
./repair-sessions.sh --rollback ~/.codex/backups/sub2api-session-repair-YYYYMMDD-HHMMSS-NNNNNNNNN
```

After reviewing it and closing every client again, apply it explicitly:

```bash
./repair-sessions.sh --rollback ~/.codex/backups/sub2api-session-repair-YYYYMMDD-HHMMSS-NNNNNNNNN --apply --client-closed
```

PowerShell uses `-Rollback <backup-directory> -Apply -ClientClosed`. Rollback
restores only this tool's recorded `rollout_path` cells and only when every cell
still equals the value written by that repair. A mismatch aborts the transaction,
so newer paths and unrelated data are preserved. The current database is backed
up under `sub2api-session-rollback-safety-*` before any rollback updates.

Provider values are diagnostic only. This tool does not rewrite them because a
database-only provider change can disagree with rollout metadata and can be
lost when Codex rebuilds its index. Unknown database schemas, corrupt databases,
ambiguous candidates, missing JSONL records, changed or wrong-home backups, and
symlinked paths fail closed.
Keep the report and backup for support instead of deleting the database.
