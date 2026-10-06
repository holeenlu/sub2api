#!/usr/bin/env python3
"""Diagnose, repair, and selectively roll back Codex rollout paths."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
from pathlib import Path
import sqlite3
import time
from typing import Any, NamedTuple


REQUIRED_THREAD_COLUMNS = {"id", "rollout_path", "archived"}
BACKUP_PREFIX = "KDAN-session-repair-"
SAFETY_PREFIX = "KDAN-session-rollback-safety-"
MANIFEST_NAME = "backup-manifest.json"


class FileIdentity(NamedTuple):
    device: int
    inode: int


class RolloutCandidate(NamedTuple):
    path: Path
    identity: FileIdentity


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Diagnose and repair Codex session rollout paths")
    parser.add_argument(
        "--codex-home",
        type=Path,
        default=Path(os.environ.get("CODEX_HOME", Path.home() / ".codex")),
    )
    parser.add_argument(
        "--database",
        type=Path,
        help="Explicit state database under the Codex home (default: the only state_*.sqlite file)",
    )
    parser.add_argument("--dry-run", action="store_true", help="Run the default read-only diagnostic")
    parser.add_argument("--apply", action="store_true", help="Apply a repair or requested rollback")
    parser.add_argument(
        "--rollback",
        type=Path,
        metavar="BACKUP_DIR",
        help="Preview selective rollback from a backup made by this tool",
    )
    parser.add_argument(
        "--client-closed",
        action="store_true",
        help="Confirm all Codex desktop, CLI, and IDE processes using this home are closed",
    )
    parser.add_argument("--json", action="store_true", help="Emit machine-readable diagnostics")
    parser.add_argument("--list", action="store_true", help="List local transcripts and CLI resume commands without a state database")
    parser.add_argument("--resume", metavar="SESSION_ID", help="Prepare a CLI resume command with the current configured provider")
    parser.add_argument("--provider", help="Use this existing provider ID from config.toml for CLI resume")
    parser.add_argument("--model", help="Use this accessible model for CLI resume")
    parser.add_argument("--project-dir", type=Path, help="Current project path if the original directory moved")
    parser.add_argument("--run", action="store_true", help="Launch the selected CLI session; requires --resume")
    parser.add_argument("--shell", choices=("posix", "powershell"), default="powershell" if os.name == "nt" else "posix")
    return parser.parse_args(argv)


def file_identity(path: Path) -> FileIdentity:
    stat_result = path.stat(follow_symlinks=False)
    return FileIdentity(stat_result.st_dev, stat_result.st_ino)


def verify_identity(path: Path, expected: FileIdentity, label: str) -> None:
    try:
        actual = file_identity(path)
    except OSError as exc:
        raise RuntimeError(f"{label} disappeared during the operation") from exc
    if actual != expected:
        raise RuntimeError(f"{label} was replaced during the operation")


def path_within_root(path: Path, root: Path) -> bool:
    try:
        path.relative_to(root)
        return True
    except ValueError:
        return False


def checked_existing_path(path: Path, root: Path, kind: str, label: str) -> tuple[Path, FileIdentity]:
    """Check lexical containment, every component, resolved containment, and type."""
    if ".." in path.parts:
        raise RuntimeError(f"{label} must not contain '..' path components")
    if not path.is_absolute():
        path = root / path
    lexical = Path(os.path.abspath(path))
    if not path_within_root(lexical, root):
        raise RuntimeError(f"{label} must be under {root}")
    try:
        resolved = lexical.resolve(strict=True)
        if not path_within_root(resolved, root):
            raise RuntimeError(f"{label} resolves outside {root}")
        current = root
        for component in lexical.relative_to(root).parts:
            current = current / component
            current.stat(follow_symlinks=False)
            if current.is_symlink():
                raise RuntimeError(f"{label} contains a symbolic-link path component")
    except OSError as exc:
        raise RuntimeError(f"{label} does not exist or cannot be inspected") from exc
    if kind == "file" and not resolved.is_file():
        raise RuntimeError(f"{label} must be a regular file")
    if kind == "dir" and not resolved.is_dir():
        raise RuntimeError(f"{label} must be a directory")
    identity = file_identity(resolved)
    verify_identity(resolved, identity, label)
    return resolved, identity


def read_session_id(path: Path) -> str | None:
    try:
        with path.open("r", encoding="utf-8") as handle:
            for line in handle:
                if not line.strip():
                    continue
                item = json.loads(line)
                if not isinstance(item, dict) or item.get("type") != "session_meta":
                    return None
                payload = item.get("payload")
                session_id = payload.get("id") if isinstance(payload, dict) else None
                return session_id if isinstance(session_id, str) and session_id else None
    except (OSError, UnicodeError, json.JSONDecodeError):
        return None
    return None


def safe_rollout_candidate(path: Path, root: Path, thread_id: str) -> RolloutCandidate | None:
    try:
        resolved, identity = checked_existing_path(path, root, "file", "rollout candidate")
        if resolved.suffix != ".jsonl" or read_session_id(resolved) != thread_id:
            return None
        verify_identity(resolved, identity, "rollout candidate")
        return RolloutCandidate(resolved, identity)
    except (OSError, RuntimeError, ValueError):
        return None


def safe_rollout_file(path: Path, root: Path, thread_id: str) -> bool:
    return safe_rollout_candidate(path, root, thread_id) is not None


def rollout_roots(home: Path) -> list[Path]:
    roots: list[Path] = []
    for path in (home / "sessions", home / "archived_sessions"):
        try:
            resolved, _ = checked_existing_path(path, home, "dir", "rollout root")
            roots.append(resolved)
        except RuntimeError:
            continue
    return roots


def find_candidate_records(home: Path, rollout_path: str, thread_id: str) -> list[RolloutCandidate]:
    raw = Path(rollout_path).expanduser()
    if ".." in raw.parts:
        return []
    candidates: dict[Path, RolloutCandidate] = {}
    for root in rollout_roots(home):
        try:
            for path in root.rglob("*.jsonl"):
                # A copied/renamed transcript (including Windows -> Unix moves)
                # is identified by session_meta.id, never by its old basename.
                candidate = safe_rollout_candidate(path, root, thread_id)
                if candidate is not None:
                    candidates[candidate.path] = candidate
        except OSError:
            continue
    return [candidates[path] for path in sorted(candidates)]


def find_candidates(home: Path, rollout_path: str, thread_id: str) -> list[Path]:
    return [candidate.path for candidate in find_candidate_records(home, rollout_path, thread_id)]


def table_schema(connection: sqlite3.Connection, table: str) -> dict[str, tuple[str, int, int]]:
    escaped = table.replace("'", "''")
    return {
        str(row[1]): (str(row[2]).upper(), int(row[3]), int(row[5]))
        for row in connection.execute(f"PRAGMA table_info('{escaped}')")
    }


def schema_status(connection: sqlite3.Connection) -> tuple[bool, set[str]]:
    schema = table_schema(connection, "threads")
    columns = set(schema)
    core_shape = (
        schema.get("id") == ("TEXT", 0, 1)
        and schema.get("rollout_path") == ("TEXT", 1, 0)
        and schema.get("archived") == ("INTEGER", 1, 0)
    )
    return REQUIRED_THREAD_COLUMNS.issubset(columns) and core_shape, columns


def integrity(connection: sqlite3.Connection) -> str:
    row = connection.execute("PRAGMA integrity_check").fetchone()
    return str(row[0]) if row else "unavailable"


def thread_invariants(connection: sqlite3.Connection) -> dict[str, Any]:
    return {
        "threads": int(connection.execute("SELECT count(*) FROM threads").fetchone()[0]),
        "archive_distribution": {
            str(archived): int(count)
            for archived, count in connection.execute(
                "SELECT archived, count(*) FROM threads GROUP BY archived ORDER BY archived"
            )
        },
    }


def scan_connection(home: Path, connection: sqlite3.Connection) -> dict[str, Any]:
    supported, columns = schema_status(connection)
    result: dict[str, Any] = {
        "schema_supported": supported,
        "thread_columns": sorted(columns),
        "integrity": integrity(connection),
        "missing_rollout_paths": [],
        "repairable_rollout_paths": [],
        "ambiguous_rollout_paths": [],
    }
    if not supported:
        result["schema_error"] = f"threads table must contain {sorted(REQUIRED_THREAD_COLUMNS)}"
        return result
    result.update(thread_invariants(connection))
    if "model_provider" in columns:
        result["providers"] = dict(
            connection.execute("SELECT model_provider, count(*) FROM threads GROUP BY model_provider")
        )
    roots = rollout_roots(home)
    for thread_id, rollout_path in connection.execute(
        "SELECT id, rollout_path FROM threads WHERE rollout_path IS NOT NULL AND rollout_path <> ''"
    ):
        thread_id = str(thread_id)
        rollout_path = str(rollout_path)
        raw_path = Path(rollout_path).expanduser()
        if ".." not in raw_path.parts and any(safe_rollout_file(raw_path, root, thread_id) for root in roots):
            continue
        candidates = find_candidates(home, rollout_path, thread_id)
        item = {"id": thread_id, "rollout_path": rollout_path, "candidates": [str(path) for path in candidates]}
        result["missing_rollout_paths"].append(item)
        if len(candidates) == 1:
            result["repairable_rollout_paths"].append(item)
        elif len(candidates) > 1:
            result["ambiguous_rollout_paths"].append(item)
    return result


def open_readonly(db: Path, expected_identity: FileIdentity | None = None) -> sqlite3.Connection:
    if expected_identity is not None:
        verify_identity(db, expected_identity, "state database")
    connection = sqlite3.connect(f"{db.as_uri()}?mode=ro", uri=True)
    try:
        connection.execute("PRAGMA query_only = ON")
        if expected_identity is not None:
            verify_identity(db, expected_identity, "state database")
        return connection
    except Exception:
        connection.close()
        raise


def snapshot(home: Path, db: Path, expected_identity: FileIdentity | None = None) -> dict[str, Any]:
    result: dict[str, Any] = {"codex_home": str(home), "database": str(db), "database_exists": db.is_file()}
    try:
        _, identity = checked_existing_path(db, home, "file", "state database")
        if expected_identity is not None and identity != expected_identity:
            raise RuntimeError("state database was replaced during the operation")
        with open_readonly(db, identity) as connection:
            result.update(scan_connection(home, connection))
        verify_identity(db, identity, "state database")
    except (OSError, RuntimeError, sqlite3.Error) as exc:
        result.update({"schema_supported": False, "integrity": "unavailable", "schema_error": str(exc)})
    return result


def quoted_identifier(value: str) -> str:
    return '"' + value.replace('"', '""') + '"'


def encoded_value(value: Any) -> bytes:
    if value is None:
        return b"n"
    if isinstance(value, bytes):
        return b"b" + len(value).to_bytes(8, "big") + value
    data = str(value).encode("utf-8", "surrogatepass")
    marker = b"i" if isinstance(value, int) else b"f" if isinstance(value, float) else b"s"
    return marker + len(data).to_bytes(8, "big") + data


def database_digest(connection: sqlite3.Connection, rollout_overrides: dict[str, str] | None = None) -> str:
    """Hash all user schema and every row, preserving duplicate row counts."""
    digest = hashlib.sha256()
    for row in connection.execute(
        "SELECT type, name, tbl_name, coalesce(sql, '') FROM sqlite_master "
        "WHERE name NOT LIKE 'sqlite_%' ORDER BY type, name, tbl_name, sql"
    ):
        digest.update(b"schema\0")
        for value in row:
            digest.update(encoded_value(value))
    tables = [row[0] for row in connection.execute(
        "SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name"
    )]
    for table in tables:
        cursor = connection.execute(f"SELECT * FROM {quoted_identifier(str(table))}")
        columns = [str(item[0]) for item in cursor.description or ()]
        id_index = columns.index("id") if table == "threads" and "id" in columns else None
        path_index = columns.index("rollout_path") if table == "threads" and "rollout_path" in columns else None
        row_hashes: list[bytes] = []
        for raw_row in cursor:
            row = list(raw_row)
            if rollout_overrides and id_index is not None and path_index is not None:
                replacement = rollout_overrides.get(str(row[id_index]))
                if replacement is not None:
                    row[path_index] = replacement
            row_digest = hashlib.sha256()
            for value in row:
                row_digest.update(encoded_value(value))
            row_hashes.append(row_digest.digest())
        digest.update(b"table\0" + str(table).encode("utf-8") + b"\0")
        for row_hash in sorted(row_hashes):
            digest.update(row_hash)
    return digest.hexdigest()


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def ensure_backup_root(home: Path) -> tuple[Path, FileIdentity]:
    backups = home / "backups"
    try:
        backups.mkdir(mode=0o700)
    except FileExistsError:
        pass
    resolved, identity = checked_existing_path(backups, home, "dir", "backup root")
    os.chmod(resolved, 0o700)
    verify_identity(resolved, identity, "backup root")
    return resolved, identity


def create_locked_backup(
    home: Path,
    db: Path,
    writer: sqlite3.Connection,
    db_identity: FileIdentity,
    prefix: str = BACKUP_PREFIX,
) -> dict[str, Any]:
    """Use another reader for backup while this connection holds BEGIN IMMEDIATE."""
    verify_identity(db, db_identity, "state database")
    backup_root, root_identity = ensure_backup_root(home)
    stamp = f"{time.strftime('%Y%m%d-%H%M%S')}-{time.time_ns() % 1_000_000_000:09d}"
    backup_dir = backup_root / f"{prefix}{stamp}"
    backup_dir.mkdir(mode=0o700)
    backup_dir, directory_identity = checked_existing_path(backup_dir, home, "dir", "backup directory")
    backup_db = backup_dir / db.name
    baseline_digest = database_digest(writer)
    source = open_readonly(db, db_identity)
    try:
        source.execute("BEGIN")
        if database_digest(source) != baseline_digest:
            raise RuntimeError("read-only backup snapshot differs from the locked database")
        with sqlite3.connect(backup_db) as destination:
            source.backup(destination)
            if integrity(destination) != "ok" or database_digest(destination) != baseline_digest:
                raise RuntimeError("consistent SQLite backup validation failed")
    finally:
        source.close()
    os.chmod(backup_db, 0o600)
    backup_identity = file_identity(backup_db)
    verify_identity(backup_root, root_identity, "backup root")
    verify_identity(backup_dir, directory_identity, "backup directory")
    verify_identity(backup_db, backup_identity, "backup database")
    verify_identity(db, db_identity, "state database")
    return {
        "directory": backup_dir,
        "directory_identity": directory_identity,
        "database": backup_db,
        "database_identity": backup_identity,
        "database_digest": baseline_digest,
        "database_sha256": sha256_file(backup_db),
    }


def write_backup_manifest(
    home: Path,
    db: Path,
    db_identity: FileIdentity,
    backup: dict[str, Any],
    repairs: list[dict[str, str]],
    operation: str,
) -> Path:
    verify_identity(backup["directory"], backup["directory_identity"], "backup directory")
    verify_identity(backup["database"], backup["database_identity"], "backup database")
    manifest = {
        "format": 1,
        "operation": operation,
        "codex_home": str(home),
        "database_name": db.name,
        "source_device": db_identity.device,
        "source_inode": db_identity.inode,
        "backup_device": backup["database_identity"].device,
        "backup_inode": backup["database_identity"].inode,
        "backup_sha256": backup["database_sha256"],
        "database_digest": backup["database_digest"],
        "repairs": repairs,
        "created_at": time.strftime("%Y-%m-%dT%H:%M:%S%z"),
    }
    manifest_path = backup["directory"] / MANIFEST_NAME
    with manifest_path.open("x", encoding="utf-8") as handle:
        json.dump(manifest, handle, ensure_ascii=False, indent=2)
        handle.write("\n")
    os.chmod(manifest_path, 0o600)
    return manifest_path


def apply_repairs(
    home: Path,
    db: Path,
    expected_identity: FileIdentity | None = None,
) -> tuple[Path, int, dict[str, Any]]:
    db_identity = expected_identity or file_identity(db)
    verify_identity(db, db_identity, "state database")
    writer = sqlite3.connect(db, timeout=5)
    backup: dict[str, Any] | None = None
    try:
        verify_identity(db, db_identity, "state database")
        writer.execute("BEGIN IMMEDIATE")
        verify_identity(db, db_identity, "state database")
        locked = scan_connection(home, writer)
        if not locked["schema_supported"] or locked["integrity"] != "ok":
            raise RuntimeError("database schema or integrity is not supported")
        backup = create_locked_backup(home, db, writer, db_identity)
        plan: list[tuple[dict[str, Any], RolloutCandidate]] = []
        for item in locked["repairable_rollout_paths"]:
            candidates = find_candidate_records(home, str(item["rollout_path"]), str(item["id"]))
            if len(candidates) != 1:
                raise RuntimeError(f"rollout candidate changed during repair for thread {item['id']}")
            plan.append((item, candidates[0]))
        repairs = [
            {"id": str(item["id"]), "old_path": str(item["rollout_path"]), "new_path": str(candidate.path)}
            for item, candidate in plan
        ]
        overrides: dict[str, str] = {}
        for item, candidate in plan:
            verify_identity(candidate.path, candidate.identity, "rollout candidate")
            if read_session_id(candidate.path) != item["id"]:
                raise RuntimeError(f"rollout candidate content changed for thread {item['id']}")
            verify_identity(candidate.path, candidate.identity, "rollout candidate")
            cursor = writer.execute(
                "UPDATE threads SET rollout_path = ? WHERE id = ? AND rollout_path = ?",
                (str(candidate.path), item["id"], item["rollout_path"]),
            )
            if cursor.rowcount != 1:
                raise RuntimeError(f"thread changed during repair: {item['id']}")
            overrides[str(item["id"])] = str(item["rollout_path"])
        for _, candidate in plan:
            verify_identity(candidate.path, candidate.identity, "rollout candidate")
        for item, candidate in plan:
            row = writer.execute("SELECT rollout_path FROM threads WHERE id = ?", (item["id"],)).fetchone()
            if row is None or row[0] != str(candidate.path):
                raise RuntimeError(f"thread path validation failed after update: {item['id']}")
        verify_identity(db, db_identity, "state database")
        if database_digest(writer, overrides) != backup["database_digest"]:
            raise RuntimeError("database content outside the approved rollout paths changed")
        if integrity(writer) != "ok":
            raise RuntimeError("database failed integrity_check after repair")
        write_backup_manifest(home, db, db_identity, backup, repairs, "repair")
        writer.commit()
        verify_identity(db, db_identity, "state database")
        return backup["directory"], len(repairs), locked
    except Exception:
        writer.rollback()
        raise
    finally:
        writer.close()


def resolve_database(home: Path, requested: Path | None) -> tuple[Path | None, str | None]:
    try:
        home = home.resolve(strict=True)
        if not home.is_dir():
            raise RuntimeError("Codex home must be a directory")
        if requested is not None:
            return checked_existing_path(requested.expanduser(), home, "file", "explicit database")[0], None
        candidates: list[Path] = []
        for path in home.glob("state_*.sqlite"):
            try:
                candidate, _ = checked_existing_path(path, home, "file", "state database")
                if candidate.parent == home:
                    candidates.append(candidate)
            except RuntimeError:
                continue
        candidates = sorted(set(candidates))
        if len(candidates) != 1:
            return None, f"expected exactly one state_*.sqlite database, found {len(candidates)}; use --database"
        return candidates[0], None
    except (OSError, RuntimeError) as exc:
        return None, str(exc)


def resolve_backup(home: Path, db: Path, requested: Path) -> tuple[dict[str, Any] | None, str | None]:
    try:
        backup_root, _ = checked_existing_path(home / "backups", home, "dir", "backup root")
        if ".." in requested.parts:
            raise RuntimeError("backup path must not contain '..' path components")
        backup_dir, directory_identity = checked_existing_path(requested, backup_root, "dir", "backup directory")
        if backup_dir.parent != backup_root or not backup_dir.name.startswith(BACKUP_PREFIX):
            raise RuntimeError("rollback accepts only a repair backup directly under this Codex home's backup root")
        manifest_path, manifest_identity = checked_existing_path(
            backup_dir / MANIFEST_NAME, backup_root, "file", "backup manifest"
        )
        with manifest_path.open("r", encoding="utf-8") as handle:
            manifest = json.load(handle)
        verify_identity(manifest_path, manifest_identity, "backup manifest")
        if not isinstance(manifest, dict) or manifest.get("format") != 1 or manifest.get("operation") != "repair":
            raise RuntimeError("backup manifest is not a supported repair backup")
        if manifest.get("codex_home") != str(home) or manifest.get("database_name") != db.name:
            raise RuntimeError("backup belongs to a different Codex home or database")
        if not all(type(manifest.get(key)) is int for key in ("source_device", "source_inode")):
            raise RuntimeError("backup source database identity is invalid")
        source_identity = FileIdentity(manifest["source_device"], manifest["source_inode"])
        if file_identity(db) != source_identity:
            raise RuntimeError("current database identity differs from the repair backup manifest")
        repairs = manifest.get("repairs")
        if not isinstance(repairs, list) or not all(
            isinstance(item, dict)
            and set(item) == {"id", "old_path", "new_path"}
            and all(isinstance(item[key], str) for key in item)
            for item in repairs
        ):
            raise RuntimeError("backup repair manifest is invalid")
        backup_db, backup_identity = checked_existing_path(backup_dir / db.name, backup_root, "file", "backup database")
        if (manifest.get("backup_device"), manifest.get("backup_inode")) != (
            backup_identity.device, backup_identity.inode
        ):
            raise RuntimeError("backup database identity differs from its manifest")
        if sha256_file(backup_db) != manifest.get("backup_sha256"):
            raise RuntimeError("backup database content differs from its manifest")
        with open_readonly(backup_db, backup_identity) as source:
            if integrity(source) != "ok" or database_digest(source) != manifest.get("database_digest"):
                raise RuntimeError("backup database validation failed")
        return {
            "directory": backup_dir,
            "directory_identity": directory_identity,
            "manifest": manifest_path,
            "manifest_identity": manifest_identity,
            "database": backup_db,
            "database_identity": backup_identity,
            "database_digest": manifest["database_digest"],
            "database_sha256": manifest["backup_sha256"],
            "expected_source_identity": source_identity,
            "repairs": repairs,
        }, None
    except (OSError, RuntimeError, ValueError, json.JSONDecodeError, sqlite3.Error) as exc:
        return None, str(exc)


def apply_rollback(
    home: Path,
    db: Path,
    db_identity: FileIdentity,
    backup: dict[str, Any],
) -> tuple[Path, int]:
    verify_identity(backup["directory"], backup["directory_identity"], "backup directory")
    verify_identity(backup["manifest"], backup["manifest_identity"], "backup manifest")
    verify_identity(backup["database"], backup["database_identity"], "backup database")
    expected_source_identity = backup["expected_source_identity"]
    if db_identity != expected_source_identity:
        raise RuntimeError("current database identity differs from the repair backup manifest")
    verify_identity(db, expected_source_identity, "state database from repair backup")
    writer = sqlite3.connect(db, timeout=5)
    try:
        verify_identity(db, expected_source_identity, "state database from repair backup")
        writer.execute("BEGIN IMMEDIATE")
        verify_identity(db, expected_source_identity, "state database from repair backup")
        supported, _ = schema_status(writer)
        if not supported or integrity(writer) != "ok":
            raise RuntimeError("current database schema or integrity is unsupported")
        safety = create_locked_backup(home, db, writer, expected_source_identity, SAFETY_PREFIX)
        repairs = backup["repairs"]
        seen: set[str] = set()
        for item in repairs:
            thread_id = item["id"]
            if thread_id in seen:
                raise RuntimeError(f"duplicate rollback entry for thread {thread_id}")
            seen.add(thread_id)
            row = writer.execute("SELECT rollout_path FROM threads WHERE id = ?", (thread_id,)).fetchone()
            if row is None or row[0] != item["new_path"]:
                raise RuntimeError(f"thread {thread_id} no longer matches the repaired path")
        baseline_digest = database_digest(writer)
        overrides: dict[str, str] = {}
        for item in repairs:
            cursor = writer.execute(
                "UPDATE threads SET rollout_path = ? WHERE id = ? AND rollout_path = ?",
                (item["old_path"], item["id"], item["new_path"]),
            )
            if cursor.rowcount != 1:
                raise RuntimeError(f"thread changed during rollback: {item['id']}")
            overrides[item["id"]] = item["new_path"]
        for item in repairs:
            row = writer.execute("SELECT rollout_path FROM threads WHERE id = ?", (item["id"],)).fetchone()
            if row is None or row[0] != item["old_path"]:
                raise RuntimeError(f"thread path validation failed after rollback: {item['id']}")
        verify_identity(db, expected_source_identity, "state database from repair backup")
        verify_identity(backup["database"], backup["database_identity"], "backup database")
        if sha256_file(backup["database"]) != backup["database_sha256"]:
            raise RuntimeError("backup database changed during rollback")
        if database_digest(writer, overrides) != baseline_digest:
            raise RuntimeError("database content outside the approved rollback paths changed")
        if integrity(writer) != "ok":
            raise RuntimeError("database failed integrity_check after rollback")
        write_backup_manifest(home, db, expected_source_identity, safety, [], "rollback-safety")
        writer.commit()
        verify_identity(db, expected_source_identity, "state database from repair backup")
        return safety["directory"], len(repairs)
    except Exception:
        writer.rollback()
        raise
    finally:
        writer.close()


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv)
    if args.list or args.resume:
        from resume_sessions import run
        return run(args)
    if args.run or args.provider or args.model or args.project_dir:
        print(json.dumps({"error": "Resume options require --list or --resume SESSION_ID"}))
        return 2
    if args.dry_run and args.apply:
        print(json.dumps({"error": "--dry-run and --apply cannot be combined"}, indent=2))
        return 2
    try:
        home = args.codex_home.expanduser().resolve(strict=True)
        if not home.is_dir():
            raise RuntimeError("Codex home must be a directory")
    except (OSError, RuntimeError) as exc:
        print(json.dumps({"error": f"Invalid Codex home: {exc}"}, ensure_ascii=False, indent=2))
        return 2
    db, database_error = resolve_database(home, args.database)
    if db is None:
        print(json.dumps({"mode": "rollback" if args.rollback else "repair", "error": database_error}, ensure_ascii=False, indent=2))
        return 2
    try:
        _, db_identity = checked_existing_path(db, home, "file", "state database")
    except RuntimeError as exc:
        print(json.dumps({"mode": "rollback" if args.rollback else "repair", "error": str(exc)}, ensure_ascii=False, indent=2))
        return 2

    if args.rollback is not None:
        backup, backup_error = resolve_backup(home, db, args.rollback.expanduser())
        payload: dict[str, Any] = {
            "mode": "rollback-apply" if args.apply else "rollback-preview",
            "database": str(db),
            "backup": str(backup["directory"]) if backup else str(args.rollback),
        }
        if backup is None:
            payload["error"] = f"Rollback refused: {backup_error}"
        elif not args.apply:
            payload["rollback_rollout_paths"] = len(backup["repairs"])
            payload["note"] = "Read-only preview. Add --apply --client-closed after reviewing this backup."
        elif not args.client_closed:
            payload["error"] = "Refusing to roll back without --client-closed confirmation"
        else:
            try:
                safety_dir, rolled_back = apply_rollback(home, db, db_identity, backup)
                payload["current_database_backup"] = str(safety_dir)
                payload["rolled_back_rollout_paths"] = rolled_back
                payload["after"] = snapshot(home, db, db_identity)
            except (OSError, RuntimeError, sqlite3.Error, json.JSONDecodeError) as exc:
                payload["error"] = f"Rollback aborted: {exc}"
        print(json.dumps(payload, ensure_ascii=False, indent=2))
        return 0 if "error" not in payload else 2

    before = snapshot(home, db, db_identity)
    payload = {"mode": "apply" if args.apply else "dry-run", "before": before}
    if args.apply and not args.client_closed:
        payload["error"] = "Refusing to write without --client-closed confirmation"
    elif args.apply and (not before.get("schema_supported") or before.get("integrity") != "ok"):
        payload["error"] = "Refusing to write because the database schema or integrity is unsupported"
    elif args.apply:
        try:
            backup_dir, repaired, locked = apply_repairs(home, db, db_identity)
            payload["locked_scan"] = locked
            payload["backup"] = str(backup_dir)
            payload["repaired_rollout_paths"] = repaired
            payload["after"] = snapshot(home, db, db_identity)
            if payload["after"].get("integrity") != "ok":
                payload["error"] = "Post-commit verification failed; use the selective rollback before reopening Codex"
        except (OSError, RuntimeError, sqlite3.Error) as exc:
            payload["error"] = f"Repair aborted: {exc}"
    else:
        payload["note"] = "Read-only diagnostic. Provider differences are advisory and are never auto-rewritten."
    print(json.dumps(payload, ensure_ascii=False, indent=2))
    return 0 if "error" not in payload else 2


if __name__ == "__main__":
    raise SystemExit(main())
