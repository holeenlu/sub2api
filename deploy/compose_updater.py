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

    def managed_services(self):
        model = json.loads(self.command(*self.compose, 'config', '--format', 'json'))
        services = model['services']
        app = self.config['service']
        if app not in services:
            raise ValueError('Configured application service is absent from the Compose project')
        portal = self.config.get('portal_service')
        if not portal and 'vendor-portal' in services and app != 'vendor-portal':
            portal = 'vendor-portal'
        if not portal:
            return [app]
        if portal == app or portal not in services:
            raise ValueError('Configured supplier portal must be a separate existing Compose service')
        expected = self.config.get('portal_image', self.config['image'] + '-vendor')
        current = services[portal].get('image', '')
        previous = self.state.get('previous_images', {}).get(portal)
        if not (current == expected or current.startswith(expected + ':')
                or current.startswith(expected + '@') or (previous and current == previous)):
            raise ValueError('Supplier portal image does not match the configured brand')
        return [app, portal]

    def write_images(self, images):
        # Preserve unrelated overrides and service settings, including ports and
        # environment. Only the reviewed application/portal image keys change.
        override = json.loads(self.override.read_text())
        services = override.setdefault('services', {})
        for service, image in images.items():
            services.setdefault(service, {})['image'] = image
        atomic_json(self.override, override)

    def state_images(self, key, legacy_key):
        # Published host updaters have durable single-application recovery state.
        # Keep that recovery contract until those interrupted updates are cleared.
        images = self.state.get(key)
        if images:
            return images
        legacy = self.state.get(legacy_key)
        return {self.config['service']: legacy} if legacy else {}

    def preflight(self, images):
        # The candidate reads the deployed DB through the same Compose config,
        # without starting workers or modifying the live image override.
        model = json.loads(self.command(*self.compose, 'config', '--format', 'json'))
        candidates = {}
        for name, image in images.items():
            service = model['services'][name]
            candidate_service = {'image': image}
            # A one-off container cannot claim a live service's static IPs.
            networks = {network: {key: '' for key in ('ipv4_address', 'ipv6_address') if (options or {}).get(key)}
                        for network, options in service.get('networks', {}).items()}
            networks = {network: options for network, options in networks.items() if options}
            if networks:
                candidate_service['networks'] = networks
            candidates[name] = candidate_service
        with tempfile.TemporaryDirectory(prefix='.update-preflight-', dir=self.directory) as tmp:
            candidate = Path(tmp) / 'candidate.json'
            atomic_json(candidate, {'services': candidates})
            self.command(*self.compose, '-f', str(candidate), 'run', '--rm', '--no-deps',
                         '--pull', 'never', '-T', '--entrypoint', '/app/sub2api',
                         self.config['service'], '--check-model-policy-migration')
            for service in images:
                if service != self.config['service']:
                    self.command(*self.compose, '-f', str(candidate), 'run', '--rm', '--no-deps',
                                 '--pull', 'never', '-T', service,
                                 'nginx', '-t', '-c', '/tmp/vendor-portal/nginx.conf')

    def restore_previous(self, message):
        previous = self.state_images('previous_images', 'previous_image')
        if not previous:
            self.save(status='failed', recovery_required=True,
                      message=message + '; old image unknown; inspect the host before retrying')
            return
        self.save(status='running', phase='recovering', recovery_required=True, message=message)
        try:
            self.write_images(previous)
            self.command(*self.compose, 'up', '-d', '--no-deps', '--pull', 'never', *previous)
            self.wait_healthy(previous)
        except Exception:
            self.save(status='failed', recovery_required=True,
                      message=message + '; automatic image recovery failed; inspect the host journal')
            return
        self.save(status='failed', phase='rolled_back', recovery_required=False,
                  message=message + '; previous image restored for all managed services (database migrations are not rolled back)')

    def recover_interrupted(self):
        phase = self.state.get('phase')
        if phase in ('preparing', 'pulling', 'preflight'):
            self.save(status='failed', recovery_required=False,
                      message='Updater interrupted before application replacement; retry after checking the host')
            return
        targets = self.state_images('target_ids', 'target_id')
        if phase in ('switching', 'checking') and targets:
            try:
                self.wait_healthy(targets)
                self.save(status='succeeded', phase='complete', recovery_required=False,
                          message='Recovered interrupted update; all target images are healthy')
                return
            except Exception:
                pass
        self.restore_previous('Updater interrupted')

    def images_for(self, version, services=None):
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
        images = {cfg['service']: cfg['image'] + '@' + manifest['image_digest']}
        for service in services or [cfg['service']]:
            if service == cfg['service']:
                continue
            portal_image = cfg.get('portal_image', cfg['image'] + '-vendor')
            if (manifest.get('portal_image') != portal_image
                    or not DIGEST.fullmatch(manifest.get('portal_image_digest', ''))):
                raise ValueError('Release is missing the matching supplier portal image/digest; application was not updated')
            images[service] = portal_image + '@' + manifest['portal_image_digest']
        return images

    def container(self, service=None):
        ids = self.command(*self.compose, 'ps', '-q', service or self.config['service']).splitlines()
        if len(ids) != 1:
            raise ValueError('Exactly one running application container is required')
        return ids[0]

    def inspect(self, container):
        return json.loads(self.command('docker', 'inspect', container))[0]

    def wait_healthy(self, expected_images):
        deadline = time.monotonic() + self.config.get('health_timeout', 180)
        while time.monotonic() < deadline:
            try:
                ready = True
                for service, image in expected_images.items():
                    info = self.inspect(self.container(service))
                    ready = ready and info['Image'] == image and info['State'].get('Health', {}).get('Status') == 'healthy'
                if ready:
                    return
            except (ValueError, RuntimeError, IndexError):
                pass
            time.sleep(3)
        raise RuntimeError('Application health check timed out')

    def apply(self, version):
        try:
            # Let the web server return its accepted response before replacement.
            time.sleep(2)
            images = self.images_for(version, self.managed_services())
            previous_images = {}
            for service in images:
                old = self.inspect(self.container(service))
                if not old.get('Config', {}).get('Healthcheck'):
                    raise ValueError('A Docker health check is required for every managed service')
                previous_images[service] = old['Image']
            self.save(status='running', version=version, phase='pulling', images=images,
                      previous_images=previous_images, recovery_required=False)
            targets = {}
            for service, image in images.items():
                self.command('docker', 'pull', image)
                targets[service] = self.inspect(image)['Id']
            self.save(phase='preflight', target_ids=targets)
            self.preflight(images)
            self.save(phase='switching', recovery_required=True)
            self.write_images(images)
            self.command(*self.compose, 'up', '-d', '--no-deps', '--pull', 'never', *images)
            self.save(phase='checking')
            self.wait_healthy(targets)
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
            # A recovered portal is pinned to its exact prior image ID. Retain
            # that trusted identity so discovery can validate the next retry.
            previous = self.state_images('previous_images', 'previous_image')
            self.state = {'previous_images': previous} if previous else {}
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
