"""Real Caddy regression for the supplier site; set CADDY_BIN to the binary."""
import functools
import http.server
import json
import os
from pathlib import Path
import shutil
import socket
import subprocess
import tempfile
import threading
import time
import unittest
import urllib.error
import urllib.request

ROOT = Path(__file__).resolve().parents[2]
CADDY = os.environ.get('CADDY_BIN') or shutil.which('caddy')


class Upstream(http.server.SimpleHTTPRequestHandler):
    def do_GET(self):
        path = self.path.split('?', 1)[0]
        if not path.startswith('/api/'):
            if (path in ['/', '/index.html', '/login', '/forgot-password',
                         '/reset-password', '/vendor', '/vendor-unavailable'] or
                    path.startswith(('/vendor/', '/legal/'))):
                payload = b'<h1>supplier SPA</h1>'
                self.send_response(200)
                self.send_header('Content-Type', 'text/html')
                self.send_header('Cache-Control', 'no-cache')
                self.send_header('Content-Length', str(len(payload)))
                self.end_headers()
                self.wfile.write(payload)
                return
            return super().do_GET()
        payload = json.dumps({
            'code': 'UNAUTHORIZED', 'path': self.path,
            'host': self.headers.get('Host'),
            'forwarded_host': self.headers.get('X-Forwarded-Host'),
            'forwarded_proto': self.headers.get('X-Forwarded-Proto'),
        }).encode()
        self.send_response(401)
        self.send_header('Content-Type', 'application/json')
        self.send_header('Content-Length', str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)

    def log_message(self, *_args):
        pass


