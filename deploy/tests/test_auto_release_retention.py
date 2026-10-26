import contextlib
import io
import json
import os
import unittest
from unittest.mock import patch

from scripts.release import prune_runs as retention

REPOSITORY = 'holeenlu/sub2api'
ENDPOINT = 'repos/' + REPOSITORY + '/actions'
WORKFLOW_ID = 42


def run_record(number, status='completed', **overrides):
    return dict(id=number, workflow_id=WORKFLOW_ID, created_at=f'2026-10-26T00:{number:02d}:00Z',
                status=status, run_attempt=1, head_branch=('main', 'TapModels', 'tokensavy')[number % 3], **overrides)


class ReleaseRunRetentionTests(unittest.TestCase):
    def api(self, runs, replacements=None, pages=None):
        replacements = replacements or {}
        by_id = {r['id']: r for r in runs}
        def call(method, path, **kwargs):
            if method == 'GET' and path == ENDPOINT + '/workflows/automatic-release.yml':
                return dict(id=WORKFLOW_ID, path=retention.WORKFLOW_PATH)
            if method == 'GET' and path == f'{ENDPOINT}/workflows/{WORKFLOW_ID}/runs?per_page=100':
                self.assertEqual(kwargs, {'paginate': True})
                return pages or [{'workflow_runs': runs}]
            prefix = ENDPOINT + '/runs/'
            self.assertTrue(path.startswith(prefix), path)
            run_id = int(path[len(prefix):])
            if method == 'GET':
                return replacements.get(run_id, by_id.get(run_id))
            self.assertEqual(method, 'DELETE')
            self.assertEqual(kwargs, {'missing_ok': True})
            self.assertIn(run_id, by_id)
        return call

    def test_preview_keeps_newest_ten_across_brands_and_all_active_runs(self):
        runs = [run_record(n, 'queued' if n == 1 else 'in_progress' if n == 2 else 'completed') for n in range(1, 16)]
        with patch.object(retention, 'api', side_effect=self.api(runs)) as api:
            report = retention.prune_runs(REPOSITORY, current_run_id=3)
        self.assertEqual(report['protected_ids'], [1, 2, 3, *range(6, 16)])
        self.assertEqual(report['candidate_ids'], [4, 5])
        self.assertEqual(report['deleted_ids'], [])
        self.assertFalse(any(c.args[0] == 'DELETE' for c in api.call_args_list))

    def test_all_pages_are_sorted_and_deduplicated_before_deleting(self):
        runs = [run_record(n) for n in range(1, 16)]
        pages = [{'workflow_runs': runs[:7]}, {'workflow_runs': runs[6:]}]
        with patch.object(retention, 'api', side_effect=self.api(runs, pages=pages)) as api, contextlib.redirect_stderr(io.StringIO()):
            report = retention.prune_runs(REPOSITORY, apply=True)
        self.assertEqual(report['total_before'], 15)
        self.assertEqual(report['deleted_ids'], [1, 2, 3, 4, 5])
        self.assertEqual([c.args[1] for c in api.call_args_list if c.args[0] == 'DELETE'],
                         [f'{ENDPOINT}/runs/{n}' for n in range(1, 6)])

    def test_current_run_missing_from_list_counts_toward_ten(self):
        runs = [run_record(n) for n in range(1, 11)]
        with patch.object(retention, 'api', side_effect=self.api(runs, {11: run_record(11, 'in_progress')})):
            report = retention.prune_runs(REPOSITORY, current_run_id=11)
        self.assertEqual(report['candidate_ids'], [1])
        self.assertEqual(report['protected_ids'], list(range(2, 12)))

    def test_restarted_or_deleted_runs_are_skipped_on_recheck(self):
        runs = [run_record(n) for n in range(1, 15)]
        restarted = dict(run_record(2), run_attempt=2)
        replacements = {1: run_record(1, 'in_progress'), 2: restarted, 3: None}
        with patch.object(retention, 'api', side_effect=self.api(runs, replacements)), contextlib.redirect_stderr(io.StringIO()):
            report = retention.prune_runs(REPOSITORY, apply=True)
        self.assertEqual(report['skipped_ids'], [1, 2, 3])
        self.assertEqual(report['deleted_ids'], [4])

    def test_other_workflow_is_rejected_before_deletion(self):
        runs = [dict(run_record(1), workflow_id=99), *[run_record(n) for n in range(2, 13)]]
        with patch.object(retention, 'api', side_effect=self.api(runs)) as api:
            with self.assertRaisesRegex(ValueError, 'another workflow'):
                retention.prune_runs(REPOSITORY, apply=True)
        self.assertFalse(any(c.args[0] == 'DELETE' for c in api.call_args_list))

    def test_permission_failures_propagate_without_deleting_anything(self):
        with patch.object(retention, 'api', side_effect=RuntimeError('403 forbidden')) as api:
            with self.assertRaisesRegex(RuntimeError, '403'):
                retention.prune_runs(REPOSITORY, apply=True)
        self.assertEqual(api.call_count, 1)

    def test_ten_or_fewer_runs_need_no_deletions(self):
        for count in (0, 9, 10):
            with self.subTest(count=count), patch.object(retention, 'api', side_effect=self.api([run_record(n) for n in range(1, count + 1)])) as api:
                self.assertEqual(retention.prune_runs(REPOSITORY, apply=True)['candidate_ids'], [])
                self.assertFalse(any(c.args[0] == 'DELETE' for c in api.call_args_list))

    def test_cli_defaults_to_preview_and_rejects_unconfigured_repository(self):
        with patch.dict(os.environ, {'GITHUB_REPOSITORY': REPOSITORY, 'GITHUB_RUN_ID': '12'}), \
             patch('sys.argv', ['prune_runs.py']), patch.object(retention, 'prune_runs', return_value={}) as prune, \
             contextlib.redirect_stdout(io.StringIO()):
            retention.main()
        prune.assert_called_once_with(REPOSITORY, apply=False, current_run_id=12)
        with patch.dict(os.environ, {'GITHUB_REPOSITORY': 'unconfigured/repository'}), \
             patch('sys.argv', ['prune_runs.py', '--apply']), patch.object(retention, 'prune_runs') as prune:
            with self.assertRaisesRegex(ValueError, 'configured release destination'):
                retention.main()
        prune.assert_not_called()
