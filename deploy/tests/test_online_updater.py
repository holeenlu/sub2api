"""Host control tests use real Unix HTTP; Docker/GitHub are explicit fixtures."""
import copy
import hashlib
import http.client
import json
import os
from pathlib import Path
import socket
import tempfile
import threading
import time
import unittest
from unittest.mock import patch

from deploy.updater import newapi_updater as updater


def manifest(version='1.1.1', commit='b' * 40):
    return {'schema_version': 2, 'repository': updater.REPOSITORY, 'branch': 'main',
            'version': version, 'commit': commit, 'tag': updater.TAG, 'platform': 'linux/amd64',
            'migrations': {'001_init.sql': 'e' * 64},
            'images': {role: 'ghcr.io/holeenlu/newapi-' + role + '@sha256:' +
                       hashlib.sha256((role + commit).encode()).hexdigest() for role in updater.ROLES}}


class ReleaseFixture:
    def __init__(self, target):
        self.target = target

    def latest(self):
        return copy.deepcopy(self.target)


class DockerFixture(updater.Updater):
    """Only Docker command responses are simulated; transaction code is real."""
    def __init__(self, *args, **kwargs):
        super().__init__(*args, **kwargs)
        self.calls = []
        self.live = self.current()['images'].copy()
        self.fail_pull = False
        self.fail_health = False
        self.pull_drift = False
        self.switch_drift = False
        self.health_calls = 0
        self.up_calls = 0

    def command(self, *args, timeout=600):
        self.calls.append(args)
        if args[:2] == ('docker', 'pull'):
            if self.fail_pull:
                raise RuntimeError('PASSWORD=secret-must-never-be-returned')
            if self.pull_drift:
                value = self.model()
                value['concurrent'] = 'operator-change'
                updater.atomic_json(self.compose_path, value)
        elif args[:3] == ('docker', 'image', 'inspect'):
            image = args[3]
            build = next(m for m in (self.current(), self.github.target) if image in m['images'].values())
            return json.dumps([{'Id': 'id-' + image.split('@')[1], 'RepoDigests': [image],
                                'Os': 'linux', 'Architecture': 'amd64', 'Config': {'Labels': {
                                    'org.opencontainers.image.revision': build['commit'],
                                    'org.opencontainers.image.version': build['version']}}}])
        elif args[:3] == ('docker', 'container', 'inspect'):
            service = args[3]
            image = self.live[self.services[service]]
            return json.dumps([{'Image': 'id-' + image.split('@')[1],
                                'State': {'Status': 'running', 'Health': {'Status': 'healthy'}}}])
        elif 'up' in args:
            self.up_calls += 1
            for service, role in self.services.items():
                self.live[role] = self.model()['services'][service]['image']
        elif 'ps' in args:
            return args[-1]
        return ''

    def wait_healthy(self, ids):
        self.health_calls += 1
        if self.fail_health and self.health_calls == 1:
            raise RuntimeError('health failed')
        super().wait_healthy(ids)
        if self.switch_drift:
            self.switch_drift = False
            value = self.model()
            value['operator_edit'] = True
            updater.atomic_json(self.compose_path, value)


class HostUpdaterTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.root = Path(self.temp.name)
        self.old = manifest('1.1.0', 'a' * 40)
        self.target = manifest()
        self.model = {'services': {name: {'image': self.old['images'][role],
                    'environment': {'SECRET': 'literal-$$-password'}, 'networks': {'private': {'ipv4_address': address}},
                    'ports': ['127.0.0.1::8080'], 'stop_grace_period': '120s'}
                    for name, role, address in [('api', 'api', '10.0.0.20'), ('web', 'web', '10.0.0.10'),
                                                ('vendor-portal', 'vendor', '10.0.0.11'), ('web-direct', 'web', '10.0.0.21')]},
                      'networks': {'private': {'external': True}}}
        self.compose_path = self.root / 'compose.yaml'
        self.manifest_path = self.root / 'release-manifest.json'
        self.compose_path.write_text(json.dumps(self.model, separators=(',', ':')) + '\n')
        self.manifest_path.write_text(json.dumps(self.old) + '\n')
        self.original = self.compose_path.read_bytes()
        self.config = {'project_directory': str(self.root), 'project_name': 'newapi',
                       'compose_file': str(self.compose_path), 'manifest_file': str(self.manifest_path),
                       'services': {'api': 'api', 'web': 'web', 'vendor-portal': 'vendor', 'web-direct': 'web'},
                       'state_directory': str(self.root / 'state'), 'health_timeout': 1}
        self.host = DockerFixture(self.config, ReleaseFixture(self.target))

    def tearDown(self):
        if not self.host.process_lock.closed:
            self.host.close()
        self.temp.cleanup()

    def complete(self, operation='sysop-test'):
        self.host.check()
        self.host.start(self.target['commit'], operation)
        deadline = time.monotonic() + 3
        while self.host.status().get('job', {}).get('state') == 'running' and time.monotonic() < deadline:
            time.sleep(0.005)
        self.assertNotEqual(self.host.status()['job']['state'], 'running')
        return self.host.status()

    def test_all_four_roles_switch_and_exact_original_configuration_is_backed_up(self):
        result = self.complete()
        self.assertEqual(result['job']['state'], 'succeeded')
        self.assertFalse(result['available'])
        self.assertEqual(self.host.current(), self.target)
        expected = copy.deepcopy(self.model)
        for service, role in self.config['services'].items():
            expected['services'][service]['image'] = self.target['images'][role]
        self.assertEqual(self.host.model(), expected)
        backup = self.root / 'state/backups/sysop-test/compose.json'
        self.assertEqual(backup.read_bytes(), self.original)
        self.assertEqual(backup.stat().st_mode & 0o777, 0o600)
        up = next(args for args in self.host.calls if 'up' in args)
        self.assertIn('--no-deps', up)
        self.assertIn('--pull', up)
        self.assertEqual(set(up[-4:]), set(self.config['services']))
        self.assertNotIn('postgres', up)
        self.assertNotIn('redis', up)

    def test_retry_is_durable_and_does_not_replace_services_again(self):
        self.complete()
        calls = self.host.up_calls
        result = self.host.start(self.target['commit'], 'sysop-test')
        self.assertEqual(result['job']['state'], 'succeeded')
        self.assertEqual(self.host.up_calls, calls)
        self.host.close()
        self.host = DockerFixture(self.config, ReleaseFixture(self.target))
        self.assertEqual(self.host.start(self.target['commit'], 'sysop-test')['job']['state'], 'succeeded')
        self.assertEqual(self.host.up_calls, 0)
        with self.assertRaises(updater.UpdateError) as error:
            self.host.start('c' * 40, 'sysop-test')
        self.assertEqual(error.exception.code, 'IDEMPOTENCY_CONFLICT')

    def test_failed_health_restores_all_previous_images_and_original_bytes(self):
        self.host.fail_health = True
        result = self.complete()
        self.assertEqual(result['job']['phase'], 'rolled_back')
        self.assertEqual(result['job']['state'], 'failed')
        self.assertEqual(self.compose_path.read_bytes(), self.original)
        self.assertEqual(self.host.current(), self.old)
        self.assertEqual(self.host.live, self.old['images'])
        self.assertEqual(self.host.up_calls, 2)

    def test_failed_pull_preserves_deployment_and_never_returns_sensitive_output(self):
        self.host.fail_pull = True
        result = self.complete()
        self.assertEqual(result['job']['phase'], 'before_replacement')
        self.assertEqual(self.compose_path.read_bytes(), self.original)
        self.assertEqual(self.host.up_calls, 0)
        self.assertNotIn('secret-must-never', json.dumps(result))

    def test_concurrent_changes_during_pull_are_preserved(self):
        self.host.pull_drift = True
        result = self.complete()
        self.assertEqual(result['job']['state'], 'failed')
        self.assertEqual(self.host.model()['concurrent'], 'operator-change')
        self.assertEqual(self.host.up_calls, 0)
        self.assertEqual(self.host.current(), self.old)

    def test_concurrent_changes_after_switch_block_rollback_instead_of_overwriting(self):
        self.host.switch_drift = True
        result = self.complete()
        self.assertEqual(result['job']['state'], 'manual_intervention_required')
        self.assertTrue(self.host.model()['operator_edit'])
        self.assertEqual(self.host.up_calls, 1)
        with self.assertRaises(updater.UpdateError):
            self.host.check()

    def test_migrations_or_missing_baseline_block_update_before_pull(self):
        self.host.github.target['migrations']['002_new.sql'] = 'f' * 64
        result = self.host.check()
        self.assertTrue(result['target']['requires_manual_migration'])
        with self.assertRaises(updater.UpdateError) as error:
            self.host.start(self.target['commit'], 'sysop-migrate')
        self.assertEqual(error.exception.code, 'MANUAL_MIGRATION_REQUIRED')
        self.assertFalse(self.host.calls)
        self.host.github.target = copy.deepcopy(self.target)
        previous = self.host.current()
        previous.pop('schema_version')
        previous.pop('migrations')
        updater.atomic_json(self.manifest_path, previous)
        self.assertTrue(self.host.check()['target']['requires_manual_migration'])

    def test_release_drift_and_downgrade_are_rejected(self):
        self.host.check()
        reviewed_commit = self.host.status()['target']['commit']
        self.host.github.target['commit'] = 'c' * 40
        self.host.start(reviewed_commit, 'sysop-drift')
        deadline = time.monotonic() + 2
        while self.host.status()['job']['state'] == 'running' and time.monotonic() < deadline:
            time.sleep(0.005)
        self.assertEqual(self.host.status()['job']['phase'], 'before_replacement')
        self.assertEqual(self.host.up_calls, 0)
        self.assertFalse(self.host.calls)
        self.host.github.target = manifest('0.9.0', 'c' * 40)
        with self.assertRaises(updater.UpdateError) as error:
            self.host.check()
        self.assertEqual(error.exception.code, 'DOWNGRADE_FORBIDDEN')
        self.host.github.target = manifest('1.1.0', 'c' * 40)
        with self.assertRaises(updater.UpdateError) as error:
            self.host.check()
        self.assertEqual(error.exception.code, 'VERSION_REUSED')

    def test_process_lock_and_interrupted_switch_fail_closed(self):
        with self.assertRaises(ValueError):
            DockerFixture(self.config, ReleaseFixture(self.target))
        job = {'id': 'sysop-interrupted', 'target_commit': self.target['commit'],
               'state': 'running', 'phase': 'switching', 'message': 'switching', 'started_at': updater.now()}
        updater.atomic_json(self.root / 'state/state.json', {'job': job, 'operations': {job['id']: job}})
        self.host.close()
        self.host = DockerFixture(self.config, ReleaseFixture(self.target))
        self.assertEqual(self.host.status()['job']['state'], 'manual_intervention_required')
        self.assertEqual(self.host.up_calls, 0)
        with self.assertRaises(updater.UpdateError):
            self.host.check()

    def test_state_publish_failure_cannot_replace_compose(self):
        self.host.check()
        original_atomic = updater.atomic_json
        def fail_switch(path, data):
            if path == self.host.state_path and data.get('job', {}).get('phase') == 'switching':
                raise OSError('disk unavailable')
            original_atomic(path, data)
        with patch.object(updater, 'atomic_json', side_effect=fail_switch):
            self.host.start(self.target['commit'], 'sysop-disk')
            deadline = time.monotonic() + 3
            while self.host.status()['job']['state'] == 'running' and time.monotonic() < deadline:
                time.sleep(0.005)
        self.assertEqual(self.compose_path.read_bytes(), self.original)
        self.assertEqual(self.host.up_calls, 0)

    def test_worker_rechecks_frozen_source_and_schema_after_acceptance(self):
        self.host.check()
        with patch.object(updater.threading, 'Thread'):
            self.host.start(self.target['commit'], 'sysop-before-worker')
        changed = manifest('1.0.9', 'd' * 40)
        changed['migrations']['002_external.sql'] = 'f' * 64
        updater.atomic_json(self.manifest_path, changed)
        model = self.host.model()
        for service, role in self.host.services.items():
            model['services'][service]['image'] = changed['images'][role]
        updater.atomic_json(self.compose_path, model)
        concurrent_bytes = self.compose_path.read_bytes()
        self.host.update(self.target)
        self.assertEqual(self.host.status()['job']['phase'], 'before_replacement')
        self.assertEqual(self.compose_path.read_bytes(), concurrent_bytes)
        self.assertEqual(self.host.current(), changed)
        self.assertEqual(self.host.up_calls, 0)
        self.assertFalse(self.host.calls)

    def test_status_remains_readable_during_github_network_verification(self):
        entered, release = threading.Event(), threading.Event()
        original = self.host.github.latest
        def slow_latest():
            entered.set()
            release.wait(2)
            return original()
        with patch.object(self.host.github, 'latest', side_effect=slow_latest):
            checker = threading.Thread(target=self.host.check)
            checker.start()
            self.assertTrue(entered.wait(1))
            started = time.monotonic()
            self.assertTrue(self.host.status()['enabled'])
            self.assertLess(time.monotonic() - started, 0.2)
            release.set()
            checker.join(2)
            self.assertFalse(checker.is_alive())

    def test_start_publishes_running_before_slow_release_refreeze(self):
        self.host.check()
        entered, release = threading.Event(), threading.Event()
        original = self.host.github.latest
        def slow_latest():
            entered.set()
            release.wait(2)
            return original()
        with patch.object(self.host.github, 'latest', side_effect=slow_latest):
            started = time.monotonic()
            accepted = self.host.start(self.target['commit'], 'sysop-slow-refreeze')
            self.assertLess(time.monotonic() - started, 0.2)
            self.assertEqual(accepted['job']['state'], 'running')
            self.assertTrue(entered.wait(1))
            self.assertEqual(self.host.status()['job']['state'], 'running')
            durable = json.loads(self.host.state_path.read_text())
            self.assertEqual(durable['job']['state'], 'running')
            self.assertEqual(self.host.up_calls, 0)
            with self.assertRaises(updater.UpdateError):
                self.host.check()
            release.set()
            deadline = time.monotonic() + 2
            while self.host.status()['job']['state'] == 'running' and time.monotonic() < deadline:
                time.sleep(0.005)
            self.assertEqual(self.host.status()['job']['state'], 'succeeded')

    def test_unix_http_rejects_source_overrides_and_only_exposes_safe_status(self):
        socket_path = str(self.root / 'control.sock')
        with updater.Server(socket_path, updater.Handler) as server:
            server.updater = self.host
            server.api_uid = os.getuid()
            worker = threading.Thread(target=server.serve_forever, kwargs={'poll_interval': 0.01})
            worker.start()
            def request(method, path, body=None):
                connection = http.client.HTTPConnection('localhost', timeout=2)
                connection.sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
                connection.sock.connect(socket_path)
                connection.request(method, path, None if body is None else json.dumps(body), {'Content-Type': 'application/json'})
                response = connection.getresponse()
                value = json.loads(response.read())
                connection.close()
                return response.status, value
            try:
                status, value = request('GET', '/status')
                self.assertEqual(status, 200)
                self.assertTrue(value['enabled'])
                status, value = request('POST', '/check', {'repository': 'evil/repo'})
                self.assertEqual(status, 400)
                status, value = request('POST', '/check', {})
                self.assertEqual(status, 200)
                self.assertEqual(value['target']['release_url'], updater.RELEASE_URL)
                status, value = request('POST', '/update', {'target_commit': self.target['commit'], 'operation_id': 'ok', 'image': 'evil'})
                self.assertEqual(status, 400)
                self.assertNotIn('literal', json.dumps(value))
                self.assertEqual(request('GET', '/anything')[0], 404)
            finally:
                server.shutdown()
                worker.join()


