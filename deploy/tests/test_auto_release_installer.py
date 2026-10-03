"""Installer regression: brand channel is independent of an existing service name."""
import importlib.util
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('installer', Path(__file__).parents[1] / 'install-compose-updater.py')
installer = importlib.util.module_from_spec(spec)
spec.loader.exec_module(installer)


class InstallerTests(unittest.TestCase):
    def test_legacy_service_and_reinstallation(self):
        self.assert_installation('kdan', 'sub2api')

    def test_tapmodels_reinstallation_migrates_release_repository(self):
        self.assert_installation('tapmodels', 'tapmodels')

    def test_tokensavy_installs_an_independent_updater(self):
        self.assert_installation('tokensavy', 'tokensavy')

    def assert_installation(self, channel, service):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            project = root / 'legacy-deploy'
            project.mkdir()
            (project / 'compose.yml').write_text('services: {}\n')
            (root / 'etc/systemd/system').mkdir(parents=True)
            (root / 'opt').mkdir()
            def mapped_path(value):
                path = Path(value)
                if str(path).startswith(('/etc/', '/opt/', '/var/lib/')):
                    return root / str(path).lstrip('/')
                return path
            original_stat = Path.stat
            def root_stat(path, *args, **kwargs):
                values = list(original_stat(path, *args, **kwargs))
                values[4] = 0  # Owner of the simulated production configuration.
                return os.stat_result(values)
            argv = ['installer', '--channel', channel, '--service', service,
                    '--directory', str(project), '--project-name', 'existing', '--compose-file', 'compose.yml']
            original_copy = installer.shutil.copyfile
            def copy(source, target):
                return original_copy(source, mapped_path(target))
            with patch('sys.argv', argv), patch.object(installer.os, 'geteuid', return_value=0), \
                 patch.object(installer, 'Path', side_effect=mapped_path), patch.object(Path, 'stat', root_stat), \
                 patch.object(installer.shutil, 'copyfile', side_effect=copy), \
                 patch.object(installer.subprocess, 'check_output', return_value=service+'\npostgres\n'), \
                 patch.object(installer.subprocess, 'run') as run:
                installer.main()
                installer.main()
            config = json.loads((root / ('etc/sub2api-updater/' + channel + '.json')).read_text())
            self.assertEqual(config['service'], service)
            self.assertEqual(config['repository'], 'holeenlu/sub2api')
            self.assertEqual(config['image'], 'ghcr.io/holeenlu/' + channel)
            self.assertEqual(config['channel'], channel)
            override = json.loads((project / 'compose.updater-socket.yml').read_text())
            self.assertEqual(list(override['services']), [service])
            self.assertEqual(override['services'][service]['environment']['UPDATE_GITHUB_TOKEN'], '${UPDATE_GITHUB_TOKEN:-}')
            self.assertIn(str(project), (root / ('etc/systemd/system/sub2api-compose-updater@' + channel + '.service.d/deployment.conf')).read_text())
            self.assertEqual(sum(call.args[0] == ['systemctl', 'restart', 'sub2api-compose-updater@' + channel] for call in run.call_args_list), 2)

    def test_missing_service_rejected_before_writing(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            (root / 'compose.yml').write_text('services: {}')
            original_stat = Path.stat
            def root_stat(path, *args, **kwargs):
                values = list(original_stat(path, *args, **kwargs)); values[4] = 0
                return os.stat_result(values)
            argv = ['installer', '--channel', 'kdan', '--directory', str(root),
                    '--project-name', 'existing', '--compose-file', 'compose.yml']
            with patch('sys.argv', argv), patch.object(installer.os, 'geteuid', return_value=0), \
                 patch.object(Path, 'stat', root_stat), \
                 patch.object(installer.subprocess, 'check_output', return_value='sub2api\n'), \
                 patch.object(installer.subprocess, 'run') as run:
                with self.assertRaises(SystemExit):
                    installer.main()
            self.assertFalse((root / 'compose.updater-socket.yml').exists())
            run.assert_not_called()


if __name__ == '__main__':
    unittest.main()
