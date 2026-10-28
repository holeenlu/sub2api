"""Opt-in real Docker proxy tests; all backend responses are explicit fixtures.

VENDOR_PORTAL_TEST_IMAGE=local-image python3 -m unittest deploy.tests.test_vendor_portal_image -v
"""
import base64
import hashlib
import http.client
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import os
from pathlib import Path
import re
import socket
import ssl
import subprocess
import sys
import tempfile
import threading
import time
import unittest
import uuid


class BackendFixture(BaseHTTPRequestHandler):
    def log_message(self, *_):
        pass

    def do_GET(self):
        if self.path == '/v1/images/edits?fixture_upload=1':
            remaining = int(self.headers.get('Content-Length', 0))
            received = 0
            while remaining:
                chunk = self.rfile.read(min(remaining, 256 * 1024))
                if not chunk:
                    break
                received += len(chunk)
                remaining -= len(chunk)
            content = json.dumps({'received_bytes': received}).encode()
            self.send_response(200)
            self.send_header('Content-Type', 'application/json')
            self.send_header('Content-Length', str(len(content)))
            self.end_headers()
            self.wfile.write(content)
            return
        if self.path == '/v1/responses?fixture_sse=1':
            self.send_response(200)
            self.send_header('Content-Type', 'text/event-stream')
            self.end_headers()
            self.wfile.write(b'data: {"fixture":1}\n\n')
            self.wfile.flush()
            self.server.sse_release.wait(5)
            self.wfile.write(b'data: [DONE]\n\n')
            return
        if self.path == '/v1/responses' and self.headers.get('Upgrade', '').lower() == 'websocket':
            key = self.headers['Sec-WebSocket-Key']
            accept = base64.b64encode(hashlib.sha1((key + '258EAFA5-E914-47DA-95CA-C5AB0DC85B11').encode()).digest()).decode()
            self.send_response(101)
            self.send_header('Upgrade', 'websocket')
            self.send_header('Connection', 'Upgrade')
            self.send_header('Sec-WebSocket-Accept', accept)
            self.end_headers()
            self.wfile.flush()
            self.connection.settimeout(5)
            header = self.rfile.read(2)
            if len(header) == 2 and header[1] & 128 and header[1] & 127 < 126:
                mask = self.rfile.read(4)
                payload = self.rfile.read(header[1] & 127)
                payload = bytes(value ^ mask[index % 4] for index, value in enumerate(payload))
                self.wfile.write(bytes([129, len(payload)]) + payload)
                self.wfile.flush()
            return
        status = int(self.path.rsplit('/', 1)[-1]) if re.search(r'/status/(401|403|409|500)$', self.path) else 200
        body = self.rfile.read(int(self.headers.get('Content-Length', 0))).decode()
        content = json.dumps(dict(path=self.path, method=self.command, body=body,
                                  host=self.headers.get('Host'), origin=self.headers.get('Origin'),
                                  proto=self.headers.get('X-Forwarded-Proto'),
                                  forwarded=self.headers.get('X-Forwarded-For'),
                                  authorization=self.headers.get('Authorization'))).encode()
        self.send_response(status)
        self.send_header('Content-Type', 'application/json')
        self.send_header('Content-Length', str(len(content)))
        self.end_headers()
        self.wfile.write(content)

    do_POST = do_GET
    do_PATCH = do_GET
    do_DELETE = do_GET
    do_OPTIONS = do_GET


