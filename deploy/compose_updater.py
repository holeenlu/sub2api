#!/usr/bin/env python3
"""Restricted, local Compose updater. Never expose the Docker socket to the app."""
import argparse
import http.server
import json
import os
from pathlib import Path
import re
import socketserver
import subprocess
import sys
import threading
import time
import urllib.request
from urllib.parse import quote, urlsplit

VERSION = re.compile(r"(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)(?:\.(?:0|[1-9][0-9]*))?")
DIGEST = re.compile(r"sha256:[0-9a-f]{64}")


class SafeRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        parsed = urlsplit(newurl)
        if parsed.scheme != 'https' or parsed.username or parsed.hostname not in (
                'api.github.com', 'github.com', 'objects.githubusercontent.com',
                'release-assets.githubusercontent.com'):
            raise ValueError('Untrusted release asset redirect')
        redirected = super().redirect_request(req, fp, code, msg, headers, newurl)
        if parsed.hostname != 'api.github.com':
            redirected.remove_header('Authorization')
        return redirected


def read_json(url, token='', asset=False):
    if not url.startswith('https://api.github.com/repos/'):
        raise ValueError('Expected GitHub API URL')
    headers = {'Accept': 'application/octet-stream' if asset else 'application/vnd.github+json',
               'User-Agent': 'Sub2API-Compose-Updater'}
    if token:
        headers['Authorization'] = 'Bearer ' + token
    req = urllib.request.Request(url, headers=headers)
    with urllib.request.build_opener(SafeRedirect()).open(req, timeout=30) as response:
        data = response.read(1024 * 1024 + 1)
        if len(data) > 1024 * 1024:
            raise ValueError('Release metadata too large')
        return json.loads(data)


def atomic_json(path, data):
    path = Path(path)
    temporary = path.with_suffix(path.suffix + '.tmp')
    temporary.write_text(json.dumps(data, indent=2) + '\n')
    os.chmod(temporary, 0o600)
    temporary.replace(path)


