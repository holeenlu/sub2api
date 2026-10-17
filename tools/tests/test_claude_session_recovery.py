"""Tests for the read-only Claude Code session discovery utility."""

import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest


SCRIPT = (
    Path(__file__).parents[2]
    / "frontend/public/downloads/tapmodels-claude-session-recovery/find_claude_sessions.py"
)


class ClaudeSessionRecoveryTest(unittest.TestCase):
    def test_lists_only_validated_sessions_without_exposing_content(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            claude_home = root / ".claude"
            project = claude_home / "projects" / "-tmp-project-with-space"
            project.mkdir(parents=True)
            session_id = "12345678-1234-4123-8123-123456789abc"
            cwd = root / "project with space"
            cwd.mkdir()
            transcript = project / f"{session_id}.jsonl"
            transcript.write_text(
                "\n".join(
                    [
                        json.dumps({"type": "queue-operation", "sessionId": session_id}),
                        json.dumps(
                            {
                                "type": "user",
                                "sessionId": session_id,
                                "cwd": str(cwd),
                                "message": {"content": "fixture secret must stay private"},
                            }
                        ),
                    ]
                )
                + "\n"
            )
            invalid_id = "87654321-4321-4321-8321-cba987654321"
            (project / f"{invalid_id}.jsonl").write_text(
                json.dumps({"sessionId": session_id, "message": "another secret"}) + "\n"
            )

            result = subprocess.run(
                [
                    sys.executable,
                    str(SCRIPT),
                    "--claude-home",
                    str(claude_home),
                    "--limit",
                    "0",
                    "--json",
                ],
                capture_output=True,
                text=True,
                timeout=10,
            )

            self.assertEqual(result.returncode, 0, result.stderr)
            payload = json.loads(result.stdout)
            self.assertEqual(payload["session_count"], 1)
            self.assertEqual(payload["sessions"][0]["session_id"], session_id)
            self.assertEqual(payload["sessions"][0]["working_directory"], str(cwd))
            self.assertTrue(payload["sessions"][0]["working_directory_exists"])
            self.assertIn(str(transcript), payload["sessions"][0]["resume_command"])
            self.assertIn("CLAUDE_CONFIG_DIR=", payload["sessions"][0]["resume_command"])
            self.assertNotIn("fixture secret", result.stdout + result.stderr)
            self.assertNotIn("another secret", result.stdout + result.stderr)
            self.assertEqual(transcript.read_text().count("\n"), 2)

    def test_missing_history_returns_a_clear_error(self):
        with tempfile.TemporaryDirectory() as temporary:
            result = subprocess.run(
                [sys.executable, str(SCRIPT), "--claude-home", temporary],
                capture_output=True,
                text=True,
                timeout=10,
            )

            self.assertEqual(result.returncode, 2)
            self.assertIn("Claude project history was not found", result.stderr)


if __name__ == "__main__":
    unittest.main()
