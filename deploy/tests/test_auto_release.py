import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch
import urllib.request

from scripts.release.plan import next_version, parts, records
from deploy.compose_updater import Updater, SafeRedirect


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


class PlannerIntegrationTests(unittest.TestCase):
    def test_draft_retry_published_retry_and_next_push(self):
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
                if args[0] != 'gh':
                    return real_run(*args)
                if args[1] == 'api':
                    return json.dumps([releases])
                if args[1:3] == ('release','create'):
                    releases.append(dict(id=len(releases)+1, tag_name=args[3], draft=True,
                        body=Path('release-notes.md').read_text()))
                    return ''
                raise AssertionError(args)
            env = {'GITHUB_REPOSITORY':'holeenlu/sub2api','GITHUB_REF':'refs/heads/main',
                   'GITHUB_OUTPUT':str(Path(directory)/'output')}
            with patch.dict(os.environ,env), patch('sys.argv',['plan.py']), \
                 patch.object(planner,'run',side_effect=fake_run), \
                 patch.object(planner,'official_base',return_value=('0.2.8',base,base)):
                planner.main()
                first = json.loads(Path('release-plan.json').read_text())
                self.assertEqual(first['version'],'0.2.8.1')
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
                self.assertEqual(json.loads(Path('release-plan.json').read_text())['version'],'0.2.8.2')
            os.chdir(original)

if __name__ == '__main__':
    unittest.main()