class ProxyImageChecks:
    image_environment = 'VENDOR_PORTAL_TEST_IMAGE'
    origin_environment = 'PORTAL_PUBLIC_ORIGIN'
    trusted_environment = 'PORTAL_TRUSTED_PROXY_CIDRS'
    @classmethod
    def docker(cls, *args, check=True):
        return subprocess.run(['docker', *args], capture_output=True, text=True, check=check)

    @classmethod
    def setUpClass(cls):
        cls.image = os.environ[cls.image_environment]
        cls.prefix = 'vendor-portal-test-' + uuid.uuid4().hex[:10]
        cls.containers = []
        cls.networks = []
        cls.addClassCleanup(cls.cleanup)
        cls.host_gateway = cls.docker('run', '--rm', '--add-host', 'host.docker.internal:host-gateway',
                                      '--entrypoint', 'getent', cls.image, 'hosts', 'host.docker.internal').stdout.split()[0]
        # Desktop's host-gateway is the Linux VM, while its special DNS name
        # reaches the native host running this fixture. Linux uses the bridge IP.
        if 'Docker Desktop' in cls.docker('info', '--format', '{{.OperatingSystem}}').stdout:
            cls.host_gateway = 'host.docker.internal'
        if ':' in cls.host_gateway:
            cls.host_gateway = '[' + cls.host_gateway + ']'
        for suffix in ['local', 'remote']:
            network = cls.prefix + '-' + suffix
            cls.docker('network', 'create', network)
            cls.networks.append(network)
        cls.temp = tempfile.TemporaryDirectory()
        cls.addClassCleanup(cls.temp.cleanup)
        cls.http = ThreadingHTTPServer(('0.0.0.0', 0), BackendFixture)
        cls.http.sse_release = threading.Event()
        cls.addClassCleanup(cls.http.server_close)
        cls.addClassCleanup(cls.http.shutdown)
        threading.Thread(target=cls.http.serve_forever, daemon=True).start()
        root = Path(cls.temp.name)
        (root / 'cert.cnf').write_text('[req]\ndistinguished_name=dn\nx509_extensions=san\n[dn]\n[san]\nsubjectAltName=DNS:backend.test\n')
        subprocess.run(['openssl', 'req', '-x509', '-newkey', 'rsa:2048', '-nodes', '-days', '1',
                        '-subj', '/CN=backend.test', '-config', str(root/'cert.cnf'),
                        '-keyout', str(root/'key.pem'), '-out', str(root/'cert.pem')],
                       check=True, capture_output=True)

    @classmethod
    def cleanup(cls):
        for name in reversed(cls.containers):
            cls.docker('rm', '-f', name, check=False)
        for name in reversed(cls.networks):
            cls.docker('network', 'rm', name, check=False)

    def start_portal(self, backend=None, extra=(), trusted='', network=None):
        name = self.prefix + '-' + uuid.uuid4().hex[:8]
        self.containers.append(name)
        self.docker('run', '-d', '--name', name, '--network', network or self.networks[1],
                    '--add-host', 'host.docker.internal:host-gateway',
                    '--read-only', '--tmpfs', '/tmp:rw,noexec,nosuid,size=128m', '--cap-drop', 'ALL',
                    '--security-opt', 'no-new-privileges:true', '-p', '127.0.0.1::8080',
                    '-e', self.origin_environment + '=https://supplier.example.test',
                    '-e', 'BACKEND_ORIGIN=' + (backend or f'http://{self.host_gateway}:{self.http.server_port}'),
                    '-e', self.trusted_environment + '=' + trusted, *extra, self.image)
        binding = self.docker('port', name, '8080', check=False)
        if binding.returncode:
            logs = self.docker('logs', name, check=False)
            self.fail(logs.stdout + logs.stderr)
        port = int(binding.stdout.strip().rsplit(':', 1)[1])
        for _ in range(60):
            try:
                if self.request(port, '/health', host='127.0.0.1')[0] == 200:
                    return name, port
            except (OSError, http.client.HTTPException):
                pass
            time.sleep(.1)
        self.fail(self.docker('logs', name, check=False).stdout + self.docker('logs', name, check=False).stderr)

    def request(self, port, path, method='GET', body=None, headers=None, host='supplier.example.test'):
        conn = http.client.HTTPConnection('127.0.0.1', port, timeout=15)
        conn.request(method, path, body, {'Host': host, **(headers or {})})
        response = conn.getresponse()
        result = response.status, dict(response.getheaders()), response.read()
        conn.close()
        return result


