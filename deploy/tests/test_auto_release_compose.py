"""Opt-in integration: RUN_COMPOSE_UPDATER_TEST=1; uses disposable local containers."""
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch
from deploy.compose_updater import Updater


@unittest.skipUnless(os.environ.get('RUN_COMPOSE_UPDATER_TEST') == '1', 'opt-in Docker Compose integration')
class ComposeIntegrationTests(unittest.TestCase):
    def test_image_switch_data_preservation_and_health_recovery(self):
        project = 'sub2api-update-test-' + str(os.getpid())
        def command(*args):
            return subprocess.check_output(args, text=True, stderr=subprocess.STDOUT).strip()
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            tags = [project + ':v1', project + ':v2', project + ':broken']
            try:
                for index, tag in enumerate(tags):
                    (root/'Dockerfile').write_text('FROM alpine:3.21\n' +
                        'RUN mkdir -p /app && printf "#!/bin/sh\\nexit 0\\n" >/app/sub2api && chmod 755 /app/sub2api\n' +
                        ('RUN touch /healthy\n' if index < 2 else '') +
                        f'RUN echo {index} >/version\n' +
                        'HEALTHCHECK --interval=1s --timeout=1s --retries=1 CMD test -f /healthy\nCMD ["sleep", "3600"]\n')
                    command('docker','build','-q','-t',tag,str(root))
                config = {'services': {'app': {'image':tags[0], 'volumes':['data:/data']},
                                       'sibling': {'image':tags[0]}}, 'volumes': {'data':{}}}
                (root/'compose.json').write_text(json.dumps(config))
                updater = Updater(dict(project_directory=directory, project_name=project,
                    compose_files=['compose.json'], service='app', state_file=str(root/'state.json'), health_timeout=10))
                self.addCleanup(lambda: subprocess.run(updater.compose+['down','--volumes'],
                    text=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL))
                command(*updater.compose,'up','-d')
                sibling = command(*updater.compose,'ps','-q','sibling')
                command('docker','exec',updater.container(),'sh','-c','echo keep >/data/marker')
                actual_command = updater.command
                def local_command(*args, **kwargs):
                    if args[:2] == ('docker','pull'):
                        return ''  # Fixtures are local; production always pulls the verified digest.
                    return actual_command(*args, **kwargs)
                with patch.object(updater,'image_for',return_value=tags[1]), \
                     patch.object(updater,'command',side_effect=local_command):
                    updater.lock.acquire(); updater.apply('0.2.8.1')
                self.assertEqual(updater.state['status'],'succeeded',updater.state)
                self.assertEqual(command('docker','exec',updater.container(),'cat','/version'),'1')
                self.assertEqual(command('docker','exec',updater.container(),'cat','/data/marker'),'keep')
                self.assertEqual(command(*updater.compose,'ps','-q','sibling'),sibling)
                with patch.object(updater,'image_for',return_value=tags[2]), \
                     patch.object(updater,'command',side_effect=local_command):
                    updater.lock.acquire(); updater.apply('0.2.8.2')
                self.assertEqual(updater.state['status'],'failed')
                self.assertIn('previous image restored',updater.state['message'])
                self.assertEqual(command('docker','exec',updater.container(),'cat','/version'),'1')
                self.assertEqual(command('docker','exec',updater.container(),'cat','/data/marker'),'keep')
                self.assertEqual(command(*updater.compose,'ps','-q','sibling'),sibling)
            finally:
                if 'updater' in locals():
                    subprocess.run(updater.compose+['down','--volumes'], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
                subprocess.run(['docker','image','rm',*tags], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
