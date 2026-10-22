import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

from scripts.release import notes
from scripts.release import plan as release_plan

ROOT = Path(__file__).resolve().parents[2]
DIGEST = 'sha256:' + 'a' * 64


class ReleaseNotesTests(unittest.TestCase):
    def setUp(self):
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        self.root = Path(directory.name)
        original = Path.cwd()
        os.chdir(self.root)
        self.addCleanup(os.chdir, original)
        self.git('init', '-q', '-b', 'main')
        # Background maintenance can race TemporaryDirectory cleanup after merges.
        self.git('config', 'maintenance.auto', 'false')
        self.git('config', 'gc.auto', '0')
        self.git('config', 'user.name', 'Release Test')
        self.git('config', 'user.email', 'release@example.test')
        self.config = json.loads((ROOT / 'scripts/release/channels.json').read_text())
        self.base = self.commit('initial', 'feat: old feature')
        self.plan = dict(channel='kdan', version='0.2.17.2', base='0.2.17',
                         upstream_commit=self.base, previous_commit=self.base,
                         previous_version='0.2.17.1', previous_tag='kdan/v0.2.17.1',
                         image='ghcr.io/holeenlu/kdan', tag='kdan/v0.2.17.2')

    def git(self, *args):
        return subprocess.check_output(['git', *args], text=True, stderr=subprocess.PIPE).strip()

    def commit(self, content, message, path='source.txt'):
        Path(path).write_text(content)
        self.git('add', path)
        self.git('commit', '-q', '-m', message)
        return self.git('rev-parse', 'HEAD')

    def render(self, **overrides):
        plan = dict(self.plan, commit=self.git('rev-parse', 'HEAD'), **overrides)
        return notes.render(plan, DIGEST, self.config)

    def test_real_range_includes_merged_work_without_merge_titles_or_twenty_commit_limit(self):
        self.git('checkout', '-q', '-b', 'feature')
        for index in range(24):
            self.commit(str(index), 'fix(api): correction ' + str(index))
        self.commit('better', 'fixup! fix(api): correction 23')
        self.commit('guide', 'docs: update guide', path='guide.txt')
        self.git('checkout', '-q', 'main')
        self.git('merge', '-q', '--no-ff', 'feature', '-m', 'Merge an internal branch')
        result = self.render()
        self.assertIn('## 问题修复', result)
        self.assertIn('api：correction 0 ', result)
        self.assertEqual(result.count('api：correction 23 '), 1)
        self.assertNotIn('old feature', result)
        self.assertNotIn('Merge an internal branch', result)
        self.assertNotIn('fixup!', result)
        self.assertIn('<summary>文档、测试与构建变更</summary>', result)
        self.assertIn('/compare/' + self.base + '..', result)

    def test_reviewed_descriptions_and_breaking_change_are_readable_and_escaped(self):
        self.commit('new', 'feat(api)!: support streaming\n\n'
                    'Release-Note: 支持流式请求并保留工具结果。\n'
                    'Release-Note: 错误文本 <script> 与 [link](javascript:bad)。\n'
                    'BREAKING CHANGE: 旧客户端需要更新。')
        result = self.render()
        self.assertIn('## 新增功能', result)
        self.assertIn('支持流式请求并保留工具结果。', result)
        self.assertNotIn('support streaming', result)
        self.assertIn('## 破坏性变更', result)
        self.assertIn('旧客户端需要更新。', result)
        self.assertNotIn('<script>', result)
        self.assertNotIn('[link](javascript:bad)', result)

    def test_history_rewrite_suppresses_old_features_but_keeps_new_editorial(self):
        previous = self.commit('old', 'feat(accounts): all account features\n\n'
                               'Release-Note: 已发布的账号功能。\nBREAKING CHANGE: 已发布的兼容性变更。')
        self.plan['previous_commit'] = previous
        self.git('reset', '-q', '--hard', self.base)
        self.commit('new', 'feat(accounts): all account features\n\n'
                    'Release-Note: 已发布的账号功能。\nRelease-Note: 修复刷新后额度丢失。\n'
                    'BREAKING CHANGE: 已发布的兼容性变更。')
        self.commit('breaking', 'fix: compatibility\n\nBREAKING CHANGE: 新的配置要求。')
        result = self.render()
        self.assertIn('发布历史经过整理', result)
        self.assertIn('修复刷新后额度丢失。', result)
        self.assertNotIn('已发布的账号功能。', result)
        self.assertNotIn('all account features', result)
        self.assertNotIn('已发布的兼容性变更。', result)
        self.assertIn('新的配置要求。', result)
        self.assertIn('两版文件变更统计', result)

    def test_patch_equivalence_and_identical_tree_do_not_advertise_old_changes(self):
        previous = self.commit('same content', 'fix: old wording')
        self.plan['previous_commit'] = previous
        self.git('reset', '-q', '--hard', self.base)
        self.commit('same content', 'fix: rewritten wording')
        self.assertIn('两版源码内容一致', self.render())
        self.commit('new guide', 'docs: new guide', path='guide.txt')
        changes, rewritten, _ = notes.changes_between(previous, self.git('rev-parse', 'HEAD'))
        self.assertTrue(rewritten)
        self.assertEqual([change[1] for change in changes], ['docs: new guide'])
        result = self.render()
        self.assertNotIn('rewritten wording', result)
        self.assertNotIn('## 问题修复', result)

    def test_version_editorial_is_used_only_for_its_exact_upgrade_range(self):
        self.commit('new', 'fix: fallback description')
        directory = Path('deploy/release-notes')
        directory.mkdir(parents=True)
        (directory / '0.2.17.2.md').write_text(
            '<!-- previous-version: 0.2.17.1 -->\n\n本次修复请求兼容性。\n\n## 问题修复\n\n- 中文具体说明。')
        result = self.render()
        self.assertIn('本次修复请求兼容性。', result)
        self.assertNotIn('fallback description', result)
        skipped = self.render(previous_version='0.2.16.1')
        self.assertNotIn('本次修复请求兼容性。', skipped)
        self.assertIn('fallback description', skipped)
        (directory / '0.2.17.2.md').write_text('Missing baseline')
        with self.assertRaisesRegex(ValueError, 'previous-version'):
            self.render()

    def test_bootstrap_uses_integrated_upstream_and_release_metadata_stays_channel_scoped(self):
        head = self.commit('new', 'feat: new feature')
        for channel in self.config:
            plan = {k: v for k, v in self.plan.items() if not k.startswith('previous_')}
            plan.update(channel=channel, tag=channel + '/v0.2.17.2',
                        image=self.config[channel]['image'], commit=head)
            with self.subTest(channel=channel):
                result = notes.render(plan, DIGEST, self.config)
                self.assertIn('首次发布', result)
                self.assertIn('docker pull ghcr.io/holeenlu/' + channel + ':0.2.17.2', result)
                self.assertIn('releases/tag/' + channel + '%2Fv0.2.17.2', result)
                self.assertIn('checksums.txt', result)
                self.assertIn('仅 Linux amd64', result)
                self.assertNotIn('arm64', result)
                self.assertIn('镜像回退不会回滚数据库', result)
                marker = result.split('\n', 1)[0]
                self.assertEqual(json.loads(marker.removeprefix('<!-- release-plan:').removesuffix(' -->')), plan)

    def test_cli_writes_notes_and_manifest_without_publishing(self):
        self.plan['commit'] = self.commit('new', 'fix: repair query')
        Path('scripts/release').mkdir(parents=True)
        Path('scripts/release/channels.json').write_text(json.dumps(self.config))
        Path('plan.json').write_text(json.dumps(self.plan))
        subprocess.run(['python3', str(ROOT / 'scripts/release/notes.py'), '--plan', 'plan.json',
                        '--output', 'notes.md', '--manifest', 'artifacts/manifest.json'],
                       env={**os.environ, 'IMAGE_DIGEST': DIGEST}, check=True, capture_output=True)
        self.assertIn('repair query', Path('notes.md').read_text())
        self.assertEqual(json.loads(Path('artifacts/manifest.json').read_text()),
                         {**self.plan, 'image_digest': DIGEST})

    def test_portal_release_requires_matching_brand_digest_and_records_pair(self):
        self.plan['commit'] = self.commit('portal', 'feat: independent supplier portal')
        self.plan['portal_image'] = self.config['kdan']['portal_image']
        with self.assertRaisesRegex(ValueError, 'portal image or digest'):
            notes.render(self.plan, DIGEST, self.config)
        wrong = dict(self.plan, portal_image=self.config['tokensavy']['portal_image'])
        with self.assertRaisesRegex(ValueError, 'portal image or digest'):
            notes.render(wrong, DIGEST, self.config, portal_digest=DIGEST)
        rendered = notes.render(self.plan, DIGEST, self.config, portal_digest=DIGEST)
        self.assertIn('kdan-vendor:0.2.17.2', rendered)
        Path('scripts/release').mkdir(parents=True)
        Path('scripts/release/channels.json').write_text(json.dumps(self.config))
        Path('plan.json').write_text(json.dumps(self.plan))
        subprocess.run(['python3', str(ROOT / 'scripts/release/notes.py'), '--plan', 'plan.json',
                        '--output', 'notes.md', '--manifest', 'manifest.json'],
                       env={**os.environ, 'IMAGE_DIGEST': DIGEST, 'PORTAL_IMAGE_DIGEST': DIGEST},
                       check=True, capture_output=True)
        manifest = json.loads(Path('manifest.json').read_text())
        self.assertEqual(manifest['portal_image_digest'], DIGEST)
        self.assertEqual(manifest['image_digest'], DIGEST)
        self.assertEqual(manifest['portal_image'], self.plan['portal_image'])

    def test_missing_baseline_does_not_fall_back_to_recent_commits(self):
        self.plan['previous_commit'] = 'f' * 40
        # A tag must resolve to the exact SHA recorded in the previous release.
        with patch.object(notes, 'git_ok', return_value=False), \
             patch.object(notes, 'git', side_effect=['', self.base]):
            with self.assertRaisesRegex(ValueError, 'no longer matches'):
                self.render()

    def test_resuming_old_draft_records_published_boundary_without_new_reservation(self):
        head = self.commit('new', 'fix: repair query')
        draft = {k: v for k, v in self.plan.items() if not k.startswith('previous_')}
        draft['commit'] = head
        previous = dict(draft, version='0.2.17.1', tag='kdan/v0.2.17.1', commit=self.base)
        releases = [dict(id=index, tag_name=p['tag'], draft=is_draft, prerelease=False,
                         body='<!-- release-plan:' + json.dumps(p) + ' -->')
                    for index, (p, is_draft) in enumerate([(previous, False), (draft, True)])]
        Path('scripts/release').mkdir(parents=True)
        Path('scripts/release/channels.json').write_text(json.dumps(self.config))
        with patch.dict(os.environ, GITHUB_REPOSITORY='holeenlu/sub2api', GITHUB_REF='refs/heads/main',
                        GITHUB_OUTPUT=str(self.root / 'outputs.txt')), \
             patch('sys.argv', ['plan.py']), \
             patch.object(release_plan, 'git', return_value=head), \
             patch.object(release_plan, 'run', return_value=json.dumps([releases])) as run:
            release_plan.main()
        saved = json.loads(Path('release-plan.json').read_text())
        self.assertEqual(saved['previous_commit'], self.base)
        self.assertEqual(saved['previous_version'], '0.2.17.1')
        self.assertIn('publish=true', Path('outputs.txt').read_text())
        run.assert_called_once_with('gh', 'api', '--paginate', '--slurp',
                                    'repos/holeenlu/sub2api/releases?per_page=100')


if __name__ == '__main__':
    unittest.main()