class Updater:
    def __init__(self, config):
        self.config = config
        self.directory = Path(config['project_directory']).resolve(strict=True)
        self.state_path = Path(config['state_file'])
        self.override = self.directory / 'compose.online-update.json'
        self.lock = threading.Lock()
        self.state = {'status': 'idle'}
        if self.state_path.exists():
            self.state = json.loads(self.state_path.read_text())
            if self.state.get('status') == 'running':
                self.state = {'status': 'failed', 'message': 'Updater interrupted; inspect containers before retrying'}
                atomic_json(self.state_path, self.state)
        self.compose = ['docker', 'compose', '--project-directory', str(self.directory),
                        '--project-name', config['project_name']]
        for file in config['compose_files']:
            path = Path(file)
            if not path.is_absolute():
                path = self.directory / path
            self.compose += ['-f', str(path.resolve(strict=True))]
        # This file is always present, so every update uses the same project model.
        if not self.override.exists():
            atomic_json(self.override, {'services': {}})
        self.compose += ['-f', str(self.override)]

    def command(self, *args, timeout=600):
        result = subprocess.run(args, cwd=self.directory, text=True, capture_output=True, timeout=timeout)
        if result.returncode:
            # Docker output can contain environment or registry credentials. Keep
            # it out of API responses; admins inspect the host journal directly.
            print(result.stderr, file=sys.stderr, flush=True)
            raise RuntimeError(f'{args[0]} operation failed (exit {result.returncode})')
        return result.stdout.strip()

    def save(self, **state):
        self.state = state
        atomic_json(self.state_path, state)

    def image_for(self, version):
        cfg = self.config
        token = Path(cfg['github_token_file']).read_text().strip() if cfg.get('github_token_file') else ''
        api = 'https://api.github.com/repos/' + cfg['repository']
        tag = cfg['channel'] + '/v' + version
        release = read_json(api + '/releases/tags/' + quote(tag, safe=''), token)
        if release.get('draft') or release.get('prerelease') or release.get('tag_name') != tag:
            raise ValueError('Requested version is not a published stable release')
        asset = next(a for a in release['assets'] if a['name'] == 'release-manifest.json')
        expected_url = api + '/releases/assets/' + str(asset['id'])
        if asset['url'] != expected_url:
            raise ValueError('Asset does not belong to the configured repository')
        manifest = read_json(expected_url, token, asset=True)
        if (manifest.get('channel') != cfg['channel'] or manifest.get('version') != version
                or manifest.get('image') != cfg['image'] or not DIGEST.fullmatch(manifest.get('image_digest', ''))):
            raise ValueError('Release manifest does not match the configured channel/image')
        return cfg['image'] + '@' + manifest['image_digest']

    def container(self):
        ids = self.command(*self.compose, 'ps', '-q', self.config['service']).splitlines()
        if len(ids) != 1:
            raise ValueError('Exactly one running application container is required')
        return ids[0]

    def inspect(self, container):
        return json.loads(self.command('docker', 'inspect', container))[0]

    def wait_healthy(self, expected_image):
        deadline = time.monotonic() + self.config.get('health_timeout', 180)
        while time.monotonic() < deadline:
            try:
                info = self.inspect(self.container())
                if info['Image'] == expected_image and info['State'].get('Health', {}).get('Status') == 'healthy':
                    return
            except (ValueError, RuntimeError, IndexError):
                pass
            time.sleep(3)
        raise RuntimeError('Application health check timed out')

    def apply(self, version):
        previous_image = None
        changed = False
        try:
            # Let the web server return its accepted response before replacement.
            time.sleep(2)
            image = self.image_for(version)
            old = self.inspect(self.container())
            if not old.get('Config', {}).get('Healthcheck'):
                raise ValueError('A Docker health check is required for online updates')
            previous_image = old['Image']
            self.command('docker', 'pull', image)
            new_image = self.inspect(image)['Id']
            atomic_json(self.override, {'services': {self.config['service']: {'image': image}}})
            changed = True
            self.command(*self.compose, 'up', '-d', '--no-deps', '--pull', 'never', self.config['service'])
            self.wait_healthy(new_image)
            self.save(status='succeeded', version=version, image=image, previous_image=previous_image)
        except Exception as error:
            message = str(error)
            if changed and previous_image:
                try:
                    # Pin the exact old image, even if the original config used latest.
                    atomic_json(self.override, {'services': {self.config['service']: {'image': previous_image}}})
                    self.command(*self.compose, 'up', '-d', '--no-deps', '--pull', 'never', self.config['service'])
                    self.wait_healthy(previous_image)
                    message += '; previous image restored (database migrations are not rolled back)'
                except Exception:
                    message += '; automatic image recovery failed; inspect the host journal'
            self.save(status='failed', version=version, message=message)
        finally:
            self.lock.release()

    def start(self, version):
        if not isinstance(version, str) or not VERSION.fullmatch(version):
            raise ValueError('Expected a three- or four-part numeric version')
        if not self.lock.acquire(blocking=False):
            raise RuntimeError('Another update is already running')
        try:
            self.save(status='running', version=version)
            threading.Thread(target=self.apply, args=(version,), daemon=False).start()
        except Exception:
            self.lock.release()
            raise


class Server(socketserver.ThreadingMixIn, socketserver.UnixStreamServer):
    daemon_threads = True


class Handler(http.server.BaseHTTPRequestHandler):
    def log_message(self, *args):
        pass

    def respond(self, status, body):
        data = json.dumps(body).encode()
        self.send_response(status)
        self.send_header('Content-Type', 'application/json')
        self.send_header('Content-Length', str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def do_GET(self):
        if self.path != '/status':
            return self.respond(404, {'error': 'Not found'})
        self.respond(200, self.server.updater.state)

    def do_POST(self):
        if self.path != '/update':
            return self.respond(404, {'error': 'Not found'})
        try:
            length = int(self.headers.get('Content-Length', '0'))
            if not 0 < length <= 256:
                raise ValueError('Invalid request size')
            body = json.loads(self.rfile.read(length))
            if set(body) != {'version'}:
                raise ValueError('Only version may be supplied')
            self.server.updater.start(body['version'])
            self.respond(202, self.server.updater.state)
        except (ValueError, KeyError, TypeError):
            self.respond(400, {'error': 'Invalid version request'})
        except RuntimeError:
            self.respond(409, {'error': 'Another update is already running'})


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--config', required=True)
    args = parser.parse_args()
    cfg = json.loads(Path(args.config).read_text())
    updater = Updater(cfg)
    socket = Path(cfg['socket'])
    socket.parent.mkdir(mode=0o750, parents=True, exist_ok=True)
    os.chown(socket.parent, 0, cfg.get('socket_gid', 1000))
    socket.unlink(missing_ok=True)
    with Server(str(socket), Handler) as server:
        server.updater = updater
        os.chown(socket, 0, cfg.get('socket_gid', 1000))
        os.chmod(socket, 0o660)
        server.serve_forever()


if __name__ == '__main__':
    main()
