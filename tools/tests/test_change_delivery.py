import importlib.util
import subprocess
import tempfile
import unittest
from pathlib import Path

spec = importlib.util.spec_from_file_location('change_delivery', Path(__file__).parents[1] / 'change_delivery.py')
delivery = importlib.util.module_from_spec(spec)
spec.loader.exec_module(delivery)


class DeliveryReviewTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.repo = Path(self.temp.name)
        self.git('init', '-q', '-b', 'main')
        self.git('config', 'user.email', 'test@example.invalid')
        self.git('config', 'user.name', 'Test')
        (self.repo / 'shared.txt').write_text('baseline\n')
        self.git('add', '.')
        self.git('commit', '-qm', 'baseline')

    def git(self, *args):
        return subprocess.check_output(['git', '-C', str(self.repo), *args], text=True).strip()

    def test_brand_checkout_does_not_assign_scope(self):
        (self.repo / 'shared.txt').write_text('changed\n')
        result = delivery.review(self.repo)
        self.assertEqual(result['current_branch'], 'main')
        self.assertEqual(result['scope'], 'unclassified')
        self.assertEqual(result['targets'], [])

    def test_shared_targets_start_at_main_and_propagate_brand_branches(self):
        (self.repo / 'shared.txt').write_text('changed\n')
        result = delivery.review(self.repo, 'shared')
        self.assertEqual([t['push_command'] for t in result['targets']], [
            'git push origin refs/heads/main:refs/heads/main',
            'git push origin refs/heads/TapModels:refs/heads/TapModels',
            'git push origin refs/heads/tokensavy:refs/heads/tokensavy',
        ])

    def test_main_target_is_kdan_delivery_branch(self):
        self.git('config', 'push.default', 'upstream')
        (self.repo / 'shared.txt').write_text('changed\n')
        before = self.git('status', '--porcelain')
        result = delivery.review(self.repo, 'main')
        self.assertEqual([t['push_command'] for t in result['targets']], [
            'git push origin refs/heads/main:refs/heads/main',
        ])
        self.assertEqual(self.git('status', '--porcelain'), before)
        self.assertEqual(self.git('log', '--format=%s', '-1'), 'baseline')

    def test_staged_scope_excludes_unstaged_and_handles_spaces(self):
        (self.repo / 'shared.txt').write_text('unstaged\n')
        (self.repo / 'brand name.txt').write_text('staged\n')
        self.git('add', 'brand name.txt')
        self.assertEqual(delivery.review(self.repo, staged=True)['files'], ['brand name.txt'])

    def test_mixed_has_no_push_plan(self):
        (self.repo / 'new file.txt').write_text('change\n')
        self.assertEqual(delivery.review(self.repo, 'mixed')['targets'], [])
        self.assertIn('new file.txt', delivery.review(self.repo)['files'])

    def test_commit_selection_ignores_worktree_changes(self):
        (self.repo / 'shared.txt').write_text('committed\n')
        self.git('commit', '-qam', 'change')
        (self.repo / 'another.txt').write_text('untracked\n')
        result = delivery.review(self.repo, 'tapmodels', commit='HEAD')
        self.assertEqual(result['files'], ['shared.txt'])
        self.assertEqual(len(result['targets']), 1)

    def test_tokensavy_has_only_its_same_repository_target(self):
        (self.repo / 'shared.txt').write_text('changed\n')
        result = delivery.review(self.repo, 'tokensavy')
        self.assertEqual([t['push_command'] for t in result['targets']], [
            'git push origin refs/heads/tokensavy:refs/heads/tokensavy',
        ])


if __name__ == '__main__':
    unittest.main()
