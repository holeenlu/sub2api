import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch
import urllib.request

from scripts.release.plan import next_version, parts, records
from deploy.compose_updater import Updater, SafeRedirect, prepare_socket_directory


class VersionTests(unittest.TestCase):
    def test_version_lifecycle(self):
        self.assertEqual(next_version('0.2.8', [], True, False), '0.2.8.1')
        self.assertEqual(next_version('0.2.8', ['0.2.8.1'], True, False), '0.2.8.2')
        self.assertEqual(next_version('0.2.8', ['0.2.8.9'], False, False), '0.2.8.10')
        self.assertEqual(next_version('0.2.9', ['0.2.8.9'], True, True), '0.2.9')
        self.assertEqual(next_version('0.2.9', ['0.2.9'], False, False), '0.2.9.1')
        self.assertEqual(next_version('0.2.9', ['0.2.8.9'], False, True), '0.2.9.1')
        self.assertEqual(next_version('0.2.9', [], True, True), '0.2.9')
        self.assertGreater(parts('0.2.9'), parts('0.2.8.99'))
        self.assertGreater(parts('0.2.8.10'), parts('0.2.8.9'))

    def test_foreign_tags_and_channels_are_not_release_history(self):
        plan = dict(channel='kdan', version='0.2.8.1', commit='a' * 40)
        body = '<!-- release-plan:' + json.dumps(plan) + ' -->'
        releases = [dict(tag_name='v1.1.4', body=body),
                    dict(tag_name='kdan/v0.2.8.1', body=body)]
        self.assertEqual(records(releases, 'sub2api'), [])
        self.assertEqual(len(records(releases, 'kdan')), 1)

    def test_invalid_versions(self):
        for version in ['0.2', '0.2.8.01', '0.2.8.1.1', '0.2.8\nstable=true', 'v0.2.8']:
            with self.assertRaises(ValueError):
                parts(version)


