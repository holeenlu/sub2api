#!/usr/bin/env python3
"""Local, manifest-bound NewAPI updater; the application never gets Docker access."""
import argparse
import copy
import datetime
import fcntl
import hashlib
import http.server
import json
import os
from pathlib import Path
import re
import socket
import socketserver
import stat
import subprocess
import threading
import time
from urllib.parse import quote, urlsplit
import urllib.request
import uuid

REPOSITORY = 'holeenlu/sub2api'
BRANCH = 'main'
TAG = 'NewAPI/latest'
API_ROOT = 'https://api.github.com/repos/' + REPOSITORY
RELEASE_URL = 'https://github.com/' + REPOSITORY + '/releases/tag/' + TAG
SHA = re.compile(r'[a-f0-9]{40}')
VERSION = re.compile(r'(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)(?:\.(?:0|[1-9][0-9]*))?')
MIGRATION = re.compile(r'[a-zA-Z0-9_.-]+\.sql')
HEX = re.compile(r'[a-f0-9]{64}')
OPERATION = re.compile(r'[a-zA-Z0-9_-]{1,128}')
ROLES = ('api', 'web', 'vendor')
MAX_METADATA = 2 * 1024 * 1024


class UpdateError(Exception):
    def __init__(self, code, message, status=409):
        super().__init__(message)
        self.code, self.message, self.status = code, message, status


def now():
    return datetime.datetime.now(datetime.timezone.utc).isoformat()


def atomic_bytes(path, payload):
    path = Path(path)
    temporary = path.parent / ('.' + path.name + '.' + uuid.uuid4().hex)
    try:
        descriptor = os.open(temporary, os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600)
        with os.fdopen(descriptor, 'wb') as output:
            output.write(payload)
            output.flush()
            os.fsync(output.fileno())
        os.replace(temporary, path)
        descriptor = os.open(path.parent, os.O_RDONLY)
        try:
            os.fsync(descriptor)
        finally:
            os.close(descriptor)
    finally:
        temporary.unlink(missing_ok=True)


def json_bytes(value):
    return (json.dumps(value, indent=2) + '\n').encode()


def atomic_json(path, value):
    atomic_bytes(path, json_bytes(value))


def trusted_https(url, hosts):
    parsed = urlsplit(url)
    return (parsed.scheme == 'https' and parsed.hostname in hosts and parsed.username is None
            and parsed.password is None and parsed.port in (None, 443) and not parsed.fragment)


class SafeRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, message, headers, url):
        hosts = {'api.github.com', 'github.com', 'objects.githubusercontent.com', 'release-assets.githubusercontent.com'}
        if not trusted_https(url, hosts):
            raise UpdateError('UNTRUSTED_RELEASE', 'Release asset redirect is not trusted', 503)
        redirected = super().redirect_request(req, fp, code, message, headers, url)
        # Redirected asset requests never need the GitHub API credential.
        if urlsplit(url).hostname != 'api.github.com':
            redirected.remove_header('Authorization')
        return redirected


