import importlib.util
import contextlib
import fcntl
import io
import json
import os
import subprocess
import sys
import tempfile
import time
import unittest
from unittest.mock import patch
from pathlib import Path

spec = importlib.util.spec_from_file_location('change_delivery', Path(__file__).parents[1] / 'change_delivery.py')
delivery = importlib.util.module_from_spec(spec)
spec.loader.exec_module(delivery)


class GitFixture(unittest.TestCase):
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


class DeliveryReviewTest(GitFixture):
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


class ValidationTest(GitFixture):
    def setUp(self):
        super().setUp()
        self.out = tempfile.TemporaryDirectory(prefix='delivery-evidence-')
        self.addCleanup(self.out.cleanup)
        self.directory = Path(self.out.name)
        self.plan = self.directory / 'plan.json'
        self.receipt = self.directory / 'receipt.json'
        self.calls = self.directory / 'calls'
        self.probe = self.directory / 'tool-version'
        self.probe.write_text('v1')
        self.checks = [self.check('quick', 'preflight'), self.check('full', 'test')]
        self.save_plan()

    def check(self, name, phase, code=None):
        code = code or f"from pathlib import Path; p=Path({str(self.calls)!r}); p.open('a').write({name!r}+chr(10))"
        return {'id': name, 'phase': phase, 'cwd': '.', 'inputs': ['shared.txt'],
                'argv': [sys.executable, '-c', code],
                'probes': [[sys.executable, '-c',
                            f"from pathlib import Path; print(Path({str(self.probe)!r}).read_text())"]]}

    def save_plan(self):
        self.plan.write_text(json.dumps({'version': 1, 'checks': self.checks}))

    def run_validation(self, status=False):
        with contextlib.redirect_stdout(io.StringIO()):
            return delivery.validate(self.repo, self.plan, self.receipt, status)

    def test_tree_identity_reuses_after_rewrite_and_unrelated_brand_change(self):
        result = self.run_validation()
        self.assertTrue(result['temporary_removed'])
        before = self.git('rev-parse', 'HEAD')
        self.git('commit', '--amend', '-qm', 'rewritten')
        self.assertNotEqual(before, self.git('rev-parse', 'HEAD'))
        self.assertTrue(self.run_validation(True)['ready'])
        (self.repo / 'brand.txt').write_text('TapModels')
        self.git('add', 'brand.txt')
        self.assertFalse(self.run_validation(True)['ready'])
        self.assertTrue(all(c['reused'] for c in self.run_validation()['checks']))
        self.assertEqual(self.calls.read_text().splitlines(), ['quick', 'full'])

    def test_changed_input_probe_command_environment_invalidate(self):
        self.run_validation()
        (self.repo / 'shared.txt').write_text('changed')
        self.git('add', 'shared.txt')
        self.assertFalse(self.run_validation(True)['ready'])
        self.run_validation()
        self.probe.write_text('v2')
        self.assertFalse(self.run_validation(True)['ready'])
        self.run_validation()
        self.checks[0]['argv'][-1] += '; pass'
        self.save_plan()
        result = self.run_validation()
        self.assertEqual([c['reused'] for c in result['checks']], [False, True])
        with patch.dict(os.environ, {'GOFLAGS': '-race'}):
            self.assertFalse(self.run_validation(True)['ready'])

    def test_failed_preflight_stops_full_and_retry_reuses_passed_check(self):
        ready = self.directory / 'ready'
        self.checks.insert(1, self.check('guard', 'preflight',
            f"from pathlib import Path; raise SystemExit(0 if Path({str(ready)!r}).exists() else 2)"))
        self.save_plan()
        with self.assertRaisesRegex(ValueError, 'guard failed'):
            self.run_validation()
        self.assertEqual(self.calls.read_text().splitlines(), ['quick'])
        self.assertFalse(self.run_validation(True)['ready'])
        ready.touch()
        result = self.run_validation()
        self.assertTrue(result['checks'][0]['reused'])
        self.assertEqual(self.calls.read_text().splitlines(), ['quick', 'full'])

    def test_unstaged_or_mid_run_changes_never_certify_result(self):
        (self.repo / 'shared.txt').write_text('unstaged')
        with self.assertRaisesRegex(ValueError, 'Stage'):
            self.run_validation()
        self.git('add', 'shared.txt')
        self.checks[0]['argv'] = [sys.executable, '-c',
            "from pathlib import Path; Path('shared.txt').write_text('changed during test')"]
        self.save_plan()
        with self.assertRaisesRegex(ValueError, 'Stage|Candidate'):
            self.run_validation()
        self.assertFalse(self.receipt.exists())
        self.assertFalse(self.calls.exists())

    def test_common_git_lock_blocks_parallel_worktree_validation(self):
        common = Path(self.git('rev-parse', '--absolute-git-dir'))
        with (common / 'sync-upstream.lock').open('a') as lock:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            with self.assertRaisesRegex(ValueError, 'Another validation'):
                self.run_validation()
        self.assertFalse(self.calls.exists())

    def test_changed_source_cancels_running_check_and_removes_its_cache(self):
        cache = self.directory / 'cache-path'
        self.checks[0]['argv'] = [sys.executable, '-c',
            f"from pathlib import Path; import os,time; Path({str(cache)!r}).write_text(os.environ['GOCACHE']); "
            "Path('shared.txt').write_text('changed'); time.sleep(30)"]
        self.save_plan()
        start = time.monotonic()
        with self.assertRaisesRegex(ValueError, 'Stage|Candidate'):
            self.run_validation()
        self.assertLess(time.monotonic() - start, 10)
        self.assertFalse(Path(cache.read_text()).exists())
        self.assertFalse(self.receipt.exists())

    def test_cli_status_reports_missing_evidence_then_reuses_without_tests(self):
        args = [sys.executable, '-B', str(Path(delivery.__file__)), 'validation-status',
                '--repo', str(self.repo), '--plan', str(self.plan), '--receipt', str(self.receipt)]
        self.assertEqual(subprocess.run(args, capture_output=True).returncode, 1)
        self.run_validation()
        result = subprocess.run(args, capture_output=True, text=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertTrue(json.loads(result.stdout)['ready'])
        self.assertEqual(self.calls.read_text().splitlines(), ['quick', 'full'])

    def test_ignored_dependency_manifest_invalidates(self):
        (self.repo / '.gitignore').write_text('node_modules/')
        self.git('add', '.gitignore')
        directory = self.repo / 'node_modules'
        directory.mkdir()
        manifest = directory / '.modules.yaml'
        manifest.write_text('dependencies: v1')
        self.checks[0]['files'] = ['node_modules/.modules.yaml']
        self.save_plan()
        self.run_validation()
        manifest.write_text('dependencies: v2')
        self.assertEqual([c['reused'] for c in self.run_validation(True)['checks']], [False, True])

    def test_invalid_phase_order_and_empty_scopes_rejected(self):
        self.checks.reverse()
        self.save_plan()
        with self.assertRaisesRegex(ValueError, 'Order checks'):
            self.run_validation()
        self.checks.reverse()
        self.checks[0]['inputs'] = []
        self.save_plan()
        with self.assertRaisesRegex(ValueError, 'Declare input scope'):
            self.run_validation()


if __name__ == '__main__':
    unittest.main()
