# KDAN Claude Code session recovery

This package scans Claude Code's local transcript directory and prints exact
commands for sessions that can still be resumed. It is useful when a session is
not visible after changing an API key, account, project directory, or
`CLAUDE_CONFIG_DIR`.

The scanner is read-only. It does not contact KDAN or Anthropic, start a
Claude session, read message text, or change transcripts, settings, credentials,
retention, or account data. Its report contains local project paths and session
IDs, so review it before sharing.

macOS / Linux:

```bash
./find-claude-sessions.sh
```

Windows PowerShell:

```powershell
.\Find-ClaudeSessions.ps1
```

The default location is `CLAUDE_CONFIG_DIR`, otherwise `~/.claude`. To scan an
older location, use `--claude-home /path/to/.claude` or `-ClaudeHome` in
PowerShell. Use `--limit 0` or `-Limit 0` to list every validated session.

Each command changes to the transcript's recorded working directory, when that
metadata exists, and then runs `claude --resume <session-id>`. Review and run the
chosen command yourself. The scanner never invokes `claude` because continuing
the conversation can make an API request after you send a message.

If a command points to a working directory that no longer exists, restore the
project at that path before resuming. The report flags this condition explicitly.

Claude Code stores CLI transcripts under
`~/.claude/projects/<project>/<session-id>.jsonl` for 30 days by default. The
scanner accepts only UUID-named regular files whose embedded `sessionId` matches
the filename. Missing, expired, deleted, cloud-only, or other OS user's sessions
cannot be restored by this package.

Official references:

- https://code.claude.com/docs/en/sessions
- https://code.claude.com/docs/en/claude-directory
