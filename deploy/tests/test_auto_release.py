import json
import os
from pathlib import Path
import tempfile
import unittest
import subprocess
from unittest.mock import patch
import urllib.request
import http.client
import socket
import threading

from scripts.release.plan import history_migrated_with_integrated_official_base, next_version, notes_baseline, parts, records, reserved_versions
from scripts.release import plan as release_plan
from deploy.compose_updater import Updater, SafeRedirect, prepare_socket_directory, Server, Handler


class VersionTests(unittest.TestCase):
    def test_requested_version_is_exact_and_rejects_regression_or_another_base(self):
        with patch.object(release_plan, 'release_for_tag', return_value=None) as lookup, \
             patch.object(release_plan, 'tag_commit', return_value=None):
            self.assertEqual(release_plan.next_available_version('repo', 'tokensavy', '0.2.18',
                             ['0.2.18.11'], False, False, requested='0.2.18.14'), '0.2.18.14')
            lookup.assert_called_once_with('repo', 'tokensavy/v0.2.18.14')
            for requested, previous, floor in [('0.2.19.14', [], None), ('bad', [], None),
                    ('0.2.18.13', ['0.2.18.13'], None), ('0.2.18.14', ['0.2.18.15'], None),
                    ('0.2.18.14', [], '0.2.18.14')]:
                with self.subTest(requested=requested, previous=previous, floor=floor), self.assertRaises(ValueError):
                    release_plan.next_available_version('repo', 'tokensavy', '0.2.18', previous,
                                                       False, False, floor=floor, requested=requested)

    def test_requested_version_never_skips_or_overwrites_hidden_release_or_tag(self):
        for release, tag in [({'id': 123}, None), (None, 'a' * 40)]:
            with self.subTest(release=release, tag=tag), \
                 patch.object(release_plan, 'release_for_tag', return_value=release), \
                 patch.object(release_plan, 'tag_commit', return_value=tag), \
                 self.assertRaisesRegex(ValueError, 'occupied'):
                release_plan.next_available_version('repo', 'kdan', '0.2.18', ['0.2.18.13'],
                                                   False, False, requested='0.2.18.14')

    def test_main_reallocates_conflicting_draft_and_rechecks_before_creating_release(self):
        for late_conflict in (False, True):
            with self.subTest(late_conflict=late_conflict), tempfile.TemporaryDirectory(prefix='release-reservation-test-') as tmp:
                old_cwd = os.getcwd()
                try:
                    os.chdir(tmp)
                    Path('scripts/release').mkdir(parents=True)
                    config = {'kdan': {'repository': 'holeenlu/sub2api', 'branch': 'main',
                              'image': 'ghcr.io/holeenlu/kdan', 'portal_image': 'ghcr.io/holeenlu/kdan-vendor', 'zh_tw': True}}
                    Path('scripts/release/channels.json').write_text(json.dumps(config))
                    head, previous, historic, upstream = ['a' * 40, 'b' * 40, 'c' * 40, 'd' * 40]
                    def record(id, version, commit, draft):
                        plan = dict(channel='kdan', version=version, tag='kdan/v' + version, commit=commit,
                                    base='0.2.18', upstream_commit=upstream, image=config['kdan']['image'])
                        return dict(id=id, tag_name=plan['tag'], draft=draft, prerelease=False,
                                    body='<!-- release-plan:' + json.dumps(plan) + ' -->')
                    releases = [record(22, '0.2.18.7', head, True), record(11, '0.2.18.7', historic, False),
                                record(10, '0.2.18.6', previous, False)]
                    refs = {'kdan/v0.2.18.6': previous, 'kdan/v0.2.18.7': head}
                    writes = []
                    def fake_run(*args):
                        if args[:3] == ('gh', 'api', '--paginate'):
                            return json.dumps([releases])
                        writes.append(args)
                        if args[2:4] == ('-X', 'POST'):
                            self.assertIn('ref=refs/tags/kdan/v0.2.18.19', args)
                            refs['kdan/v0.2.18.19'] = head
                            return '{}'
                        self.assertEqual(args[2:4], ('--method', 'POST'))
                        request = json.loads(Path('release-request.json').read_text())
                        self.assertEqual(request['tag_name'], 'kdan/v0.2.18.19')
                        return json.dumps(dict(request, id=99))
                    def fake_git(*args):
                        if args[0] == 'ls-remote':
                            ref = args[-1]
                            sha = refs.get(ref.removeprefix('refs/tags/'))
                            return sha + '\t' + ref if sha else ''
                        if args[0] == 'show':
                            return '2026-10-26T10:00:00+08:00'
                        return head
                    def lookup(repository, tag):
                        version = int(tag.rsplit('.', 1)[1])
                        if version <= 18 or (late_conflict and tag in refs):
                            return {'id': 100 + version}
                        return None
                    with patch.dict(os.environ, GITHUB_REPOSITORY='holeenlu/sub2api', GITHUB_REF='refs/heads/main',
                                    GITHUB_OUTPUT=str(Path(tmp) / 'outputs')), patch('sys.argv', ['plan.py']), \
                         patch.object(release_plan, 'run', side_effect=fake_run), \
                         patch.object(release_plan, 'git', side_effect=fake_git), \
                         patch.object(release_plan, 'official_base', return_value=('0.2.18', upstream, upstream)), \
                         patch.object(release_plan, 'reserved_versions', return_value=['0.2.18.6', '0.2.18.7']), \
                         patch.object(release_plan, 'tag_commit', side_effect=refs.get), \
                         patch.object(release_plan, 'release_for_tag', side_effect=lookup), \
                         patch.object(release_plan.subprocess, 'run', return_value=subprocess.CompletedProcess([], 0)):
                        if late_conflict:
                            with self.assertRaisesRegex(ValueError, 'refusing a duplicate draft'):
                                release_plan.main()
                            self.assertEqual(len(writes), 1)
                        else:
                            release_plan.main()
                            saved = json.loads(Path('release-plan.json').read_text())
                            self.assertEqual(saved['version'], '0.2.18.19')
                            self.assertEqual(saved['previous_version'], '0.2.18.6')
                            self.assertEqual(saved['previous_commit'], previous)
                            self.assertIn('release_id=99', Path('outputs').read_text())
                            self.assertEqual(len(writes), 2)
                finally:
                    os.chdir(old_cwd)

    def test_hidden_historical_releases_are_skipped_without_writes(self):
        calls = []
        def lookup(repository, tag):
            calls.append(tag)
            return {'id': 100 + int(tag.rsplit('.', 1)[1])} if int(tag.rsplit('.', 1)[1]) <= 18 else None
        with patch.object(release_plan, 'release_for_tag', side_effect=lookup), \
             patch.object(release_plan, 'tag_commit', return_value=None) as refs, \
             patch.object(release_plan, 'run') as writes:
            version = release_plan.next_available_version('holeenlu/sub2api', 'kdan', '0.2.18',
                                                          ['0.2.18.6'], False, False)
        self.assertEqual(version, '0.2.18.19')
        self.assertEqual(calls, ['kdan/v0.2.18.' + str(n) for n in range(7, 20)])
        refs.assert_called_once_with('kdan/v0.2.18.19')
        writes.assert_not_called()

    def test_markerless_release_names_are_reserved(self):
        releases = [{'tag_name': tag} for tag in ['kdan/v0.2.18.9', 'kdan/v0.2.18.11',
                    'tokensavy/v0.2.18.99', 'kdan/v0.2.18.01', 'kdan/v0.2.18.99-rc1']]
        self.assertEqual(release_plan.listed_versions(releases, 'kdan'), ['0.2.18.9', '0.2.18.11'])

    def test_tag_reservations_and_channel_isolation_remain_enforced(self):
        with patch.object(release_plan, 'release_for_tag', return_value=None) as lookup, \
             patch.object(release_plan, 'tag_commit', side_effect=['a' * 40, None]):
            version = release_plan.next_available_version('holeenlu/sub2api', 'tokensavy', '0.2.18',
                                                          ['0.2.18.6'], False, False)
        self.assertEqual(version, '0.2.18.8')
        self.assertEqual([call.args[1] for call in lookup.call_args_list],
                         ['tokensavy/v0.2.18.7', 'tokensavy/v0.2.18.8'])

    def test_lookup_failure_does_not_allocate_or_create_anything(self):
        with patch.object(release_plan, 'release_for_tag', side_effect=RuntimeError('403 forbidden')), \
             patch.object(release_plan, 'tag_commit') as refs:
            with self.assertRaisesRegex(RuntimeError, '403'):
                release_plan.next_available_version('holeenlu/sub2api', 'kdan', '0.2.18', [], False, False)
        refs.assert_not_called()

    def test_excessive_history_is_bounded(self):
        with patch.object(release_plan, 'release_for_tag', return_value={'id': 1}) as lookup:
            with self.assertRaisesRegex(ValueError, '100 checks'):
                release_plan.next_available_version('holeenlu/sub2api', 'kdan', '0.2.18', [], False, False)
        self.assertEqual(lookup.call_count, 100)

    def test_conflicting_same_source_draft_is_not_resumed(self):
        plan = dict(channel='kdan', version='0.2.18.7', tag='kdan/v0.2.18.7', commit='a' * 40)
        draft = dict(id=22, tag_name=plan['tag'], draft=True)
        published = dict(id=11, tag_name=plan['tag'], draft=False)
        with patch.object(release_plan, 'release_for_tag', return_value=published), \
             patch.object(release_plan, 'tag_commit') as refs:
            self.assertIsNone(release_plan.resumable_release('repo', [draft, published], [(plan, draft)], plan['commit']))
            # Also reject a conflict omitted from the list but found directly.
            self.assertIsNone(release_plan.resumable_release('repo', [draft], [(plan, draft)], plan['commit']))
        refs.assert_not_called()

    def test_unique_draft_resumes_but_moved_tag_or_baseline_does_not(self):
        plan = dict(channel='kdan', version='0.2.18.7', tag='kdan/v0.2.18.7', commit='a' * 40)
        draft = dict(id=22, tag_name=plan['tag'], draft=True)
        with patch.object(release_plan, 'release_for_tag', return_value=None), \
             patch.object(release_plan, 'tag_commit', return_value=plan['commit']) as ref:
            self.assertEqual(release_plan.resumable_release('repo', [draft], [(plan, draft)], plan['commit']), (plan, draft))
            ref.return_value = 'b' * 40
            self.assertIsNone(release_plan.resumable_release('repo', [draft], [(plan, draft)], plan['commit']))
            ref.side_effect = [plan['commit'], 'b' * 40]
            plan.update(previous_tag='kdan/v0.2.18.6', previous_commit='c' * 40)
            self.assertIsNone(release_plan.resumable_release('repo', [draft], [(plan, draft)], plan['commit']))

    def test_published_same_source_is_preferred_to_a_different_unused_draft(self):
        published_plan = dict(channel='kdan', version='0.2.18.6', tag='kdan/v0.2.18.6', commit='a' * 40)
        draft_plan = dict(published_plan, version='0.2.18.7', tag='kdan/v0.2.18.7')
        published = dict(id=11, tag_name=published_plan['tag'], draft=False)
        draft = dict(id=22, tag_name=draft_plan['tag'], draft=True)
        with patch.object(release_plan, 'release_for_tag', return_value=published), \
             patch.object(release_plan, 'tag_commit', return_value=published_plan['commit']):
            self.assertEqual(release_plan.resumable_release('repo', [draft, published],
                             [(draft_plan, draft), (published_plan, published)], published_plan['commit']),
                             (published_plan, published))

    def test_notes_skip_published_history_with_missing_or_rebound_tags(self):
        history = [(dict(version='0.2.18.' + str(n), commit=sha),
                    dict(tag_name='kdan/v0.2.18.' + str(n), draft=False))
                   for n, sha in [(5, 'a' * 40), (6, 'b' * 40), (7, 'c' * 40)]]
        refs = {'kdan/v0.2.18.5': 'a' * 40, 'kdan/v0.2.18.6': 'b' * 40, 'kdan/v0.2.18.7': 'd' * 40}
        self.assertEqual(notes_baseline(history, '0.2.18.19', resolve_tag=refs.get)['previous_version'], '0.2.18.6')
        self.assertEqual(notes_baseline(history, '0.2.18.19', resolve_tag=lambda tag: None), {})

    def test_annotated_reservation_tag_uses_peeled_commit(self):
        with patch.object(release_plan, 'git', return_value='b' * 40 + '\trefs/tags/kdan/v0.2.18.7\n' +
                          'a' * 40 + '\trefs/tags/kdan/v0.2.18.7^{}'):
            self.assertEqual(release_plan.tag_commit('kdan/v0.2.18.7'), 'a' * 40)

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

    def test_tag_reservations_are_channel_scoped_and_strict_versions(self):
        refs = ['tokensavy/v0.2.18.3', 'tokensavy/v0.2.18.10',
                'tokensavy/v0.2.18.10^{}', 'tokensavy/v0.2.18.01',
                'tokensavy/v0.2.18.99-rc1', 'tapmodels/v0.2.18.99', 'v0.2.18']
        with patch('scripts.release.plan.git', return_value='\n'.join(
                'a' * 40 + '\trefs/tags/' + ref for ref in refs)) as git:
            versions = reserved_versions('tokensavy')
        git.assert_called_once_with('ls-remote', '--tags', '--refs', 'origin', 'refs/tags/tokensavy/v*')
        self.assertEqual(versions, ['0.2.18.3', '0.2.18.10'])
        self.assertEqual(next_version('0.2.18', versions, False, False), '0.2.18.11')

    def test_notes_use_previous_published_version_in_the_same_channel(self):
        releases = []
        for channel, version, draft, prerelease in [
            ('kdan', '0.2.17.9', False, False),
            ('kdan', '0.2.17.10', False, False),
            ('kdan', '0.2.17.11', True, False),
            ('kdan', '0.2.17.12', False, True),
            ('kdan', '0.2.17.13', True, False),
            ('tapmodels', '0.2.17.12', False, False),
        ]:
            plan = dict(channel=channel, version=version, commit='source-' + version)
            releases.append(dict(tag_name=channel + '/v' + version, draft=draft, prerelease=prerelease,
                                 body='<!-- release-plan:' + json.dumps(plan) + ' -->'))
        history = records(releases, 'kdan')
        self.assertEqual(notes_baseline(history, '0.2.17.13'), dict(
            previous_version='0.2.17.10', previous_tag='kdan/v0.2.17.10', previous_commit='source-0.2.17.10'))
        self.assertEqual(notes_baseline(history, '0.2.17.10')['previous_version'], '0.2.17.9')
        self.assertEqual(notes_baseline(history, '0.2.17.1'), {})

    def test_rebuilt_history_requires_integrated_official_base(self):
        with patch('scripts.release.plan.ancestor', side_effect=lambda a, b: (a, b) == ('upstream-base', 'new-head')):
            self.assertTrue(history_migrated_with_integrated_official_base(
                {'commit': 'old-head', 'upstream_commit': 'upstream-base'}, 'new-head'))
            self.assertFalse(history_migrated_with_integrated_official_base(
                {'commit': 'old-head'}, 'new-head'))
            self.assertFalse(history_migrated_with_integrated_official_base(
                {'commit': 'old-head', 'upstream_commit': 'unrelated-base'}, 'new-head'))


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
        with patch.object(u, 'images_for', return_value={'kdan': 'ghcr.io/holeenlu/kdan@sha256:' + 'b'*64}), \
             patch.object(u, 'managed_services', return_value=['kdan']), \
             patch.object(u, 'container', return_value='app'), \
             patch.object(u, 'inspect', side_effect=[{'Image': 'sha256:old', 'Config': {'Healthcheck': {"Test": ['CMD','true']}}}, {'Id': 'sha256:new'}]), \
             patch.object(u, 'command') as command, \
             patch.object(u, 'preflight'), \
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
        with patch.object(u, 'images_for', return_value={'kdan': 'image'}), \
             patch.object(u, 'managed_services', return_value=['kdan']), \
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
        with patch.object(u, 'images_for', return_value={'kdan': 'image'}), \
             patch.object(u, 'managed_services', return_value=['kdan']), \
             patch.object(u, 'container', return_value='app'), \
             patch.object(u, 'inspect', side_effect=[{'Image': 'old', 'Config': {'Healthcheck': {'Test': ['CMD', 'true']}}}, {'Id': 'new'}]), \
             patch.object(u, 'command') as command, \
             patch.object(u, 'preflight', side_effect=RuntimeError('preflight failed')):
            u.apply('0.2.13.6')
        self.assertEqual(u.override.read_bytes(), before)
        self.assertEqual(u.state['phase'], 'preflight')
        self.assertEqual(u.state_images('previous_images', 'previous_image'), {'kdan': 'old'})
        self.assertFalse(any('up' in c.args for c in command.call_args_list))

    def test_prepared_update_reuses_validation_and_preserves_paired_rollback(self):
        u = self.updater
        images = {'kdan': 'backend-new', 'vendor-portal': 'portal-new'}
        targets = {service: 'id-' + service for service in images}
        def inspect(value):
            if value in images:
                return {'Image': 'old-' + value, 'Config': {'Healthcheck': {'Test': ['CMD', 'true']}}}
            return {'Id': targets[next(service for service, image in images.items() if image == value)]}
        for fails in (False, True):
            with self.subTest(rollback=fails), patch.object(u, 'preparation_fingerprint', return_value='same'), \
                 patch.object(u, 'managed_services', return_value=list(images)), \
                 patch.object(u, 'images_for', return_value=images) as manifest, \
                 patch.object(u, 'container', side_effect=lambda service: service), \
                 patch.object(u, 'inspect', side_effect=inspect), patch.object(u, 'command') as command, \
                 patch.object(u, 'preflight') as preflight, \
                 patch.object(u, 'wait_healthy', side_effect=[RuntimeError('unhealthy'), None] if fails else [None]), \
                 patch('deploy.compose_updater.time.sleep'), patch('deploy.compose_updater.threading.Thread') as thread:
                self.assertEqual(u.prepare('0.2.18.7'), {'images': images, 'target_ids': targets})
                u.start('0.2.18.7', prepared=True)
                self.assertIsNone(u.prepared)
                u.apply(*thread.call_args.kwargs['args'], **thread.call_args.kwargs['kwargs'])
                manifest.assert_called_once()
                preflight.assert_called_once_with(images)
                self.assertEqual([c.args for c in command.call_args_list if c.args[:2] == ('docker', 'pull')],
                                 [('docker', 'pull', image) for image in images.values()])
                replacements = [c.args for c in command.call_args_list if 'up' in c.args]
                self.assertEqual(len(replacements), 2 if fails else 1)
                self.assertTrue(all('--no-deps' in c and '--pull' in c and 'never' in c for c in replacements))
                self.assertEqual(u.state['status'], 'failed' if fails else 'succeeded')
                self.assertFalse(u.state['recovery_required'])
                self.assertFalse(u.lock.locked())
                with self.assertRaisesRegex(RuntimeError, 'absent or expired'):
                    u.start('0.2.18.7', prepared=True)

    def test_prepared_update_rejects_stale_config_version_and_image_before_switching(self):
        u = self.updater
        candidate = dict(version='0.2.18.7', images={'kdan': 'image'}, target_ids={'kdan': 'id'},
                         fingerprint='same', prepared_at=100)
        for reason in ('version', 'expired', 'clock', 'config', 'services', 'image', 'missing'):
            with self.subTest(reason=reason), patch('deploy.compose_updater.time.monotonic', return_value=101), \
                 patch.object(u, 'preparation_fingerprint', return_value='changed' if reason == 'config' else 'same'), \
                 patch.object(u, 'managed_services', return_value=['other'] if reason == 'services' else ['kdan']), \
                 patch.object(u, 'inspect', return_value={'Id': 'changed' if reason == 'image' else 'id'}), \
                 patch('deploy.compose_updater.threading.Thread') as thread:
                u.prepared = dict(candidate)
                if reason == 'expired':
                    u.prepared['prepared_at'] = -1000
                if reason == 'clock':
                    u.prepared['prepared_at'] = 200
                if reason == 'missing':
                    u.prepared = None
                before = u.override.read_bytes()
                with self.assertRaises(RuntimeError):
                    u.start('0.2.18.8' if reason == 'version' else '0.2.18.7', prepared=True)
                self.assertEqual(before, u.override.read_bytes())
                self.assertFalse(u.lock.locked())
                thread.assert_not_called()

    def test_preparation_failure_and_concurrent_update_never_leave_reusable_candidate(self):
        u = self.updater
        u.lock.acquire()
        with self.assertRaisesRegex(RuntimeError, 'already running'):
            u.prepare('0.2.18.7')
        u.lock.release()
        with patch.object(u, 'preparation_fingerprint', return_value='same'), \
             patch.object(u, 'managed_services', return_value=['kdan']), \
             patch.object(u, 'images_for', return_value={'kdan': 'image'}), \
             patch.object(u, 'command'), patch.object(u, 'inspect', return_value={'Id': 'id'}), \
             patch.object(u, 'preflight', side_effect=RuntimeError('preflight failed')):
            with self.assertRaisesRegex(RuntimeError, 'preflight failed'):
                u.prepare('0.2.18.7')
        self.assertIsNone(u.prepared)
        self.assertFalse(u.lock.locked())
        self.assertEqual(u.state['status'], 'idle')

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
        model = {'services': {'kdan': {'networks': {
            'backend': {'ipv4_address': '172.30.95.5', 'ipv6_address': 'fd00::5', 'aliases': ['kdan']},
            'dynamic': None,
        }}}}
        expected_networks = {'backend': {'ipv4_address': '', 'ipv6_address': ''}}
        def command(*args, **kwargs):
            self.assertEqual(u.override.read_bytes(), before)
            if args[len(u.compose):] == ('config', '--format', 'json'):
                return json.dumps(model)
            self.assertEqual(args[-1], '--check-model-policy-migration')
            self.assertIn('--no-deps', args)
            candidate_path = Path(args[len(u.compose) + 1])
            candidate = json.loads(candidate_path.read_text())['services']['kdan']
            self.assertEqual(candidate['image'], 'candidate')
            self.assertEqual(candidate.get('networks', {}), expected_networks)
        with patch.object(u, 'command', side_effect=command):
            u.preflight({'kdan': 'candidate'})
            model['services']['kdan'] = {}
            expected_networks = {}
            u.preflight({'kdan': 'candidate'})

    def test_manifest_cannot_change_brand_or_image(self):
        release = {'tag_name':'kdan/v0.2.8.1', 'assets':[{'name':'release-manifest.json', 'id':42,
            'url':'https://api.github.com/repos/holeenlu/sub2api/releases/assets/42'}]}
        manifest = {'channel':'tapmodels', 'version':'0.2.8.1', 'image':'ghcr.io/holeenlu/kdan', 'image_digest':'sha256:'+'a'*64}
        with patch('deploy.compose_updater.read_json', side_effect=[release, manifest]):
            with self.assertRaisesRegex(ValueError, 'manifest'):
                self.updater.images_for('0.2.8.1')

    def test_portal_manifest_is_required_and_brand_scoped_for_all_channels(self):
        u = self.updater
        for channel in ('kdan', 'tapmodels', 'tokensavy'):
            with self.subTest(channel=channel):
                image = 'ghcr.io/holeenlu/' + channel
                u.config.update(channel=channel, image=image)
                release = {'tag_name': channel+'/v0.2.8.1', 'assets': [
                    {'name': 'release-manifest.json', 'id': 42,
                     'url': 'https://api.github.com/repos/holeenlu/sub2api/releases/assets/42'}]}
                manifest = dict(channel=channel, version='0.2.8.1', image=image,
                                image_digest='sha256:'+'a'*64, portal_image=image+'-vendor',
                                portal_image_digest='sha256:'+'b'*64)
                with patch('deploy.compose_updater.read_json', side_effect=[release, manifest]):
                    self.assertEqual(u.images_for('0.2.8.1', ['kdan', 'vendor-portal']), {
                        'kdan': image+'@sha256:'+'a'*64, 'vendor-portal': image+'-vendor@sha256:'+'b'*64})
                for bad in [{**manifest, 'portal_image': 'ghcr.io/other/portal'},
                            {**manifest, 'portal_image_digest': 'latest'},
                            {k: v for k, v in manifest.items() if not k.startswith('portal_')}]:
                    with patch('deploy.compose_updater.read_json', side_effect=[release, bad]):
                        with self.assertRaisesRegex(ValueError, 'matching supplier portal'):
                            u.images_for('0.2.8.1', ['kdan', 'vendor-portal'])

    def test_standalone_portal_selects_only_matching_portal_digest_and_nginx_preflight(self):
        u = self.updater
        u.config.update(portal_only=True, service='vendor-portal')
        manifest = dict(channel='kdan', version='0.2.8.1', image='ghcr.io/holeenlu/kdan',
                        image_digest='sha256:'+'a'*64, portal_image='ghcr.io/holeenlu/kdan-vendor',
                        portal_image_digest='sha256:'+'b'*64)
        release = {'tag_name': 'kdan/v0.2.8.1', 'assets': [{'name': 'release-manifest.json', 'id': 42,
                   'url': 'https://api.github.com/repos/holeenlu/sub2api/releases/assets/42'}]}
        with patch('deploy.compose_updater.read_json', side_effect=[release, manifest]):
            images = u.images_for('0.2.8.1')
        self.assertEqual(images, {'vendor-portal': manifest['portal_image']+'@'+manifest['portal_image_digest']})
        model = {'services': {'vendor-portal': {'image': manifest['portal_image']+':0.2.8'}}}
        with patch.object(u, 'command', return_value=json.dumps(model)) as command:
            self.assertEqual(u.managed_services(), ['vendor-portal'])
            u.preflight(images)
        runs = [call.args for call in command.call_args_list if 'run' in call.args]
        self.assertEqual(len(runs), 1)
        self.assertEqual(runs[0][-4:], ('nginx', '-t', '-c', '/tmp/vendor-portal/nginx.conf'))
        self.assertNotIn('--check-model-policy-migration', runs[0])

    def test_portal_http_requires_trusted_authorization_and_exact_running_backend_version(self):
        u = self.updater
        socket_path = str(Path(self.temp.name) / 'updater.sock')
        request_closed = threading.Event()
        class EarlyRejectServer(Server):
            def shutdown_request(self, request):
                try:
                    super().shutdown_request(request)
                finally:
                    request_closed.set()
        class UnixConnection(http.client.HTTPConnection):
            def connect(self):
                self.sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
                self.sock.connect(socket_path)
        with EarlyRejectServer(socket_path, Handler) as server:
            server.updater = u
            thread = threading.Thread(target=server.serve_forever, daemon=True)
            thread.start()
            self.addCleanup(server.shutdown)
            def request(method, path, body=None, headers=None, delay_body=False):
                conn = UnixConnection('localhost', timeout=3)
                payload = json.dumps(body) if body is not None else None
                send = conn.send
                def send_after_rejection(data):
                    if payload is not None and data == payload.encode():
                        self.assertTrue(request_closed.wait(3), 'Server did not close the rejected request')
                    send(data)
                if delay_body:
                    conn.send = send_after_rejection
                try:
                    try:
                        conn.request(method, path, payload, headers or {})
                    except BrokenPipeError:
                        # An early HTTP rejection may close the socket before
                        # the body is sent. Still require a real, valid response.
                        pass
                    response = conn.getresponse()
                    return response.status, json.loads(response.read())
                finally:
                    conn.close()
            with patch.object(u, 'portal_status', return_value={'backend_version': '0.2.8.1', 'mode': 'portal'}) as status, \
                 patch.object(u, 'start') as start:
                for delay_body in (True, False):
                    with self.subTest(delay_body=delay_body):
                        request_closed.clear()
                        self.assertEqual(request('POST', '/portal/update', {'version': '0.2.8.1'},
                                                 delay_body=delay_body)[0], 403)
                status.assert_not_called()
                headers = {'X-Portal-Authorized': '1', 'X-Portal-Backend-Version': '0.2.8.1'}
                self.assertEqual(request('POST', '/portal/update', {'version': '0.2.8.2'}, headers)[0], 409)
                self.assertEqual(request('POST', '/portal/update', {'version': '0.2.8.1', 'image': 'attacker'}, headers)[0], 400)
                start.assert_not_called()
                self.assertEqual(request('POST', '/portal/update', {'version': '0.2.8.1'}, headers)[0], 202)
                start.assert_called_once_with('0.2.8.1')
                self.assertEqual(request('GET', '/portal/status', headers=headers)[0], 200)
            server.shutdown()

    def test_discovers_portal_without_taking_over_an_unrelated_service(self):
        u = self.updater
        model = {'services': {'kdan': {}, 'postgres': {},
                              'vendor-portal': {'image': 'ghcr.io/holeenlu/kdan-vendor:0.2.8.1'}}}
        with patch.object(u, 'command', side_effect=lambda *a, **kw: json.dumps(model)):
            self.assertEqual(u.managed_services(), ['kdan', 'vendor-portal'])
            model['services']['vendor-portal']['image'] = 'postgres:16'
            with self.assertRaisesRegex(ValueError, 'configured brand'):
                u.managed_services()
            del model['services']['vendor-portal']
            self.assertEqual(u.managed_services(), ['kdan'])
            u.config['portal_service'] = 'missing'
            with self.assertRaisesRegex(ValueError, 'separate existing'):
                u.managed_services()

    @patch('deploy.compose_updater.time.sleep')
    def test_portal_failure_restores_both_images_and_preserves_other_overrides(self, _):
        u = self.updater
        original = {'services': {'kdan': {'environment': {'KEEP': 'yes'}},
                                'redis': {'image': 'redis:7'}}}
        u.override.write_text(json.dumps(original))
        images = {'kdan': 'backend-new', 'vendor-portal': 'portal-new'}
        old = lambda image: {'Image': image, 'Config': {'Healthcheck': {'Test': ['CMD', 'true']}}}
        u.lock.acquire()
        with patch.object(u, 'managed_services', return_value=list(images)), \
             patch.object(u, 'images_for', return_value=images), \
             patch.object(u, 'container', side_effect=lambda service=None: service or 'kdan'), \
             patch.object(u, 'inspect', side_effect=[old('old-backend'), old('old-portal'),
                                                     {'Id': 'new-backend'}, {'Id': 'new-portal'}]), \
             patch.object(u, 'command') as command, patch.object(u, 'preflight') as preflight, \
             patch.object(u, 'wait_healthy', side_effect=[RuntimeError('portal unhealthy'), None]) as health:
            u.apply('0.2.8.1')
        preflight.assert_called_once_with(images)
        self.assertEqual(health.call_args_list[0].args[0], {'kdan': 'new-backend', 'vendor-portal': 'new-portal'})
        restored = json.loads(u.override.read_text())['services']
        self.assertEqual(restored['kdan'], {'environment': {'KEEP': 'yes'}, 'image': 'old-backend'})
        self.assertEqual(restored['vendor-portal']['image'], 'old-portal')
        self.assertEqual(restored['redis'], original['services']['redis'])
        updates = [c.args for c in command.call_args_list if 'up' in c.args]
        self.assertEqual(len(updates), 2)
        self.assertTrue(all(c[-2:] == ('kdan', 'vendor-portal') and '--no-deps' in c for c in updates))
        self.assertFalse(u.state['recovery_required'])
        self.assertFalse(u.lock.locked())

    def test_restart_cannot_accept_a_mixed_backend_and_portal_pair(self):
        u = self.updater
        u.save(status='running', phase='checking', target_ids={'kdan': 'new', 'vendor-portal': 'new-portal'},
               previous_images={'kdan': 'old', 'vendor-portal': 'old-portal'}, recovery_required=True)
        with patch.object(Updater, 'wait_healthy', side_effect=[RuntimeError('portal still old'), None]), \
             patch.object(Updater, 'command'):
            restarted = Updater(u.config)
        self.assertEqual(restarted.state['status'], 'failed')
        self.assertEqual(restarted.state['phase'], 'rolled_back')
        self.assertEqual(json.loads(u.override.read_text())['services'], {
            'kdan': {'image': 'old'}, 'vendor-portal': {'image': 'old-portal'}})

    def test_portal_preflight_checks_nginx_and_clears_fixed_network_addresses(self):
        u = self.updater
        model = {'services': {'kdan': {}, 'vendor-portal': {
            'networks': {'edge': {'ipv4_address': '172.30.88.5'}}}}}
        commands = []
        def command(*args, **kwargs):
            if args[-3:] == ('config', '--format', 'json'):
                return json.dumps(model)
            candidates = json.loads(Path(args[len(u.compose)+1]).read_text())['services']
            self.assertEqual(candidates['vendor-portal']['networks'], {'edge': {'ipv4_address': ''}})
            self.assertIn('--no-deps', args)
            commands.append(args)
        with patch.object(u, 'command', side_effect=command):
            u.preflight({'kdan': 'backend', 'vendor-portal': 'portal'})
        self.assertEqual(commands[0][-1], '--check-model-policy-migration')
        self.assertEqual(commands[1][-5:], ('vendor-portal', 'nginx', '-t', '-c', '/tmp/vendor-portal/nginx.conf'))

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
            current_head = git('rev-parse', 'HEAD')
            git('checkout', '--detach', current_head)
            advanced = git('commit-tree', git('rev-parse', 'HEAD^{tree}'), '-p', current_head,
                           '-m', 'channel advanced before preparation')
            git('update-ref', 'refs/heads/' + branch, advanced)
            releases = []
            tags = {}
            real_run = planner.run
            real_popen = subprocess.Popen
            def local_process(args, *positional, **kwargs):
                if args[0] != 'git' or args[1] in ('fetch', 'push', 'ls-remote'):
                    raise AssertionError(f'Unexpected external process in planner integration test: {args}')
                return real_popen(args, *positional, **kwargs)
            def fake_api(method, path, **kwargs):
                from urllib.parse import unquote
                prefix = 'repos/holeenlu/sub2api/releases/tags/'
                self.assertEqual(method, 'GET')
                self.assertEqual(kwargs, {'missing_ok': True})
                self.assertTrue(path.startswith(prefix))
                tag = unquote(path[len(prefix):])
                return next((r for r in releases if r['tag_name'] == tag and not r['draft']), None)
            def fake_run(*args):
                if args[:2] == ('git', 'ls-remote'):
                    import fnmatch
                    patterns = args[args.index('origin') + 1:]
                    return '\n'.join(sha + '\t' + ref for ref, sha in tags.items()
                                     if any(fnmatch.fnmatchcase(ref, pattern) for pattern in patterns))
                if args[0] != 'gh':
                    return real_run(*args)
                if args[1:4] == ('api', '-X', 'POST'):
                    self.assertEqual(args[4], 'repos/holeenlu/sub2api/git/refs')
                    ref, sha = args[6].removeprefix('ref='), args[8].removeprefix('sha=')
                    self.assertNotIn(ref, tags)
                    tags[ref] = sha
                    return '{}'
                if args[1:4] == ('api','--method','POST'):
                    payload = json.loads(Path('release-request.json').read_text())
                    releases.append(dict(id=len(releases)+1, **payload))
                    # The creation response is sufficient; do not require a
                    # second list request to observe an eventually visible draft.
                    return json.dumps(releases[-1])
                if args[1:] == ('api', '--paginate', '--slurp',
                                'repos/holeenlu/sub2api/releases?per_page=100'):
                    return json.dumps([releases])
                raise AssertionError(args)
            env = {'GITHUB_REPOSITORY':'holeenlu/sub2api','GITHUB_REF':'refs/heads/' + branch,
                   'GITHUB_OUTPUT':str(Path(directory)/'output')}
            with patch.dict(os.environ,env), patch('sys.argv',['plan.py']), \
                 patch.object(planner,'run',side_effect=fake_run), \
                 patch.object(planner,'api',side_effect=fake_api), \
                 patch.object(subprocess,'Popen',side_effect=local_process), \
                 patch.object(planner,'official_base',return_value=('0.2.13',base,base)):
                planner.main()
                first = json.loads(Path('release-plan.json').read_text())
                self.assertEqual(first['version'], '0.2.13.3' if channel == 'tapmodels' else '0.2.13.1')
                self.assertEqual(first['channel'], channel)
                self.assertEqual(first['commit'], current_head)
                self.assertNotEqual(git('rev-parse', 'refs/heads/' + branch), first['commit'])
                self.assertEqual(tags['refs/tags/' + first['tag']], first['commit'])
                self.assertEqual(first['image'], 'ghcr.io/holeenlu/' + channel)
                self.assertEqual(first['tag'], channel + '/v' + first['version'])
                # Branch rewrites during a build do not change the selected release source.
                rebuilt_channel = git('commit-tree', git('rev-parse', 'HEAD^{tree}'), '-p', base,
                                      '-m', 'channel rewritten during build')
                git('update-ref', 'refs/heads/' + branch, rebuilt_channel)
                planner.main()  # Failed build resumes its draft without allocating again.
                self.assertEqual(json.loads(Path('release-plan.json').read_text()), first)
                self.assertEqual(len(releases),1)
                releases[0]['draft'] = False
                Path('output').write_text('')
                planner.main()  # Published SHA only repairs latest promotion if needed.
                self.assertIn('publish=false',Path('output').read_text())
                self.assertIn('promote=true',Path('output').read_text())
                self.assertEqual(len(releases),1)
                Path('custom').write_text('second')
                git('add','custom'); git('commit','-m','second')
                current_head = git('rev-parse', 'HEAD')
                planner.main()
                self.assertEqual(json.loads(Path('release-plan.json').read_text())['version'], '0.2.13.4' if channel == 'tapmodels' else '0.2.13.2')
                releases[-1]['draft'] = False
                git('checkout', '--detach', first['commit'])
                Path('output').write_text('')
                planner.main()  # An older published release must not move latest backwards.
                self.assertEqual(len(releases), 2)
                self.assertEqual(Path('output').read_text(), 'publish=false\n')
                rebuilt = git('commit-tree', git('rev-parse', current_head + '^{tree}'), '-p', base,
                              '-m', 'current rebuilt customization')
                current_head = rebuilt
                git('checkout', '--detach', rebuilt)
                planner.main()
                self.assertEqual(len(releases), 3)
                self.assertEqual(json.loads(Path('release-plan.json').read_text())['commit'], rebuilt)
                # A deleted draft leaves its tag reserved across a branch rebuild.
                orphan = releases.pop()
                orphan_version = json.loads(orphan['body'].split('release-plan:')[1].split(' -->')[0])['version']
                reserved = dict(tags)
                current_head = git('commit-tree', git('rev-parse', 'HEAD^{tree}'), '-p', base,
                                   '-m', 'new source after orphaned reservation')
                git('checkout', '--detach', current_head)
                planner.main()
                recovered = json.loads(Path('release-plan.json').read_text())
                self.assertEqual(parts(recovered['version'])[3], parts(orphan_version)[3] + 1)
                self.assertEqual(recovered['commit'], current_head)
                self.assertEqual(recovered['previous_commit'],
                                 json.loads(releases[1]['body'].split('release-plan:')[1].split(' -->')[0])['commit'])
                self.assertEqual({ref: tags[ref] for ref in reserved}, reserved)
                planner.main()  # The new draft remains resumable.
                self.assertEqual(len(releases), 3)
                # An explicit new version can rebuild already-published source.
                releases[-1]['draft'] = False
                with patch('sys.argv', ['plan.py', '--version', '0.2.13.14']):
                    planner.main()
                    requested = json.loads(Path('release-plan.json').read_text())
                    self.assertEqual(requested['version'], '0.2.13.14')
                    self.assertEqual(requested['commit'], current_head)
                    self.assertEqual(len(releases), 4)
                    planner.main()  # Same requested version resumes the same draft.
                    self.assertEqual(json.loads(Path('release-plan.json').read_text()), requested)
                    self.assertEqual(len(releases), 4)
                    releases[-1]['draft'] = False
                    Path('output').write_text('')
                    planner.main()  # Published exact-version retry stays read-only.
                    self.assertIn('publish=false', Path('output').read_text())
                    self.assertEqual(len(releases), 4)
            os.chdir(original)

if __name__ == '__main__':
    unittest.main()
