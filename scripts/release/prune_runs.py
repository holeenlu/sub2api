"""Keep the newest manual-release runs without interrupting active releases."""
import argparse
from datetime import datetime
import json
import os
from pathlib import Path
import sys

if __package__:
    from .publish import api
else:
    from publish import api

WORKFLOW_PATH = '.github/workflows/automatic-release.yml'
KEEP_RUNS = 10


def prune_runs(repository, *, apply=False, current_run_id=None):
    endpoint = f'repos/{repository}/actions'
    workflow = api('GET', endpoint + '/workflows/automatic-release.yml')
    if workflow['path'] != WORKFLOW_PATH:
        raise ValueError('Refusing to clean a different workflow')
    workflow_id = workflow['id']
    pages = api('GET', f'{endpoint}/workflows/{workflow_id}/runs?per_page=100', paginate=True)
    runs = {run['id']: run for page in pages for run in page['workflow_runs']}
    # The current run can be absent from an eventually consistent list.
    if current_run_id is not None:
        current = api('GET', f'{endpoint}/runs/{current_run_id}')
        if current['id'] != current_run_id:
            raise ValueError('Current workflow run ID changed')
        runs[current_run_id] = current
    if any(run['workflow_id'] != workflow_id for run in runs.values()):
        raise ValueError('Refusing to clean runs belonging to another workflow')
    ordered = sorted(runs.values(), key=lambda r: (datetime.fromisoformat(r['created_at'].replace('Z', '+00:00')), r['id']), reverse=True)
    protected = {r['id'] for r in ordered[:KEEP_RUNS]}
    protected.update(r['id'] for r in ordered if r['status'] != 'completed')
    if current_run_id is not None:
        protected.add(current_run_id)
    candidates = [r['id'] for r in reversed(ordered) if r['id'] not in protected]
    report = dict(repository=repository, workflow_id=workflow_id, keep=KEEP_RUNS,
                  total_before=len(runs), protected_ids=sorted(protected),
                  candidate_ids=candidates, deleted_ids=[], skipped_ids=[], applied=apply)
    if not apply:
        return report
    for run_id in candidates:
        path = f'{endpoint}/runs/{run_id}'
        current = api('GET', path, missing_ok=True)
        if current is None:
            report['skipped_ids'].append(run_id)
            continue
        if current['id'] != run_id or current['workflow_id'] != workflow_id:
            raise ValueError('Workflow run identity changed before deletion')
        # A user may have rerun an old workflow after the snapshot.
        if current['status'] != 'completed' or current.get('run_attempt') != runs[run_id].get('run_attempt'):
            report['skipped_ids'].append(run_id)
            continue
        api('DELETE', path, missing_ok=True)
        report['deleted_ids'].append(run_id)
        print(f'Deleted completed workflow run {run_id}', file=sys.stderr)
    return report


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--apply', action='store_true', help='Delete eligible old runs; otherwise preview only')
    args = parser.parse_args()
    repository = os.environ['GITHUB_REPOSITORY']
    channels = json.loads(Path('scripts/release/channels.json').read_text())
    if repository not in {cfg['repository'] for cfg in channels.values()}:
        raise ValueError('Repository is not a configured release destination')
    current_run_id = int(os.environ['GITHUB_RUN_ID']) if os.environ.get('GITHUB_RUN_ID') else None
    print(json.dumps(prune_runs(repository, apply=args.apply, current_run_id=current_run_id), indent=2))


if __name__ == '__main__':
    main()
