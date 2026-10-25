import contextlib
import io
import json
import os
import subprocess
import tempfile
from pathlib import Path
import unittest
from unittest.mock import patch

import yaml

from scripts.release import plan


ROOT = Path(__file__).resolve().parents[2]


class ManualReleaseWorkflowTests(unittest.TestCase):
    def test_workflows_are_present_and_require_manual_dispatch(self):
        workflows_dir = ROOT / '.github/workflows'
        self.assertTrue((workflows_dir / 'automatic-release.yml').is_file())
        self.assertTrue((workflows_dir / 'quality-gate.yml').is_file())
        for path in sorted(workflows_dir.glob('*.y*ml')):
            with self.subTest(workflow=path.name):
                # BaseLoader preserves the GitHub YAML key "on" as a string.
                workflow = yaml.load(path.read_text(), Loader=yaml.BaseLoader)
                self.assertEqual(workflow['on'], {'workflow_dispatch': ''})
                if path.name != 'automatic-release.yml':
                    continue
                self.assertEqual(workflow['name'], 'Manual versioned release')
                self.assertEqual(
                    workflow['jobs']['select']['if'],
                    "github.event_name == 'workflow_dispatch'",
                )

    def test_complete_notes_are_checked_before_builds_and_kept_in_final_release(self):
        workflow = yaml.load((ROOT / '.github/workflows/automatic-release.yml').read_text(), Loader=yaml.BaseLoader)
        steps = workflow['jobs']['prepare']['steps']
        names = [step.get('name', '') for step in steps]
        preflight = names.index('Verify complete release notes before builds')
        self.assertGreater(preflight, names.index('Reserve version (or resume failed build)'))
        self.assertLess(preflight, names.index('Select Linux amd64 GoReleaser target'))
        self.assertIn('release_content(json.load(source))', steps[preflight]['run'])
        self.assertIn('scripts/release/notes.py', str(workflow['jobs']['publish']))

    def test_manual_channel_selection_preserves_brand_targets(self):
        config = json.loads((ROOT / 'scripts/release/channels.json').read_text())
        cases = [
            ('holeenlu/sub2api', 'main', 'kdan'),
            ('holeenlu/sub2api', 'KDAN', ''),
            ('erwinlin/TapModels', 'main', ''),
            ('holeenlu/sub2api', 'TapModels', 'tapmodels'),
            ('holeenlu/sub2api', 'tokensavy', 'tokensavy'),
            ('holeenlu/sub2api', 'tapmodels', ''),
            ('holeenlu/sub2api', 'Tokensavy', ''),
            ('holeenlu/sub2api', 'feature/example', ''),
        ]
        for repository, branch, expected in cases:
            with self.subTest(repository=repository, branch=branch):
                output = io.StringIO()
                with patch.dict(os.environ, {
                    'GITHUB_REPOSITORY': repository,
                    'GITHUB_REF': 'refs/heads/' + branch,
                }), patch('sys.argv', ['plan.py', '--select']), \
                     patch.object(Path, 'read_text', return_value=json.dumps(config)), \
                     patch.object(plan, 'run') as run, \
                     contextlib.redirect_stdout(output):
                    plan.main()
                self.assertEqual(output.getvalue().strip(), 'channel=' + expected)
                run.assert_not_called()

    def test_channels_are_unique_and_stay_in_this_repository(self):
        config = json.loads((ROOT / 'scripts/release/channels.json').read_text())
        self.assertEqual(set(config), {'kdan', 'tapmodels', 'tokensavy'})
        for channel, branch in [('kdan', 'main'), ('tapmodels', 'TapModels'), ('tokensavy', 'tokensavy')]:
            self.assertEqual(config[channel]['repository'], 'holeenlu/sub2api')
            self.assertEqual(config[channel]['branch'], branch)
            self.assertEqual(config[channel]['image'], 'ghcr.io/holeenlu/' + channel)
            self.assertEqual(config[channel]['portal_image'], 'ghcr.io/holeenlu/' + channel + '-vendor')
        workflow = yaml.load((ROOT / '.github/workflows/automatic-release.yml').read_text(), Loader=yaml.BaseLoader)
        # Preparation uses the selected workflow branch. Every build/publication
        # job thereafter pins the commit resolved by that preparation job.
        for job in ('build-frontend', 'build-binaries', 'publish', 'promote'):
            checkout = workflow['jobs'][job]['steps'][0]
            self.assertEqual(checkout['with']['ref'], '${{ needs.prepare.outputs.commit }}')

    def test_publication_is_bound_to_reserved_id_and_checked_before_writes(self):
        workflow = (ROOT / '.github/workflows/automatic-release.yml').read_text()
        self.assertNotIn('gh release upload', workflow)
        self.assertNotIn('gh release edit', workflow)
        jobs = yaml.load(workflow, Loader=yaml.BaseLoader)['jobs']
        prepare = jobs['prepare']['steps']
        checks = [step for step in prepare if step.get('run') == 'python3 scripts/release/publish.py --check']
        self.assertEqual(len(checks), 1)
        self.assertEqual(checks[0]['env']['RELEASE_ID'], '${{ steps.plan.outputs.release_id }}')
        self.assertLess(prepare.index(checks[0]), next(i for i, step in enumerate(prepare) if step.get('name') == 'Select Linux amd64 GoReleaser target'))
        commands = '\n'.join(step.get('run', '') for step in jobs['publish']['steps'])
        self.assertLess(commands.index('scripts/release/publish.py --check'), commands.index('bash .github/release-tools/release-images.sh'))
        self.assertLess(commands.index('scripts/release/notes.py'), commands.rindex('scripts/release/publish.py'))

    def test_release_uses_native_linux_amd64_artifacts_and_image_checks(self):
        workflow = yaml.load((ROOT / '.github/workflows/automatic-release.yml').read_text(), Loader=yaml.BaseLoader)
        for job in workflow['jobs'].values():
            self.assertEqual(job['runs-on'], 'ubuntu-latest')
            self.assertFalse(any('setup-qemu' in step.get('uses', '') for step in job['steps']))
        publish = workflow['jobs']['publish']['steps']
        binary_download = next(step for step in publish
                               if step.get('with', {}).get('path') == 'release-input')
        self.assertEqual(binary_download['with']['name'], 'automatic-binary-linux-amd64')
        commands = '\n'.join(step.get('run', '') for step in publish)
        self.assertIn('cp release-input/*_linux_amd64.tar.gz dist/release/', commands)
        self.assertNotIn('*.zip', commands)
        self.assertIn('for image in "$RELEASE_IMAGE" "$RELEASE_PORTAL_IMAGE"', commands)
        self.assertIn('docker pull --platform linux/amd64', commands)
        self.assertIn("--format '{{.Os}}/{{.Architecture}}')\" = linux/amd64", commands)
        self.assertIn('docker run --rm --platform linux/amd64', commands)

    def test_brands_build_independently_but_publication_and_brand_promotion_stay_serial(self):
        workflow = yaml.load((ROOT / '.github/workflows/automatic-release.yml').read_text(), Loader=yaml.BaseLoader)
        brand_lock = workflow['concurrency']
        publish_lock = workflow['jobs']['publish']['concurrency']
        self.assertIn('${{ github.ref }}', brand_lock['group'])
        self.assertNotIn('${{ github.ref }}', publish_lock['group'])
        self.assertNotEqual(brand_lock['group'], publish_lock['group'])
        for lock in (brand_lock, publish_lock):
            self.assertIn('${{ github.repository }}', lock['group'])
            self.assertEqual(lock['cancel-in-progress'], 'false')
            self.assertEqual(lock['queue'], 'max')
        self.assertEqual(workflow['jobs']['promote']['needs'], ['prepare', 'publish'])

    def test_go_dependency_change_can_restore_same_brand_target_cache(self):
        workflow = yaml.load((ROOT / '.github/workflows/automatic-release.yml').read_text(), Loader=yaml.BaseLoader)
        cache = next(step['with'] for step in workflow['jobs']['build-binaries']['steps']
                     if step.get('uses', '').startswith('actions/cache@'))
        prefixes = cache['restore-keys'].splitlines()
        self.assertEqual(len(prefixes), 2)
        self.assertIn("hashFiles('backend/go.mod', 'backend/go.sum')", prefixes[0])
        self.assertNotIn('hashFiles', prefixes[1])
        self.assertTrue(prefixes[0].startswith(prefixes[1]))
        for boundary in ('runner.os', 'matrix.goos', 'matrix.goarch', 'needs.prepare.outputs.channel'):
            self.assertIn('${{ ' + boundary + ' }}', prefixes[1])
        self.assertIn('${{ needs.prepare.outputs.commit }}', cache['key'])

    def test_frontend_checks_run_once_and_block_both_builds_on_failure(self):
        workflow = yaml.load((ROOT / '.github/workflows/automatic-release.yml').read_text(), Loader=yaml.BaseLoader)
        command = next(step['run'] for step in workflow['jobs']['build-frontend']['steps']
                       if 'vue-tsc' in step.get('run', ''))
        # Exercise the actual shell sequence without installing frontend dependencies.
        # Failure of either gate must prevent both production builds.
        with tempfile.TemporaryDirectory(prefix='release-workflow-test-') as directory:
            base = Path(directory)
            pnpm = base / 'pnpm'
            pnpm.write_text('#!/bin/bash\nprintf "%s|%s\\n" "$*" "${VITE_VENDOR_PORTAL:-}" >> "$CALLS"\n'
                            'if [[ "$*" == "$FAIL_STEP" ]]; then exit 42; fi\n')
            pnpm.chmod(0o755)
            for failure in ('', 'exec vue-tsc -b',
                            'exec vitest run src/i18n/__tests__/localeKeyCompleteness.spec.ts '
                            'src/components/common/__tests__/VersionBadge.spec.ts '
                            'src/components/common/__tests__/VersionBadgeCompose.spec.ts '
                            'src/features/vendor/__tests__/VendorEditor.spec.ts '
                            'src/features/vendor/__tests__/AccountForm.spec.ts src/components/common/__tests__/VersionBadgePortal.spec.ts'):
                with self.subTest(failure=failure):
                    calls = base / 'calls'
                    calls.write_text('')
                    env = {**os.environ, 'PATH': directory + os.pathsep + os.environ['PATH'],
                           'CALLS': str(calls), 'FAIL_STEP': failure, 'VITE_VENDOR_PORTAL': ''}
                    result = subprocess.run(['bash', '-e', '-o', 'pipefail', '-c', command], env=env,
                                            capture_output=True, text=True)
                    actual = calls.read_text().splitlines()
                    if failure:
                        self.assertEqual(result.returncode, 42, result.stderr)
                        self.assertFalse(any('exec vite build' in call for call in actual))
                    else:
                        self.assertEqual(result.returncode, 0, result.stderr)
                        self.assertEqual(len(actual), 4)
                        self.assertEqual(actual[0], 'exec vue-tsc -b|')
                        self.assertIn('localeKeyCompleteness.spec.ts', actual[1])
                        self.assertIn('VendorEditor.spec.ts', actual[1])
                        self.assertIn('AccountForm.spec.ts', actual[1])
                        self.assertEqual(actual[2:], ['exec vite build|',
                                                     'exec vite build --outDir dist-vendor|true'])
        # Vite must not repeat the explicit gate; local package builds still run it.
        self.assertIn('enableBuild: false', (ROOT / 'frontend/vite.config.ts').read_text())
        scripts = json.loads((ROOT / 'frontend/package.json').read_text())['scripts']
        for name in ('build', 'build:vendor'):
            self.assertIn('vue-tsc -b &&', scripts[name])

    def test_portal_artifact_and_publication_share_the_frozen_backend_release(self):
        workflow = yaml.load((ROOT / '.github/workflows/automatic-release.yml').read_text(), Loader=yaml.BaseLoader)
        jobs = workflow['jobs']
        self.assertIn('portal_image', jobs['prepare']['outputs'])
        frontend = jobs['build-frontend']['steps']
        self.assertTrue(any('VITE_VENDOR_PORTAL=true pnpm exec vite build --outDir dist-vendor' in step.get('run', '') for step in frontend))
        self.assertTrue(any(step.get('with', {}).get('name') == 'automatic-vendor-frontend-dist' for step in frontend))
        self.assertTrue(any(step.get('with', {}).get('name') == 'automatic-vendor-frontend-dist' for step in jobs['publish']['steps']))
        for name in ['publish', 'promote']:
            self.assertEqual(jobs[name]['env']['RELEASE_PORTAL_IMAGE'], '${{ needs.prepare.outputs.portal_image }}')
        publish = '\n'.join(step.get('run', '') for step in jobs['publish']['steps'])
        self.assertLess(publish.index('PORTAL_IMAGE_DIGEST='), publish.index('scripts/release/notes.py'))
        self.assertLess(publish.index('scripts/release/notes.py'), publish.rindex('scripts/release/publish.py'))
        promote = '\n'.join(step.get('run', '') for step in jobs['promote']['steps'])
        self.assertLess(promote.index('inspect "$RELEASE_PORTAL_IMAGE@$PORTAL_DIGEST"'), promote.index('create --tag "$RELEASE_IMAGE:latest"'))


if __name__ == '__main__':
    unittest.main()
