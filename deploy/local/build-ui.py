#!/usr/bin/env python3
"""Build both UI roles from one stable, minimal working-tree snapshot.

Includes tracked and untracked source, never dependencies, secrets or local SDKs.
The temporary context is removed on success, failure and catchable cancellation;
its hash manifest remains. SIGKILL cannot guarantee cleanup or a terminal manifest.
This records build provenance, not acceptance of concurrent frontend changes.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import signal
import subprocess
import tempfile
import time
from datetime import datetime, timezone

ROOT = Path(__file__).resolve().parents[2]
INPUTS = [
    'frontend', 'docs/legal', 'LICENSE',
    'backend/internal/pkg/claude/prompts/catalog.json',
    'backend/internal/service/scheduling_quota_defaults.json',
    'deploy/web/Dockerfile', 'deploy/web/Dockerfile.dockerignore',
    'deploy/web/entrypoint.sh', 'deploy/web/nginx.conf.template',
    'deploy/vendor-portal/entrypoint.sh', 'deploy/vendor-portal/nginx.conf.template',
]
EXCLUDED_DIRS = {'node_modules', 'dist', 'dist-vendor', 'coverage', '.vite', '.cache', '.git', '__pycache__'}
TERMINATE_TIMEOUT = 5.0
KILL_TIMEOUT = 5.0
POLL_INTERVAL = 0.05


class SnapshotCancelled(KeyboardInterrupt):
    def __init__(self, signum):
        self.signum = signum
        super().__init__('Snapshot build cancelled by ' + signal.Signals(signum).name)


class Cancellation:
    """Defer signals to checkpoints so spawning and cleanup cannot be interrupted."""
    def __init__(self):
        self.signum = None

    def __enter__(self):
        self.previous = {}
        for signum in (signal.SIGINT, signal.SIGTERM):
            self.previous[signum] = signal.signal(signum, self.request)
        return self

    def request(self, signum, _frame):
        if self.signum is None:
            self.signum = signum

    def check(self):
        if self.signum is not None:
            raise SnapshotCancelled(self.signum)

    def __exit__(self, *_exc):
        for signum, handler in self.previous.items():
            signal.signal(signum, handler)


def stop_docker(process):
    """Signal only this Popen child/session, then reap it within bounded waits."""
    def send(signum):
        try:
            if os.name == 'posix':
                os.killpg(process.pid, signum)
            elif process.poll() is None:
                (process.terminate if signum == signal.SIGTERM else process.kill)()
        except ProcessLookupError:
            pass

    def alive():
        process.poll()  # Reap our direct child, even while its descendants exit.
        if os.name != 'posix':
            return process.returncode is None
        try:
            os.killpg(process.pid, 0)
            return True
        except ProcessLookupError:
            return False

    def wait(timeout):
        deadline = time.monotonic() + timeout
        while alive():
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                return False
            time.sleep(min(POLL_INTERVAL, remaining))
        return True

    try:
        send(signal.SIGTERM)
        if not wait(TERMINATE_TIMEOUT):
            send(signal.SIGKILL)
            if not wait(KILL_TIMEOUT):
                raise subprocess.TimeoutExpired(process.args, TERMINATE_TIMEOUT + KILL_TIMEOUT)
    finally:
        for stream in (process.stdout, process.stderr):
            if stream is not None:
                stream.close()


def run_docker(command, cancellation, *, timeout, cwd=None, capture_output=False):
    cancellation.check()
    # A dedicated session prevents cancellation from signalling the caller,
    # another build, or another task's Docker client. No daemon resources are removed.
    process = subprocess.Popen(command, cwd=cwd, start_new_session=os.name == 'posix',
                               stdout=subprocess.PIPE if capture_output else None,
                               stderr=subprocess.PIPE if capture_output else None, text=True)
    try:
        deadline = time.monotonic() + timeout
        while True:
            cancellation.check()
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                raise subprocess.TimeoutExpired(command, timeout)
            try:
                stdout, stderr = process.communicate(timeout=min(POLL_INTERVAL, remaining))
                break
            except subprocess.TimeoutExpired:
                continue
        cancellation.check()
        if process.returncode:
            raise subprocess.CalledProcessError(process.returncode, command, stdout, stderr)
        return subprocess.CompletedProcess(command, process.returncode, stdout, stderr)
    except BaseException:
        stop_docker(process)
        raise


def source_files():
    result = subprocess.run(['git', 'ls-files', '-z', '--cached', '--others', '--exclude-standard', '--', *INPUTS],
                            cwd=ROOT, capture_output=True, check=True, timeout=30)
    files = []
    for relative in sorted(set(os.fsdecode(result.stdout).split('\0')) - {''}):
        path = Path(relative)
        if any(part in EXCLUDED_DIRS for part in path.parts) or path.name.startswith('.env'):
            continue
        if path.suffix in {'.log', '.tsbuildinfo', '.pyc'}:
            continue
        source = ROOT / path
        if source.is_symlink():
            raise RuntimeError('Snapshot refuses symlink input: ' + relative)
        if source.is_file():
            files.append(relative)
    return files


def capture(destination=None, check_cancelled=lambda: None):
    entries = []
    for relative in source_files():
        check_cancelled()
        source = ROOT / relative
        content = source.read_bytes()
        mode = source.stat().st_mode & 0o777
        entries.append({'path': relative, 'bytes': len(content), 'sha256': hashlib.sha256(content).hexdigest(), 'mode': mode})
        if destination is not None:
            target = destination / relative
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes(content)
            target.chmod(mode)
    return entries


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--web-image', default=os.environ.get('WEB_IMAGE', 'sub2api-web:rebuild-ui'))
    parser.add_argument('--vendor-image', default=os.environ.get('VENDOR_PORTAL_IMAGE', 'sub2api-vendor:rebuild-ui'))
    parser.add_argument('--version', default='rebuild-ui')
    parser.add_argument('--manifest', type=Path, default=ROOT / '.local/rebuild-integration/ui-source-manifest.json')
    args = parser.parse_args(argv)
    args.manifest = args.manifest.resolve()
    args.manifest.parent.mkdir(parents=True, exist_ok=True)
    manifest = {'schema': 1, 'captured_at': datetime.now(timezone.utc).isoformat(),
                'git_head': None, 'source_sha256': None,
                'scope': 'Working-tree snapshot including uncommitted frontend; not full UI acceptance',
                'files': [], 'images': {}, 'status': 'capturing'}

    def save():
        args.manifest.write_text(json.dumps(manifest, ensure_ascii=False, separators=(',', ':')) + '\n')

    with Cancellation() as cancellation:
        try:
            with tempfile.TemporaryDirectory(prefix='ui-source-', dir=args.manifest.parent) as temporary:
                context = Path(temporary)
                cancellation.check()
                revision = subprocess.run(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True,
                                          capture_output=True, check=True, timeout=30).stdout.strip()
                manifest['git_head'] = revision
                # A second complete hash pass rejects a context torn by concurrent saves.
                for attempt in range(3):
                    cancellation.check()
                    try:
                        entries = capture(context, cancellation.check)
                        if entries == capture(check_cancelled=cancellation.check):
                            break
                    except FileNotFoundError:
                        pass
                    for path in context.iterdir():
                        if path.is_dir():
                            shutil.rmtree(path)
                        else:
                            path.unlink()
                else:
                    raise RuntimeError('Frontend changed during all three snapshot attempts; retry after the current save.')
                cancellation.check()
                digest = hashlib.sha256(json.dumps(entries, sort_keys=True, separators=(',', ':')).encode()).hexdigest()
                manifest.update(source_sha256=digest, files=entries, status='building')
                save()
                print(f'Snapshot {digest}: {len(entries)} files, {sum(e["bytes"] for e in entries)} bytes', flush=True)
                for target, image in [('main', args.web_image), ('vendor', args.vendor_image)]:
                    run_docker(['docker', 'build', '--progress=plain', '-f', 'deploy/web/Dockerfile',
                                '--target', target, '--build-arg', 'VERSION=' + args.version,
                                '--build-arg', 'COMMIT=' + revision, '--build-arg', 'SOURCE_SHA256=' + digest,
                                '-t', image, '.'], cancellation, cwd=context, timeout=3600)
                    inspected = json.loads(run_docker(['docker', 'image', 'inspect', image], cancellation,
                                                     capture_output=True, timeout=30).stdout)[0]
                    manifest['images'][target] = {'tag': image, 'id': inspected['Id'], 'bytes': inspected['Size']}
                    save()
            cancellation.check()
            manifest['status'] = 'built'
        except KeyboardInterrupt:
            manifest['status'] = 'cancelled'
            raise
        except Exception:
            manifest['status'] = 'cancelled' if cancellation.signum is not None else 'failed'
            raise
        finally:
            save()
    print('Context removed; manifest: ' + str(args.manifest), flush=True)


if __name__ == '__main__':
    try:
        main()
    except SnapshotCancelled as exc:
        raise SystemExit(128 + exc.signum)
    except KeyboardInterrupt:
        raise SystemExit(130)
