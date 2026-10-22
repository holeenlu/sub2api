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
import tempfile
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
    with os.fdopen(os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600), 'w') as output:
        json.dump(data, output, indent=2)
        output.write('\n')
        output.flush()
        os.fsync(output.fileno())
    temporary.replace(path)
    directory = os.open(path.parent, os.O_RDONLY)
    try:
        os.fsync(directory)
    finally:
        os.close(directory)


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
        if self.state.get('status') == 'running' or self.state.get('recovery_required'):
            self.recover_interrupted()

    def command(self, *args, timeout=600):
        result = subprocess.run(args, cwd=self.directory, text=True, capture_output=True, timeout=timeout)
        if result.returncode:
            # Docker output can contain environment or registry credentials. Keep
            # it out of API responses; admins inspect the host journal directly.
            print(result.stderr, file=sys.stderr, flush=True)
            raise RuntimeError(f'{args[0]} operation failed (exit {result.returncode})')
        return result.stdout.strip()

    def save(self, **state):
        state = {**self.state, **state}
        atomic_json(self.state_path, state)
        self.state = state

    def preflight(self, image):
        # The candidate reads the deployed DB through the same Compose config,
        # without starting workers or modifying the live image override.
        model = json.loads(self.command(*self.compose, 'config', '--format', 'json'))
        service = model['services'][self.config['service']]
        candidate_service = {'image': image}
        # A one-off container cannot claim the live application's static IPs.
        # Empty scalar overrides clear them; null would inherit the old value.
        networks = {name: {key: '' for key in ('ipv4_address', 'ipv6_address') if (options or {}).get(key)}
                    for name, options in service.get('networks', {}).items()}
        networks = {name: options for name, options in networks.items() if options}
        if networks:
            candidate_service['networks'] = networks
        with tempfile.TemporaryDirectory(prefix='.update-preflight-', dir=self.directory) as tmp:
            candidate = Path(tmp) / 'candidate.json'
            atomic_json(candidate, {'services': {self.config['service']: candidate_service}})
            self.command(*self.compose, '-f', str(candidate), 'run', '--rm', '--no-deps',
                         '--pull', 'never', '-T', '--entrypoint', '/app/sub2api',
                         self.config['service'], '--check-model-policy-migration')

    def restore_previous(self, message):
        previous = self.state.get('previous_image')
        if not previous:
            self.save(status='failed', recovery_required=True,
                      message=message + '; old image unknown; inspect the host before retrying')
            return
        self.save(status='running', phase='recovering', recovery_required=True, message=message)
        try:
            atomic_json(self.override, {'services': {self.config['service']: {'image': previous}}})
            self.command(*self.compose, 'up', '-d', '--no-deps', '--pull', 'never', self.config['service'])
            self.wait_healthy(previous)
        except Exception:
            self.save(status='failed', recovery_required=True,
                      message=message + '; automatic image recovery failed; inspect the host journal')
            return
        self.save(status='failed', phase='rolled_back', recovery_required=False,
                  message=message + '; previous image restored (database migrations are not rolled back)')

    def recover_interrupted(self):
        phase = self.state.get('phase')
        if phase in ('preparing', 'pulling', 'preflight'):
            self.save(status='failed', recovery_required=False,
                      message='Updater interrupted before application replacement; retry after checking the host')
            return
        if phase in ('switching', 'checking') and self.state.get('target_id'):
            try:
                info = self.inspect(self.container())
                if info['Image'] == self.state['target_id']:
                    self.wait_healthy(self.state['target_id'])
                    self.save(status='succeeded', phase='complete', recovery_required=False,
                              message='Recovered interrupted update; target image is healthy')
                    return
            except Exception:
                pass
        self.restore_previous('Updater interrupted')

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
        try:
            # Let the web server return its accepted response before replacement.
            time.sleep(2)
            image = self.image_for(version)
            old = self.inspect(self.container())
            if not old.get('Config', {}).get('Healthcheck'):
                raise ValueError('A Docker health check is required for online updates')
            previous_image = old['Image']
            self.save(status='running', version=version, phase='pulling', image=image,
                      previous_image=previous_image, recovery_required=False)
            self.command('docker', 'pull', image)
            new_image = self.inspect(image)['Id']
            self.save(phase='preflight', target_id=new_image)
            self.preflight(image)
            self.save(phase='switching', recovery_required=True)
            atomic_json(self.override, {'services': {self.config['service']: {'image': image}}})
            self.command(*self.compose, 'up', '-d', '--no-deps', '--pull', 'never', self.config['service'])
            self.save(phase='checking')
            self.wait_healthy(new_image)
            self.save(status='succeeded', phase='complete', recovery_required=False)
        except Exception as error:
            message = str(error)
            if self.state.get('recovery_required'):
                self.restore_previous(message)
            else:
                self.save(status='failed', version=version, message=message)
        finally:
            self.lock.release()

    def start(self, version):
        if not isinstance(version, str) or not VERSION.fullmatch(version):
            raise ValueError('Expected a three- or four-part numeric version')
        if not self.lock.acquire(blocking=False):
            raise RuntimeError('Another update is already running')
        try:
            if self.state.get('recovery_required'):
                raise RuntimeError('Interrupted update requires recovery; inspect the host journal')
            self.state = {}
            self.save(status='running', version=version, phase='preparing', recovery_required=False)
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
        except RuntimeError as error:
            self.respond(409, {'error': str(error)})


def prepare_socket_directory(path, gid):
    path.mkdir(mode=0o750, parents=True, exist_ok=True)
    os.chown(path, 0, gid)
    # systemd UMask=0077 narrows mkdir(0750) to 0700. Restore group
    # traversal explicitly so the non-root app can reach the socket.
    os.chmod(path, 0o750)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--config', required=True)
    args = parser.parse_args()
    cfg = json.loads(Path(args.config).read_text())
    updater = Updater(cfg)
    socket = Path(cfg['socket'])
    prepare_socket_directory(socket.parent, cfg.get('socket_gid', 1000))
    socket.unlink(missing_ok=True)
    with Server(str(socket), Handler) as server:
        server.updater = updater
        os.chown(socket, 0, cfg.get('socket_gid', 1000))
        os.chmod(socket, 0o660)
        server.serve_forever()


if __name__ == '__main__':
    main()
