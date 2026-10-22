#!/usr/bin/env python3
"""Delivery review and resumable validation; never commits, pushes or deploys."""

import argparse
import fcntl
import hashlib
import json
import os
import shutil
import signal
import subprocess
import tempfile
import time
from pathlib import Path


TARGETS = {
    'shared': [('main', 'origin', 'main'), ('TapModels', 'origin', 'TapModels'),
               ('tokensavy', 'origin', 'tokensavy')],
    'main': [('main', 'origin', 'main')],
    'tapmodels': [('TapModels', 'origin', 'TapModels')],
    'tokensavy': [('tokensavy', 'origin', 'tokensavy')],
}
RECEIPT_VERSION = 1
BUILD_ENV = ('PATH', 'GOFLAGS', 'GOTOOLCHAIN', 'GOWORK', 'GOOS', 'GOARCH',
             'CGO_ENABLED', 'CC', 'CXX', 'CFLAGS', 'CGO_CFLAGS', 'CGO_LDFLAGS',
             'NODE_OPTIONS', 'NODE_ENV', 'TZ', 'LANG', 'LC_ALL', 'PYTHONPATH')


def digest(value):
    return hashlib.sha256(value).hexdigest()


def save_receipt(path, data):
    path.parent.mkdir(parents=True, exist_ok=True)
    temporary = path.with_name(path.name + '.tmp')
    temporary.write_text(json.dumps(data, ensure_ascii=False, indent=2) + chr(10))
    temporary.chmod(0o600)
    temporary.replace(path)


def candidate_tree(repo):
    if git(repo, 'diff', '--name-only') or git(repo, 'ls-files', '--others', '--exclude-standard'):
        raise ValueError('Stage the reviewed candidate first; unstaged/untracked inputs cannot be certified.')
    return git(repo, 'write-tree')


def load_plan(path):
    plan = json.loads(path.read_text())
    checks = plan.get('checks', [])
    if plan.get('version') != 1 or not checks:
        raise ValueError('Validation plan requires version 1 and nonempty checks.')
    ids, last_phase = set(), -1
    for check in checks:
        phase = {'preflight': 0, 'test': 1, 'build': 2}.get(check.get('phase'))
        if phase is None or phase < last_phase:
            raise ValueError('Order checks by preflight, test, build; stop at the first failure.')
        last_phase = phase
        if not check.get('id') or check['id'] in ids:
            raise ValueError('Every check needs a unique id.')
        ids.add(check['id'])
        for argv in [check.get('argv')] + check.get('probes', []):
            if not isinstance(argv, list) or not argv or not all(isinstance(a, str) and a for a in argv):
                raise ValueError('Commands and probes must be nonempty argv arrays.')
        for name in [check.get('cwd', '.')] + check.get('inputs', ['.']) + check.get('files', []):
            if not isinstance(name, str) or not name or Path(name).is_absolute() or '..' in Path(name).parts:
                raise ValueError('Input paths and cwd must be relative to the repository.')
        if not check.get('inputs', ['.']) or not check.get('probes'):
            raise ValueError('Declare input scope and tool/dependency probes for every check.')
    return plan


def executable_identity(command, cwd, env):
    executable = (cwd / command).resolve() if '/' in command else shutil.which(command, path=env.get('PATH'))
    if not executable or not Path(executable).is_file():
        raise ValueError(f'Executable unavailable: {command}')
    path = Path(executable).resolve()
    return {'path': str(path), 'sha256': digest(path.read_bytes())}


def check_fingerprint(repo, tree, check, env):
    cwd = (repo / check.get('cwd', '.')).resolve()
    cwd.relative_to(repo)
    inputs = check.get('inputs', ['.'])
    scope = git(repo, 'ls-tree', '-r', '-z', '--full-tree', tree, '--',
                *[':(literal)' + p if p != '.' else '.' for p in inputs])
    # Extra ignored dependency manifests (e.g. node_modules/.modules.yaml)
    # must be declared; tracked manifests belong in the normal input scope.
    files = {}
    for name in check.get('files', []):
        path = (repo / name).resolve()
        path.relative_to(repo)
        files[name] = digest(path.read_bytes())
    probes = []
    for argv in check['probes']:
        result = subprocess.run(argv, cwd=cwd, env=env, capture_output=True, timeout=30, check=True)
        probes.append({'argv': argv, 'executable': executable_identity(argv[0], cwd, env),
                       'output': digest(result.stdout + result.stderr)})
    settings = {k: env.get(k) for k in sorted(set(BUILD_ENV) | set(check.get('env_keys', []))
                                             | {k for k in env if k.startswith(('VITE_', 'npm_config_'))})}
    value = {'schema': RECEIPT_VERSION, 'runner': digest(Path(__file__).read_bytes()),
             'check': check, 'scope': scope, 'files': files, 'probes': probes,
             'executable': executable_identity(check['argv'][0], cwd, env),
             'environment': settings}
    # Never persist environment values or probe output, which may contain secrets.
    return digest(json.dumps(value, sort_keys=True).encode())


