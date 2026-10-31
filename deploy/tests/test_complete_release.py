"""Complete release contracts: immutable packaging and separated roles."""
import hashlib
import json
import os
from pathlib import Path
import shutil
import tarfile
import tempfile
import unittest
from unittest.mock import patch
import yaml

from deploy.release import release

ROOT = Path(__file__).resolve().parents[2]


class CompleteReleaseTests(unittest.TestCase):
    def images(self):
        return {role: f'ghcr.io/holeenlu/newapi-{role}@sha256:' + str(n) * 64
                for n, role in enumerate(release.ROLES, 1)}

    def test_compose_runs_all_roles_without_building_or_exposing_databases(self):
        images = self.images()
        config = release.compose(images)
        self.assertEqual(set(config['services']), {'api', 'web', 'vendor-portal', 'postgres', 'redis'})
        self.assertEqual(set(config['volumes']), {'app_data', 'postgres_data', 'redis_data'})
        for role, service in [('api', 'api'), ('web', 'web'), ('vendor', 'vendor-portal')]:
            item = config['services'][service]
            self.assertEqual(item['image'], images[role])
            self.assertNotIn('build', item)
            self.assertNotEqual(item['pull_policy'], 'never')
            self.assertIn('127.0.0.1', item['ports'][0])
            if role != 'api':
                self.assertEqual(item['depends_on'], {'api': {'condition': 'service_healthy'}})
                self.assertTrue(item['read_only'])
                self.assertFalse(any(key.endswith('PASSWORD') or key in ('JWT_SECRET', 'TOTP_ENCRYPTION_KEY')
                                     for key in item['environment']))
        for name in ('postgres', 'redis'):
            self.assertNotIn('ports', config['services'][name])

    def test_bundle_contains_both_frontends_api_and_usable_immutable_compose(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            for source in ('deploy/api', 'deploy/web', 'deploy/release'):
                shutil.copytree(ROOT / source, root / source)
            shutil.copy(ROOT / 'deploy/docker-compose.vendor.yml', root / 'deploy/docker-compose.vendor.yml')
            for role in ('main', 'vendor'):
                path = root / 'dist/ui' / role
                path.mkdir(parents=True)
                (path / 'index.html').write_text('<html>' + role + '</html>')
            binary = root / 'dist/api'
            binary.mkdir(parents=True)
            (binary / 'sub2api').write_bytes(b'fixture-binary')
            (binary / 'sub2api').chmod(0o755)
            (binary / 'resources').mkdir()
            (binary / 'resources/prices.json').write_text('{}')
            (root / 'LICENSE').write_text('license')
            with patch.object(release, 'ROOT', root), patch.object(release, 'run'), \
                 patch.dict(os.environ, VERSION='1.0.0', COMMIT='a' * 40, GITHUB_REPOSITORY='holeenlu/sub2api'):
                release.bundle(self.images())
            out = root / 'dist/release'
            with tarfile.open(out / 'NewAPI-1.0.0-deployment-linux-amd64.tar.gz') as tar:
                self.assertTrue({'api/sub2api', 'api/resources/prices.json', 'frontend/main/index.html',
                                 'frontend/vendor/index.html', 'compose.yaml', '.env.example', 'README.md',
                                 'release-manifest.json', 'LICENSE'}.issubset(tar.getnames()))
                self.assertEqual(tar.getmember('api/sub2api').mode & 0o111, 0o111)
                config = json.load(tar.extractfile('compose.yaml'))
                self.assertEqual(config['services']['api']['image'], self.images()['api'])
                self.assertNotIn('.env', tar.getnames())
            for line in (out / 'checksums.txt').read_text().splitlines():
                digest, name = line.split()
                self.assertEqual(digest, hashlib.sha256((out / name).read_bytes()).hexdigest())
            self.assertEqual(json.loads((out / 'release-manifest.json').read_text())['commit'], 'a' * 40)

    def test_metadata_rejects_shell_content_and_wrong_destination(self):
        with patch.dict(os.environ, VERSION='1.0.0', COMMIT='a' * 40, GITHUB_REPOSITORY='holeenlu/sub2api'):
            self.assertEqual(release.metadata(), ('1.0.0', 'a' * 40))
            for key, value in [('VERSION', '1.0.0;echo bad'), ('COMMIT', 'main'), ('GITHUB_REPOSITORY', 'other/repo')]:
                with self.subTest(key=key), patch.dict(os.environ, {key: value}), self.assertRaises(ValueError):
                    release.metadata()

    def test_workflow_pins_source_and_reuses_artifacts_instead_of_rebuilding(self):
        workflow = yaml.load((ROOT / '.github/workflows/automatic-release.yml').read_text(), Loader=yaml.BaseLoader)
        self.assertEqual(set(workflow['on']), {'workflow_dispatch'})
        jobs = workflow['jobs']
        self.assertEqual(set(jobs), {'prepare', 'build-frontend', 'build-binaries', 'publish', 'promote'})
        self.assertEqual(jobs['build-frontend']['needs'], 'prepare')
        self.assertEqual(jobs['build-binaries']['needs'], 'prepare')
        self.assertEqual(set(jobs['publish']['needs']), {'prepare', 'build-frontend', 'build-binaries'})
        self.assertEqual(set(jobs['promote']['needs']), {'prepare', 'publish'})
        for name in ('build-frontend', 'build-binaries', 'publish'):
            self.assertEqual(jobs[name]['steps'][0]['with']['ref'], '${{ needs.prepare.outputs.commit }}')
        commands = '\n'.join(step.get('run', '') for step in jobs['build-frontend']['steps'])
        self.assertEqual(commands.count('pnpm run check:i18n'), 1)
        self.assertEqual(commands.count('pnpm exec vue-tsc'), 1)
        self.assertEqual(commands.count('pnpm exec vite build'), 2)
        publish = '\n'.join(step.get('run', '') for step in jobs['publish']['steps'])
        self.assertIn('--build-context release-artifacts=./dist/api', publish)
        self.assertIn('--build-context release-artifacts=./dist/ui', publish)
        self.assertNotIn('go build', publish)
        self.assertNotIn('pnpm install', publish)
        self.assertLess(publish.index('release.py smoke'), publish.index('docker push'))
        self.assertLess(publish.index('release.py bundle'), publish.index('release.py publish'))
        self.assertIn('test_vendor_portal_image', publish)
        self.assertNotIn('push:', (ROOT / '.github/workflows/quality-gate.yml').read_text())


if __name__ == '__main__':
    unittest.main()
