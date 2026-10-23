"""Runtime permissions must not depend on host modes or brand names."""
from pathlib import Path
import shlex
import unittest
import yaml

ROOT = Path(__file__).resolve().parents[2]


class RuntimePermissionsTests(unittest.TestCase):
    def test_runtime_files_are_readable_and_executable(self):
        for filename in ('Dockerfile', 'Dockerfile.goreleaser', 'deploy/Dockerfile'):
            with self.subTest(filename=filename):
                lines = (ROOT / filename).read_text().splitlines()
                command = next(line for line in lines if line.startswith('CMD '))
                import json
                binary = json.loads(command[4:])[0]
                for destination in (binary, '/app/docker-entrypoint.sh'):
                    copies = [shlex.split(line) for line in lines if line.startswith('COPY ')
                              and shlex.split(line)[-1] == destination]
                    self.assertEqual(len(copies), 1, destination)
                    self.assertIn('--chmod=755', copies[0])

    def test_goreleaser_contexts_include_only_runtime_resources(self):
        for filename in ('.goreleaser.yaml', '.goreleaser.simple.yaml'):
            config = yaml.safe_load((ROOT / filename).read_text())
            for image in config['dockers']:
                with self.subTest(filename=filename, image=image['id']):
                    self.assertEqual(set(image['extra_files']),
                                     {'deploy/docker-entrypoint.sh', 'backend/resources'})


if __name__ == '__main__':
    unittest.main()