def stop_process(process):
    if process.poll() is not None:
        return
    try:
        os.killpg(process.pid, signal.SIGTERM)
    except ProcessLookupError:
        process.wait()
        return
    try:
        process.wait(timeout=5)
    except subprocess.TimeoutExpired:
        os.killpg(process.pid, signal.SIGKILL)
        process.wait()


def run_check(repo, tree, check, env, log):
    start = time.monotonic()
    with log.open('wb') as output:
        process = subprocess.Popen(check['argv'], cwd=repo / check.get('cwd', '.'), env=env,
                                   stdout=output, stderr=subprocess.STDOUT, start_new_session=True)
        try:
            while process.poll() is None:
                try:
                    process.wait(timeout=1)
                except subprocess.TimeoutExpired:
                    if candidate_tree(repo) != tree:
                        raise ValueError('Candidate changed; stopped obsolete validation.')
                    if time.monotonic() - start > check.get('timeout_seconds', 900):
                        raise ValueError('Validation timed out.')
            if candidate_tree(repo) != tree:
                raise ValueError('Candidate changed during validation.')
            if process.returncode:
                # Full logs are task-owned, temporary, and removed on exit.
                with log.open('rb') as stream:
                    stream.seek(max(0, log.stat().st_size - 4096))
                    tail = stream.read().decode(errors='replace')
                raise ValueError(f'Check {check["id"]} failed ({process.returncode}): {tail}')
        finally:
            stop_process(process)
    return round(time.monotonic() - start, 3)


def validate(repo, plan_path, receipt_path, check_only=False):
    """Run reviewed checks or inspect reuse eligibility; no Git mutation/publication."""
    repo = repo.resolve()
    plan = load_plan(plan_path)
    plan_hash = digest(plan_path.read_bytes())
    tree = candidate_tree(repo)
    receipt_path = receipt_path.resolve()
    if receipt_path == plan_path.resolve():
        raise ValueError('The receipt must not overwrite its plan.')
    if receipt_path.is_relative_to(repo):
        relative = str(receipt_path.relative_to(repo))
        ignored = subprocess.run(['git', '-C', str(repo), 'check-ignore', '-q', '--', relative])
        if ignored.returncode != 0 or git(repo, 'ls-files', '--', relative):
            raise ValueError('Store receipts outside source files, in an ignored evidence directory.')
    if receipt_path.exists():
        receipt = json.loads(receipt_path.read_text())
        if receipt.get('version') != RECEIPT_VERSION:
            raise ValueError('Unsupported receipt schema; preserve it and start a new receipt.')
    else:
        receipt = {'version': RECEIPT_VERSION, 'results': {}}
    common = Path(git(repo, 'rev-parse', '--git-common-dir'))
    if not common.is_absolute():
        common = repo / common
    # One validator per repository, including its worktrees. A second invocation
    # exits immediately instead of competing with a heavy compiler.
    with (common / 'sync-upstream.lock').open('a') as lock:
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError as exc:
            raise ValueError('Another validation is active; reuse its checkpoint, do not start duplicate checks.') from exc
        env = dict(os.environ)
        report = []
        with tempfile.TemporaryDirectory(prefix='sub2api-validation-') as temporary:
            temporary = Path(temporary)
            run_env = {**env, 'GOCACHE': str(temporary / 'go-cache'),
                       'GOTMPDIR': str(temporary / 'go-tmp'), 'PYTHONDONTWRITEBYTECODE': '1'}
            for name in ('go-cache', 'go-tmp'):
                (temporary / name).mkdir()
            for check in plan['checks']:
                if digest(plan_path.read_bytes()) != plan_hash:
                    raise ValueError('Validation plan changed during execution.')
                key = check_fingerprint(repo, tree, check, env)
                cached = receipt['results'].get(key)
                reusable = bool(cached and cached.get('exit') == 0)
                report.append({'id': check['id'], 'reused': reusable})
                if check_only or reusable:
                    continue
                print(f'VALIDATE {check["id"]}', flush=True)
                seconds = run_check(repo, tree, check, run_env, temporary / 'output.log')
                if check_fingerprint(repo, tree, check, env) != key:
                    raise ValueError('Toolchain, dependency or environment changed during validation.')
                receipt['results'][key] = {'id': check['id'], 'exit': 0, 'seconds': seconds,
                                           'tree': tree, 'argv': check['argv'], 'cwd': check.get('cwd', '.')}
                save_receipt(receipt_path, receipt)
            if candidate_tree(repo) != tree:
                raise ValueError('Candidate changed; validation cannot be frozen.')
            if digest(plan_path.read_bytes()) != plan_hash:
                raise ValueError('Validation plan changed during execution.')
            if not check_only:
                receipt.update(candidate_tree=tree, plan_hash=plan_hash)
                save_receipt(receipt_path, receipt)
        frozen = (receipt.get('candidate_tree') == tree
                  and receipt.get('plan_hash') == plan_hash)
        return {'tree': tree, 'same_candidate': receipt.get('candidate_tree') == tree,
                'ready': frozen and all(r['reused'] for r in report) if check_only else True, 'checks': report,
                'temporary_removed': not temporary.exists(),
                'limitations': 'Validation evidence only; review, tree equivalence, remote leases and local closeout remain required.'}


