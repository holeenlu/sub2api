import importlib.util
import contextlib
import io
import json
import os
from pathlib import Path
import sqlite3
import tempfile
import unittest
from unittest import mock


SCRIPT = Path(__file__).parents[2] / "frontend/public/downloads/tapmodels-session-repair/repair_sessions.py"
spec = importlib.util.spec_from_file_location("tapmodels_session_repair", SCRIPT)
repair = importlib.util.module_from_spec(spec)
spec.loader.exec_module(repair)


class SessionRepairTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.home = Path(self.temp.name).resolve()
        self.db = self.home / "state_5.sqlite"
        self.sessions = self.home / "sessions/2026/09/15"
        self.sessions.mkdir(parents=True)
        with sqlite3.connect(self.db) as connection:
            connection.execute(
                """CREATE TABLE threads (
                    id TEXT PRIMARY KEY,
                    rollout_path TEXT NOT NULL,
                    archived INTEGER NOT NULL DEFAULT 0,
                    model_provider TEXT NOT NULL DEFAULT ''
                )"""
            )
            connection.execute("CREATE TABLE settings (name TEXT PRIMARY KEY, value TEXT NOT NULL)")
            connection.execute("INSERT INTO settings(name, value) VALUES ('theme', 'light')")

    def add_thread(self, thread_id, rollout_path, archived=0, provider="OpenAI"):
        with sqlite3.connect(self.db) as connection:
            connection.execute(
                "INSERT INTO threads(id, rollout_path, archived, model_provider) VALUES (?, ?, ?, ?)",
                (thread_id, str(rollout_path), archived, provider),
            )

    def add_rollout(self, name, thread_id, root=None):
        path = (root or self.sessions) / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(json.dumps({"type": "session_meta", "payload": {"id": thread_id}}) + "\n", encoding="utf-8")
        return path

    def test_candidate_requires_matching_session_meta_id(self):
        self.add_thread("thread-one", "/stale/rollout-one.jsonl")
        self.add_rollout("rollout-one.jsonl", "different-thread")
        report = repair.snapshot(self.home.resolve(), self.db)
        self.assertEqual(report["integrity"], "ok")
        self.assertEqual(report["repairable_rollout_paths"], [])
        self.assertEqual(report["missing_rollout_paths"][0]["candidates"], [])

    @unittest.skipUnless(hasattr(os, "symlink"), "symlinks unavailable")
    def test_candidate_rejects_symlink_escape(self):
        outside = self.home / "outside"
        outside.mkdir()
        self.add_rollout("rollout-link.jsonl", "thread-link", outside)
        link = self.sessions / "linked"
        link.symlink_to(outside, target_is_directory=True)
        self.add_thread("thread-link", "/stale/rollout-link.jsonl")
        report = repair.snapshot(self.home.resolve(), self.db)
        self.assertEqual(report["repairable_rollout_paths"], [])

    def test_unknown_schema_fails_closed(self):
        self.db.unlink()
        with sqlite3.connect(self.db) as connection:
            connection.execute("CREATE TABLE threads (id TEXT PRIMARY KEY)")
        report = repair.snapshot(self.home.resolve(), self.db)
        self.assertFalse(report["schema_supported"])
        self.assertIn("threads table must contain", report["schema_error"])

    def test_apply_requires_client_closed_confirmation(self):
        self.add_thread("thread-one", "/stale/rollout-one.jsonl")
        self.add_rollout("rollout-one.jsonl", "thread-one")
        status = repair.main(["--codex-home", str(self.home), "--apply"])
        self.assertEqual(status, 2)
        with sqlite3.connect(self.db) as connection:
            self.assertEqual(connection.execute("SELECT rollout_path FROM threads").fetchone()[0], "/stale/rollout-one.jsonl")
        self.assertFalse((self.home / "backups").exists())

    def test_apply_repairs_proven_path_and_creates_consistent_backup(self):
        old_path = "/stale/rollout-one.jsonl"
        rollout = self.add_rollout("rollout-one.jsonl", "thread-one")
        self.add_thread("thread-one", old_path, archived=1, provider="openai")
        (self.home / "auth.json").write_text('{"secret":"must-not-copy"}', encoding="utf-8")
        status = repair.main(["--codex-home", str(self.home), "--apply", "--client-closed", "--json"])
        self.assertEqual(status, 0)
        with sqlite3.connect(self.db) as connection:
            row = connection.execute("SELECT rollout_path, archived, model_provider FROM threads").fetchone()
        self.assertEqual(row, (str(rollout.resolve()), 1, "openai"))
        backups = list((self.home / "backups").glob("tapmodels-session-repair-*"))
        self.assertEqual(len(backups), 1)
        self.assertEqual(backups[0].stat().st_mode & 0o777, 0o700)
        backup_db = backups[0] / "state_5.sqlite"
        self.assertEqual(backup_db.stat().st_mode & 0o777, 0o600)
        self.assertFalse((backups[0] / "auth.json").exists())
        with sqlite3.connect(backup_db) as connection:
            backup_row = connection.execute("SELECT rollout_path, archived, model_provider FROM threads").fetchone()
            self.assertEqual(connection.execute("PRAGMA integrity_check").fetchone()[0], "ok")
        self.assertEqual(backup_row, (old_path, 1, "openai"))

    def test_multiple_databases_require_explicit_selection(self):
        sqlite3.connect(self.home / "state_6.sqlite").close()
        database, error = repair.resolve_database(self.home.resolve(), None)
        self.assertIsNone(database)
        self.assertIn("found 2", error)
        database, error = repair.resolve_database(self.home.resolve(), Path("state_5.sqlite"))
        self.assertEqual(database, self.db.absolute().resolve())
        self.assertIsNone(error)

    @unittest.skipUnless(hasattr(os, "symlink"), "symlinks unavailable")
    def test_database_rejects_symlinked_parent_component(self):
        real = self.home / "real"
        real.mkdir()
        nested_db = real / "state_nested.sqlite"
        sqlite3.connect(nested_db).close()
        (self.home / "linked").symlink_to(real, target_is_directory=True)
        database, error = repair.resolve_database(self.home.resolve(), Path("linked/state_nested.sqlite"))
        self.assertIsNone(database)
        self.assertIn("symbolic-link path component", error)

    def test_database_rejects_parent_directory_component(self):
        database, error = repair.resolve_database(
            self.home.resolve(), Path("unused/../state_5.sqlite")
        )
        self.assertIsNone(database)
        self.assertIn("must not contain '..'", error)

    def test_readonly_uri_handles_reserved_path_characters(self):
        renamed = self.home / "state_hash#question?.sqlite"
        os.replace(self.db, renamed)
        self.db = renamed
        report = repair.snapshot(self.home, self.db)
        self.assertEqual(report["integrity"], "ok")
        self.assertTrue(report["schema_supported"])

    @unittest.skipUnless(hasattr(os, "symlink"), "symlinks unavailable")
    def test_backup_root_rejects_symlink(self):
        outside = self.home / "outside-backups"
        outside.mkdir()
        (self.home / "backups").symlink_to(outside, target_is_directory=True)
        with self.assertRaisesRegex(RuntimeError, "symbolic-link path component"):
            repair.ensure_backup_root(self.home.resolve())

    def test_database_replacement_after_validation_is_rejected(self):
        self.add_rollout("rollout-one.jsonl", "thread-one")
        self.add_thread("thread-one", "/stale/rollout-one.jsonl")
        original = repair.snapshot

        def replace_after_snapshot(*args, **kwargs):
            report = original(*args, **kwargs)
            replacement = self.home / "replacement.sqlite"
            with sqlite3.connect(replacement) as connection:
                connection.execute(
                    "CREATE TABLE threads (id TEXT PRIMARY KEY, rollout_path TEXT NOT NULL, archived INTEGER NOT NULL)"
                )
            os.replace(replacement, self.db)
            return report

        with mock.patch.object(repair, "snapshot", side_effect=replace_after_snapshot):
            with contextlib.redirect_stdout(io.StringIO()) as output:
                status = repair.main([
                    "--codex-home", str(self.home), "--database", "state_5.sqlite",
                    "--apply", "--client-closed",
                ])
        self.assertEqual(status, 2)
        self.assertIn("state database was replaced", output.getvalue())

    def test_candidate_replacement_aborts_and_rolls_back(self):
        old_path = "/stale/rollout-one.jsonl"
        rollout = self.add_rollout("rollout-one.jsonl", "thread-one")
        self.add_thread("thread-one", old_path)
        original = repair.find_candidate_records
        calls = 0

        def replace_after_inspection(*args, **kwargs):
            nonlocal calls
            result = original(*args, **kwargs)
            calls += 1
            if calls == 2 and result:
                replacement = rollout.with_suffix(".replacement")
                replacement.write_text(rollout.read_text(encoding="utf-8"), encoding="utf-8")
                os.replace(replacement, rollout)
            return result

        with mock.patch.object(repair, "find_candidate_records", side_effect=replace_after_inspection):
            with self.assertRaisesRegex(RuntimeError, "rollout candidate was replaced"):
                repair.apply_repairs(self.home.resolve(), self.db.resolve())
        with sqlite3.connect(self.db) as connection:
            self.assertEqual(connection.execute("SELECT rollout_path FROM threads").fetchone()[0], old_path)

    def test_write_lock_blocks_other_column_and_table_writes(self):
        old_path = "/stale/rollout-one.jsonl"
        self.add_rollout("rollout-one.jsonl", "thread-one")
        self.add_thread("thread-one", old_path)
        original = repair.create_locked_backup
        attempts = []

        def attempt_concurrent_writes(*args, **kwargs):
            for statement in (
                "UPDATE threads SET model_provider = 'changed' WHERE id = 'thread-one'",
                "UPDATE settings SET value = 'dark' WHERE name = 'theme'",
            ):
                try:
                    with sqlite3.connect(self.db, timeout=0) as connection:
                        connection.execute(statement)
                except sqlite3.OperationalError as exc:
                    attempts.append(str(exc))
            return original(*args, **kwargs)

        with mock.patch.object(repair, "create_locked_backup", side_effect=attempt_concurrent_writes):
            repair.apply_repairs(self.home.resolve(), self.db.resolve())
        self.assertEqual(len(attempts), 2)
        self.assertTrue(all("locked" in message for message in attempts))
        with sqlite3.connect(self.db) as connection:
            self.assertEqual(connection.execute("SELECT model_provider FROM threads").fetchone()[0], "OpenAI")
            self.assertEqual(connection.execute("SELECT value FROM settings").fetchone()[0], "light")

    def test_full_database_validation_failure_rolls_back(self):
        old_path = "/stale/rollout-one.jsonl"
        self.add_rollout("rollout-one.jsonl", "thread-one")
        self.add_thread("thread-one", old_path)
        original = repair.database_digest

        def fail_final_validation(connection, rollout_overrides=None):
            value = original(connection, rollout_overrides)
            return "forced-mismatch" if rollout_overrides else value

        with mock.patch.object(repair, "database_digest", side_effect=fail_final_validation):
            with self.assertRaisesRegex(RuntimeError, "outside the approved rollout paths"):
                repair.apply_repairs(self.home.resolve(), self.db.resolve())
        with sqlite3.connect(self.db) as connection:
            row = connection.execute(
                "SELECT rollout_path, model_provider FROM threads WHERE id = 'thread-one'"
            ).fetchone()
            self.assertEqual(row, (old_path, "OpenAI"))
            self.assertEqual(connection.execute("SELECT value FROM settings").fetchone()[0], "light")

    def test_trigger_rewriting_repaired_path_aborts_and_rolls_back(self):
        old_path = "/stale/rollout-one.jsonl"
        self.add_rollout("rollout-one.jsonl", "thread-one")
        self.add_thread("thread-one", old_path)
        with sqlite3.connect(self.db) as connection:
            connection.execute(
                """CREATE TRIGGER rewrite_path AFTER UPDATE OF rollout_path ON threads
                   BEGIN
                     UPDATE threads SET rollout_path = '/trigger/rewritten.jsonl' WHERE id = NEW.id;
                   END"""
            )
        with self.assertRaisesRegex(RuntimeError, "thread path validation failed"):
            repair.apply_repairs(self.home.resolve(), self.db.resolve())
        with sqlite3.connect(self.db) as connection:
            self.assertEqual(connection.execute("SELECT rollout_path FROM threads").fetchone()[0], old_path)

    def test_trigger_modifying_other_column_and_table_aborts_and_rolls_back(self):
        old_path = "/stale/rollout-trigger.jsonl"
        self.add_rollout("rollout-trigger.jsonl", "thread-trigger")
        self.add_thread("thread-trigger", old_path)
        with sqlite3.connect(self.db) as connection:
            connection.execute(
                """CREATE TRIGGER mutate_unrelated AFTER UPDATE OF rollout_path ON threads
                   BEGIN
                     UPDATE threads SET model_provider = 'trigger-provider' WHERE id = NEW.id;
                     UPDATE settings SET value = 'trigger-theme' WHERE name = 'theme';
                   END"""
            )
        with self.assertRaisesRegex(RuntimeError, "outside the approved rollout paths"):
            repair.apply_repairs(self.home, self.db)
        with sqlite3.connect(self.db) as connection:
            row = connection.execute(
                "SELECT rollout_path, model_provider FROM threads WHERE id = 'thread-trigger'"
            ).fetchone()
            self.assertEqual(row, (old_path, "OpenAI"))
            self.assertEqual(connection.execute("SELECT value FROM settings").fetchone()[0], "light")

    def test_selective_rollback_preserves_new_unrelated_data(self):
        old_path = "/stale/rollout-one.jsonl"
        rollout = self.add_rollout("rollout-one.jsonl", "thread-one")
        self.add_thread("thread-one", old_path)
        backup_dir, _, _ = repair.apply_repairs(self.home.resolve(), self.db.resolve())
        with sqlite3.connect(self.db) as connection:
            connection.execute("UPDATE threads SET model_provider = 'new-provider' WHERE id = 'thread-one'")
            connection.execute("UPDATE settings SET value = 'dark' WHERE name = 'theme'")
        backup, error = repair.resolve_backup(self.home.resolve(), self.db.resolve(), backup_dir)
        self.assertIsNone(error)
        safety, count = repair.apply_rollback(
            self.home.resolve(), self.db.resolve(), repair.file_identity(self.db), backup
        )
        self.assertEqual(count, 1)
        self.assertTrue(safety.name.startswith(repair.SAFETY_PREFIX))
        with sqlite3.connect(self.db) as connection:
            row = connection.execute(
                "SELECT rollout_path, model_provider FROM threads WHERE id = 'thread-one'"
            ).fetchone()
            self.assertEqual(row, (old_path, "new-provider"))
            self.assertEqual(connection.execute("SELECT value FROM settings").fetchone()[0], "dark")
        self.assertNotEqual(str(rollout), old_path)

    def test_wal_mode_repair_backup_and_selective_rollback(self):
        old_path = "/stale/rollout-wal.jsonl"
        rollout = self.add_rollout("rollout-wal.jsonl", "thread-wal")
        self.add_thread("thread-wal", old_path)
        with sqlite3.connect(self.db) as connection:
            self.assertEqual(connection.execute("PRAGMA journal_mode=WAL").fetchone()[0], "wal")
        backup_dir, count, _ = repair.apply_repairs(self.home.resolve(), self.db.resolve())
        self.assertEqual(count, 1)
        backup, error = repair.resolve_backup(self.home.resolve(), self.db.resolve(), backup_dir)
        self.assertIsNone(error)
        _, rolled_back = repair.apply_rollback(
            self.home.resolve(), self.db.resolve(), repair.file_identity(self.db), backup
        )
        self.assertEqual(rolled_back, 1)
        with sqlite3.connect(self.db) as connection:
            self.assertEqual(connection.execute("SELECT rollout_path FROM threads").fetchone()[0], old_path)
            self.assertEqual(connection.execute("PRAGMA integrity_check").fetchone()[0], "ok")
        self.assertNotEqual(str(rollout), old_path)

    def test_rollback_preview_is_read_only_and_apply_requires_closed_client(self):
        old_path = "/stale/rollout-one.jsonl"
        rollout = self.add_rollout("rollout-one.jsonl", "thread-one")
        self.add_thread("thread-one", old_path)
        backup_dir, _, _ = repair.apply_repairs(self.home.resolve(), self.db.resolve())
        with contextlib.redirect_stdout(io.StringIO()):
            self.assertEqual(repair.main(["--codex-home", str(self.home), "--rollback", str(backup_dir)]), 0)
            self.assertEqual(
                repair.main(["--codex-home", str(self.home), "--rollback", str(backup_dir), "--apply"]),
                2,
            )
        with sqlite3.connect(self.db) as connection:
            self.assertEqual(connection.execute("SELECT rollout_path FROM threads").fetchone()[0], str(rollout.resolve()))

    def test_rollback_refuses_cas_mismatch(self):
        self.add_rollout("rollout-one.jsonl", "thread-one")
        self.add_thread("thread-one", "/stale/rollout-one.jsonl")
        backup_dir, _, _ = repair.apply_repairs(self.home.resolve(), self.db.resolve())
        with sqlite3.connect(self.db) as connection:
            connection.execute("UPDATE threads SET rollout_path = '/new/user/path.jsonl' WHERE id = 'thread-one'")
        backup, error = repair.resolve_backup(self.home.resolve(), self.db.resolve(), backup_dir)
        self.assertIsNone(error)
        with self.assertRaisesRegex(RuntimeError, "no longer matches"):
            repair.apply_rollback(
                self.home.resolve(), self.db.resolve(), repair.file_identity(self.db), backup
            )
        with sqlite3.connect(self.db) as connection:
            self.assertEqual(connection.execute("SELECT rollout_path FROM threads").fetchone()[0], "/new/user/path.jsonl")

    def test_rollback_refuses_replaced_backup_database(self):
        self.add_rollout("rollout-one.jsonl", "thread-one")
        self.add_thread("thread-one", "/stale/rollout-one.jsonl")
        backup_dir, _, _ = repair.apply_repairs(self.home.resolve(), self.db.resolve())
        backup_db = backup_dir / self.db.name
        replacement = backup_dir / "replacement.sqlite"
        replacement.write_bytes(backup_db.read_bytes())
        os.replace(replacement, backup_db)
        backup, error = repair.resolve_backup(self.home.resolve(), self.db.resolve(), backup_dir)
        self.assertIsNone(backup)
        self.assertIn("identity differs", error)

    def test_rollback_refuses_backup_from_another_home(self):
        self.add_rollout("rollout-one.jsonl", "thread-one")
        self.add_thread("thread-one", "/stale/rollout-one.jsonl")
        backup_dir, _, _ = repair.apply_repairs(self.home, self.db)
        other_home = self.home / "other-home"
        other_home.mkdir()
        other_db = other_home / self.db.name
        with sqlite3.connect(other_db) as connection:
            connection.execute(
                "CREATE TABLE threads (id TEXT PRIMARY KEY, rollout_path TEXT NOT NULL, archived INTEGER NOT NULL)"
            )
        (other_home / "backups").mkdir()
        backup, error = repair.resolve_backup(other_home, other_db, backup_dir)
        self.assertIsNone(backup)
        self.assertIn("must be under", error)


if __name__ == "__main__":
    unittest.main()