class GitHubVerificationTests(unittest.TestCase):
    def release(self, target):
        raw = json.dumps(target).encode()
        asset = {'name': 'release-manifest.json', 'id': 123, 'url': updater.API_ROOT + '/releases/assets/123',
                 'digest': 'sha256:' + hashlib.sha256(raw).hexdigest()}
        release = {'tag_name': updater.TAG, 'draft': False, 'prerelease': False,
                   'name': 'NewAPI ' + target['version'], 'assets': [asset]}
        replies = [release, raw, {'object': {'type': 'commit', 'sha': target['commit']}}]
        return release, replies

    def test_manifest_asset_digest_and_tag_commit_are_verified(self):
        target = manifest()
        _, replies = self.release(target)
        with patch.object(updater.GitHub, 'read', side_effect=replies):
            self.assertEqual(updater.GitHub().latest(), target)
        release, replies = self.release(target)
        release['assets'][0]['digest'] = 'sha256:' + '0' * 64
        with patch.object(updater.GitHub, 'read', side_effect=replies), self.assertRaises(updater.UpdateError):
            updater.GitHub().latest()
        _, replies = self.release(target)
        replies[2]['object']['sha'] = 'c' * 40
        with patch.object(updater.GitHub, 'read', side_effect=replies), self.assertRaises(updater.UpdateError):
            updater.GitHub().latest()

    def test_role_exchange_wrong_repository_and_untrusted_redirect_are_rejected(self):
        for key, value in [('repository', 'evil/repo'), ('branch', 'NewAPI'), ('platform', 'linux/arm64')]:
            target = manifest()
            target[key] = value
            with self.subTest(key=key), self.assertRaises(updater.UpdateError):
                updater.validate_manifest(target)
        target = manifest()
        target['images']['api'] = target['images']['web']
        with self.assertRaises(updater.UpdateError):
            updater.validate_manifest(target)
        request = updater.urllib.request.Request(updater.API_ROOT + '/releases/assets/123', headers={'Authorization': 'Bearer secret'})
        redirected = updater.SafeRedirect().redirect_request(request, None, 302, 'Found', {}, 'https://release-assets.githubusercontent.com/file')
        self.assertIsNone(redirected.get_header('Authorization'))
        for destination in ('http://api.github.com/x', 'https://evil.example/file', 'https://api.github.com.evil.example/file', 'https://user@api.github.com/file', 'https://api.github.com:444/file'):
            with self.subTest(destination=destination), self.assertRaises(updater.UpdateError):
                updater.SafeRedirect().redirect_request(request, None, 302, 'Found', {}, destination)


if __name__ == '__main__':
    unittest.main()
