"""Prepare explicit CLI recovery using local transcripts and the current provider."""
from __future__ import annotations

import json
import os
from pathlib import Path
import shlex
import subprocess

from repair_sessions import checked_existing_path, read_session_id, rollout_roots, safe_rollout_candidate


def current_config(home: Path, provider: str | None, model: str | None) -> tuple[str, str | None]:
    try:
        import tomllib
    except ImportError as exc:
        raise RuntimeError("Session resume requires Python 3.11+ to read config.toml") from exc
    path = home / "config.toml"
    if not path.is_file():
        raise RuntimeError("Configure the current Codex provider first: config.toml was not found")
    try:
        config = tomllib.loads(path.read_text(encoding="utf-8"))
    except (ValueError, UnicodeError) as exc:
        # Parser errors can include source lines containing credentials.
        raise RuntimeError("config.toml is invalid; fix its TOML syntax before resuming") from exc
    profile = config.get("profile")
    profile_config = config.get("profiles", {}).get(profile, {}) if profile else {}
    target = provider or profile_config.get("model_provider") or config.get("model_provider", "openai")
    selected_model = model or profile_config.get("model") or config.get("model")
    if not isinstance(target, str) or not target.strip():
        raise RuntimeError("The selected model_provider must be a nonempty provider ID")
    if target not in {"openai", "ollama", "lmstudio"} and target not in config.get("model_providers", {}):
        raise RuntimeError(f"Model provider {target!r} is not defined in config.toml (IDs are case-sensitive)")
    if selected_model is not None and not isinstance(selected_model, str):
        raise RuntimeError("The selected model must be a string")
    return target, selected_model


def local_sessions(home: Path) -> tuple[list[dict], list[str]]:
    found: dict[str, list[dict]] = {}
    warnings: list[str] = []
    for root in rollout_roots(home):
        for path in root.rglob("*.jsonl"):
            try:
                path, _ = checked_existing_path(path, root, "file", "transcript")
                session_id = read_session_id(path)
                if not session_id or safe_rollout_candidate(path, root, session_id) is None:
                    continue
                with path.open(encoding="utf-8") as stream:
                    record = next(json.loads(line) for line in stream if line.strip())
                meta = record["payload"]
                found.setdefault(session_id, []).append({
                    "session_id": session_id,
                    "transcript_path": str(path),
                    "recorded_provider": meta.get("model_provider"),
                    "working_directory": meta.get("cwd"),
                    "modified_at": path.stat().st_mtime,
                })
            except (OSError, RuntimeError, ValueError, StopIteration):
                warnings.append(f"Skipped unreadable or invalid transcript: {path.name}")
    sessions = []
    for session_id, records in found.items():
        if len(records) != 1:
            warnings.append(f"Ambiguous session {session_id}: {len(records)} transcripts; no resume command generated")
        else:
            sessions.extend(records)
    return sorted(sessions, key=lambda item: item["modified_at"], reverse=True), warnings


def invocation(session: dict, provider: str, model: str | None, project_dir: Path | None) -> tuple[list[str], Path]:
    original = session.get("working_directory")
    cwd = project_dir or (Path(original).expanduser() if isinstance(original, str) and original else None)
    if cwd is None or not cwd.is_absolute() or not cwd.is_dir():
        raise RuntimeError("Original project directory is unavailable; supply --project-dir with its current absolute path")
    cwd = cwd.resolve()
    command = ["codex", "resume", session["session_id"], "--cd", str(cwd), "-c",
               "model_provider=" + json.dumps(provider, ensure_ascii=True)]
    if model:
        command.extend(["--model", model])
    return command, cwd


def quoted_command(home: Path, command: list[str], shell: str) -> str:
    if shell == "powershell":
        quote = lambda value: "'" + value.replace("'", "''") + "'"
        return f"$env:CODEX_HOME = {quote(str(home))}; & " + " ".join(quote(item) for item in command)
    return "env CODEX_HOME=" + shlex.quote(str(home)) + " " + shlex.join(command)


def run(args) -> int:
    try:
        if args.apply or args.rollback or args.dry_run or (args.list and args.resume):
            raise RuntimeError("Choose either session discovery/resume or index diagnosis/repair")
        if args.run and not args.resume:
            raise RuntimeError("--run requires --resume SESSION_ID")
        home = args.codex_home.expanduser().resolve(strict=True)
        project = args.project_dir.expanduser().resolve(strict=True) if args.project_dir else None
        provider, model = current_config(home, args.provider, args.model)
        sessions, warnings = local_sessions(home)
        if args.resume:
            sessions = [session for session in sessions if session["session_id"] == args.resume]
            if len(sessions) != 1:
                raise RuntimeError("No unique local transcript matches that session ID. Check CODEX_HOME and duplicate files")
        for session in sessions:
            try:
                command, _ = invocation(session, provider, model, project)
                session["resume_command"] = quoted_command(home, command, args.shell)
            except RuntimeError as exc:
                session["resume_command"] = None
                session["error"] = str(exc)
        if args.run:
            command, cwd = invocation(sessions[0], provider, model, project)
            env = dict(os.environ, CODEX_HOME=str(home))
            # No prompt, automatic message, auth-file edits, or database writes.
            return subprocess.run(command, cwd=cwd, env=env, check=False).returncode
        print(json.dumps({
            "mode": "resume-preview" if args.resume else "list",
            "codex_home": str(home), "provider": provider, "model": model,
            "sessions": sessions, "warnings": warnings,
            "note": "Commands use the current configured provider for this CLI launch only. Review before running. Desktop filters and credentials are not changed.",
        }, ensure_ascii=False, indent=2))
        return 2 if args.resume and sessions[0].get("error") else 0
    except (OSError, RuntimeError) as exc:
        print(json.dumps({"error": str(exc)}, ensure_ascii=False, indent=2))
        return 2
