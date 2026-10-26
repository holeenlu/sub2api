#!/usr/bin/env python3
"""Review, resumable validation and a lock for explicitly supplied delivery commands."""

import argparse
import contextlib
import fcntl
import hashlib
import json
import math
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
GO_CACHE_LIMIT_KIB = 8 * 1024 * 1024
BUILD_ENV = ('PATH', 'GOFLAGS', 'GOTOOLCHAIN', 'GOWORK', 'GOOS', 'GOARCH',
             'CGO_ENABLED', 'CC', 'CXX', 'CFLAGS', 'CGO_CFLAGS', 'CGO_LDFLAGS',
             'NODE_OPTIONS', 'NODE_ENV', 'TZ', 'LANG', 'LC_ALL', 'PYTHONPATH', 'GOCACHEPROG')


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


@contextlib.contextmanager
def repo_lock(repo, name, wait_seconds=0):
    if not math.isfinite(wait_seconds) or wait_seconds < 0:
        raise ValueError('Lock wait must be a finite nonnegative number of seconds.')
    common = Path(git(repo, 'rev-parse', '--git-common-dir'))
    if not common.is_absolute():
        common = repo / common
    deadline = time.monotonic() + wait_seconds
    with (common / name).open('a') as lock:
        announced = False
        while True:
            try:
                fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
                break
            except BlockingIOError as exc:
                remaining = deadline - time.monotonic()
                if remaining <= 0:
                    raise ValueError(f'Another validation or delivery is active ({name}); lock wait expired.') from exc
                if not announced:
                    print(f'WAIT {name} (up to {wait_seconds:g}s)', flush=True)
                    announced = True
                time.sleep(min(0.25, remaining))
        yield


def shared_cache_status(path):
    size = int(subprocess.check_output(['du', '-sk', str(path)], text=True).split()[0]) if path.exists() else 0
    return {'path': str(path), 'size_kib': size, 'limit_kib': GO_CACHE_LIMIT_KIB,
            'within_limit': size <= GO_CACHE_LIMIT_KIB}


def delivery_run(repo, command, wait_seconds=0):
    """Serialize the caller's whole freeze/validate/publish/closeout command, not just its push."""
    if not command:
        raise ValueError('delivery-run requires --command followed by an explicitly authorized command.')
    with repo_lock(repo.resolve(), 'change-delivery.lock', wait_seconds):
        process = subprocess.Popen(command, cwd=repo, start_new_session=True)
        try:
            code = process.wait()
            if code:
                raise ValueError(f'Delivery command failed ({code}); inspect its checkpoint before retrying.')
        finally:
            stop_process(process)
    return {'command_exit': code, 'limitations': 'Local cooperative lock only; exact remote leases remain required.'}


def validate(repo, plan_path, receipt_path, check_only=False, wait_seconds=0, go_cache=None):
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
    if go_cache is not None:
        go_cache = go_cache.resolve()
        if check_only or go_cache.is_relative_to(repo) or go_cache in (Path('/'), Path.home()):
            raise ValueError('--go-cache must select a verified external compiler cache for validate only.')
    # Status only fingerprints staged inputs and reads atomically saved receipts;
    # it neither competes for the compiler lock nor creates build directories.
    lock = contextlib.nullcontext() if check_only else repo_lock(repo, 'sync-upstream.lock', wait_seconds)
    with lock:
        if candidate_tree(repo) != tree or digest(plan_path.read_bytes()) != plan_hash:
            raise ValueError('Candidate or plan changed while waiting; review before validating.')
        if receipt_path.exists():
            receipt = json.loads(receipt_path.read_text())
            if receipt.get('version') != RECEIPT_VERSION:
                raise ValueError('Unsupported receipt schema; preserve it and start a new receipt.')
        else:
            receipt = {'version': RECEIPT_VERSION, 'results': {}}
        if go_cache is not None:
            go_cache.mkdir(parents=True, exist_ok=True)
            if not shared_cache_status(go_cache)['within_limit']:
                raise ValueError('Shared Go cache exceeds 8 GiB; review ownership before cleanup. Nothing was deleted.')
        env = dict(os.environ)
        report = []
        temporary_context = contextlib.nullcontext(None) if check_only else tempfile.TemporaryDirectory(prefix='sub2api-validation-')
        with temporary_context as directory:
            temporary = Path(directory) if directory is not None else None
            run_env = env
            if temporary is not None:
                run_env = {**env, 'GOCACHE': str(go_cache or temporary / 'go-cache'),
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
                receipt['shared_go_cache'] = shared_cache_status(go_cache) if go_cache else None
                save_receipt(receipt_path, receipt)
        cache = receipt.get('shared_go_cache')
        if cache and check_only:
            cache = shared_cache_status(Path(cache['path']))
        frozen = (receipt.get('candidate_tree') == tree
                  and receipt.get('plan_hash') == plan_hash)
        return {'tree': tree, 'same_candidate': receipt.get('candidate_tree') == tree,
                'ready': frozen and all(r['reused'] for r in report) if check_only else True, 'checks': report,
                'temporary_removed': temporary is None or not temporary.exists(),
                'shared_go_cache': cache,
                'cleanup_complete': cache is None or cache['within_limit'],
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
                        choices=['review', 'validate', 'validation-status', 'delivery-run'])
    parser.add_argument('--repo', type=Path, default=Path.cwd())
    parser.add_argument('--scope', choices=[*TARGETS, 'mixed'])
    parser.add_argument('--plan', type=Path)
    parser.add_argument('--receipt', type=Path)
    parser.add_argument('--wait-seconds', type=float, default=0,
                        help='Bounded wait for the validation or delivery lock; default fails immediately.')
    parser.add_argument('--go-cache', type=Path,
                        help='Reuse a verified external Go compiler cache (8 GiB ceiling); never deletes it.')
    parser.add_argument('--command', nargs=argparse.REMAINDER,
                        help='Explicit command to run under the complete-delivery lock.')
    selection = parser.add_mutually_exclusive_group()
    selection.add_argument('--staged', action='store_true')
    selection.add_argument('--commit')
    args = parser.parse_args()
    try:
        if args.action == 'delivery-run':
            if args.plan or args.receipt or args.scope or args.staged or args.commit or args.go_cache:
                parser.error('delivery-run accepts only --repo, --wait-seconds and --command.')
            result = delivery_run(args.repo, args.command, args.wait_seconds)
        elif args.action == 'review':
            if args.plan or args.receipt or args.command or args.go_cache or args.wait_seconds:
                parser.error('Review does not accept validation or delivery options.')
            result = review(args.repo, args.scope, args.staged, args.commit)
        else:
            if not args.plan or not args.receipt or args.scope or args.staged or args.commit or args.command:
                parser.error('Validation requires --plan and --receipt; do not combine review options.')
            result = validate(args.repo, args.plan, args.receipt, args.action == 'validation-status',
                              args.wait_seconds, args.go_cache)
        print(json.dumps(result, ensure_ascii=False, indent=2))
        if result.get('ready') is False or result.get('cleanup_complete') is False:
            parser.exit(1)
    except (OSError, subprocess.SubprocessError, ValueError, KeyboardInterrupt) as exc:
        parser.exit(1, f'Delivery review failed: {exc}\n')


if __name__ == '__main__':
    def interrupt(signum, frame):
        raise KeyboardInterrupt

    signal.signal(signal.SIGTERM, interrupt)
    main()
