#!/usr/bin/env python3
"""Read-only delivery review. Scope is a human/agent semantic decision, never HEAD."""

import argparse
import json
import subprocess
from pathlib import Path


TARGETS = {
    'shared': [('KDAN', 'origin', 'KDAN'), ('TapModels', 'origin', 'TapModels'),
               ('TapModels', 'erwinlin', 'main')],
    'public': [('holeen/main', 'origin', 'main')],
    'kdan': [('KDAN', 'origin', 'KDAN')],
    'tapmodels': [('TapModels', 'origin', 'TapModels'), ('TapModels', 'erwinlin', 'main')],
}


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
                         'Choose shared/public/kdan/tapmodels/mixed after reviewing the actual diff'),
        'files': files,
        'review_clues': clues,
        'targets': [{'source': branch, 'remote': remote, 'destination': f'refs/heads/{dest}',
                     'push_command': f'git push {remote} refs/heads/{branch}:refs/heads/{dest}'}
                    for branch, remote, dest in targets],
        'limitations': 'Local read-only report; no fetch, content review, merge, commit, push, or live remote verification performed.',
    }


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--repo', type=Path, default=Path.cwd())
    parser.add_argument('--scope', choices=[*TARGETS, 'mixed'])
    selection = parser.add_mutually_exclusive_group()
    selection.add_argument('--staged', action='store_true')
    selection.add_argument('--commit')
    args = parser.parse_args()
    try:
        print(json.dumps(review(args.repo, args.scope, args.staged, args.commit), ensure_ascii=False, indent=2))
    except (subprocess.CalledProcessError, ValueError) as exc:
        parser.exit(1, f'Delivery review failed: {exc}\n')


if __name__ == '__main__':
    main()
