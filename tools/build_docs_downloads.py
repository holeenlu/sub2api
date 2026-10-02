#!/usr/bin/env python3
"""Build reproducible KDAN documentation downloads; --check rejects stale archives."""

from __future__ import annotations

import argparse
import hashlib
import io
from pathlib import Path
import stat
import zipfile

DOWNLOADS = Path(__file__).resolve().parents[1] / "frontend/public/downloads"
PACKAGES = {
    "kdan-codex-session-repair": (
        "kdan-codex-session-repair",
        ("README.md", "repair_sessions.py", "resume_sessions.py", "repair-sessions.sh", "RepairSessions.ps1"),
    ),
    "kdan-claude-session-recovery": (
        "kdan-claude-session-recovery",
        ("README.md", "find_claude_sessions.py", "find-claude-sessions.sh", "Find-ClaudeSessions.ps1"),
    ),
}


def archive_bytes(name: str) -> bytes:
    source, files = PACKAGES[name]
    output = io.BytesIO()
    with zipfile.ZipFile(output, "w", compression=zipfile.ZIP_DEFLATED, compresslevel=9) as archive:
        for relative in sorted(files):
            path = DOWNLOADS / source / relative
            if path.is_symlink() or not path.is_file():
                raise RuntimeError(f"Expected regular package source: {path}")
            info = zipfile.ZipInfo(f"{name}/{relative}", date_time=(2026, 1, 1, 0, 0, 0))
            info.create_system = 3
            mode = 0o755 if path.suffix in {".py", ".sh"} else 0o644
            info.external_attr = (stat.S_IFREG | mode) << 16
            info.compress_type = zipfile.ZIP_DEFLATED
            archive.writestr(info, path.read_bytes(), compresslevel=9)
    return output.getvalue()


def build(check: bool = False) -> None:
    outputs = {f"{name}.zip": archive_bytes(name) for name in PACKAGES}
    outputs["SHA256SUMS.txt"] = "".join(
        f"{hashlib.sha256(data).hexdigest()}  {name}\n" for name, data in sorted(outputs.items())
    ).encode()
    for name, data in outputs.items():
        destination = DOWNLOADS / name
        if check:
            if not destination.is_file() or destination.read_bytes() != data:
                raise RuntimeError(f"Stale download: {name}; run python3 tools/build_docs_downloads.py")
        else:
            destination.write_bytes(data)
        print(f"{'verified' if check else 'built'} {name}")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true")
    build(parser.parse_args().check)
