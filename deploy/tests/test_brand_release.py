"""Brand packaging and installer safety checks; no Docker daemon or network."""
import os
import json
from pathlib import Path
import re
import subprocess
import shutil
import sys
import tempfile
import unittest

from deploy.release_metadata import release_metadata
from deploy.upstream_sync_instructions import render_instructions


ROOT = Path(__file__).resolve().parents[2]


class BrandReleaseTests(unittest.TestCase):
    def read_yaml(self, path):
        if not shutil.which('ruby'):
            self.skipTest('Ruby YAML parser unavailable')
        output = subprocess.check_output(
            ['ruby', '-ryaml', '-rjson', '-e', 'puts JSON.generate(YAML.load_file(ARGV[0]))', str(ROOT / path)],
            text=True)
        return json.loads(output)

    def test_watch_instructions_preserve_branches_and_history(self):
        for branch, local, destinations in (
            ('public', 'holeen/main', ['origin/main']),
            ('kdan', 'KDAN', ['origin/KDAN']),
            ('tapmodels', 'TapModels', ['origin/TapModels', 'erwinlin/main']),
        ):
            with self.subTest(branch=branch):
                instructions = render_instructions(branch)
                self.assertIn('./deploy/sync-upstream.sh prepare --branches ' + branch, instructions)
                self.assertIn('./deploy/sync-upstream.sh status', instructions)
                self.assertIn(local, instructions)
                for destination in destinations:
                    self.assertIn(destination, instructions)
                self.assertNotIn('git rebase', instructions)
                self.assertNotIn('--force', instructions)
                self.assertNotIn('deploy/sync.sh', instructions)
                self.assertNotIn('KDAN:main', instructions)
        for path in (ROOT / '.github/workflows').glob('*sync*.yml'):
            source = path.read_text()
            if '上游更新監看' not in source:
                continue
            with self.subTest(workflow=path.name):
                self.assertIn('python3 deploy/upstream_sync_instructions.py ', source)
                self.assertNotIn('git rebase', source)
                self.assertNotIn('--force-with-lease', source)
                self.assertNotIn('deploy/sync.sh', source)

    def test_release_version_channels(self):
        for ref, version, image_tag, prerelease, stable in (
            ("refs/heads/main", "", "", "false", "false"),
            ("refs/heads/KDAN", "", "", "false", "false"),
            ("refs/tags/1.2.3", "1.2.3", "1.2.3", "false", "true"),
            ("refs/tags/1.2.3-rc.1", "1.2.3-rc.1", "1.2.3-rc.1", "true", "false"),
            ("refs/tags/1.2.3-0", "1.2.3-0", "1.2.3-0", "true", "false"),
            ("refs/tags/1.2.3+build-dev.01", "1.2.3+build-dev.01", "1.2.3_build-dev.01", "false", "true"),
            ("refs/tags/1.2.3-rc.1+build.2", "1.2.3-rc.1+build.2", "1.2.3-rc.1_build.2", "true", "false"),
        ):
            with self.subTest(ref=ref):
                self.assertEqual(release_metadata(ref), {
                    "version": version, "image_tag": image_tag,
                    "prerelease": prerelease, "stable": stable,
                })

    def test_invalid_release_tags_fail_before_publication(self):
        for tag in ("v1.2.3", "1", "1.2", "01.2.3", "1.02.3", "1.2.03",
                    "1.2.3-", "1.2.3-01", "1.2.3-rc..1", "1.2.3+",
                    "1.2.3/other", "1.2.3\nstable=true"):
            with self.subTest(tag=tag):
                result = subprocess.run(
                    [sys.executable, str(ROOT / "deploy/release_metadata.py"), "refs/tags/" + tag],
                    text=True, capture_output=True)
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(result.stdout, "")

    def test_release_workflows_use_the_same_channel_decision(self):
        for path in sorted((ROOT / ".github/workflows").glob("*-docker-image.yml")):
            with self.subTest(workflow=path.name):
                workflow = self.read_yaml(path)
                build = workflow["jobs"]["build"]
                step = next(step for step in build["steps"] if step.get("id") == "release-tag")
                with tempfile.TemporaryDirectory() as directory:
                    output = Path(directory) / "output"
                    subprocess.run(["bash", "-eu", "-c", step["run"]], cwd=ROOT, check=True,
                                   env={**os.environ, "GITHUB_REF": "refs/tags/1.2.3-rc.1",
                                        "GITHUB_OUTPUT": str(output)})
                    values = dict(line.split("=", 1) for line in output.read_text().splitlines())
                self.assertEqual(values["stable"], "false")
                self.assertEqual(values["prerelease"], "true")
                metadata = next(step for step in build["steps"] if step.get("id") == "meta")["with"]
                self.assertEqual(metadata["flavor"], "latest=false")
                self.assertIn("type=raw,value=latest,enable=${{ steps.release-tag.outputs.stable == 'true' }}", metadata["tags"])
                release = workflow["jobs"]["release"]
                self.assertIn("needs.build.outputs.pushed == 'true'", release["if"])
                self.assertEqual(build["outputs"]["prerelease"], "${{ steps.release-tag.outputs.prerelease }}")
                publish = next(step for step in release["steps"] if "run" in step)
                self.assertEqual(publish["env"]["PRERELEASE"], "${{ needs.build.outputs.prerelease }}")
                self.assertIn('--prerelease="$PRERELEASE"', publish["run"])

    def test_workflow_branch_and_image_contract(self):
        for brand, branches, image in (
            ('kdan', ['KDAN'], 'ghcr.io/holeenlu/kdan'),
            ('tapmodels', ['main', 'TapModels'], 'ghcr.io/erwinlin/tapmodels'),
        ):
            ci_path = f'.github/workflows/{brand}-ci.yml'
            if not (ROOT / ci_path).exists():
                continue
            with self.subTest(brand=brand):
                ci = self.read_yaml(ci_path)
                events = ci.get('on', ci.get('true'))
                self.assertEqual(events['push']['branches'], branches)
                self.assertEqual(events['pull_request']['branches'], branches)
                workflow = self.read_yaml(f'.github/workflows/{brand}-docker-image.yml')
                events = workflow.get('on', workflow.get('true'))
                self.assertEqual(events['push']['branches'], ['KDAN'] if brand == 'kdan' else ['main'])
                self.assertIn('github.repository ==', workflow['jobs']['build']['if'])
                for filename in ('docker-compose.yml', 'docker-compose.local.yml', 'docker-compose.standalone.yml'):
                    service = self.read_yaml('deploy/' + filename)['services'][brand]
                    self.assertEqual(service['image'], '${' + brand.upper() + '_IMAGE:-' + image + ':latest}')
                self.assertIn("RELEASE_DOCKER_IMAGE = '" + image + "'", (ROOT / 'frontend/src/config/brand.ts').read_text())

    def test_disabled_installer_does_not_stop_or_modify_service(self):
        source = (ROOT / 'deploy/install.sh').read_text()
        if 'require_installer_enabled()' not in source:
            self.skipTest('shared upstream installer has no brand opt-in gate')
        def function(name):
            match = re.search(r'^' + name + r'\(\) \{\n.*?^\}', source, re.M | re.S)
            self.assertIsNotNone(match, name)
            return match.group()
        gate = function('require_installer_enabled')
        variable = re.search(r'\$(\w+_INSTALLER_ENABLED)', gate).group(1)
        for operation in ('upgrade', 'install_version'):
            with self.subTest(operation=operation):
                script = '\n'.join([
                    'set -eu',
                    'print_error() { printf "%s\\n" "$*"; }',
                    'systemctl() { echo UNEXPECTED_MUTATION; exit 99; }',
                    'cp() { echo UNEXPECTED_MUTATION; exit 99; }',
                    'curl() { echo UNEXPECTED_NETWORK; exit 99; }',
                    gate, function(operation), operation + ' 1.0.0',
                ])
                result = subprocess.run(['bash', '-c', script], text=True, capture_output=True,
                                        env={**os.environ, variable: 'false', 'GITHUB_REPO': 'example/private'})
                self.assertEqual(result.returncode, 1, result.stderr)
                self.assertIn('release artifacts are not published', result.stdout)
                self.assertNotIn('UNEXPECTED_', result.stdout)
                self.assertEqual(result.stderr, '')

    def test_branded_images_preserve_release_driver_entrypoint(self):
        source = (ROOT / 'Dockerfile.goreleaser').read_text()
        match = re.search(r'^ARG BINARY_NAME=(\w+)$', source, re.M)
        if match is None:
            self.skipTest('shared image already uses sub2api')
        name = match.group(1)
        self.assertIn('COPY ${BINARY_NAME} /app/' + name, source)
        for filename in ('Dockerfile', 'Dockerfile.goreleaser'):
            self.assertIn('RUN ln -s /app/' + name + ' /app/sub2api', (ROOT / filename).read_text())


if __name__ == '__main__':
    unittest.main()
