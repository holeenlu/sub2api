Codex users should follow [Codex session recovery](/apps/session-recovery-codex). Claude Code sessions are not Codex SQLite data and must not be passed to the Codex repair utility.

## Identify what disappeared

| Symptom | First action |
| --- | --- |
| Empty picker in the current project | Return to the project and run `claude --resume`; widen with `Ctrl+W` or `Ctrl+A` |
| Different OS user, host, or `CLAUDE_CONFIG_DIR` | Locate the original Claude config directory; local sessions do not automatically move between hosts |
| Project or worktree path changed | Restore the original working directory, then resume by session ID |
| Missing after changing a TapModels key or login | Keys do not move local files; check the OS user, config directory, and project path |
| Deleted, expired, or cloud-only history | The local scanner cannot recreate it; use the original host, client, or backup |

Claude Code CLI stores sessions at `~/.claude/projects/<project>/<session-id>.jsonl`, or below `CLAUDE_CONFIG_DIR` when configured. Local records older than 30 days can be cleaned up by default.

## Try Claude Code first

Return to the original project directory and run:

```bash
claude --resume
```

`claude --continue` resumes the newest session in the current directory, and `/resume` opens the picker from a session. The picker starts with the current project; press `Ctrl+W` for other worktrees in the repository or `Ctrl+A` for every project on this machine. With a known ID, use:

```bash
claude --resume <session-id>
```

## Download the read-only recovery utility

[Download the TapModels Claude Code session recovery bundle](/downloads/tapmodels-claude-session-recovery.zip). On macOS or Linux:

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

The scanner accepts only transcript filename UUIDs that match their embedded `sessionId`, then prints an exact `claude --resume <session-id>` command with the original working directory. It does not start Claude, read or print message text, or change JSONL, accounts, keys, or settings. Reports contain local paths and session IDs, so review them before sharing.

## Scan another directory and resume

To scan an older config directory:

```bash
bash find-claude-sessions.sh --claude-home "/old/.claude"
```

PowerShell uses `-ClaudeHome`. If the report says the recorded working directory is missing, restore the project at that path before manually running the generated command. Opening a session does not send a message; sending a follow-up makes a model request.

The utility cannot recreate deleted, expired, other-host, or cloud-only sessions, and it does not alter Claude retention or account ownership.

Sources: [Claude Code session management](https://code.claude.com/docs/en/sessions) and [Claude's local data directory](https://code.claude.com/docs/en/claude-directory). The utility was tested on temporary fixtures, not real local Claude history.