def git(repo, *args):
    return subprocess.check_output(['git', '-C', str(repo), *args], text=True).rstrip('\n')


def changed_paths(repo, staged=False, commit=None):
    if commit:
        sha = git(repo, 'rev-parse', '--verify', '--end-of-options', commit + '^{commit}')
        parents = git(repo, 'rev-list', '--parents', '-n', '1', sha).split()[1:]
        if len(parents) > 1:
            raise ValueError('Merge commit: inspect every parent and combined diff explicitly; this tool does not infer its scope.')
        output = git(repo, 'diff-tree', '--root', '--no-commit-id', '-r', '--no-renames', '--name-only', '-z', sha)
    elif staged:
        output = git(repo, 'diff', '--cached', '--no-renames', '--name-only', '-z')
    else:
        output = git(repo, 'diff', 'HEAD', '--no-renames', '--name-only', '-z')
        output += '\0' + git(repo, 'ls-files', '--others', '--exclude-standard', '-z')
    return sorted(set(filter(None, output.split('\0'))))


def review(repo, scope=None, staged=False, commit=None):
    files = changed_paths(repo, staged, commit)
    clues = []
    for name in files:
        lower = name.lower()
        tags = []
        if 'kdan' in lower:
            tags.append('KDAN path: inspect whether change is brand-specific')
        if 'tapmodels' in lower:
            tags.append('TapModels path: inspect whether change is brand-specific')
        if 'tokensavy' in lower:
            tags.append('Tokensavy path: inspect whether change is brand-specific')
        if ('brand' in lower or lower.startswith(('docs/legal/', 'assets/', 'frontend/public/'))
                or name.endswith('HomeView.vue') or '/i18n/' in name):
            tags.append('Brand/content boundary: inspect actual diff, not the whole file')
        if lower.startswith(('deploy/', '.github/workflows/')):
            tags.append('Deployment/CI boundary: check external side effects before pushing')
        if tags:
            clues.append({'path': name, 'review': tags})
    targets = TARGETS.get(scope, []) if files else []
    return {
        'current_branch': git(repo, 'branch', '--show-current') or '(detached)',
        'scope': scope or 'unclassified',
        'scope_status': ('No changes selected' if not files else
                         'Split common and brand changes before committing' if scope == 'mixed' else
                         'Declared scope; verify semantic ownership and authorization before executing' if scope else
                         'Choose shared/main/tapmodels/tokensavy/mixed after reviewing the actual diff'),
        'files': files,
        'review_clues': clues,
        'targets': [{'source': branch, 'remote': remote, 'destination': f'refs/heads/{dest}',
                     'push_command': f'git push {remote} refs/heads/{branch}:refs/heads/{dest}'}
                    for branch, remote, dest in targets],
        'limitations': 'Local read-only report; no fetch, content review, merge, commit, push, or live remote verification performed.',
    }


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action', nargs='?', default='review',
                        choices=['review', 'validate', 'validation-status'])
    parser.add_argument('--repo', type=Path, default=Path.cwd())
    parser.add_argument('--scope', choices=[*TARGETS, 'mixed'])
    parser.add_argument('--plan', type=Path)
    parser.add_argument('--receipt', type=Path)
    selection = parser.add_mutually_exclusive_group()
    selection.add_argument('--staged', action='store_true')
    selection.add_argument('--commit')
    args = parser.parse_args()
    try:
        if args.action == 'review':
            if args.plan or args.receipt:
                parser.error('--plan/--receipt require validate or validation-status')
            result = review(args.repo, args.scope, args.staged, args.commit)
        else:
            if not args.plan or not args.receipt or args.scope or args.staged or args.commit:
                parser.error('Validation requires --plan and --receipt; do not combine review options.')
            result = validate(args.repo, args.plan, args.receipt, args.action == 'validation-status')
        print(json.dumps(result, ensure_ascii=False, indent=2))
        if result.get('ready') is False:
            parser.exit(1)
    except (OSError, subprocess.SubprocessError, ValueError, KeyboardInterrupt) as exc:
        parser.exit(1, f'Delivery review failed: {exc}\n')


if __name__ == '__main__':
    main()