class GitHub:
    def __init__(self, token_file=None):
        self.token_file = token_file

    def read(self, url, asset=False):
        if not trusted_https(url, {'api.github.com'}) or not url.startswith(API_ROOT + '/'):
            raise UpdateError('UNTRUSTED_RELEASE', 'Unexpected release metadata destination', 503)
        headers = {'Accept': 'application/octet-stream' if asset else 'application/vnd.github+json',
                   'User-Agent': 'NewAPI-Manifest-Updater', 'X-GitHub-Api-Version': '2022-11-28'}
        if self.token_file:
            headers['Authorization'] = 'Bearer ' + Path(self.token_file).read_text().strip()
        try:
            request = urllib.request.Request(url, headers=headers)
            with urllib.request.build_opener(SafeRedirect()).open(request, timeout=15) as response:
                raw = response.read(MAX_METADATA + 1)
            if len(raw) > MAX_METADATA:
                raise ValueError('oversized response')
            return raw if asset else json.loads(raw)
        except UpdateError:
            raise
        except Exception:
            raise UpdateError('RELEASE_UNAVAILABLE', 'Published release metadata could not be verified', 503) from None

    def latest(self):
        release = self.read(API_ROOT + '/releases/tags/' + quote(TAG, safe=''))
        if release.get('draft') or release.get('prerelease') or release.get('tag_name') != TAG:
            raise UpdateError('UNTRUSTED_RELEASE', 'A published stable NewAPI release is required', 503)
        assets = [asset for asset in release.get('assets', []) if asset.get('name') == 'release-manifest.json']
        if len(assets) != 1:
            raise UpdateError('UNTRUSTED_RELEASE', 'Release must contain exactly one manifest', 503)
        asset = assets[0]
        if (type(asset.get('id')) is not int or asset.get('url') != API_ROOT + '/releases/assets/' + str(asset['id'])
                or not re.fullmatch(r'sha256:[a-f0-9]{64}', asset.get('digest', ''))):
            raise UpdateError('UNTRUSTED_RELEASE', 'Release manifest asset identity is invalid', 503)
        raw = self.read(asset['url'], asset=True)
        if 'sha256:' + hashlib.sha256(raw).hexdigest() != asset['digest']:
            raise UpdateError('UNTRUSTED_RELEASE', 'Release manifest checksum did not match', 503)
        try:
            manifest = json.loads(raw)
        except (ValueError, TypeError):
            raise UpdateError('UNTRUSTED_RELEASE', 'Release manifest is invalid', 503) from None
        validate_manifest(manifest)
        if release.get('name') != 'NewAPI ' + manifest['version']:
            raise UpdateError('UNTRUSTED_RELEASE', 'Release version does not match its manifest', 503)
        ref = self.read(API_ROOT + '/git/ref/tags/' + quote(TAG, safe=''))['object']
        for _ in range(4):
            if ref.get('type') != 'tag':
                break
            if not SHA.fullmatch(ref.get('sha', '')):
                raise UpdateError('UNTRUSTED_RELEASE', 'Release tag identity is invalid', 503)
            ref = self.read(API_ROOT + '/git/tags/' + ref['sha'])['object']
        if ref.get('type') != 'commit' or ref.get('sha') != manifest['commit']:
            raise UpdateError('RELEASE_CHANGED', 'Release tag does not match the manifest; check again', 409)
        return manifest


def validate_manifest(value, current=False):
    if not isinstance(value, dict) or not VERSION.fullmatch(value.get('version', '')) or not SHA.fullmatch(value.get('commit', '')):
        raise UpdateError('INVALID_MANIFEST', 'Release build identity is invalid', 503)
    if value.get('tag') != TAG or value.get('platform') != 'linux/amd64':
        raise UpdateError('INVALID_MANIFEST', 'Release channel or platform is invalid', 503)
    images = value.get('images')
    if not isinstance(images, dict) or set(images) != set(ROLES):
        raise UpdateError('INVALID_MANIFEST', 'All three release image roles are required', 503)
    for role in ROLES:
        expected = r'ghcr\.io/holeenlu/newapi-' + role + r'@sha256:[a-f0-9]{64}'
        if not isinstance(images[role], str) or not re.fullmatch(expected, images[role]):
            raise UpdateError('INVALID_MANIFEST', 'Release image role or immutable digest is invalid', 503)
    # Existing 1.1.0 installations can be inspected, but cannot update until
    # an operator establishes their trusted migration baseline.
    if current and value.get('schema_version') != 2:
        return
    migrations = value.get('migrations')
    if (value.get('schema_version') != 2 or value.get('repository') != REPOSITORY or value.get('branch') != BRANCH
            or not isinstance(migrations, dict) or not migrations):
        raise UpdateError('INVALID_MANIFEST', 'A main release with a migration inventory is required', 503)
    for name, digest in migrations.items():
        if not MIGRATION.fullmatch(name) or not isinstance(digest, str) or not HEX.fullmatch(digest):
            raise UpdateError('INVALID_MANIFEST', 'Release migration inventory is invalid', 503)


