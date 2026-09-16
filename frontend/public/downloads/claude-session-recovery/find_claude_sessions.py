#!/usr/bin/env python3
"""Find resumable local Claude Code sessions without changing Claude data."""

from __future__ import annotations

import argparse
from dataclasses import asdict, dataclass
from datetime import datetime, timezone
import json
import os
from pathlib import Path
import re
import shlex
import sys
from typing import Iterable


UUID_RE = re.compile(
    r"^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$"
)
MAX_RECORDS = 256
MAX_LINE_BYTES = 2 * 1024 * 1024


@dataclass(frozen=True)
class Session:
    session_id: str
    project_key: str
    working_directory: str | None
    working_directory_exists: bool | None
    modified_at: str
    bytes: int


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description=(
            "List validated Claude Code transcript IDs and print resume commands. "
            "This tool is read-only and never prints conversation content."
        )
    )
    parser.add_argument(
        "--claude-home",
        type=Path,
        default=Path(os.environ.get("CLAUDE_CONFIG_DIR", Path.home() / ".claude")),
        help="Claude configuration directory (default: CLAUDE_CONFIG_DIR or ~/.claude)",
    )
    parser.add_argument(
        "--limit",
        type=int,
        default=20,
        help="Maximum sessions to print, newest first; use 0 for all (default: 20)",
    )
    parser.add_argument(
        "--shell",
        choices=("posix", "powershell", "plain"),
        default="powershell" if os.name == "nt" else "posix",
        help="Command syntax to print",
    )
    parser.add_argument("--commands-only", action="store_true", help="Print only resume commands")
    parser.add_argument("--json", action="store_true", help="Print machine-readable metadata")
    return parser.parse_args(argv)


def read_metadata(path: Path) -> tuple[str | None, str | None]:
    session_id = None
    cwd = None
    with path.open("r", encoding="utf-8", errors="replace") as stream:
        for _ in range(MAX_RECORDS):
            line = stream.readline(MAX_LINE_BYTES + 1)
            if not line:
                break
            if len(line) > MAX_LINE_BYTES:
                continue
            try:
                record = json.loads(line)
            except (json.JSONDecodeError, TypeError):
                continue
            if not isinstance(record, dict):
                continue
            candidate_id = record.get("sessionId")
            if isinstance(candidate_id, str) and UUID_RE.fullmatch(candidate_id):
                session_id = candidate_id
            candidate_cwd = record.get("cwd")
            if isinstance(candidate_cwd, str) and candidate_cwd:
                cwd = candidate_cwd
            if session_id and cwd:
                break
    return session_id, cwd


def scan_sessions(claude_home: Path) -> tuple[list[Session], list[str]]:
    projects = claude_home.expanduser().resolve() / "projects"
    if not projects.is_dir():
        raise RuntimeError(f"Claude project history was not found: {projects}")

    sessions: list[Session] = []
    warnings: list[str] = []
    for project in projects.iterdir():
        if project.is_symlink() or not project.is_dir():
            continue
        for transcript in project.glob("*.jsonl"):
            if transcript.is_symlink() or not transcript.is_file():
                continue
            if not UUID_RE.fullmatch(transcript.stem):
                continue
            try:
                session_id, cwd = read_metadata(transcript)
                metadata = transcript.stat(follow_symlinks=False)
            except OSError as exc:
                warnings.append(f"Skipped unreadable transcript {transcript.name}: {exc.strerror or exc}")
                continue
            if session_id != transcript.stem:
                warnings.append(f"Skipped unverified transcript {transcript.name}: session ID did not match")
                continue
            sessions.append(
                Session(
                    session_id=session_id,
                    project_key=project.name,
                    working_directory=cwd,
                    working_directory_exists=Path(cwd).is_dir() if cwd else None,
                    modified_at=datetime.fromtimestamp(metadata.st_mtime, timezone.utc).isoformat(),
                    bytes=metadata.st_size,
                )
            )
    sessions.sort(key=lambda item: item.modified_at, reverse=True)
    return sessions, warnings


def powershell_quote(value: str) -> str:
    return "'" + value.replace("'", "''") + "'"


def resume_command(session: Session, shell: str) -> str:
    cwd = session.working_directory
    if shell == "powershell":
        prefix = f"Set-Location -LiteralPath {powershell_quote(cwd)}; " if cwd else ""
    elif shell == "posix":
        prefix = f"cd -- {shlex.quote(cwd)} && " if cwd else ""
    else:
        prefix = f"[{cwd}] " if cwd else ""
    return f"{prefix}claude --resume {session.session_id}"


def selected_sessions(sessions: list[Session], limit: int) -> Iterable[Session]:
    if limit < 0:
        raise RuntimeError("--limit must be zero or greater")
    return sessions if limit == 0 else sessions[:limit]


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv)
    try:
        sessions, warnings = scan_sessions(args.claude_home)
        selected = list(selected_sessions(sessions, args.limit))
    except RuntimeError as exc:
        print(f"error: {exc}", file=sys.stderr)
        return 2

    if args.json:
        payload = {
            "claude_home": str(args.claude_home.expanduser().resolve()),
            "session_count": len(sessions),
            "sessions": [
                {**asdict(session), "resume_command": resume_command(session, args.shell)}
                for session in selected
            ],
            "warnings": warnings,
        }
        print(json.dumps(payload, ensure_ascii=False, indent=2))
        return 0

    if not args.commands_only:
        print(f"Claude home: {args.claude_home.expanduser().resolve()}")
        print(f"Validated local sessions: {len(sessions)}; showing: {len(selected)}")
        print("Run a command below yourself. Opening a session is not automated.")
    for session in selected:
        print(resume_command(session, args.shell))
    if not args.commands_only:
        for session in selected:
            if session.working_directory and not session.working_directory_exists:
                print(
                    f"warning: recorded working directory no longer exists for {session.session_id}: "
                    f"{session.working_directory}",
                    file=sys.stderr,
                )
        for warning in warnings:
            print(f"warning: {warning}", file=sys.stderr)
        if not sessions:
            print("No validated local Claude Code sessions were found.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