class ComposeUpdateTests(unittest.TestCase):
    def test_socket_directory_remains_group_accessible_under_systemd_umask(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'socket'
            previous = os.umask(0o077)
            try:
                with patch('deploy.compose_updater.os.chown') as chown:
                    prepare_socket_directory(path, 1000)
                    chown.assert_called_once_with(path, 0, 1000)
                self.assertEqual(path.stat().st_mode & 0o777, 0o750)
            finally:
                os.umask(previous)

    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        directory = Path(self.temp.name)
        (directory / 'compose.yml').write_text('services: {}')
        self.updater = Updater(dict(project_directory=str(directory), project_name='existing-project',
            compose_files=['compose.yml'], service='kdan', repository='holeenlu/sub2api',
            channel='kdan', image='ghcr.io/holeenlu/kdan', state_file=str(directory / 'state.json')))

    @patch('deploy.compose_updater.time.sleep')
    def test_health_failure_restores_exact_old_image_without_touching_dependencies(self, _):
        u = self.updater
        u.lock.acquire()
        with patch.object(u, 'image_for', return_value='ghcr.io/holeenlu/kdan@sha256:' + 'b'*64), \
             patch.object(u, 'container', return_value='app'), \
             patch.object(u, 'inspect', side_effect=[{'Image': 'sha256:old', 'Config': {'Healthcheck': {"Test": ['CMD','true']}}}, {'Id': 'sha256:new'}]), \
             patch.object(u, 'command') as command, \
             patch.object(u, 'wait_healthy', side_effect=[RuntimeError('unhealthy'), None]):
            u.apply('0.2.8.1')
        self.assertEqual(u.state['status'], 'failed')
        self.assertIn('previous image restored', u.state['message'])
        self.assertEqual(json.loads(u.override.read_text())['services']['kdan']['image'], 'sha256:old')
        updates = [call.args for call in command.call_args_list if 'up' in call.args]
        self.assertEqual(len(updates), 2)
        self.assertTrue(all('--no-deps' in args and args[-1] == 'kdan' for args in updates))
        self.assertFalse(u.lock.locked())

    @patch('deploy.compose_updater.time.sleep')
    def test_pull_failure_leaves_existing_configuration_and_container(self, _):
        u = self.updater
        before = u.override.read_bytes()
        u.lock.acquire()
        with patch.object(u, 'image_for', return_value='image'), \
             patch.object(u, 'container', return_value='app'), \
             patch.object(u, 'inspect', return_value={'Image': 'old', 'Config': {'Healthcheck': {'Test': ['CMD', 'true']}}}), \
             patch.object(u, 'command', side_effect=RuntimeError('pull failed')) as command:
            u.apply('0.2.8.2')
        self.assertEqual(before, u.override.read_bytes())
        self.assertEqual(command.call_count, 1)
        self.assertEqual(u.state['status'], 'failed')

    @patch('deploy.compose_updater.time.sleep')
    def test_preflight_failure_never_replaces_application(self, _):
        u = self.updater
        before = u.override.read_bytes()
        u.lock.acquire()
        with patch.object(u, 'image_for', return_value='image'), \
             patch.object(u, 'container', return_value='app'), \
             patch.object(u, 'inspect', side_effect=[{'Image': 'old', 'Config': {'Healthcheck': {'Test': ['CMD', 'true']}}}, {'Id': 'new'}]), \
             patch.object(u, 'command') as command, \
             patch.object(u, 'preflight', side_effect=RuntimeError('preflight failed')):
            u.apply('0.2.13.6')
        self.assertEqual(u.override.read_bytes(), before)
        self.assertEqual(u.state['phase'], 'preflight')
        self.assertEqual(u.state['previous_image'], 'old')
        self.assertFalse(any('up' in c.args for c in command.call_args_list))

    def test_restart_recovers_each_durable_phase(self):
        for phase in ('preparing', 'pulling', 'preflight', 'switching', 'checking', 'recovering'):
            with self.subTest(phase=phase):
                u = self.updater
                u.save(status='running', phase=phase, version='0.2.13.6', image='target',
                       target_id='new', previous_image='old', recovery_required=phase in ('switching', 'checking', 'recovering'))
                calls = [RuntimeError('target not healthy'), None] if phase in ('switching', 'checking') else [None]
                with patch.object(Updater, 'command') as command, patch.object(Updater, 'container', return_value='app'), \
                     patch.object(Updater, 'inspect', return_value={'Image': 'new'}), patch.object(Updater, 'wait_healthy', side_effect=calls):
                    restarted = Updater(u.config)
                self.assertEqual(restarted.state['version'], '0.2.13.6')
                self.assertEqual(restarted.state['previous_image'], 'old')
                self.assertFalse(restarted.state['recovery_required'])
                if phase in ('preparing', 'pulling', 'preflight'):
                    command.assert_not_called()
                else:
                    self.assertEqual(json.loads(u.override.read_text())['services']['kdan']['image'], 'old')
                    self.assertIn('--no-deps', command.call_args.args)

    def test_restart_keeps_healthy_target_and_blocks_failed_recovery(self):
        u = self.updater
        u.save(status='running', phase='checking', target_id='new', previous_image='old', version='0.2.13.6')
        with patch.object(Updater, 'wait_healthy'), patch.object(Updater, 'command') as command, \
             patch.object(Updater, 'container', return_value='app'), patch.object(Updater, 'inspect', return_value={'Image': 'new'}):
            restarted = Updater(u.config)
        self.assertEqual(restarted.state['status'], 'succeeded')
        command.assert_not_called()
        u.save(status='running', phase='switching', recovery_required=True)
        with patch.object(Updater, 'wait_healthy', side_effect=RuntimeError('unhealthy')), patch.object(Updater, 'command'):
            restarted = Updater(u.config)
        self.assertTrue(restarted.state['recovery_required'])
        with self.assertRaisesRegex(RuntimeError, 'requires recovery'):
            restarted.start('0.2.13.7')
        self.assertEqual(restarted.state['previous_image'], 'old')
        self.assertFalse(restarted.lock.locked())

    def test_candidate_check_does_not_overwrite_live_override(self):
        u = self.updater
        before = u.override.read_bytes()
        def command(*args, **kwargs):
            self.assertEqual(u.override.read_bytes(), before)
            self.assertEqual(args[-1], '--check-model-policy-migration')
            self.assertIn('--no-deps', args)
            candidate_path = Path(args[len(u.compose) + 1])
            self.assertEqual(json.loads(candidate_path.read_text())['services']['kdan']['image'], 'candidate')
        with patch.object(u, 'command', side_effect=command):
            u.preflight('candidate')

    def test_manifest_cannot_change_brand_or_image(self):
        release = {'tag_name':'kdan/v0.2.8.1', 'assets':[{'name':'release-manifest.json', 'id':42,
            'url':'https://api.github.com/repos/holeenlu/sub2api/releases/assets/42'}]}
        manifest = {'channel':'tapmodels', 'version':'0.2.8.1', 'image':'ghcr.io/holeenlu/kdan', 'image_digest':'sha256:'+'a'*64}
        with patch('deploy.compose_updater.read_json', side_effect=[release, manifest]):
            with self.assertRaisesRegex(ValueError, 'manifest'):
                self.updater.image_for('0.2.8.1')

    def test_request_validation_and_lock(self):
        for version in ['latest', '1.2.3; echo bad', '../x', 123]:
            with self.assertRaises(ValueError):
                self.updater.start(version)
        self.updater.lock.acquire()
        with self.assertRaises(RuntimeError):
            self.updater.start('0.2.8.1')
        self.updater.lock.release()

    def test_asset_redirect_strips_credentials(self):
        request = urllib.request.Request('https://api.github.com/repos/a/b/releases/assets/1',
                                         headers={'Authorization':'Bearer secret'})
        target = SafeRedirect().redirect_request(request, None, 302, '', {},
            'https://release-assets.githubusercontent.com/asset')
        self.assertIsNone(target.get_header('Authorization'))
        with self.assertRaises(ValueError):
            SafeRedirect().redirect_request(request, None, 302, '', {}, 'https://evil.test/asset')


class MigrationVersionTests(unittest.TestCase):
    def test_floor_preserves_version_order_across_repositories(self):
        self.assertEqual(next_version('0.2.13', [], False, False, floor='0.2.13.2'), '0.2.13.3')
        self.assertEqual(next_version('0.2.13', ['0.2.13.5'], False, False, floor='0.2.13.2'), '0.2.13.6')
        self.assertEqual(next_version('0.2.14', [], False, False, floor='0.2.13.2'), '0.2.14.1')
        with self.assertRaises(ValueError):
            next_version('0.2.11', [], False, False, floor='0.2.13.2')


class PlannerIntegrationTests(unittest.TestCase):
    def test_draft_retry_published_retry_and_next_push(self):
        self.assert_channel_lifecycle('kdan', 'main')

    def test_tapmodels_uses_this_repository_branch_and_image(self):
        self.assert_channel_lifecycle('tapmodels', 'TapModels')

    def test_tokensavy_uses_its_own_release_history(self):
        self.assert_channel_lifecycle('tokensavy', 'tokensavy')

    def assert_channel_lifecycle(self, channel, branch):
        import subprocess
        import shutil
        import scripts.release.plan as planner
        root = Path(__file__).resolve().parents[2]
        original = os.getcwd()
        with tempfile.TemporaryDirectory() as directory:
            os.chdir(directory)
            self.addCleanup(os.chdir, original)
            Path('scripts/release').mkdir(parents=True)
            shutil.copyfile(root/'scripts/release/channels.json', 'scripts/release/channels.json')
            def git(*args):
                return subprocess.check_output(['git', *args], text=True, stderr=subprocess.DEVNULL).strip()
            git('init')
            git('config','user.name','Release Test')
            git('config','user.email','release@example.invalid')
            git('add','scripts')
            git('commit','-m','base')
            base = git('rev-parse','HEAD')
            Path('custom').write_text('first')
            git('add','custom'); git('commit','-m','custom')
            releases = []
            real_run = planner.run
            def fake_run(*args):
                if args[:2] == ('git', 'ls-remote'):
                    match = next((r for r in releases if 'refs/tags/' + r['tag_name'] == args[-1]), None)
                    if match:
                        return json.loads(match['body'].split('release-plan:')[1].split(' -->')[0])['commit'] + '\t' + args[-1]
                    return ''
                if args[0] != 'gh':
                    return real_run(*args)
                if args[1:4] == ('api','--method','POST'):
                    payload = json.loads(Path('release-request.json').read_text())
                    releases.append(dict(id=len(releases)+1, **payload))
                    # The creation response is sufficient; do not require a
                    # second list request to observe an eventually visible draft.
                    return json.dumps(releases[-1])
                if args[1] == 'api':
                    return json.dumps([releases])
                raise AssertionError(args)
            env = {'GITHUB_REPOSITORY':'holeenlu/sub2api','GITHUB_REF':'refs/heads/' + branch,
                   'GITHUB_OUTPUT':str(Path(directory)/'output')}
            with patch.dict(os.environ,env), patch('sys.argv',['plan.py']), \
                 patch.object(planner,'run',side_effect=fake_run), \
                 patch.object(planner,'official_base',return_value=('0.2.13',base,base)):
                planner.main()
                first = json.loads(Path('release-plan.json').read_text())
                self.assertEqual(first['version'], '0.2.13.3' if channel == 'tapmodels' else '0.2.13.1')
                self.assertEqual(first['channel'], channel)
                self.assertEqual(first['image'], 'ghcr.io/holeenlu/' + channel)
                self.assertEqual(first['tag'], channel + '/v' + first['version'])
                planner.main()  # Failed build resumes its draft without allocating again.
                self.assertEqual(len(releases),1)
                releases[0]['draft'] = False
                Path('output').write_text('')
                planner.main()  # Published SHA only repairs latest promotion if needed.
                self.assertIn('publish=false',Path('output').read_text())
                self.assertIn('promote=true',Path('output').read_text())
                self.assertEqual(len(releases),1)
                Path('custom').write_text('second')
                git('add','custom'); git('commit','-m','second')
                planner.main()
                self.assertEqual(json.loads(Path('release-plan.json').read_text())['version'], '0.2.13.4' if channel == 'tapmodels' else '0.2.13.2')
            os.chdir(original)

if __name__ == '__main__':
    unittest.main()
