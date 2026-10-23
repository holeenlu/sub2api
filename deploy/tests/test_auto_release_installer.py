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

    def test_portal_detection_and_custom_service_for_all_brands(self):
        for channel in ('kdan', 'tapmodels', 'tokensavy'):
            for portal in ('vendor-portal', 'supply-ui'):
                with self.subTest(channel=channel, portal=portal):
                    self.assert_installation(channel, channel, portal)

    def test_standalone_portal_preserves_environment_and_uses_separate_updater(self):
        for channel in ('kdan', 'tapmodels', 'tokensavy'):
            self.assert_installation(channel, 'vendor-portal', 'vendor-portal', portal_only=True)

    def assert_installation(self, channel, service, portal=None, portal_only=False):
        instance = channel + ('-portal' if portal_only else '')
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            project = root / 'legacy-deploy'
            project.mkdir()
            (project / 'compose.yml').write_text('services: {}\n')
            (project / 'portal.env').write_text('BACKEND_ORIGIN=http://backend:8080\n')
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
            if portal_only:
                argv += ['--portal-only', '--env-file', 'portal.env']
            if portal and portal != 'vendor-portal':
                argv += ['--portal-service', portal]
            original_copy = installer.shutil.copyfile
            def copy(source, target):
                return original_copy(source, mapped_path(target))
            with patch('sys.argv', argv), patch.object(installer.os, 'geteuid', return_value=0), \
                 patch.object(installer, 'Path', side_effect=mapped_path), patch.object(Path, 'stat', root_stat), \
                 patch.object(installer.shutil, 'copyfile', side_effect=copy), \
                 patch.object(installer.subprocess, 'check_output', return_value=service+'\npostgres\n'+(portal or '')), \
                 patch.object(installer.subprocess, 'run') as run:
                installer.main()
                installer.main()
            config = json.loads((root / ('etc/sub2api-updater/' + instance + '.json')).read_text())
            self.assertEqual(config['service'], service)
            self.assertEqual(config['repository'], 'holeenlu/sub2api')
            self.assertEqual(config['image'], 'ghcr.io/holeenlu/' + channel)
            self.assertEqual(config['channel'], channel)
            if portal:
                self.assertEqual(config['portal_service'], portal)
                self.assertEqual(config['portal_image'], 'ghcr.io/holeenlu/'+channel+'-vendor')
            else:
                self.assertNotIn('portal_service', config)
            filename = 'compose.portal-updater-socket.yml' if portal_only else 'compose.updater-socket.yml'
            override = json.loads((project / filename).read_text())
            self.assertEqual(set(override['services']), {service, *([portal] if portal else [])})
            if portal_only:
                self.assertTrue(config['portal_only'])
                self.assertEqual(config['env_files'], ['portal.env'])
                self.assertEqual(config['socket_gid'], 101)
                self.assertNotIn('environment', override['services'][service])
            else:
                self.assertEqual(override['services'][service]['environment']['UPDATE_GITHUB_TOKEN'], '${UPDATE_GITHUB_TOKEN:-}')
            if portal:
                self.assertEqual(override['services'][portal]['volumes'], ['/run/sub2api-updater-' + instance + ':/run/portal-updater:ro'])
            self.assertIn(str(project), (root / ('etc/systemd/system/sub2api-compose-updater@' + instance + '.service.d/deployment.conf')).read_text())
            self.assertEqual(sum(call.args[0] == ['systemctl', 'restart', 'sub2api-compose-updater@' + instance] for call in run.call_args_list), 2)

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
