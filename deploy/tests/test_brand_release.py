"""Brand packaging and installer safety checks; no Docker daemon or network."""
import os
import json
from pathlib import Path
import re
import subprocess
import shutil
import unittest


ROOT = Path(__file__).resolve().parents[2]


class BrandReleaseTests(unittest.TestCase):
    def test_workflow_branch_and_image_contract(self):
        if not shutil.which('ruby'):
            self.skipTest('Ruby YAML parser unavailable')
        def yaml(path):
            output = subprocess.check_output(
                ['ruby', '-ryaml', '-rjson', '-e', 'puts JSON.generate(YAML.load_file(ARGV[0]))', str(ROOT / path)],
                text=True)
            return json.loads(output)
        for brand, branches, image in (
            ('kdan', ['KDAN'], 'ghcr.io/holeenlu/kdan'),
            ('tapmodels', ['main', 'TapModels'], 'ghcr.io/erwinlin/tapmodels'),
        ):
            ci_path = f'.github/workflows/{brand}-ci.yml'
            if not (ROOT / ci_path).exists():
                continue
            with self.subTest(brand=brand):
                ci = yaml(ci_path)
                events = ci.get('on', ci.get('true'))
                self.assertEqual(events['push']['branches'], branches)
                self.assertEqual(events['pull_request']['branches'], branches)
                workflow = yaml(f'.github/workflows/{brand}-docker-image.yml')
                events = workflow.get('on', workflow.get('true'))
                self.assertEqual(events['push']['branches'], ['KDAN'] if brand == 'kdan' else ['main'])
                self.assertIn('github.repository ==', workflow['jobs']['build']['if'])
                for filename in ('docker-compose.yml', 'docker-compose.local.yml', 'docker-compose.standalone.yml'):
                    service = yaml('deploy/' + filename)['services'][brand]
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
