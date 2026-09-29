import contextlib
import io
import json
import os
from pathlib import Path
import unittest
from unittest.mock import patch

import yaml

from scripts.release import plan


ROOT = Path(__file__).resolve().parents[2]


class ManualReleaseWorkflowTests(unittest.TestCase):
    def test_only_release_workflow_is_present_and_requires_manual_dispatch(self):
        workflows = sorted(
            path for path in (ROOT / '.github/workflows').iterdir()
            if path.suffix in {'.yml', '.yaml'}
        )
        self.assertEqual([path.name for path in workflows], ['automatic-release.yml'])
        for path in workflows:
            with self.subTest(workflow=path.name):
                # BaseLoader preserves the GitHub YAML key "on" as a string.
                workflow = yaml.load(path.read_text(), Loader=yaml.BaseLoader)
                self.assertEqual(workflow['on'], {'workflow_dispatch': ''})
                self.assertEqual(workflow['name'], 'Manual versioned release')
                self.assertEqual(
                    workflow['jobs']['select']['if'],
                    "github.event_name == 'workflow_dispatch'",
                )

    def test_manual_channel_selection_preserves_brand_targets(self):
        config = json.loads((ROOT / 'scripts/release/channels.json').read_text())
        cases = [
            ('holeenlu/sub2api', 'main', 'kdan'),
            ('holeenlu/sub2api', 'KDAN', ''),
            ('erwinlin/TapModels', 'main', 'tapmodels'),
            ('holeenlu/sub2api', 'TapModels', ''),
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

    def test_published_release_is_marked_latest(self):
        workflow = (ROOT / '.github/workflows/automatic-release.yml').read_text()
        self.assertIn('gh api -X PATCH "repos/$GITHUB_REPOSITORY/releases/$RELEASE_ID" -F draft=false -F make_latest=true', workflow)
        self.assertNotIn('make_latest=false', workflow)


if __name__ == '__main__':
    unittest.main()
