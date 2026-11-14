"""Exercise snapshot cancellation with real tiny processes, never real Docker/Git."""
import hashlib
import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import tempfile
import time
import unittest


SCRIPT = Path(__file__).resolve().parents[1] / 'local/build-ui.py'
RUNNER = r'''
import importlib.util
import os
from pathlib import Path
import signal
import subprocess
import sys
import time

spec = importlib.util.spec_from_file_location('ui_snapshot', sys.argv[1])
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
module.ROOT = Path(sys.argv[2])
module.INPUTS = ['fixture.txt']
module.TERMINATE_TIMEOUT = 0.2
module.KILL_TIMEOUT = 1.0
module.POLL_INTERVAL = 0.02

if os.environ.get('DIRECT_KEYBOARD_INTERRUPT'):
    original = subprocess.Popen
    class InterruptingPopen(original):
        def communicate(self, *args, **kwargs):
            if self.args[:2] == ['docker', 'build']:
                ready = Path(os.environ['FAKE_STATE']) / 'main-build.json'
                deadline = time.monotonic() + 5
                while not ready.exists():
                    if self.poll() is not None or time.monotonic() >= deadline:
                        raise RuntimeError('fake build did not become ready')
                    time.sleep(0.01)
                raise KeyboardInterrupt
            return super().communicate(*args, **kwargs)
    module.subprocess.Popen = InterruptingPopen

before = {s: signal.getsignal(s) for s in (signal.SIGINT, signal.SIGTERM)}
try:
    module.main(['--manifest', sys.argv[3], '--web-image', 'fixture-web',
                 '--vendor-image', 'fixture-vendor'])
except module.SnapshotCancelled as exc:
    raise SystemExit(128 + exc.signum)
except KeyboardInterrupt:
    raise SystemExit(130)
finally:
    assert all(signal.getsignal(s) == handler for s, handler in before.items())
'''

FAKE_GIT = r'''
import sys
if sys.argv[1:] == ['rev-parse', 'HEAD']:
    print('a' * 40)
elif sys.argv[1] == 'ls-files':
    sys.stdout.write('fixture.txt\0')
else:
    raise SystemExit('Unexpected fake Git invocation')
'''

FAKE_DOCKER = r"""
import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import time

state = Path(os.environ['FAKE_STATE'])
args = sys.argv[1:]
if args[0] == 'build':
    target = args[args.index('--target') + 1]
    stage = target + '-build'
elif args[:2] == ['image', 'inspect']:
    target = 'main' if args[2] == 'fixture-web' else 'vendor'
    stage = target + '-inspect'
else:
    raise SystemExit('Unexpected fake Docker invocation')
with (state / 'calls.jsonl').open('a') as log:
    log.write(json.dumps({'stage': stage, 'args': args}) + '\n')

mode = os.environ.get('FAKE_MODE', 'success')
if mode == 'fail' and stage == os.environ.get('FAKE_BLOCK_STAGE', 'main-build'):
    (state / (stage + '.json')).write_text(json.dumps({'pid': os.getpid(), 'cwd': os.getcwd()}))
    raise SystemExit(7)

if mode in ('tree', 'stubborn') and stage == os.environ.get('FAKE_BLOCK_STAGE', 'main-build'):
    stopping = False
    def stop(_signum, _frame):
        global stopping
        (state / (stage + '.term')).write_text('TERM received')
        stopping = mode != 'stubborn'
    signal.signal(signal.SIGTERM, stop)
    worker = None
    if mode == 'tree':
        child_ready = state / (stage + '.child')
        child_source = '''
import os, signal, sys, time
from pathlib import Path
signal.signal(signal.SIGTERM, lambda *_: sys.exit(0))
Path(sys.argv[1]).write_text(str(os.getpid()))
while True: time.sleep(0.02)
'''
        worker = subprocess.Popen([sys.executable, '-B', '-c', child_source, str(child_ready)])
        deadline = time.monotonic() + 5
        while not child_ready.exists():
            if worker.poll() is not None or time.monotonic() >= deadline:
                raise SystemExit('fake worker did not start')
            time.sleep(0.01)
    marker = {'pid': os.getpid(), 'pgid': os.getpgrp(), 'cwd': os.getcwd(),
              'child': worker.pid if worker else None}
    (state / (stage + '.json')).write_text(json.dumps(marker))
    while not stopping:
        time.sleep(0.02)
    if worker is not None:
        worker.wait(timeout=3)
    (state / (stage + '.exited')).write_text('graceful exit')
    raise SystemExit(0)

if args[:2] == ['image', 'inspect']:
    print(json.dumps([{'Id': 'sha256:fixture-' + target, 'Size': 123}]))
"""