def version_tuple(version):
    parts = tuple(map(int, version.split('.')))
    return parts + (0,) * (4 - len(parts))


class Updater:
    def __init__(self, config, github=None):
        self.config = config
        self.directory = Path(config['project_directory']).resolve(strict=True)
        self.compose_path = Path(config['compose_file']).resolve(strict=True)
        self.manifest_path = Path(config['manifest_file']).resolve(strict=True)
        if self.compose_path.parent != self.directory or self.manifest_path.parent != self.directory:
            raise ValueError('Compose and manifest must belong to the configured project directory')
        self.services = config['services']
        if (not isinstance(self.services, dict) or not 3 <= len(self.services) <= 8
                or set(self.services.values()) != set(ROLES)
                or any(not re.fullmatch(r'[a-zA-Z0-9][a-zA-Z0-9_.-]{0,63}', name) for name in self.services)):
            raise ValueError('Expected explicit application service-to-role mapping')
        if not re.fullmatch(r'[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}', config['project_name']):
            raise ValueError('Invalid Compose project name')
        if (config.get('repository', REPOSITORY) != REPOSITORY or config.get('branch', BRANCH) != BRANCH
                or config.get('tag', TAG) != TAG):
            raise ValueError('Only the NewAPI product release on main is trusted')
        self.state_dir = Path(config['state_directory'])
        self.state_dir.mkdir(parents=True, exist_ok=True, mode=0o700)
        os.chmod(self.state_dir, 0o700)
        self.state_path = self.state_dir / 'state.json'
        self.process_lock = open(self.state_dir / 'service.lock', 'a')
        try:
            fcntl.flock(self.process_lock.fileno(), fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            self.process_lock.close()
            raise ValueError('Another updater owns this deployment') from None
        self.mutex = threading.RLock()
        self.mutation_lock = threading.Lock()
        self.github = github or GitHub(config.get('github_token_file'))
        self.compose = ['docker', 'compose', '--project-directory', str(self.directory),
                        '--project-name', config['project_name'], '-f', str(self.compose_path)]
        self.state = json.loads(self.state_path.read_text()) if self.state_path.exists() else {'operations': {}}
        if self.state.get('job', {}).get('state') == 'running':
            phase = self.state['job']['phase']
            safe = phase in ('preparing', 'pulling', 'preflight')
            self.job(state='failed' if safe else 'manual_intervention_required', phase='interrupted',
                     message=('Updater stopped before replacing application services; check again' if safe else
                              'Updater interrupted during application replacement; inspect and reconcile the host before continuing'),
                     finished_at=now())
        self.current()

    def close(self):
        fcntl.flock(self.process_lock.fileno(), fcntl.LOCK_UN)
        self.process_lock.close()

    def command(self, *args, timeout=600):
        try:
            result = subprocess.run(args, cwd=self.directory, capture_output=True, text=True, timeout=timeout)
            if result.returncode:
                raise ValueError('command failed')
            return result.stdout.strip()
        except Exception:
            # Command output, environment and arguments can contain credentials.
            raise UpdateError('HOST_OPERATION_FAILED', 'A host operation failed; inspect the deployment locally', 503) from None

    def current(self):
        value = json.loads(self.manifest_path.read_text())
        validate_manifest(value, current=True)
        return value

    def model(self):
        value = json.loads(self.compose_path.read_text())
        if not isinstance(value.get('services'), dict) or not set(self.services) <= set(value['services']):
            raise UpdateError('DEPLOYMENT_CHANGED', 'Managed application services are missing', 409)
        return value

    def assert_images(self, model, manifest):
        for name, role in self.services.items():
            if model['services'][name].get('image') != manifest['images'][role]:
                raise UpdateError('DEPLOYMENT_CHANGED', 'Application images differ from the recorded release; reconcile the host first', 409)

    def save(self):
        atomic_json(self.state_path, self.state)

    def job(self, **changes):
        with self.mutex:
            self.state['job'].update(changes)
            self.state['operations'][self.state['job']['id']] = copy.deepcopy(self.state['job'])
            self.save()

    def blocked(self):
        job = self.state.get('job', {})
        if job.get('state') in ('running', 'manual_intervention_required'):
            raise UpdateError('UPDATE_BUSY', 'An update is running or requires host reconciliation', 409)

    def status(self):
        with self.mutex:
            current = self.current()
            manifest = self.state.get('target_manifest')
            target = None
            available = False
            if manifest:
                target = {'version': manifest['version'], 'commit': manifest['commit'], 'release_url': RELEASE_URL,
                          'requires_manual_migration': (not current.get('migrations') or current['migrations'] != manifest['migrations'])}
                available = manifest['commit'] != current['commit'] and version_tuple(manifest['version']) > version_tuple(current['version'])
            value = {'enabled': True, 'repository': REPOSITORY, 'branch': BRANCH,
                     'current': {key: current[key] for key in ('version', 'commit')}, 'available': available}
            if target:
                value['target'] = target
            if self.state.get('job'):
                value['job'] = copy.deepcopy(self.state['job'])
            return value

    def check(self):
        # Network work serializes mutations without blocking status polling.
        with self.mutation_lock:
            with self.mutex:
                self.blocked()
                original_manifest = self.manifest_path.read_bytes()
                original_compose = self.compose_path.read_bytes()
                current = self.current()
                self.assert_images(self.model(), current)
            target = self.github.latest()
            validate_manifest(target)
            with self.mutex:
                self.blocked()
                if self.manifest_path.read_bytes() != original_manifest or self.compose_path.read_bytes() != original_compose:
                    raise UpdateError('DEPLOYMENT_CHANGED', 'Deployment changed during release verification', 409)
                if version_tuple(target['version']) < version_tuple(current['version']):
                    raise UpdateError('DOWNGRADE_FORBIDDEN', 'Published release is older than this deployment', 409)
                if version_tuple(target['version']) == version_tuple(current['version']) and target['commit'] != current['commit']:
                    raise UpdateError('VERSION_REUSED', 'A different build reused the deployed version; publish a new version', 409)
                if target['commit'] == current['commit'] and target['images'] != current['images']:
                    raise UpdateError('VERSION_REUSED', 'Published images changed for the deployed build', 409)
                self.state['target_manifest'] = target
                self.save()
                return self.status()

    def start(self, target_commit, operation_id):
        if not isinstance(target_commit, str) or not SHA.fullmatch(target_commit) or not isinstance(operation_id, str) or not OPERATION.fullmatch(operation_id):
            raise UpdateError('INVALID_UPDATE', 'A checked target commit and operation identity are required', 400)
        # Acceptance performs no network work and never waits for a release check.
        # The durable running state must exist before the API releases its system
        # operation gate; all slow verification belongs to the accepted worker.
        with self.mutex:
            previous = self.state['operations'].get(operation_id)
            if previous:
                if previous['target_commit'] != target_commit:
                    raise UpdateError('IDEMPOTENCY_CONFLICT', 'Operation identity was already used for a different target', 409)
                value = self.status()
                value['job'] = copy.deepcopy(previous)
                return value
            self.blocked()
            target = copy.deepcopy(self.state.get('target_manifest'))
            if not target or target['commit'] != target_commit:
                raise UpdateError('RELEASE_CHANGED', 'Check the release again before updating', 409)
            validate_manifest(target)
            current = self.current()
            if target['commit'] == current['commit'] or version_tuple(target['version']) <= version_tuple(current['version']):
                raise UpdateError('NO_UPDATE', 'No newer release is available', 409)
            if not current.get('migrations') or current['migrations'] != target['migrations']:
                raise UpdateError('MANUAL_MIGRATION_REQUIRED', 'Database migration changes require a separately reviewed deployment', 409)
            self.assert_images(self.model(), current)
            self.state['plan'] = {'manifest_sha256': hashlib.sha256(self.manifest_path.read_bytes()).hexdigest(),
                                  'compose_sha256': hashlib.sha256(self.compose_path.read_bytes()).hexdigest()}
            self.state['job'] = {'id': operation_id, 'target_commit': target_commit, 'state': 'running',
                                 'phase': 'preparing', 'message': 'Preparing the verified release', 'started_at': now()}
            self.job()
            worker = threading.Thread(target=self.update, args=(copy.deepcopy(target),), daemon=True)
            worker.start()
            return self.status()

    def inspect_image(self, image, manifest):
        values = json.loads(self.command('docker', 'image', 'inspect', image))
        if len(values) != 1:
            raise UpdateError('IMAGE_MISMATCH', 'Release image could not be verified', 503)
        info = values[0]
        labels = info.get('Config', {}).get('Labels') or {}
        if (info.get('Os') != 'linux' or info.get('Architecture') != 'amd64'
                or labels.get('org.opencontainers.image.revision') != manifest['commit']
                or labels.get('org.opencontainers.image.version') != manifest['version']
                or image not in info.get('RepoDigests', [])):
            raise UpdateError('IMAGE_MISMATCH', 'Release image identity or platform does not match', 503)
        return info['Id']

    def wait_healthy(self, image_ids):
        deadline = time.monotonic() + int(self.config.get('health_timeout', 600))
        while time.monotonic() < deadline:
            healthy = True
            for name, image_id in image_ids.items():
                container = self.command(*self.compose, 'ps', '-a', '-q', name, timeout=30)
                if not container or '\n' in container:
                    healthy = False
                    continue
                infos = json.loads(self.command('docker', 'container', 'inspect', container, timeout=30))
                info = infos[0]
                state = info.get('State', {})
                if (info.get('Image') != image_id or state.get('Status') != 'running'
                        or state.get('Health', {}).get('Status') != 'healthy'):
                    healthy = False
            if healthy:
                return
            time.sleep(2)
        raise UpdateError('HEALTH_TIMEOUT', 'Application services did not become healthy', 503)

    def update(self, target):
        switched = False
        backup = None
        old_model = old_manifest = None
        old_compose_bytes = old_manifest_bytes = new_compose_bytes = target_manifest_bytes = None
        try:
            # Re-freeze the moving release only after running is durably visible.
            latest = self.github.latest()
            validate_manifest(latest)
            if latest != target:
                raise UpdateError('RELEASE_CHANGED', 'Release changed after it was checked; check again', 409)
            with self.mutex:
                old_compose_bytes = self.compose_path.read_bytes()
                old_manifest_bytes = self.manifest_path.read_bytes()
                plan = self.state['plan']
                if (hashlib.sha256(old_compose_bytes).hexdigest() != plan['compose_sha256']
                        or hashlib.sha256(old_manifest_bytes).hexdigest() != plan['manifest_sha256']):
                    raise UpdateError('DEPLOYMENT_CHANGED', 'Deployment changed after accepting the update', 409)
                old_model, old_manifest = json.loads(old_compose_bytes), json.loads(old_manifest_bytes)
                validate_manifest(old_manifest)
                validate_manifest(target)
                if (old_manifest['migrations'] != target['migrations']
                        or target['commit'] == old_manifest['commit']
                        or version_tuple(target['version']) <= version_tuple(old_manifest['version'])):
                    raise UpdateError('DEPLOYMENT_CHANGED', 'Deployment eligibility changed before preparation', 409)
                self.assert_images(old_model, old_manifest)
            self.job(phase='pulling', message='Pulling and verifying all release images')
            image_ids = {}
            for role in ROLES:
                image = target['images'][role]
                self.command('docker', 'pull', '--platform', 'linux/amd64', image)
                image_ids[role] = self.inspect_image(image, target)
            self.job(phase='preflight', message='Validating the current deployment and recovery images')
            previous_ids = {role: self.inspect_image(old_manifest['images'][role], old_manifest) for role in ROLES}
            self.command(*self.compose, 'config', '--quiet', timeout=30)
            # Drift may occur during a slow pull; never overwrite concurrent host edits.
            with self.mutex:
                if self.compose_path.read_bytes() != old_compose_bytes or self.manifest_path.read_bytes() != old_manifest_bytes:
                    raise UpdateError('DEPLOYMENT_CHANGED', 'Deployment changed while images were being prepared', 409)
                backup = self.state_dir / 'backups' / self.state['job']['id']
                backup.mkdir(parents=True, mode=0o700)
                atomic_bytes(backup / 'compose.json', old_compose_bytes)
                atomic_bytes(backup / 'manifest.json', old_manifest_bytes)
                new_model = copy.deepcopy(old_model)
                for name, role in self.services.items():
                    new_model['services'][name]['image'] = target['images'][role]
                new_compose_bytes = json_bytes(new_model)
                target_manifest_bytes = json_bytes(target)
                self.state['recovery'] = {'backup_directory': str(backup), 'target_manifest': target,
                                          'original_compose_sha256': hashlib.sha256(old_compose_bytes).hexdigest(),
                                          'target_compose_sha256': hashlib.sha256(new_compose_bytes).hexdigest()}
                self.job(phase='switching', message='Replacing application services with the verified release')
                switched = True
                atomic_bytes(self.compose_path, new_compose_bytes)
            self.command(*self.compose, 'up', '-d', '--no-build', '--no-deps', '--pull', 'never', *self.services)
            self.job(phase='checking', message='Checking all application roles')
            self.wait_healthy({name: image_ids[role] for name, role in self.services.items()})
            if self.compose_path.read_bytes() != new_compose_bytes or self.manifest_path.read_bytes() != old_manifest_bytes:
                raise UpdateError('DEPLOYMENT_CHANGED', 'Deployment changed during application replacement', 409)
            self.job(phase='committing', message='Recording the deployed release')
            atomic_bytes(self.manifest_path, target_manifest_bytes)
            self.job(state='succeeded', phase='complete', message='All application roles are healthy on the verified release', finished_at=now())
        except Exception:
            if not switched:
                self.job(state='failed', phase='before_replacement', message='Update failed before replacing application services; the deployment was preserved', finished_at=now())
                return
            try:
                if (self.compose_path.read_bytes() not in (old_compose_bytes, new_compose_bytes)
                        or self.manifest_path.read_bytes() not in (old_manifest_bytes, target_manifest_bytes)):
                    raise UpdateError('DEPLOYMENT_CHANGED', 'Concurrent host changes require manual recovery', 409)
                self.job(phase='rolling_back', message='Restoring the previous images for all application roles')
                atomic_bytes(self.compose_path, old_compose_bytes)
                self.command(*self.compose, 'up', '-d', '--no-build', '--no-deps', '--pull', 'never', *self.services)
                self.wait_healthy({name: previous_ids[role] for name, role in self.services.items()})
                if self.compose_path.read_bytes() != old_compose_bytes or self.manifest_path.read_bytes() not in (old_manifest_bytes, target_manifest_bytes):
                    raise UpdateError('DEPLOYMENT_CHANGED', 'Host changed during image recovery', 409)
                atomic_bytes(self.manifest_path, old_manifest_bytes)
                self.job(state='failed', phase='rolled_back', message='Previous application images restored; database and runtime data were not restored', finished_at=now())
            except Exception:
                self.job(state='manual_intervention_required', phase='recovery_required', message='Application recovery needs host inspection; further updates are blocked', finished_at=now())


class Handler(http.server.BaseHTTPRequestHandler):
    server_version = 'NewAPIUpdater'

    def log_message(self, *_args):
        pass

    def reply(self, status, value):
        payload = json.dumps(value).encode()
        self.send_response(status)
        self.send_header('Content-Type', 'application/json')
        self.send_header('Content-Length', str(len(payload)))
        self.send_header('Cache-Control', 'no-store')
        self.end_headers()
        self.wfile.write(payload)

    def allowed_peer(self):
        if hasattr(socket, 'SO_PEERCRED'):
            import struct
            _pid, uid, _gid = struct.unpack('3i', self.connection.getsockopt(socket.SOL_SOCKET, socket.SO_PEERCRED, 12))
            return uid in (0, self.server.api_uid)
        # macOS test runs use filesystem permissions; production service is Linux.
        return True

    def do_GET(self):
        self.handle_api('GET')

    def do_POST(self):
        self.handle_api('POST')

    def handle_api(self, method):
        try:
            self.connection.settimeout(45)
            if not self.allowed_peer():
                raise UpdateError('FORBIDDEN', 'Local peer is not permitted', 403)
            if method == 'GET' and self.path == '/status':
                value = self.server.updater.status()
            elif method == 'POST' and self.path in ('/check', '/update'):
                if self.headers.get('Transfer-Encoding'):
                    raise UpdateError('INVALID_REQUEST', 'Chunked control requests are not supported', 400)
                try:
                    length = int(self.headers.get('Content-Length', '0'))
                    if not 0 < length <= 4096:
                        raise ValueError()
                    body = json.loads(self.rfile.read(length))
                    if not isinstance(body, dict):
                        raise ValueError()
                except (ValueError, TypeError):
                    raise UpdateError('INVALID_REQUEST', 'A small JSON object is required', 400) from None
                if self.path == '/check':
                    if body:
                        raise UpdateError('INVALID_REQUEST', 'Check accepts no release source overrides', 400)
                    value = self.server.updater.check()
                else:
                    if set(body) != {'target_commit', 'operation_id'}:
                        raise UpdateError('INVALID_REQUEST', 'Expected only a target commit and operation identity', 400)
                    value = self.server.updater.start(body['target_commit'], body['operation_id'])
            else:
                raise UpdateError('NOT_FOUND', 'Control operation not found', 404)
            self.reply(200, value)
        except UpdateError as error:
            self.reply(error.status, {'code': error.code, 'message': error.message})
        except Exception:
            self.reply(503, {'code': 'UPDATER_UNAVAILABLE', 'message': 'Updater could not complete the operation; inspect the host locally'})


class Server(socketserver.ThreadingMixIn, socketserver.UnixStreamServer):
    daemon_threads = True


def serve(updater):
    path = Path(updater.config['socket'])
    path.parent.mkdir(parents=True, mode=0o750, exist_ok=True)
    os.chmod(path.parent, 0o750)
    os.chown(path.parent, 0, int(updater.config['socket_gid']))
    if path.exists():
        if not stat.S_ISSOCK(path.lstat().st_mode):
            raise ValueError('Refusing to replace a non-socket control path')
        path.unlink()
    with Server(str(path), Handler) as server:
        server.updater = updater
        server.api_uid = int(updater.config.get('api_uid', 1000))
        os.chown(path, 0, int(updater.config['socket_gid']))
        os.chmod(path, 0o660)
        try:
            server.serve_forever(poll_interval=0.2)
        finally:
            path.unlink(missing_ok=True)


def load_config(path):
    path = Path(path)
    info = path.stat()
    if info.st_uid != 0 or stat.S_IMODE(info.st_mode) != 0o600:
        raise ValueError('Updater config must be owned by root with mode 0600')
    return json.loads(path.read_text())


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--config', required=True)
    args = parser.parse_args()
    if os.geteuid() != 0:
        raise ValueError('The host updater must run as root')
    updater = Updater(load_config(args.config))
    try:
        serve(updater)
    finally:
        updater.close()


if __name__ == '__main__':
    main()
