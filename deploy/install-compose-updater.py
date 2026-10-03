#!/usr/bin/env python3
"""Install the local update service; application recreation is a separate command."""
import argparse
import json
import os
from pathlib import Path
import shlex
import shutil
import subprocess


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--channel', required=True, choices=['sub2api', 'kdan', 'tapmodels', 'tokensavy'])
    parser.add_argument('--directory', required=True)
    parser.add_argument('--project-name', required=True, help='Existing Compose project name; never guess it')
    parser.add_argument('--service', help='Existing Compose application service name (defaults to channel)')
    parser.add_argument('--compose-file', action='append', required=True, help='Repeat for every existing override, in order')
    parser.add_argument('--socket-gid', type=int, default=1000)
    parser.add_argument('--github-token-file', help='Root-readable file for private GitHub release downloads')
    args = parser.parse_args()
    if os.geteuid() != 0:
        parser.error('Run as root on the Docker host')
    source = Path(__file__).resolve().parent
    directory = Path(args.directory).resolve(strict=True)
    if '\n' in str(directory) or '\r' in str(directory):
        parser.error('Compose directory must not contain line breaks')
    # Root will execute Compose configuration. Reject app-writable configuration.
    for file in [directory, *(directory / name for name in args.compose_file)]:
        stat = file.stat()
        if stat.st_uid != 0 or stat.st_mode & 0o022:
            parser.error(f'Compose directory/files must be root-owned and not group/world writable: {file}')
    channel = args.channel
    service = args.service or channel
    compose = ['docker', 'compose', '--project-directory', str(directory), '--project-name', args.project_name]
    for file in args.compose_file:
        compose += ['-f', str(directory / file)]
    services = subprocess.check_output(compose + ['config', '--services'], text=True).splitlines()
    if service not in services:
        parser.error(f'Application service {service!r} is absent from the existing Compose project; use --service')
    repository = 'holeenlu/sub2api'
    image = 'ghcr.io/holeenlu/' + channel
    cfg_dir = Path('/etc/sub2api-updater')
    cfg_dir.mkdir(mode=0o700, exist_ok=True)
    runtime = Path('/opt/sub2api-updater')
    runtime.mkdir(mode=0o700, exist_ok=True)
    state_dir = Path('/var/lib/sub2api-updater') / channel
    state_dir.mkdir(mode=0o700, parents=True, exist_ok=True)
    socket_dir = '/run/sub2api-updater-' + channel
    socket_file = directory / 'compose.updater-socket.yml'
    # JSON is valid YAML and avoids interpolating arbitrary user strings into YAML.
    socket_file.write_text(json.dumps({'services': {service: {
        'environment': {'UPDATE_CHECK_ENABLED': 'true', 'UPDATE_COMPOSE_SOCKET': '/run/compose-updater/updater.sock',
                        'UPDATE_GITHUB_TOKEN': '${UPDATE_GITHUB_TOKEN:-}'},
        'volumes': [socket_dir + ':/run/compose-updater:ro']}}}, indent=2) + '\n')
    config = dict(project_directory=str(directory), project_name=args.project_name,
        compose_files=args.compose_file + [socket_file.name], service=service, repository=repository,
        channel=channel, image=image, socket=socket_dir+'/updater.sock', socket_gid=args.socket_gid,
        state_file=str(state_dir/'state.json'), health_timeout=180)
    if args.github_token_file:
        config['github_token_file'] = str(Path(args.github_token_file).resolve(strict=True))
    config_path = cfg_dir / (channel + '.json')
    config_path.write_text(json.dumps(config, indent=2) + '\n')
    os.chmod(config_path, 0o600)
    shutil.copyfile(source/'compose_updater.py', runtime/'compose_updater.py')
    shutil.copyfile(source/'compose-updater@.service', '/etc/systemd/system/sub2api-compose-updater@.service')
    # ProtectHome=read-only otherwise blocks the image override in legacy /root
    # deployments. Grant writes only to this root-owned Compose directory.
    drop_in = Path('/etc/systemd/system') / ('sub2api-compose-updater@' + channel + '.service.d')
    drop_in.mkdir(mode=0o755, exist_ok=True)
    (drop_in / 'deployment.conf').write_text('[Service]\nReadWritePaths=' +
        json.dumps(str(directory).replace('%', '%%'), ensure_ascii=False) + '\n')
    subprocess.run(['systemctl', 'daemon-reload'], check=True)
    subprocess.run(['systemctl', 'enable', 'sub2api-compose-updater@'+channel], check=True)
    subprocess.run(['systemctl', 'restart', 'sub2api-compose-updater@'+channel], check=True)
    command = ['docker', 'compose', '--project-directory', str(directory), '--project-name', args.project_name]
    for file in config['compose_files'] + ['compose.online-update.json']:
        command += ['-f', str(directory/file)]
    print('Service installed. After database backup, connect the existing app using:')
    print(shlex.join(command + ['up','-d','--no-deps','--pull','never',service]))
    print('Use these same Compose files for subsequent manual maintenance.')


if __name__ == '__main__':
    main()
