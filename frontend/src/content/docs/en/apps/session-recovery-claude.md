This tool works with local Claude Code JSONL transcripts. For Codex, use the [Codex tool](/apps/session-recovery-codex). Official cloud history requires the original account or host.

## Rule out connection problems first

Check a new session with your current key and model. Authentication, 503, unavailable-model, and balance errors need connection, permission, or server troubleshooting. Use the steps below when existing local history is missing from the session list.

## Resume a transcript directly

Run claude --resume to find a session, or provide a known ID. For a moved project or history in another directory, use an absolute JSONL path to avoid picker scope issues:

```bash
claude --resume "/absolute/path/to/SESSION_ID.jsonl"
```


Run this in the project directory where you want to continue. Replace the example with a real local file. Your Claude Code version must support resuming by absolute transcript path; update older clients first.

## Download and scan

Requires Python 3.10+. [Download the Claude Code tool](/downloads/kdan-claude-session-recovery.zip), extract it, and enter kdan-claude-session-recovery:

```bash
bash find-claude-sessions.sh
```


```powershell
.\Find-ClaudeSessions.ps1
```


The scan uses CLAUDE_CONFIG_DIR or ~/.claude. It checks each filename UUID against the recorded sessionId and prints verified absolute paths and resume commands. It does not print message content or keys, edit transcripts, or automatically launch Claude.

## Moved projects and old configuration directories

Use --claude-home to select a configuration directory containing projects. If the project moved, use --project-dir for its existing current directory:

```bash
bash find-claude-sessions.sh --claude-home "/path/to/.claude" --project-dir "/path/to/project"
```


```powershell
.\Find-ClaudeSessions.ps1 -ClaudeHome "C:\path\to\.claude" -ProjectDir "C:\path\to\project"
```


PowerShell uses -ClaudeHome and -ProjectDir. If the original project directory is missing and no replacement is supplied, the tool asks for a directory instead of printing a broken cd command.

Generated commands set CLAUDE_CONFIG_DIR and resume by absolute transcript path. Check that settings.json in that directory uses your intended provider and credentials. To keep your current client configuration, you can instead run claude --resume with the absolute transcript path in an already configured terminal.

## Run and verify

Copy a generated command into the matching shell. Check that the original conversation opens, then send an explicit new message to verify the API connection. Continuing creates API usage. PowerShell commands stop if changing directories fails.

The tool cannot recreate deleted files, download remote or cloud history, or guarantee migration of official-account cloud conversations to a gateway. An empty scan requires checking the OS user, original host, configuration directory, and your backups.

[Claude Code CLI --resume](https://code.claude.com/docs/en/cli-reference) · 2026-10-02
