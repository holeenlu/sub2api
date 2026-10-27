"""Verify the delivered ZIPs, including the paths executed by the tutorials."""

import hashlib
import importlib.util
import json
import os
from pathlib import Path
import stat
import subprocess
import sys
import tempfile
import unittest
import zipfile

ROOT = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location("docs_downloads", ROOT / "tools/build_docs_downloads.py")
builder = importlib.util.module_from_spec(spec)
spec.loader.exec_module(builder)


class DocsDownloadsTest(unittest.TestCase):
    def test_archives_match_sources_names_permissions_and_manifest(self):
        manifest = {}
        for line in (builder.DOWNLOADS / "SHA256SUMS.txt").read_text().splitlines():
            digest, name = line.split("  ", 1)
            manifest[name] = digest
        self.assertEqual(set(manifest), {f"{name}.zip" for name in builder.PACKAGES})
        for name, (source, files) in builder.PACKAGES.items():
            with self.subTest(package=name):
                path = builder.DOWNLOADS / f"{name}.zip"
                self.assertEqual(path.read_bytes(), builder.archive_bytes(name), "Rebuild stale archives")
                self.assertEqual(hashlib.sha256(path.read_bytes()).hexdigest(), manifest[path.name])
                with zipfile.ZipFile(path) as archive:
                    self.assertEqual(set(archive.namelist()), {f"{name}/{f}" for f in files})
                    for relative in files:
                        entry = archive.getinfo(f"{name}/{relative}")
                        self.assertEqual(archive.read(entry), (builder.DOWNLOADS / source / relative).read_bytes())
                        mode = entry.external_attr >> 16
                        self.assertTrue(stat.S_ISREG(mode))
                        self.assertEqual(stat.S_IMODE(mode), 0o755 if relative.endswith((".py", ".sh")) else 0o644)

    def test_both_extracted_skills_run_with_documented_provider_and_key(self):
        with tempfile.TemporaryDirectory() as temporary:
            home = Path(temporary)
            config_home = home / ".codex"
            config_home.mkdir()
            (config_home / "config.toml").write_text('''model_provider = "tapmodels"
[model_providers.tapmodels]
name = "TapModels"
base_url = "https://example.test/v1"
wire_api = "responses"
env_key = "TAPMODELS_API_KEY"
''')
            env = {k: v for k, v in os.environ.items() if k not in {"TAPMODELS_BASE_URL", "OPENAI_API_KEY"}}
            env.update(CODEX_HOME=str(config_home), TAPMODELS_API_KEY="fixture-only-key", PYTHONDONTWRITEBYTECODE="1")
            skills = home / ".agents/skills"
            for name in ("gpt-image-flare", "gpt-image-sunburst"):
                with zipfile.ZipFile(builder.DOWNLOADS / f"{name}.zip") as archive:
                    archive.extractall(skills)
            for name in ("gpt-image-flare", "gpt-image-sunburst"):
                with self.subTest(skill=name):
                    script = skills / name / "scripts/generate.py"
                    self.assertTrue((skills / name / "SKILL.md").is_file())
                    for args in (["--check-config"], ["--prompt", "fixture", "--dry-run"]):
                        result = subprocess.run([sys.executable, str(script), *args], env=env, capture_output=True, text=True, timeout=10)
                        self.assertEqual(result.returncode, 0, result.stderr)
                        self.assertNotIn("fixture-only-key", result.stdout + result.stderr)
                        payload = json.loads(result.stdout)
                        if "--dry-run" in args:
                            self.assertEqual(payload["payload"]["model"], name.replace("gpt-image-", "gpt-image-2.5-"))

    def test_extracted_recovery_entrypoint_is_callable_without_user_data(self):
        with tempfile.TemporaryDirectory() as temporary:
            target = Path(temporary)
            name = "tapmodels-codex-session-repair"
            with zipfile.ZipFile(builder.DOWNLOADS / f"{name}.zip") as archive:
                archive.extractall(target)
            result = subprocess.run(["bash", "repair-sessions.sh", "--help"], cwd=target / name, capture_output=True, text=True, timeout=10)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertIn("--client-closed", result.stdout)

    def test_extracted_claude_recovery_entrypoint_is_read_only_and_callable(self):
        with tempfile.TemporaryDirectory() as temporary:
            target = Path(temporary)
            name = "tapmodels-claude-session-recovery"
            with zipfile.ZipFile(builder.DOWNLOADS / f"{name}.zip") as archive:
                archive.extractall(target)
            result = subprocess.run(
                ["bash", "find-claude-sessions.sh", "--help"],
                cwd=target / name,
                capture_output=True,
                text=True,
                timeout=10,
            )
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertIn("--claude-home", result.stdout)
            self.assertIn("read-only", result.stdout)


if __name__ == "__main__":
    unittest.main()
