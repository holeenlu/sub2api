"""Tokensavy fresh-deployment boundaries and credential lifecycle."""
import contextlib
import importlib.util
import io
import json
import os
from pathlib import Path
import shutil
import stat
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]
SPEC = importlib.util.spec_from_file_location('tokensavy_init', ROOT / 'deploy/tokensavy/init.py')
INIT = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(INIT)


class TokensavyDeploymentTests(unittest.TestCase):
    def initialize(self, directory):
        with contextlib.redirect_stdout(io.StringIO()):
            INIT.initialize(directory, 'tokensavy.ai', 'ikung1970@gmail.com')
        return dict(line.split('=', 1) for line in (directory / '.env').read_text().splitlines() if '=' in line)

    def test_independent_secrets_restrictive_permissions_and_no_overwrite(self):
        with tempfile.TemporaryDirectory() as tmp:
            a, b = Path(tmp) / 'a', Path(tmp) / 'b'
            first, second = self.initialize(a), self.initialize(b)
            keys = ['ADMIN_PASSWORD', 'POSTGRES_PASSWORD', 'REDIS_PASSWORD', 'JWT_SECRET', 'TOTP_ENCRYPTION_KEY']
            self.assertEqual(len({first[key] for key in keys}), len(keys))
            for key in keys:
                self.assertNotEqual(first[key], second[key])
                self.assertGreaterEqual(len(first[key]), 32)
            for name in ['.env', 'credentials.txt']:
                self.assertEqual(stat.S_IMODE((a / name).stat().st_mode), 0o600)
            original = (a / '.env').read_bytes()
            with self.assertRaises(FileExistsError):
                self.initialize(a)
            self.assertEqual((a / '.env').read_bytes(), original)

    def test_invalid_inputs_do_not_write_env(self):
        with tempfile.TemporaryDirectory() as tmp:
            directory = Path(tmp)
            for domain, email in [('https://tokensavy.ai', 'a@b.com'), ('tokensavy.ai', 'x\nJWT_SECRET=bad')]:
                with self.assertRaises(ValueError):
                    INIT.initialize(directory, domain, email)
            self.assertFalse((directory / '.env').exists())

    @unittest.skipUnless(shutil.which('docker'), 'Docker CLI required for Compose rendering')
    def test_compose_uses_brand_release_image_and_private_database(self):
        with tempfile.TemporaryDirectory() as tmp:
            directory = Path(tmp)
            values = self.initialize(directory)
            rendered = subprocess.check_output([
                'docker', 'compose', '--env-file', str(directory / '.env'), '-f',
                str(ROOT / 'deploy/tokensavy/compose.yaml'), 'config', '--format', 'json'
            ], text=True, env={key: value for key, value in os.environ.items() if key not in values})
            config = json.loads(rendered)
            app = config['services']['tokensavy']
            self.assertEqual(config['name'], 'tokensavy')
            self.assertEqual(app['image'], 'ghcr.io/holeenlu/tokensavy:latest')
            self.assertNotIn('build', app)
            self.assertNotEqual(app.get('pull_policy'), 'never')
            self.assertEqual(app['environment']['UPDATE_CHECK_ENABLED'], 'true')
            self.assertEqual(app['environment']['ADMIN_EMAIL'], 'ikung1970@gmail.com')
            self.assertEqual(app['environment']['ADMIN_PASSWORD'], values['ADMIN_PASSWORD'])
            self.assertEqual(app['ports'][0]['host_ip'], '127.0.0.1')
            self.assertEqual(app['environment']['SERVER_FRONTEND_URL'], 'https://tokensavy.ai')
            for name in ['postgres', 'redis']:
                self.assertFalse(config['services'][name].get('ports'))
            self.assertEqual(config['services']['postgres']['environment']['PGDATA'], '/var/lib/postgresql/data')
            self.assertEqual(config['services']['caddy']['environment']['DOMAIN'], 'tokensavy.ai')
            for volume in config['volumes'].values():
                self.assertTrue(volume['name'].startswith('tokensavy_'))
            self.assertNotIn('tapmodels', rendered.lower())
            self.assertNotIn('erwinlin', rendered.lower())


if __name__ == '__main__':
    unittest.main()
