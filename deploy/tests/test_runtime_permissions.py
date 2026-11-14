"""Runtime permissions and API build isolation."""
import json
import shlex
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]

class RuntimePermissionsTests(unittest.TestCase):
    def test_runtime_files_are_readable_and_executable(self):
        lines = (ROOT / 'Dockerfile').read_text().splitlines()
        binary = json.loads(next(x[4:] for x in lines if x.startswith('CMD ')))[0]
        self.assertEqual(binary, '/app/sub2api')
        for destination in (binary, '/app/docker-entrypoint.sh'):
            copies = [shlex.split(x) for x in lines if x.startswith('COPY ') and shlex.split(x)[-1] == destination]
            self.assertEqual(len(copies), 2 if destination == binary else 1)
            self.assertTrue(all('--chmod=755' in copy for copy in copies))

    def test_api_build_has_no_frontend_or_external_artifact_input(self):
        source = (ROOT / 'Dockerfile').read_text()
        self.assertNotIn('COPY frontend', source)
        self.assertNotIn('COPY sub2api ', source)
        self.assertIn('./cmd/server', source)
        self.assertIn('go build -mod=readonly', source)
        self.assertIn('COPY LICENSE /app/LICENSE', source)
        self.assertIn('deploy/api/docker-entrypoint.sh', source)

    def test_entrypoint_is_neutral_and_drops_root(self):
        source = (ROOT / 'deploy/api/docker-entrypoint.sh').read_text()
        self.assertIn('exec su-exec sub2api', source)
        self.assertIn('set -- /app/sub2api', source)
        self.assertNotIn('/app/kdan', source)
        self.assertNotIn('eval ', source)

if __name__ == '__main__':
    unittest.main()
