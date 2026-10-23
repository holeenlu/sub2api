"""Exercise downloaded recovery commands against isolated local CLI doubles."""
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

DOWNLOADS = Path(__file__).parents[2] / "frontend/public/downloads"
CODEX = DOWNLOADS / "tokensavy-session-repair/repair_sessions.py"
CLAUDE = DOWNLOADS / "tokensavy-claude-session-recovery/find_claude_sessions.py"
ID = "12345678-1234-4123-8123-123456789abc"


class RecoveryCommandsTest(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name).resolve()
        self.project = self.root / "project with ' quote $(not-a-command)"
        self.project.mkdir()
        self.codex = self.root / "old codex"
        self.codex.mkdir()
        (self.codex / "config.toml").write_text(
            'model_provider = "tokensavy"\nmodel = "available-model"\n'
            '[model_providers.tokensavy]\nname = "Tokensavy"\n'
            'base_url = "https://example.invalid/v1"\nexperimental_bearer_token = "PRIVATE-KEY"\n'
        )
        self.rollout = self.codex / "sessions/renamed.jsonl"
        self.rollout.parent.mkdir()
        self.rollout.write_text(json.dumps({
            "type": "session_meta", "payload": {"id": ID, "cwd": str(self.project), "model_provider": "missing-old-provider"}
        }) + '\n' + json.dumps({"type": "response_item", "message": "PRIVATE-MESSAGE"}) + '\n')
        self.claude = self.root / "old claude"
        self.transcript = self.claude / f"projects/old-project/{ID}.jsonl"
        self.transcript.parent.mkdir(parents=True)
        self.transcript.write_text(json.dumps({"sessionId": ID, "cwd": str(self.project), "message": "PRIVATE-MESSAGE"})+'\n')
        self.capture = self.root / "captured.json"
        self.bin = self.root / "bin"
        self.bin.mkdir()
        for name in ("codex", "claude"):
            stub = self.bin / name
            stub.write_text("#!" + sys.executable + "\nimport json, os, sys\n"
                            "from pathlib import Path\n"
                            f"Path({str(self.capture)!r}).write_text(json.dumps({{'args': sys.argv[1:], 'cwd': os.getcwd(), 'codex_home': os.environ.get('CODEX_HOME'), 'claude_home': os.environ.get('CLAUDE_CONFIG_DIR')}}))\n")
            stub.chmod(0o755)
        self.env = dict(os.environ, PATH=str(self.bin) + os.pathsep + os.environ.get("PATH", ""))

    def invoke(self, script, home, *options):
        flag = "--codex-home" if script == CODEX else "--claude-home"
        return subprocess.run([sys.executable, str(script), flag, str(home), *options],
                              text=True, capture_output=True, env=self.env, timeout=10)

    def test_codex_list_needs_no_database_and_does_not_leak_credentials(self):
        result = self.invoke(CODEX, self.codex, "--list")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        data = json.loads(result.stdout)
        self.assertEqual(data["provider"], "tokensavy")
        self.assertEqual(data["sessions"][0]["recorded_provider"], "missing-old-provider")
        self.assertNotIn("PRIVATE-KEY", result.stdout)
        self.assertNotIn("PRIVATE-MESSAGE", result.stdout)
        self.assertFalse(self.capture.exists())

    def test_codex_preview_command_and_explicit_run_preserve_files(self):
        original = self.rollout.read_bytes()
        result = self.invoke(CODEX, self.codex, "--resume", ID, "--shell", "posix")
        command = json.loads(result.stdout)["sessions"][0]["resume_command"]
        subprocess.run(["/bin/sh", "-c", command], env=self.env, check=True, timeout=10)
        expected = json.loads(self.capture.read_text())
        self.assertEqual(expected["codex_home"], str(self.codex))
        self.assertIn('model_provider="tokensavy"', expected["args"])
        self.assertEqual(expected["args"][-2:], ["--model", "available-model"])
        self.capture.unlink()
        result = self.invoke(CODEX, self.codex, "--resume", ID, "--run")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        actual = json.loads(self.capture.read_text())
        self.assertEqual(actual["args"], expected["args"])
        self.assertEqual(actual["cwd"], str(self.project))
        self.assertEqual(self.rollout.read_bytes(), original)
        self.assertFalse(list(self.codex.glob("state_*.sqlite")))

    def test_codex_rejects_missing_provider_and_invalid_toml_without_secrets(self):
        result = self.invoke(CODEX, self.codex, "--resume", ID, "--provider", "OpenAI")
        self.assertEqual(result.returncode, 2)
        self.assertIn("not defined", result.stdout)
        (self.codex / "config.toml").write_text('model_provider = "PRIVATE-KEY')
        result = self.invoke(CODEX, self.codex, "--list")
        self.assertEqual(result.returncode, 2)
        self.assertNotIn("PRIVATE-KEY", result.stdout + result.stderr)

    def test_codex_moved_project_requires_explicit_new_directory(self):
        self.project.rmdir()
        result = self.invoke(CODEX, self.codex, "--resume", ID)
        self.assertEqual(result.returncode, 2)
        self.assertIsNone(json.loads(result.stdout)["sessions"][0]["resume_command"])
        result = self.invoke(CODEX, self.codex, "--resume", ID, "--project-dir", str(self.root), "--run")
        self.assertEqual(result.returncode, 0, result.stdout)
        self.assertEqual(json.loads(self.capture.read_text())["cwd"], str(self.root))

    def test_codex_duplicate_session_and_mixed_modes_do_not_launch(self):
        (self.rollout.parent / "duplicate.jsonl").write_bytes(self.rollout.read_bytes())
        result = self.invoke(CODEX, self.codex, "--resume", ID, "--run")
        self.assertEqual(result.returncode, 2)
        result = self.invoke(CODEX, self.codex, "--resume", ID, "--apply")
        self.assertEqual(result.returncode, 2)
        self.assertFalse(self.capture.exists())

    def test_claude_absolute_resume_preserves_custom_home_and_quoting(self):
        original = self.transcript.read_bytes()
        result = self.invoke(CLAUDE, self.claude, "--json", "--shell", "posix")
        data = json.loads(result.stdout)
        command = data["sessions"][0]["resume_command"]
        subprocess.run(["/bin/sh", "-c", command], env=self.env, check=True, timeout=10)
        captured = json.loads(self.capture.read_text())
        self.assertEqual(captured["args"], ["--resume", str(self.transcript)])
        self.assertEqual(captured["cwd"], str(self.project))
        self.assertEqual(captured["claude_home"], str(self.claude))
        self.assertNotIn("PRIVATE-MESSAGE", result.stdout)
        self.assertEqual(self.transcript.read_bytes(), original)

    def test_claude_finds_metadata_after_many_records_and_oversized_line(self):
        self.transcript.write_text('{}\n' * 300 + 'x' * (2 * 1024 * 1024 + 20) + '\n' + self.transcript.read_text())
        result = self.invoke(CLAUDE, self.claude, "--json")
        self.assertEqual(json.loads(result.stdout)["session_count"], 1)

    def test_claude_moved_project_requires_override_and_powershell_stops_on_error(self):
        self.project.rmdir()
        result = self.invoke(CLAUDE, self.claude, "--json")
        self.assertIsNone(json.loads(result.stdout)["sessions"][0]["resume_command"])
        result = self.invoke(CLAUDE, self.claude, "--json", "--project-dir", str(self.root), "--shell", "powershell")
        command = json.loads(result.stdout)["sessions"][0]["resume_command"]
        self.assertIn("-ErrorAction Stop", command)
        self.assertIn("$env:CLAUDE_CONFIG_DIR", command)
        self.assertIn(str(self.transcript), command)
        result = self.invoke(CLAUDE, self.claude, "--project-dir", str(self.root / "absent"))
        self.assertEqual(result.returncode, 2)
