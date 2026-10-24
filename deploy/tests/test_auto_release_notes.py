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
        self.snapshot = "# Complete customization\n\n" + "\n\n".join(
            "## " + title + "\n\n" + ("旧入口已经移除" if title == "删除与退役" else "累计账号能力")
            for title in notes.FULL_NOTES_SECTIONS)
        self.snapshot += "\n\n[guide](guide.md#usage) [upgrade](../deploy/AUTOMATIC_RELEASE.md) [external](https://example.test/guide) [local](#新增功能)\n"
        Path(notes.FULL_NOTES_PATH).parent.mkdir(parents=True)
        Path(notes.FULL_NOTES_PATH).write_text(self.snapshot)
        self.git('add', notes.FULL_NOTES_PATH)
        self.base = self.commit('initial', 'feat: old feature')
        self.plan = dict(channel='kdan', version='0.2.17.2', base='0.2.17',
                         upstream_commit=self.base, previous_commit=self.base,
                         previous_version='0.2.17.1', previous_tag='kdan/v0.2.17.1',
                         image='ghcr.io/holeenlu/kdan', tag='kdan/v0.2.17.2')

    def git(self, *args):
        return subprocess.check_output(['git', *args], text=True, stderr=subprocess.PIPE).strip()

    def commit(self, content, message, path='source.txt'):
        Path(path).parent.mkdir(parents=True, exist_ok=True)
        Path(path).write_text(content)
        self.git('add', path)
        self.git('commit', '-q', '-m', message)
        return self.git('rev-parse', 'HEAD')

    def commit_snapshot(self, description, source=None, message='docs: complete notes'):
        if source is not None:
            Path('source.txt').write_text(source)
            self.git('add', 'source.txt')
        return self.commit(self.snapshot + '\n' + description, message, path=notes.FULL_NOTES_PATH)

    def render(self, **overrides):
        plan = dict(self.plan, **overrides)
        plan.setdefault('commit', self.git('rev-parse', 'HEAD'))
        return notes.render(plan, DIGEST, self.config)

    def test_every_version_contains_full_snapshot_even_for_same_tree(self):
        result = self.render()
        self.assertIn('累计账号能力', result)
        self.assertIn('## 删除与退役', result)
        self.assertIn('旧入口已经移除', result)
        self.assertIn('两版源码内容一致', result)
        self.assertNotIn('old feature', result)

    def test_rewritten_history_uses_current_complete_state_not_commit_titles(self):
        previous = self.commit_snapshot('旧能力已提供', source='old')
        self.plan['previous_commit'] = previous
        self.git('reset', '-q', '--hard', self.base)
        head = self.commit_snapshot('新能力已提供', source='new', message='feat: aggregate history')
        result = self.render()
        self.assertIn('累计账号能力', result)
        self.assertIn('新能力已提供', result)
        self.assertNotIn('旧能力已提供', result)
        self.assertNotIn('aggregate history', result)
        self.assertIn('两版文件变更统计', result)
        self.assertIn('/compare/' + previous + '..' + head, result)

    def test_snapshot_and_relative_links_are_pinned_to_release_commit(self):
        head = self.commit_snapshot('当前有效能力')
        Path(notes.FULL_NOTES_PATH).write_text('uncommitted text must not publish')
        result = self.render(commit=head)
        self.assertIn('当前有效能力', result)
        self.assertNotIn('uncommitted text', result)
        self.assertIn('/blob/' + head + '/docs/guide.md#usage', result)
        self.assertIn('/blob/' + head + '/deploy/AUTOMATIC_RELEASE.md', result)
        self.assertIn('[external](https://example.test/guide)', result)
        self.assertIn('[local](#新增功能)', result)

    def test_missing_or_incomplete_notes_fail_closed(self):
        self.git('rm', notes.FULL_NOTES_PATH)
        self.git('commit', '-q', '-m', 'docs: remove notes')
        with self.assertRaisesRegex(ValueError, 'complete CUSTOMIZATION'):
            self.render()
        for content in ('# Empty', self.snapshot.replace('旧入口已经移除', '')):
            with self.subTest(content=content[:30]):
                self.commit(content, 'docs: incomplete', path=notes.FULL_NOTES_PATH)
                with self.assertRaisesRegex(ValueError, 'nonempty section'):
                    self.render()

    def test_runtime_dependency_and_configuration_changes_require_updated_snapshot(self):
        for path in ['backend/main.go', 'frontend/pnpm-lock.yaml', 'deploy/runtime.py', '.github/workflows/release.yml']:
            with self.subTest(path=path):
                self.git('reset', '-q', '--hard', self.base)
                Path(path).parent.mkdir(parents=True, exist_ok=True)
                self.commit('new', 'fix: changed source', path=path)
                with self.assertRaisesRegex(ValueError, 'without updating complete release notes'):
                    self.render()
        self.commit_snapshot('已更新对应功能及升级说明')
        self.assertIn('已更新对应功能及升级说明', self.render())

    def test_version_test_and_document_only_changes_can_reuse_effective_snapshot(self):
        for path in ['backend/cmd/server/VERSION', 'backend/internal/service/gateway_test.go',
                     'frontend/src/__tests__/ui.spec.ts', 'deploy/tests/test_notes.py', 'docs/guide.md',
                     'deploy/AUTOMATIC_RELEASE.md']:
            Path(path).parent.mkdir(parents=True, exist_ok=True)
            self.commit('updated', 'chore: metadata', path=path)
        self.assertIn('累计账号能力', self.render())

    def test_version_editorial_only_supplements_full_snapshot_for_exact_range(self):
        directory = Path('deploy/release-notes')
        directory.mkdir(parents=True)
        self.commit('<!-- previous-version: 0.2.17.1 -->\n\n本版本补充说明。',
                    'docs: version note', path=str(directory / '0.2.17.2.md'))
        result = self.render()
        self.assertIn('累计账号能力', result)
        self.assertIn('本版本补充说明。', result)
        self.assertIn('<summary>本版本相对上一版的补充说明</summary>', result)
        skipped = self.render(previous_version='0.2.16.1')
        self.assertIn('累计账号能力', skipped)
        self.assertNotIn('本版本补充说明。', skipped)
        self.commit('Missing baseline', 'docs: invalid version note', path=str(directory / '0.2.17.2.md'))
        with self.assertRaisesRegex(ValueError, 'previous-version'):
            self.render()

    def test_bootstrap_uses_integrated_upstream_and_release_metadata_stays_channel_scoped(self):
        head = self.commit_snapshot('本版新增能力', source='new')
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
        self.plan['commit'] = self.commit_snapshot('修复查询并保留全部能力', source='new')
        Path('scripts/release').mkdir(parents=True)
        Path('scripts/release/channels.json').write_text(json.dumps(self.config))
        Path('plan.json').write_text(json.dumps(self.plan))
        subprocess.run(['python3', str(ROOT / 'scripts/release/notes.py'), '--plan', 'plan.json',
                        '--output', 'notes.md', '--manifest', 'artifacts/manifest.json'],
                       env={**os.environ, 'IMAGE_DIGEST': DIGEST}, check=True, capture_output=True)
        self.assertIn('累计账号能力', Path('notes.md').read_text())
        self.assertIn('修复查询并保留全部能力', Path('notes.md').read_text())
        self.assertEqual(json.loads(Path('artifacts/manifest.json').read_text()),
                         {**self.plan, 'image_digest': DIGEST})

    def test_portal_release_requires_matching_brand_digest_and_records_pair(self):
        self.plan['commit'] = self.commit_snapshot('独立供应商门户', source='portal')
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
        for channel, config in self.config.items():
            with self.subTest(channel=channel):
                plan = dict(self.plan, channel=channel, image=config['image'], portal_image=config['portal_image'])
                Path('plan.json').write_text(json.dumps(plan))
                subprocess.run(['python3', str(ROOT / 'scripts/release/notes.py'), '--plan', 'plan.json',
                                '--output', 'notes.md', '--manifest', 'manifest.json'],
                               env={**os.environ, 'IMAGE_DIGEST': DIGEST, 'PORTAL_IMAGE_DIGEST': DIGEST},
                               check=True, capture_output=True)
                manifest = json.loads(Path('manifest.json').read_text())
                self.assertEqual(manifest['portal_image_digest'], DIGEST)
                self.assertEqual(manifest['image_digest'], DIGEST)
                self.assertEqual(manifest['portal_image'], plan['portal_image'])
                override = json.loads(Path('compose.vendor-release.json').read_text())
                self.assertEqual(override, {'services': {'vendor-portal': {'image': config['portal_image']+'@'+DIGEST}}})
                rendered = Path('notes.md').read_text()
                self.assertIn('后端和门户必须一并升级', rendered)
                self.assertIn('compose.vendor-release.json', rendered)

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
             patch.object(release_plan, 'run', side_effect=[head, json.dumps([releases])]) as run:
            release_plan.main()
        saved = json.loads(Path('release-plan.json').read_text())
        self.assertEqual(saved['previous_commit'], self.base)
        self.assertEqual(saved['previous_version'], '0.2.17.1')
        self.assertIn('publish=true', Path('outputs.txt').read_text())
        self.assertEqual(run.call_count, 2)
        run.assert_called_with('gh', 'api', '--paginate', '--slurp',
                               'repos/holeenlu/sub2api/releases?per_page=100')


if __name__ == '__main__':
    unittest.main()