@unittest.skipUnless(os.environ.get('VENDOR_PORTAL_TEST_IMAGE'), 'VENDOR_PORTAL_TEST_IMAGE not set')
class VendorPortalImageTests(ProxyImageChecks, unittest.TestCase):
    def test_query_secrets_stay_out_of_success_and_failure_logs(self):
        secret = 'fixture-oauth-state-do-not-log'
        for backend, expected in [(None, 200), ('http://127.0.0.1:9', 502)]:
            with self.subTest(backend=backend):
                name, port = self.start_portal(backend) if backend else self.start_portal()
                self.assertEqual(self.request(port, '/api/v1/settings/public?state=' + secret)[0], expected)
                result = self.docker('logs', name)
                self.assertNotIn(secret, result.stdout + result.stderr)

    def test_vendor_keeps_its_existing_64_mib_body_limit(self):
        _, port = self.start_portal()
        connection = http.client.HTTPConnection('127.0.0.1', port, timeout=10)
        try:
            connection.putrequest('POST', '/api/v1/vendor/accounts', skip_host=True)
            connection.putheader('Host', 'supplier.example.test')
            connection.putheader('Content-Length', str(65 * 1024 * 1024))
            connection.endheaders()
            self.assertEqual(connection.getresponse().status, 413)
        finally:
            connection.close()

    def test_static_routes_cache_and_denied_gateway_paths(self):
        _, port = self.start_portal()
        for path in ['/', '/login', '/vendor/platform', '/vendor/accounts', '/vendor/settlements']:
            with self.subTest(path=path):
                status, headers, body = self.request(port, path)
                self.assertEqual(status, 200)
                self.assertIn(b'<html', body)
                self.assertEqual(headers['Cache-Control'], 'no-store')
        index = self.request(port, '/')[2].decode()
        asset = re.search(r'(?:src|href)="(/assets/[^" ]+\.js)"', index)[1]
        self.assertIn('immutable', self.request(port, asset)[1]['Cache-Control'])
        for path in ['/assets/missing.js', '/api/missing', '/api/v10/users', '/v1/responses', '/v1beta/models', '/responses', '/backend-api/codex/responses', '/antigravity/models', '/admin/settings', '/register', '/setup', '/v3/contents/generations/tasks', '/chat/completions', '/models', '/images/generations', '/realtime']:
            self.assertEqual(self.request(port, path)[0], 404, path)
        self.assertEqual(self.request(port, '/', host='attacker.example')[0], 404)
        self.assertEqual(self.request(port, '/assets/missing.js')[1]['Cache-Control'], 'no-store')

    def test_remote_api_keeps_path_scope_body_and_authorization(self):
        _, port = self.start_portal()
        for method, path in [('GET', '/api/v1/admin/vendors?page=1&search=a%2Fb'),
                             ('PATCH', '/api/v1/admin/accounts/9?vendor_id=2'),
                             ('POST', '/api/v1/vendor/accounts'), ('DELETE', '/api/v1/vendor/accounts/9'),
                             ('OPTIONS', '/api/v1/vendor/accounts')]:
            with self.subTest(method=method, path=path):
                status, _, body = self.request(port, path, method, '{"name":"fixture"}',
                    {'Authorization': 'Bearer fixture-only', 'Origin': 'https://supplier.example.test',
                     'X-Forwarded-For': '203.0.113.77', 'X-Forwarded-Proto': 'http', 'X-Forwarded-Host': 'attacker.example'})
                data = json.loads(body)
                self.assertEqual(status, 200)
                self.assertEqual(data['path'], path)
                self.assertEqual(data['method'], method)
                self.assertEqual(data['body'], '{"name":"fixture"}')
                self.assertEqual(data['host'], 'supplier.example.test')
                self.assertEqual(data['proto'], 'https')
                self.assertEqual(data['origin'], 'https://supplier.example.test')
                self.assertEqual(data['authorization'], 'Bearer fixture-only')
                self.assertNotEqual(data['forwarded'], '203.0.113.77')
        for code in [401, 403, 409, 500]:
            status, headers, body = self.request(port, '/api/v1/status/' + str(code))
            self.assertEqual(status, code)
            self.assertEqual(headers['Content-Type'], 'application/json')
            self.assertNotIn(b'<html', body)

    def test_same_network_backend_may_start_after_portal(self):
        root = Path(__file__).resolve().parents[2]
        project = self.prefix + '-compose'
        command = ['docker', 'compose', '-p', project]
        for file in ['api/compose.yaml', 'docker-compose.vendor.yml']:
            command.extend(['-f', str(root/'deploy'/file)])
        env = {**os.environ, 'VENDOR_PORTAL_IMAGE': self.image, 'PORTAL_PORT': '0',
               'PORTAL_PUBLIC_ORIGIN': 'https://supplier.example.test', 'BACKEND_ORIGIN': 'http://placeholder:8080',
               'POSTGRES_PASSWORD': 'fixture-only', 'REDIS_PASSWORD': 'fixture-only', 'ADMIN_EMAIL': 'test@example.invalid', 'ADMIN_PASSWORD': 'fixture-only', 'JWT_SECRET': 'a' * 64, 'TOTP_ENCRYPTION_KEY': 'b' * 64}
        config = json.loads(subprocess.run([*command, 'config', '--format', 'json'],
                            env=env, capture_output=True, text=True, check=True).stdout)
        backend = next(key for key in config['services'] if key not in {'vendor-portal', 'postgres', 'redis'})
        env['BACKEND_ORIGIN'] = 'http://' + backend + ':8080'
        # Only the backend command/image is replaced; use the actual Compose
        # network declarations, using the shared project default network.
        override = Path(self.temp.name)/'backend-fixture.json'
        override.write_text(json.dumps({'services': {backend: {'image': 'python:3.12-alpine', 'pull_policy': 'missing'}}}))
        command.extend(['-f', str(override)])
        self.addCleanup(lambda: subprocess.run([*command, 'down', '-v', '--remove-orphans'],
                                             env=env, capture_output=True, check=True))
        subprocess.run([*command, 'up', '-d', '--no-deps', 'vendor-portal'],
                       env=env, capture_output=True, text=True, check=True)
        binding = subprocess.run([*command, 'port', 'vendor-portal', '8080'],
                                 env=env, capture_output=True, text=True, check=True).stdout
        port = int(binding.strip().rsplit(':', 1)[1])
        subprocess.run([*command, 'run', '-d', '--no-deps', '--use-aliases', '--entrypoint', 'python',
                        '-v', str(Path(__file__).resolve()) + ':/fixture.py:ro',
                        backend, '/fixture.py', '--fixture'],
                       env=env, capture_output=True, text=True, check=True)
        for _ in range(60):
            try:
                status, _, body = self.request(port, '/api/v1/vendor/me')
            except (OSError, http.client.HTTPException):
                time.sleep(.1)
                continue
            if status == 200:
                self.assertEqual(json.loads(body)['host'], 'supplier.example.test')
                return
            time.sleep(.1)
        self.fail('late backend never became available')

    def test_explicit_proxy_trust_preserves_client_ip(self):
        _, port = self.start_portal()
        peer = json.loads(self.request(port, '/api/v1/vendor/me')[2])['forwarded']
        _, trusted = self.start_portal(trusted=peer)
        data = json.loads(self.request(trusted, '/api/v1/vendor/me', headers={'X-Forwarded-For': '198.51.100.24'})[2])
        self.assertEqual(data['forwarded'], '198.51.100.24')

    def test_https_backend_verifies_certificate_and_preserves_supplier_host(self):
        upstream = self.prefix + '-tls'
        self.containers.append(upstream)
        self.docker('run', '-d', '--name', upstream, '--network', self.networks[1],
                    '--network-alias', 'backend.test', '--network-alias', 'wrong.test',
                    '-v', str(Path(__file__).resolve()) + ':/fixture.py:ro', '-v', self.temp.name + ':/tls:ro',
                    '-e', 'FIXTURE_TLS=1', 'python:3.12-alpine', 'python', '/fixture.py', '--fixture')
        ca = str(Path(self.temp.name) / 'cert.pem') + ':/etc/ssl/certs/ca-certificates.crt:ro'
        backend = 'https://backend.test:8080'
        name, port = self.start_portal(backend, extra=('-v', ca))
        for _ in range(60):
            status, _, body = self.request(port, '/api/v1/vendor/me')
            if status == 200:
                break
            time.sleep(.1)
        self.assertEqual(status, 200, (body, self.docker('logs', name, check=False).stderr))
        self.assertEqual(json.loads(body)['host'], 'supplier.example.test')
        untrusted_name, untrusted = self.start_portal(backend)
        self.assertEqual(self.request(untrusted, '/api/v1/vendor/me')[0], 502)
        self.assertIn('certificate verify error', self.docker('logs', untrusted_name, check=False).stderr)
        wrong_name, wrong = self.start_portal('https://wrong.test:8080', extra=('-v', ca))
        self.assertEqual(self.request(wrong, '/api/v1/vendor/me')[0], 502)
        self.assertIn('does not match', self.docker('logs', wrong_name, check=False).stderr)

    def test_dead_backend_does_not_become_spa_success(self):
        _, port = self.start_portal('http://127.0.0.1:9')
        self.assertEqual(self.request(port, '/vendor/platform')[0], 200)
        self.assertEqual(self.request(port, '/api/v1/admin/vendors')[0], 502)

    def test_configuration_rejects_injection_paths_and_credentials(self):
        for value in ['https://user:secret@backend.test', 'http://backend.test/api/v1',
                      'http://backend.test?x=1', 'http://backend.test:0', 'http://backend.test:65536',
                      'http://backend.test\ninclude /tmp/bad;', 'file:///tmp/config']:
            with self.subTest(value=value):
                result = self.docker('run', '--rm', '-e', 'PORTAL_PUBLIC_ORIGIN=https://supplier.example.test',
                                     '-e', 'BACKEND_ORIGIN=' + value, self.image, check=False)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn('vendor-portal:', result.stderr)

    def test_image_runs_without_root_backend_or_database_credentials(self):
        data = json.loads(self.docker('image', 'inspect', self.image).stdout)[0]['Config']
        self.assertEqual(data['User'], '101:101')
        self.assertFalse(any(re.match(r'(DATABASE_|REDIS_|JWT_SECRET|TOTP_ENCRYPTION_KEY)', value) for value in data['Env']))
        self.assertEqual(data['Entrypoint'], ['/usr/local/bin/vendor-portal-entrypoint'])
        result = self.docker('run', '--rm', '--entrypoint', 'sh', self.image, '-c',
                             'test ! -e /app/kdan && test ! -e /app/sub2api && ! command -v node && ! command -v go')
        self.assertEqual(result.returncode, 0)

    def test_compose_topologies_keep_storage_and_secrets_on_backend(self):
        root = Path(__file__).resolve().parents[2]
        env = {**os.environ, 'VENDOR_PORTAL_IMAGE': self.image,
               'PORTAL_PUBLIC_ORIGIN': 'https://supplier.example.test', 'BACKEND_ORIGIN': 'http://api:8080',
               'POSTGRES_PASSWORD': 'fixture-only', 'REDIS_PASSWORD': 'fixture-only', 'ADMIN_EMAIL': 'test@example.invalid', 'ADMIN_PASSWORD': 'fixture-only', 'JWT_SECRET': 'a' * 64, 'TOTP_ENCRYPTION_KEY': 'b' * 64,
               'SERVER_TRUSTED_PROXIES': '192.0.2.1/32'}
        for mode, files in {
            'same-host': ['api/compose.yaml', 'docker-compose.vendor.yml'],
            'portal-only': ['docker-compose.vendor.yml'],
            'portal-tls': ['docker-compose.vendor.yml', 'docker-compose.vendor-tls.yml'],
        }.items():
            with self.subTest(mode=mode):
                command = ['docker', 'compose', '-p', 'portal-config-test']
                for file in files:
                    command.extend(['-f', str(root/'deploy'/file)])
                result = subprocess.run([*command, 'config', '--format', 'json'], cwd=root,
                                        env=env, capture_output=True, text=True, check=True)
                services = json.loads(result.stdout)['services']
                portal = services['vendor-portal']
                self.assertTrue(portal['read_only'])
                self.assertEqual(set(portal['environment']), {'PORTAL_PUBLIC_ORIGIN', 'BACKEND_ORIGIN', 'PORTAL_TRUSTED_PROXY_CIDRS'})
                backend_names = set(services) - {'vendor-portal', 'vendor-edge', 'postgres', 'redis'}
                if mode == 'same-host':
                    self.assertEqual(len(backend_names), 1)
                    backend_service = services[backend_names.pop()]
                    self.assertTrue(set(portal['networks']) & set(backend_service['networks']))
                if mode == 'same-host':
                    self.assertEqual(len(services), 4)
                    self.assertTrue({'postgres', 'redis'} <= services.keys())
                elif mode == 'portal-only':
                    self.assertEqual(set(services), {'vendor-portal'})
                else:
                    self.assertEqual(set(services), {'vendor-portal', 'vendor-edge'})
                    self.assertEqual(portal['environment']['PORTAL_TRUSTED_PROXY_CIDRS'], '172.30.88.2/32')


