"""Complete NewAPI release packaging and isolated deployment checks."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import secrets
import shutil
import subprocess
import tarfile
import tempfile
import time
from urllib.parse import quote
from urllib.request import Request, urlopen

ROOT = Path(__file__).resolve().parents[2]
TAG = 'NewAPI/latest'
ROLES = ('api', 'web', 'vendor')


def run(*args, **kwargs):
    return subprocess.run(args, check=True, text=True, **kwargs)


def api(method, path, payload=None, file=None, missing=False):
    args = ['gh', 'api', '--method', method, path]
    if payload is not None:
        args += ['--input', '-']
    if file is not None:
        args += ['--input', str(file), '-H', 'Content-Type: application/octet-stream']
    p = subprocess.run(args, input=json.dumps(payload) if payload is not None else None,
                       capture_output=True, text=True)
    data = json.loads(p.stdout) if p.stdout.strip() else None
    if p.returncode:
        if missing and isinstance(data, dict) and str(data.get('status')) == '404':
            return None
        raise RuntimeError(f'{method} {path}: {p.stderr.strip()}')
    return data


def metadata():
    version, commit = os.environ['VERSION'], os.environ['COMMIT']
    if not re.fullmatch(r'[0-9]+\.[0-9]+\.[0-9]+(?:\.[0-9]+)?', version):
        raise ValueError('Version must be a numeric three- or four-part version')
    if not re.fullmatch(r'[a-f0-9]{40}', commit):
        raise ValueError('Commit must be the full selected source SHA')
    if os.environ['GITHUB_REPOSITORY'] != 'holeenlu/sub2api':
        raise ValueError('Unexpected release repository')
    return version, commit


def compose(images):
    import yaml
    config = yaml.safe_load((ROOT / 'deploy/api/compose.yaml').read_text())
    config['services'].update(yaml.safe_load((ROOT / 'deploy/web/compose.yaml').read_text())['services'])
    config['services'].update(yaml.safe_load((ROOT / 'deploy/docker-compose.vendor.yml').read_text())['services'])
    for role, service in [('api', 'api'), ('web', 'web'), ('vendor', 'vendor-portal')]:
        item = config['services'][service]
        item.pop('build', None)
        item['image'] = images[role]
        item['pull_policy'] = 'missing'
        if role != 'api':
            item['depends_on'] = {'api': {'condition': 'service_healthy'}}
    for service in config['services'].values():
        service['logging'] = {'driver': 'json-file', 'options': {'max-size': '20m', 'max-file': '3'}}
    return config


def archive(path, source):
    with tarfile.open(path, 'w:gz') as tar:
        for item in sorted(source.iterdir()):
            tar.add(item, arcname=item.name)


def smoke(images):
    """Initialise only new, temporary databases; always remove owned resources."""
    with tempfile.TemporaryDirectory(prefix='newapi-release-smoke-') as directory:
        path = Path(directory)
        project = 'newapi-release-test-' + secrets.token_hex(5)
        config = compose(images)
        for name in ('api', 'web', 'vendor-portal'):
            config['services'][name]['ports'] = ['127.0.0.1::8080']
            config['services'][name]['pull_policy'] = 'never'
        (path / 'compose.json').write_text(json.dumps(config))
        password = secrets.token_hex(24)
        env = dict(os.environ, COMPOSE_PROJECT_NAME=project,
                   POSTGRES_PASSWORD=secrets.token_hex(24), REDIS_PASSWORD=secrets.token_hex(24),
                   ADMIN_EMAIL='release@example.invalid', ADMIN_PASSWORD=password,
                   JWT_SECRET=secrets.token_hex(32), TOTP_ENCRYPTION_KEY=secrets.token_hex(32),
                   WEB_PUBLIC_ORIGIN='http://localhost', PORTAL_PUBLIC_ORIGIN='http://supplier.localhost',
                   BACKEND_ORIGIN='http://api:8080', SETUP_MIGRATION_TIMEOUT_SECONDS='300')
        args = ['docker', 'compose', '--project-directory', directory, '-f', str(path / 'compose.json')]
        def command(*tail, **kwargs):
            return run(*args, *tail, env=env, **kwargs)
        def port(service):
            return command('port', service, '8080', capture_output=True).stdout.strip().rsplit(':', 1)[-1]
        def request(service, route, host=None, data=None):
            body = json.dumps(data).encode() if data is not None else None
            headers = {'Host': host or 'localhost', 'Content-Type': 'application/json'}
            with urlopen(Request(f'http://127.0.0.1:{port(service)}{route}', data=body, headers=headers), timeout=15) as response:
                return response.status, response.read(), response.headers.get('Content-Type', '')
        try:
            command('up', '-d', '--no-build', '--wait', '--wait-timeout', '360')
            assert json.loads(request('api', '/setup/status')[1])['data']['needs_setup'] is False
            for service, host in [('web', 'localhost'), ('vendor-portal', 'supplier.localhost')]:
                status, body, content_type = request(service, '/', host)
                assert status == 200 and 'text/html' in content_type and b'<html' in body.lower()
                assert request(service, '/api/v1/settings/public', host)[0] == 200
            status, body, _ = request('web', '/api/v1/auth/login', data={'email': env['ADMIN_EMAIL'], 'password': password})
            assert status == 200 and json.loads(body)['data']['access_token']
            command('restart', 'api')
            command('up', '-d', '--no-build', '--wait', '--wait-timeout', '180')
            assert json.loads(request('api', '/setup/status')[1])['data']['needs_setup'] is False
            assert json.loads(request('web', '/api/v1/auth/login', data={'email': env['ADMIN_EMAIL'], 'password': password})[1])['data']['access_token']
            print('PASS: API/DB/Redis, main UI, vendor UI, proxied login and restart persistence')
        finally:
            command('down', '--volumes', '--remove-orphans', '--timeout', '20')


def bundle(images):
    version, commit = metadata()
    out = ROOT / 'dist/release'
    out.mkdir(parents=True, exist_ok=True)
    binary = ROOT / 'dist/api'
    archive(out / f'NewAPI-{version}-linux-amd64.tar.gz', binary)
    manifest = {'version': version, 'commit': commit, 'tag': TAG, 'platform': 'linux/amd64', 'images': images}
    for role in ROLES:
        if not re.fullmatch(r'ghcr\.io/holeenlu/newapi-(api|web|vendor)@sha256:[a-f0-9]{64}', images[role]):
            raise ValueError('Bundle requires verified immutable role image digests')
        if not images[role].split('@')[0].endswith('-' + role):
            raise ValueError('Image roles must not be exchanged')
    (out / 'release-manifest.json').write_text(json.dumps(manifest, indent=2) + '\n')
    with tempfile.TemporaryDirectory(prefix='newapi-bundle-') as directory:
        path = Path(directory)
        (path / 'compose.yaml').write_text(json.dumps(compose(images), indent=2) + '\n')
        (path / '.env.example').write_text('\n'.join(line for line in (ROOT / 'deploy/api/.env.example').read_text().splitlines() if not line.startswith(('APP_IMAGE=', '# API deployment;', '# External frontend integration'))) + '\n')
        with (path / '.env.example').open('a') as f:
            f.write('\n# Public origins (HTTPS when placed behind your reverse proxy).\nWEB_PUBLIC_ORIGIN=\nPORTAL_PUBLIC_ORIGIN=\nBACKEND_ORIGIN=http://api:8080\nWEB_BIND_HOST=127.0.0.1\nWEB_PORT=8082\nPORTAL_BIND_HOST=127.0.0.1\nPORTAL_PORT=8081\n')
        shutil.copy(out / 'release-manifest.json', path / 'release-manifest.json')
        shutil.copy(ROOT / 'deploy/release/README.md', path / 'README.md')
        shutil.copy(ROOT / 'LICENSE', path / 'LICENSE')
        shutil.copytree(binary, path / 'api')
        for role in ('main', 'vendor'):
            shutil.copytree(ROOT / 'dist/ui' / role, path / 'frontend' / role)
        archive(out / f'NewAPI-{version}-deployment-linux-amd64.tar.gz', path)
    (out / 'checksums.txt').write_text(''.join(hashlib.sha256(f.read_bytes()).hexdigest() + '  ' + f.name + '\n'
                                              for f in sorted(out.iterdir()) if f.name != 'checksums.txt'))
    run('sha256sum', '-c', 'checksums.txt', cwd=out)


def publish():
    version, commit = metadata()
    repo = 'repos/' + os.environ['GITHUB_REPOSITORY']
    # Preserve the existing single moving release, but bind uploads to its ID.
    release = api('GET', repo + '/releases/tags/' + quote(TAG, safe=''), missing=True)
    notes = (ROOT / 'deploy/release/README.md').read_text()
    if release is None:
        release = api('POST', repo + '/releases', {'tag_name': TAG, 'target_commitish': commit,
                       'name': 'NewAPI ' + version, 'body': notes, 'draft': True, 'make_latest': 'false'})
    release_id = release['id']
    if release['tag_name'] != TAG:
        raise ValueError('Unexpected Release identity')
    existing = {a['name']: a for a in api('GET', repo + f'/releases/{release_id}')['assets']}
    out = ROOT / 'dist/release'
    for file in sorted(out.iterdir()):
        if file.name in existing:
            api('DELETE', repo + '/releases/assets/' + str(existing[file.name]['id']))
        asset = api('POST', f'https://uploads.github.com/{repo}/releases/{release_id}/assets?name=' + quote(file.name, safe=''), file=file)
        if asset.get('digest') != 'sha256:' + hashlib.sha256(file.read_bytes()).hexdigest():
            raise ValueError('Uploaded asset digest mismatch: ' + file.name)
    ref = repo + '/git/refs/tags/' + TAG
    tag = api('GET', repo + '/git/ref/tags/' + quote(TAG, safe=''), missing=True)
    if tag is None:
        api('POST', repo + '/git/refs', {'ref': 'refs/tags/' + TAG, 'sha': commit})
    else:
        api('PATCH', ref, {'sha': commit, 'force': True})
    body = f'# NewAPI {version}\n\nSource commit: `{commit}`\n\n' + notes
    api('PATCH', repo + f'/releases/{release_id}', {'name': 'NewAPI ' + version, 'body': body, 'draft': False, 'make_latest': 'false'})
    # Only remove previous NewAPI-specific assets after all replacements verify.
    keep = {f.name for f in out.iterdir()}
    for name, asset in existing.items():
        if name not in keep and name.startswith('NewAPI-'):
            api('DELETE', repo + '/releases/assets/' + str(asset['id']))
    print('Published https://github.com/holeenlu/sub2api/releases/tag/NewAPI/latest')


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('command', choices=['smoke', 'bundle', 'publish'])
    args = parser.parse_args()
    if args.command == 'publish':
        publish()
    else:
        images = json.loads(Path('dist/images.json').read_text())
        {'smoke': smoke, 'bundle': bundle}[args.command](images)


if __name__ == '__main__':
    main()
