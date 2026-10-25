"""Render the actual API Compose contract without starting containers."""
import json
import os
import re
import shutil
import subprocess
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
BASE = ROOT / 'deploy/api/compose.yaml'
REQUIRED = dict(POSTGRES_PASSWORD='fixture-db', REDIS_PASSWORD='fixture-cache',
                ADMIN_EMAIL='test@example.invalid', ADMIN_PASSWORD='fixture-admin',
                JWT_SECRET='a' * 64, TOTP_ENCRYPTION_KEY='b' * 64)

@unittest.skipUnless(shutil.which('docker'), 'Docker Compose CLI not installed')
class APIComposeTests(unittest.TestCase):
    def render(self, values=None, missing=None, single=False, vendor=False, check=True):
        env = dict(os.environ)
        for key in re.findall(r'\$\{([A-Z0-9_]+)', BASE.read_text()):
            env.pop(key, None)
        env.pop('COMPOSE_FILE', None)
        env.pop('APP_CONTAINER_NAME', None)
        env.update(REQUIRED)
        if values: env.update(values)
        if missing: env.pop(missing, None)
        command = ['docker', 'compose', '--env-file', '/dev/null', '-f', str(BASE)]
        if single: command += ['-f', str(ROOT / 'deploy/api/compose.single.yaml')]
        if vendor: command += ['-f', str(ROOT / 'deploy/docker-compose.vendor.yml')]
        result = subprocess.run(command + ['config', '--format', 'json'], cwd=ROOT,
                                env=env, capture_output=True, text=True)
        if not check: return result
        self.assertEqual(result.returncode, 0, result.stderr)
        return json.loads(result.stdout)

    def test_secure_defaults_and_storage_are_isolated(self):
        config = self.render()
        self.assertEqual(set(config['services']), {'api', 'postgres', 'redis'})
        api = config['services']['api']
        self.assertEqual(api['image'], 'sub2api:local')
        self.assertEqual(api['build']['context'], str(ROOT))
        self.assertEqual(api['build']['dockerfile'], 'Dockerfile')
        self.assertNotIn('container_name', api)
        self.assertIn('no-new-privileges:true', api['security_opt'])
        self.assertEqual(api['ports'][0]['host_ip'], '127.0.0.1')
        self.assertEqual(api['environment']['RUN_MODE'], 'standard')
        self.assertEqual(api['environment']['SERVER_TRUSTED_PROXIES'], '')
        self.assertFalse(any(k.startswith('GATEWAY_') for k in api['environment']))
        self.assertFalse(any('docker.sock' in str(v) or 'updater.sock' in str(v) for v in api['volumes']))
        self.assertEqual(set(config['volumes']), {'app_data', 'postgres_data', 'redis_data'})
        self.assertTrue(all(v['name'].startswith('sub2api_') and not v.get('external') for v in config['volumes'].values()))
        self.assertNotIn('ports', config['services']['postgres'])
        self.assertNotIn('ports', config['services']['redis'])

    def test_required_secrets_fail_before_start(self):
        for key in REQUIRED:
            with self.subTest(key=key):
                result = self.render(missing=key, check=False)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(key, result.stderr)

    def test_brand_names_do_not_share_storage(self):
        a = self.render({'COMPOSE_PROJECT_NAME': 'brand-a', 'APP_IMAGE': 'example/api:v1'})
        b = self.render({'COMPOSE_PROJECT_NAME': 'brand-b', 'APP_IMAGE': 'example/api:v1'})
        self.assertEqual(a['services']['api']['image'], b['services']['api']['image'])
        self.assertFalse({v['name'] for v in a['volumes'].values()} & {v['name'] for v in b['volumes'].values()})
        self.assertNotEqual(a['networks']['default']['name'], b['networks']['default']['name'])

    def test_explicit_proxy_scope_and_single_container(self):
        config = self.render({'SERVER_TRUSTED_PROXIES': '192.0.2.1/32', 'APP_CONTAINER_NAME': 'fixture-api'}, single=True)
        self.assertEqual(config['services']['api']['container_name'], 'fixture-api')
        self.assertEqual(config['services']['api']['environment']['SERVER_TRUSTED_PROXIES'], '192.0.2.1/32')
        self.assertNotEqual(self.render(single=True, check=False).returncode, 0)

    def test_supplier_portal_keeps_backend_secrets_out(self):
        config = self.render({'VENDOR_PORTAL_IMAGE':'example/portal:v1',
                              'PORTAL_PUBLIC_ORIGIN':'https://supplier.example.test',
                              'BACKEND_ORIGIN':'http://api:8080'}, vendor=True)
        portal = config['services']['vendor-portal']
        self.assertTrue(portal['read_only'])
        self.assertFalse(any(k in portal['environment'] for k in REQUIRED))
        self.assertTrue(set(portal['networks']) & set(config['services']['api']['networks']))
        self.assertNotIn('portal-updater', (ROOT / 'deploy/vendor-portal/nginx.conf.template').read_text())

if __name__ == '__main__':
    unittest.main()