@unittest.skipUnless(os.name == 'posix', 'Process-group signal checks require POSIX')
class UISnapshotTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='ui-snapshot-test-')
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.source = self.root / 'source'
        self.source.mkdir()
        (self.source / 'fixture.txt').write_text('snapshot fixture\n')
        self.state = self.root / 'state'
        self.state.mkdir()
        self.bin = self.root / 'bin'
        self.bin.mkdir()
        for name, body in [('git', FAKE_GIT), ('docker', FAKE_DOCKER)]:
            executable = self.bin / name
            executable.write_text('#!' + sys.executable + '\n' + body)
            executable.chmod(0o755)
        self.manifest = self.root / 'output/manifest.json'
        self.runners = []
        self.addCleanup(self.cleanup_processes)

    def cleanup_processes(self):
        for process in self.runners:
            if process.poll() is None:
                process.terminate()
                try:
                    process.wait(timeout=4)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait(timeout=2)
            for stream in (process.stdout, process.stderr):
                if stream is not None:
                    stream.close()
        # Only fixture sessions recorded inside this test's private directory.
        # This is failure-path cleanup, after assertions have checked for leaks.
        for path in self.state.glob('*.json'):
            marker = json.loads(path.read_text())
            if marker.get('pgid') == marker['pid']:
                try:
                    os.killpg(marker['pgid'], signal.SIGKILL)
                except ProcessLookupError:
                    pass

    def start(self, mode='success', stage='main-build', direct_interrupt=False):
        env = {**os.environ, 'PATH': str(self.bin), 'PYTHONDONTWRITEBYTECODE': '1',
               'FAKE_STATE': str(self.state), 'FAKE_MODE': mode, 'FAKE_BLOCK_STAGE': stage}
        env.pop('DIRECT_KEYBOARD_INTERRUPT', None)
        if direct_interrupt:
            env['DIRECT_KEYBOARD_INTERRUPT'] = '1'
        process = subprocess.Popen([sys.executable, '-B', '-c', RUNNER, str(SCRIPT),
                                    str(self.source), str(self.manifest)], env=env,
                                   stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                   text=True, start_new_session=True)
        self.runners.append(process)
        return process

    def ready(self, process, stage='main-build'):
        path = self.state / (stage + '.json')
        deadline = time.monotonic() + 5
        while time.monotonic() < deadline:
            if path.exists():
                try:
                    marker = json.loads(path.read_text())
                except json.JSONDecodeError:
                    continue
                self.assertEqual(marker['pid'], marker['pgid'])
                self.assertNotEqual(marker['pgid'], os.getpgrp())
                return marker
            if process.poll() is not None:
                self.fail('runner ended before fixture readiness: ' + str(process.communicate()))
            time.sleep(0.01)
        self.fail('fake Docker process did not become ready')

    def finish(self, process, status, returncode):
        stdout, stderr = process.communicate(timeout=5)
        self.assertEqual(process.returncode, returncode, stdout + stderr)
        manifest = json.loads(self.manifest.read_text())
        self.assertEqual(manifest['status'], status, manifest)
        self.assertEqual(list(self.manifest.parent.glob('ui-source-*')), [])
        self.assertEqual((self.source / 'fixture.txt').read_text(), 'snapshot fixture\n')
        return manifest

    def assert_gone(self, marker):
        for pid in [marker['pid'], marker.get('child')]:
            if pid is not None:
                with self.assertRaises(ProcessLookupError):
                    os.kill(pid, 0)
        with self.assertRaises(ProcessLookupError):
            os.killpg(marker['pgid'], 0)
        self.assertFalse(Path(marker['cwd']).exists())

    def test_success_records_both_images_and_exact_fixture_input(self):
        manifest = self.finish(self.start(), 'built', 0)
        self.assertEqual(set(manifest['images']), {'main', 'vendor'})
        self.assertEqual(manifest['git_head'], 'a' * 40)
        self.assertEqual([entry['path'] for entry in manifest['files']], ['fixture.txt'])
        self.assertEqual(manifest['files'][0]['sha256'], hashlib.sha256(b'snapshot fixture\n').hexdigest())
        self.assertEqual(manifest['source_sha256'], hashlib.sha256(json.dumps(
            manifest['files'], sort_keys=True, separators=(',', ':')).encode()).hexdigest())

    def test_build_failure_records_failed_and_removes_context(self):
        self.finish(self.start('fail'), 'failed', 1)
        marker = json.loads((self.state / 'main-build.json').read_text())
        self.assertFalse(Path(marker['cwd']).exists())
        calls = [json.loads(line)['stage'] for line in (self.state / 'calls.jsonl').read_text().splitlines()]
        self.assertEqual(calls, ['main-build'])

    def test_inspection_failure_records_failed_and_removes_context(self):
        self.finish(self.start('fail', 'main-inspect'), 'failed', 1)
        calls = [json.loads(line)['stage'] for line in (self.state / 'calls.jsonl').read_text().splitlines()]
        self.assertEqual(calls, ['main-build', 'main-inspect'])

    def test_sigint_cancels_and_waits_for_owned_process_tree(self):
        process = self.start('tree')
        marker = self.ready(process)
        process.send_signal(signal.SIGINT)
        self.finish(process, 'cancelled', 130)
        self.assertTrue((self.state / 'main-build.exited').exists())
        self.assert_gone(marker)

    def test_sigterm_preserves_completed_image_and_cleans_current_tree(self):
        process = self.start('tree', 'vendor-build')
        marker = self.ready(process, 'vendor-build')
        process.send_signal(signal.SIGTERM)
        manifest = self.finish(process, 'cancelled', 143)
        self.assertEqual(set(manifest['images']), {'main'})
        self.assert_gone(marker)

    def test_keyboard_interrupt_exception_cleans_real_child_tree(self):
        process = self.start('tree', direct_interrupt=True)
        self.finish(process, 'cancelled', 130)
        marker = json.loads((self.state / 'main-build.json').read_text())
        self.assert_gone(marker)

    def test_inspection_cancellation_cleans_captured_output_process(self):
        process = self.start('tree', 'main-inspect')
        marker = self.ready(process, 'main-inspect')
        process.send_signal(signal.SIGTERM)
        self.finish(process, 'cancelled', 143)
        # Inspection runs outside the disposable build context.
        marker.pop('cwd')
        for pid in [marker['pid'], marker['child']]:
            with self.assertRaises(ProcessLookupError):
                os.kill(pid, 0)

    def test_repeated_sigterm_forces_only_owned_unresponsive_process(self):
        unrelated = subprocess.Popen([sys.executable, '-B', '-c', 'import time; time.sleep(30)'],
                                     start_new_session=True)
        self.runners.append(unrelated)
        process = self.start('stubborn')
        marker = self.ready(process)
        started = time.monotonic()
        process.send_signal(signal.SIGTERM)
        deadline = started + 2
        while not (self.state / 'main-build.term').exists():
            self.assertLess(time.monotonic(), deadline)
            time.sleep(0.01)
        process.send_signal(signal.SIGTERM)
        self.finish(process, 'cancelled', 143)
        self.assertLess(time.monotonic() - started, 3)
        self.assertFalse((self.state / 'main-build.exited').exists())
        self.assert_gone(marker)
        self.assertIsNone(unrelated.poll(), 'An unrelated process was terminated')


if __name__ == '__main__':
    unittest.main()
