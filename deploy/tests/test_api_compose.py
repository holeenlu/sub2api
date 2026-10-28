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
    def render(self, values=None, missing=None, single=False, vendor=False, ui=False, web=False, check=True):
        sources = [BASE]
        if single: sources.append(ROOT / 'deploy/api/compose.single.yaml')
        if web or ui: sources.append(ROOT / 'deploy/web/compose.yaml')
        if vendor or ui: sources.append(ROOT / 'deploy/docker-compose.vendor.yml')
        if ui: sources.append(ROOT / 'deploy/local/compose.ui.yaml')
        env = dict(os.environ)
        for source in sources:
            for key in re.findall(r'\$\{([A-Z0-9_]+)', source.read_text()):
                env.pop(key, None)
        env.pop('COMPOSE_FILE', None)
        env.pop('APP_CONTAINER_NAME', None)
        env.update(REQUIRED)
        if values: env.update(values)
        if missing: env.pop(missing, None)
        command = ['docker', 'compose', '--env-file', '/dev/null']
        for source in sources: command += ['-f', str(source)]
        result = subprocess.run(command + ['config', '--format', 'json'], cwd=ROOT,
                                env=env, capture_output=True, text=True)
        if not check: return result
        self.assertEqual(result.returncode, 0, result.stderr)
        return json.loads(result.stdout)

    def test_generic_frontend_defaults_do_not_conflict(self):
        config = self.render(web=True, vendor=True, values={
            'WEB_PUBLIC_ORIGIN': 'https://console.example.test',
            'PORTAL_PUBLIC_ORIGIN': 'https://supplier.example.test',
            'BACKEND_ORIGIN': 'http://api:8080',
        })
        ports = [config['services'][role]['ports'][0]['published'] for role in ['api', 'web', 'vendor-portal']]
        self.assertEqual(len(set(ports)), 3)
        self.assertEqual(ports, ['8080', '8082', '8081'])

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

    def test_local_ui_shares_api_network_without_exposing_storage_or_secrets(self):
        config = self.render({'COMPOSE_PROJECT_NAME': 'ui-contract-test',
                              'WEB_IMAGE': 'example/web:snapshot', 'VENDOR_PORTAL_IMAGE': 'example/vendor:snapshot',
                              'WEB_PUBLIC_ORIGIN': 'http://127.0.0.1:32181',
                              'PORTAL_PUBLIC_ORIGIN': 'http://127.0.0.1:32182',
                              'BACKEND_ORIGIN': 'http://api:8080'}, ui=True)
        self.assertEqual(set(config['services']), {'api', 'postgres', 'redis', 'web', 'vendor-portal'})
        for name, port, image in [('web', 32181, 'example/web:snapshot'), ('vendor-portal', 32182, 'example/vendor:snapshot')]:
            service = config['services'][name]
            self.assertEqual(service['image'], image)
            self.assertEqual(service['pull_policy'], 'never')
            self.assertTrue(service['read_only'])
            self.assertEqual(service['cap_drop'], ['ALL'])
            self.assertIn('no-new-privileges:true', service['security_opt'])
            self.assertEqual(len(service['ports']), 1)
            self.assertEqual(service['ports'][0]['host_ip'], '127.0.0.1')
            self.assertEqual(int(service['ports'][0]['published']), port)
            self.assertEqual(service['environment']['BACKEND_ORIGIN'], 'http://api:8080')
            self.assertFalse(any(key in service['environment'] for key in REQUIRED))
            self.assertFalse(service.get('volumes'))
            self.assertTrue(set(service['networks']) & set(config['services']['api']['networks']))
        self.assertNotIn('ports', config['services']['postgres'])
        self.assertNotIn('ports', config['services']['redis'])

if __name__ == '__main__':
    unittest.main()