@unittest.skipUnless(os.environ.get('WEB_TEST_IMAGE'), 'WEB_TEST_IMAGE not set')
class WebImageTests(ProxyImageChecks, unittest.TestCase):
    image_environment = 'WEB_TEST_IMAGE'
    origin_environment = 'WEB_PUBLIC_ORIGIN'
    trusted_environment = 'WEB_TRUSTED_PROXY_CIDRS'

    def test_spa_assets_and_rootless_runtime(self):
        name, port = self.start_portal()
        for path in ['/', '/login', '/dashboard', '/keys', '/admin/accounts', '/setup', '/legal/privacy',
                     '/images', '/images/gallery', '/chat/history', '/models/plaza/history',
                     '/videos/gallery/history', '/messages/inbox', '/alpha/overview',
                     '/contents/drafts', '/backend-api/overview', '/antigravity/overview']:
            status, headers, body = self.request(port, path)
            self.assertEqual(status, 200, path)
            self.assertIn(b'<html', body)
            self.assertEqual(headers['Cache-Control'], 'no-store')
        index = self.request(port, '/')[2].decode()
        asset = re.search(r'(?:src|href)="(/assets/[^" ]+\.js)"', index)[1]
        self.assertIn('immutable', self.request(port, asset)[1]['Cache-Control'])
        self.assertEqual(self.request(port, '/assets/missing.js')[0], 404)
        self.assertEqual(self.request(port, '/assets/missing.js')[1]['Cache-Control'], 'no-store')
        self.assertEqual(self.request(port, '/', host='attacker.example')[0], 404)
        self.assertEqual(self.docker('exec', name, 'id', '-u').stdout.strip(), '101')
        self.assertEqual(self.docker('exec', name, 'sh', '-c', 'test -r /usr/share/licenses/sub2api/LICENSE && ! command -v node && ! command -v go').returncode, 0)

    def test_gateway_and_control_paths_preserve_error_body_and_trust_boundary(self):
        _, port = self.start_portal()
        for path in ['/api/v1/settings/public', '/v1/responses', '/v1beta/models', '/responses',
                     '/backend-api/codex/responses', '/antigravity/v1/messages', '/setup/status',
                     '/v3/contents/generations/tasks', '/contents/generations/tasks', '/models',
                     '/messages/count_tokens', '/alpha/search', '/chat/completions', '/embeddings',
                     '/images/edits', '/videos', '/tts', '/stt', '/custom-voices', '/realtime', '/web_search', '/x_search']:
            status, _, body = self.request(port, path, 'POST', '{"fixture":true}',
                {'Authorization': 'Bearer fixture-only', 'X-Forwarded-For': '203.0.113.77',
                 'X-Forwarded-Proto': 'http', 'X-Forwarded-Host': 'attacker.example'})
            data = json.loads(body)
            self.assertEqual(status, 200)
            self.assertEqual(data['path'], path)
            self.assertEqual(data['body'], '{"fixture":true}')
            self.assertEqual(data['authorization'], 'Bearer fixture-only')
            self.assertEqual(data['host'], 'supplier.example.test')
            self.assertEqual(data['proto'], 'https')
            self.assertNotEqual(data['forwarded'], '203.0.113.77')
        for code in [401, 403, 409, 500]:
            status, headers, _ = self.request(port, '/api/v1/status/' + str(code))
            self.assertEqual(status, code)
            self.assertEqual(headers['Content-Type'], 'application/json')
        _, dead = self.start_portal('http://127.0.0.1:9')
        self.assertEqual(self.request(dead, '/api/v1/settings/public')[0], 502)

    def test_sse_first_event_arrives_before_terminal_event(self):
        _, port = self.start_portal()
        self.http.sse_release.clear()
        connection = http.client.HTTPConnection('127.0.0.1', port, timeout=2)
        try:
            connection.request('GET', '/v1/responses?fixture_sse=1', headers={'Host': 'supplier.example.test'})
            response = connection.getresponse()
            self.assertEqual(response.status, 200)
            self.assertEqual(response.headers['Content-Type'], 'text/event-stream')
            self.assertEqual(response.readline(), b'data: {"fixture":1}\n')
            self.assertFalse(self.http.sse_release.is_set())
            self.http.sse_release.set()
            self.assertIn(b'data: [DONE]', response.read())
        finally:
            self.http.sse_release.set()
            connection.close()

    def test_main_streams_uploads_larger_than_vendor_limit(self):
        _, port = self.start_portal()
        connection = http.client.HTTPConnection('127.0.0.1', port, timeout=30)
        try:
            connection.putrequest('POST', '/v1/images/edits?fixture_upload=1', skip_host=True)
            connection.putheader('Host', 'supplier.example.test')
            connection.putheader('Content-Type', 'application/octet-stream')
            connection.putheader('Content-Length', str(65 * 1024 * 1024))
            connection.endheaders()
            chunk = b'x' * (1024 * 1024)
            for _ in range(65):
                connection.send(chunk)
            response = connection.getresponse()
            self.assertEqual(response.status, 200)
            self.assertEqual(json.loads(response.read())['received_bytes'], 65 * 1024 * 1024)
        finally:
            connection.close()

    def test_capability_image_token_and_query_are_absent_from_all_logs(self):
        token = 'fixture-capability-do-not-log'
        query = 'fixture-query-do-not-log'
        for backend, status in [(None, 200), ('http://127.0.0.1:9', 502)]:
            with self.subTest(backend=backend):
                name, port = self.start_portal(backend) if backend else self.start_portal()
                self.assertEqual(self.request(port, '/api/bps-images/' + token + '?proof=' + query)[0], status)
                if backend is None:
                    rejected = self.request(port, '/api/bps-images/' + token + '?proof=' + query,
                                            headers={'Cookie': 'fixture=' + 'x' * 20000})[0]
                    self.assertIn(rejected, [400, 431])
                result = self.docker('logs', name)
                logs = result.stdout + result.stderr
                self.assertIn('/api/bps-images/[redacted]', logs)
                self.assertNotIn(token, logs)
                self.assertNotIn(query, logs)

    def test_registered_alias_trailing_slashes_reach_backend(self):
        _, port = self.start_portal()
        for method, path in [('GET', '/models/'), ('GET', '/setup/status/'), ('POST', '/images/edits/')]:
            with self.subTest(method=method, path=path):
                status, headers, body = self.request(port, path, method=method, body=b'{}' if method == 'POST' else None)
                self.assertEqual(status, 200)
                self.assertEqual(headers['Content-Type'], 'application/json')
                self.assertEqual(json.loads(body)['path'], path)

    def test_websocket_upgrade_and_bidirectional_frame(self):
        _, port = self.start_portal()
        with socket.create_connection(('127.0.0.1', port), timeout=5) as connection:
            connection.sendall(b'GET /v1/responses HTTP/1.1\r\nHost: supplier.example.test\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n\r\n')
            reader = connection.makefile('rb')
            self.addCleanup(reader.close)
            self.assertIn(b'101', reader.readline())
            headers = {}
            while True:
                line = reader.readline()
                if line == b'\r\n':
                    break
                self.assertTrue(line, 'Upgrade connection ended before headers')
                key, value = line.decode().split(':', 1)
                headers[key.lower()] = value.strip()
            self.assertEqual(headers['sec-websocket-accept'], 's3pPLMBiTxaQ9kYGzzhZRbK+xOo=')
            payload, mask = b'fixture-echo', b'1234'
            connection.sendall(bytes([129, 128 | len(payload)]) + mask + bytes(value ^ mask[index % 4] for index, value in enumerate(payload)))
            self.assertEqual(reader.read(2), bytes([129, len(payload)]))
            self.assertEqual(reader.read(len(payload)), payload)


if __name__ == '__main__':
    if '--fixture' in sys.argv:
        server = ThreadingHTTPServer(('0.0.0.0', 8080), BackendFixture)
        if os.environ.get('FIXTURE_TLS'):
            tls = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
            tls.load_cert_chain('/tls/cert.pem', '/tls/key.pem')
            server.socket = tls.wrap_socket(server.socket, server_side=True)
        server.serve_forever()
    else:
        unittest.main()