class PortalChecks:
    config_name = 'vendor-portal.Caddyfile'
    html_cache_control = 'no-store'

    @classmethod
    def setUpClass(cls):
        cls.tmp = tempfile.TemporaryDirectory(prefix='vendor-caddy-')
        cls.addClassCleanup(cls.tmp.cleanup)
        root = Path(cls.tmp.name)
        (root / 'index.html').write_text('<h1>supplier SPA</h1>')
        (root / 'assets').mkdir()
        (root / 'assets' / 'app.js').write_text('/* supplier asset */')
        (root / 'theme-init.js').write_text('/* supplier theme */')
        (root / 'logo.svg').write_text('<svg></svg>')
        cls.upstream = http.server.ThreadingHTTPServer(
            ('127.0.0.1', 0), functools.partial(Upstream, directory=str(root)))
        cls.addClassCleanup(cls.upstream.server_close)
        threading.Thread(target=cls.upstream.serve_forever, daemon=True).start()
        cls.addClassCleanup(cls.upstream.shutdown)
        with socket.socket() as listener:
            listener.bind(('127.0.0.1', 0))
            port = listener.getsockname()[1]
        cls.origin = f'http://127.0.0.1:{port}'
        config = (ROOT / 'deploy/examples' / cls.config_name).read_text()
        config = config.replace('supplier.example.invalid', cls.origin)
        config = config.replace('/srv/sub2api/vendor-frontend', str(root))
        config = config.replace('127.0.0.1:8080', f'127.0.0.1:{cls.upstream.server_port}')
        config_path = root / 'Caddyfile'
        config_path.write_text('''{
 admin off
 auto_https off
}
''' + config)
        cls.log = (root / 'caddy.log').open('w+')
        cls.addClassCleanup(cls.log.close)
        cls.process = subprocess.Popen(
            [CADDY, 'run', '--config', str(config_path), '--adapter', 'caddyfile'],
            stdout=cls.log, stderr=cls.log,
        )
        cls.addClassCleanup(cls.stop_caddy)
        for _ in range(100):
            if cls.process.poll() is not None:
                break
            try:
                with urllib.request.urlopen(cls.origin + '/vendor/platform', timeout=.2):
                    return
            except (urllib.error.URLError, TimeoutError):
                time.sleep(.05)
        cls.log.seek(0)
        raise RuntimeError('Local Caddy failed to start: ' + cls.log.read())

    @classmethod
    def stop_caddy(cls):
        if cls.process.poll() is None:
            cls.process.terminate()
            cls.process.wait(timeout=5)

    def request(self, path):
        try:
            response = urllib.request.urlopen(self.origin + path, timeout=3)
        except urllib.error.HTTPError as error:
            response = error
        with response:
            return response.status, response.headers, response.read().decode()

    def test_api_preserves_full_path_query_and_supplier_host(self):
        for path in ['/api/v1/admin/vendors?page=1&page_size=20',
                     '/api/v1/admin/vendors/usage', '/api/v1/admin/vendors/settlements',
                     '/api/v1/admin/vendors/group-supply', '/api/v1/admin/vendors/audit-logs',
                     '/api/v1/admin/groups/all', '/api/v1/admin/compliance',
                     '/api/v1/admin/accounts?vendor_id=0',
                     '/api/v1/admin/proxies?vendor_id=3', '/api/v1/vendor/me',
                     '/api/v1/auth/me', '/api/v1/settings/public']:
            with self.subTest(path=path):
                status, headers, body = self.request(path)
                self.assertEqual(status, 401)
                self.assertIn('application/json', headers['Content-Type'])
                data = json.loads(body)
                self.assertEqual(data['path'], path)
                self.assertEqual(data['host'], '127.0.0.1')
                self.assertEqual(data['forwarded_host'], '127.0.0.1')
                self.assertEqual(data['forwarded_proto'], 'http')

    def test_spa_direct_navigation_and_refresh(self):
        for path in ['/', '/vendor/platform', '/vendor/platform?tab=suppliers',
                     '/vendor/platform?tab=accounts', '/vendor/platform?tab=proxies',
                     '/vendor/platform?tab=usage', '/vendor/platform?tab=settlements',
                     '/vendor/platform?tab=supply', '/vendor/platform?tab=audit',
                     '/vendor/accounts', '/vendor/child-accounts', '/vendor/child-proxies',
                     '/vendor/profile', '/login', '/forgot-password',
                     '/reset-password', '/vendor-unavailable', '/legal/privacy']:
            with self.subTest(path=path):
                status, headers, body = self.request(path)
                self.assertEqual(status, 200)
                self.assertEqual(headers['Cache-Control'], self.html_cache_control)
                self.assertIn('supplier SPA', body)

    def test_assets_and_blocked_paths_do_not_fall_back_to_spa(self):
        self.assertEqual(self.request('/assets/app.js')[2], '/* supplier asset */')
        self.assertEqual(self.request('/theme-init.js')[2], '/* supplier theme */')
        self.assertEqual(self.request('/logo.svg')[2], '<svg></svg>')
        for path in ['/assets/missing.js', '/admin', '/admin/vendors',
                     '/register', '/setup', '/api/unknown']:
            with self.subTest(path=path):
                status, _, body = self.request(path)
                self.assertEqual(status, 404)
                self.assertNotIn('supplier SPA', body)


@unittest.skipUnless(CADDY, 'Set CADDY_BIN to run real proxy checks')
class VendorPortalCaddyTests(PortalChecks, unittest.TestCase):
    pass


@unittest.skipUnless(CADDY, 'Set CADDY_BIN to run real proxy checks')
class VendorPortalBackendCaddyTests(PortalChecks, unittest.TestCase):
    config_name = 'vendor-portal-backend.Caddyfile'
    html_cache_control = 'no-cache'

    def test_does_not_expose_the_shared_backend_gateway_or_customer_pages(self):
        for path in ['/v1/chat/completions', '/v1/responses', '/backend-api/settings',
                     '/v1beta/models', '/health', '/dashboard', '/keys', '/payment']:
            with self.subTest(path=path):
                status, _, body = self.request(path)
                self.assertEqual(status, 404)
                self.assertEqual(body, 'Not found')


if __name__ == '__main__':
    unittest.main()
