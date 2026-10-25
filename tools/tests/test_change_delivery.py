import importlib.util
import contextlib
import fcntl
import io
import json
import os
import signal
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
    def test_maintenance_checkout_has_no_publication_target(self):
        (self.repo / 'shared.txt').write_text('changed\n')
        result = delivery.review(self.repo)
        self.assertEqual(result['current_branch'], 'main')
        self.assertEqual(result['scope'], 'independent_rebuild')
        self.assertEqual(result['targets'], [])

    def test_only_rebuild_ref_is_reported_without_mutation(self):
        self.git('checkout', '-qb', 'codex/sub2api-rebuild')
        (self.repo / 'shared.txt').write_text('changed\n')
        before = self.git('status', '--porcelain')
        result = delivery.review(self.repo)
        self.assertEqual([t['push_command'] for t in result['targets']], [
            'git push origin HEAD:refs/heads/codex/sub2api-rebuild',
        ])
        self.assertEqual(self.git('status', '--porcelain'), before)
        self.assertEqual(self.git('log', '--format=%s', '-1'), 'baseline')

    def test_staged_selection_excludes_unstaged_and_handles_spaces(self):
        (self.repo / 'shared.txt').write_text('unstaged\n')
        (self.repo / 'file name.txt').write_text('staged\n')
        self.git('add', 'file name.txt')
        self.assertEqual(delivery.review(self.repo, staged=True)['files'], ['file name.txt'])

    def test_untracked_files_are_included(self):
        (self.repo / 'new file.txt').write_text('change\n')
        self.assertIn('new file.txt', delivery.review(self.repo)['files'])

    def test_commit_selection_ignores_worktree_changes(self):
        self.git('checkout', '-qb', 'codex/sub2api-rebuild')
        (self.repo / 'shared.txt').write_text('committed\n')
        self.git('commit', '-qam', 'change')
        (self.repo / 'another.txt').write_text('untracked\n')
        result = delivery.review(self.repo, commit='HEAD')
        self.assertEqual(result['files'], ['shared.txt'])
        self.assertEqual(len(result['targets']), 1)

    def test_detached_checkout_has_no_publication_target(self):
        self.git('checkout', '-q', '--detach')
        (self.repo / 'shared.txt').write_text('change\n')
        self.assertEqual(delivery.review(self.repo)['targets'], [])


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
        self.run_validation()
        receipt = self.receipt.read_bytes()
        common = Path(self.git('rev-parse', '--absolute-git-dir'))
        with (common / 'sync-upstream.lock').open('a') as lock:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            with patch.object(delivery.tempfile, 'TemporaryDirectory', side_effect=AssertionError('status created cache')):
                self.assertTrue(self.run_validation(True)['ready'])
            with self.assertRaisesRegex(ValueError, 'Another validation'):
                self.run_validation()
        self.assertEqual(self.receipt.read_bytes(), receipt)
        self.assertEqual(self.calls.read_text().splitlines(), ['quick', 'full'])

    def test_bounded_lock_wait_and_release_across_worktrees(self):
        other = self.directory / 'worktree'
        self.git('worktree', 'add', '--detach', str(other))
        marker = self.directory / 'lock-marker'
        command = [sys.executable, '-B', str(Path(delivery.__file__)), 'delivery-run',
                   '--repo', str(other), '--wait-seconds', '0.1', '--command',
                   sys.executable, '-c', f'from pathlib import Path; Path({str(marker)!r}).touch()']
        with delivery.repo_lock(self.repo, 'change-delivery.lock'):
            blocked = subprocess.run(command, capture_output=True, text=True, timeout=5)
            self.assertNotEqual(blocked.returncode, 0)
            self.assertIn('wait expired', blocked.stderr)
            self.assertFalse(marker.exists())
            # The delivery lock must not deadlock its nested validator.
            self.assertTrue(self.run_validation()['ready'])
            command[command.index('0.1')] = '5'
            waiting = subprocess.Popen(command, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
            self.addCleanup(lambda: waiting.poll() is None and waiting.kill())
            self.assertIn('WAIT', waiting.stdout.readline())
        output, error = waiting.communicate(timeout=5)
        self.assertEqual(waiting.returncode, 0, error)
        self.assertTrue(marker.exists())

    def test_delivery_failure_releases_lock_and_does_not_retry(self):
        with self.assertRaisesRegex(ValueError, 'failed'):
            delivery.delivery_run(self.repo, [sys.executable, '-c', 'raise SystemExit(3)'])
        with delivery.repo_lock(self.repo, 'change-delivery.lock'):
            pass
        for value in (-1, float('nan'), float('inf')):
            with self.assertRaisesRegex(ValueError, 'finite nonnegative'):
                with delivery.repo_lock(self.repo, 'change-delivery.lock', value):
                    pass

    def test_explicit_shared_cache_survives_but_temporary_outputs_do_not(self):
        cache = self.directory / 'shared-go-cache'
        observed = self.directory / 'build-paths.json'
        self.checks = [self.check('cache', 'test',
            f"import os,json; from pathlib import Path; Path({str(observed)!r}).write_text(json.dumps([os.environ['GOCACHE'],os.environ['GOTMPDIR']])); "
            "(Path(os.environ['GOCACHE'])/'artifact').write_text('cached')")]
        self.save_plan()
        with contextlib.redirect_stdout(io.StringIO()):
            result = delivery.validate(self.repo, self.plan, self.receipt, go_cache=cache)
        self.assertTrue(result['ready'])
        self.assertTrue(result['cleanup_complete'])
        actual_cache, temp = json.loads(observed.read_text())
        self.assertEqual(Path(actual_cache), cache.resolve())
        self.assertTrue((cache / 'artifact').exists())
        self.assertFalse(Path(temp).exists())
        self.assertTrue(self.run_validation(True)['ready'])
        with patch.object(delivery, 'GO_CACHE_LIMIT_KIB', 0):
            self.assertFalse(self.run_validation(True)['cleanup_complete'])
            with self.assertRaisesRegex(ValueError, 'exceeds'):
                delivery.validate(self.repo, self.plan, self.receipt, go_cache=cache)
        self.assertTrue((cache / 'artifact').exists(), 'must not purge shared cache')
        with self.assertRaisesRegex(ValueError, 'external'):
            delivery.validate(self.repo, self.plan, self.receipt, go_cache=self.repo)

    def test_termination_stops_delivery_child_before_releasing_lock(self):
        started, late = self.directory / 'started', self.directory / 'late'
        code = f"import time; from pathlib import Path; Path({str(started)!r}).touch(); time.sleep(2); Path({str(late)!r}).touch()"
        proc = subprocess.Popen([sys.executable, '-B', str(Path(delivery.__file__)), 'delivery-run',
                                 '--repo', str(self.repo), '--command', sys.executable, '-c', code],
                                stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        self.addCleanup(lambda: proc.poll() is None and proc.kill())
        deadline = time.monotonic() + 5
        while not started.exists() and time.monotonic() < deadline:
            time.sleep(0.02)
        self.assertTrue(started.exists())
        proc.send_signal(signal.SIGTERM)
        proc.communicate(timeout=5)
        self.assertNotEqual(proc.returncode, 0)
        with delivery.repo_lock(self.repo, 'change-delivery.lock'):
            time.sleep(2.1)
        self.assertFalse(late.exists())

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
