#!/usr/bin/env python3
"""Release installer and fixed-scope host updater. No shell supplied by the WebUI."""
import argparse
import contextlib
import fcntl
import hashlib
import http.server
import json
import os
from pathlib import Path, PurePosixPath
import re
import secrets
import shutil
import socket
import socketserver
import struct
import subprocess
import tarfile
import threading
import time
import urllib.request
import urllib.error
from urllib.parse import urlparse

REPO = 'czy-code-923/emby-cloud-runtime'
ROOT = Path('/srv/emby-cloud')
STATE = Path('/var/lib/emby-cloud-updater')
SOCKET = Path('/run/emby-cloud-updater/updater.sock')
VERSION_RE = re.compile(r'^v?(\d+)\.(\d+)\.(\d+)$')
PROTECTED = {'.env', 'images.lock.yaml', 'runtime', 'backups', '.git', '.emby-cloud.json'}


def version_tuple(value):
    match = VERSION_RE.fullmatch(value)
    if not match:
        raise ValueError('Only stable vX.Y.Z releases can be installed')
    return tuple(map(int, match.groups()))


def write_json(path, value):
    path.parent.mkdir(parents=True, exist_ok=True)
    temp = path.with_name(path.name + '.new')
    temp.write_text(json.dumps(value, ensure_ascii=False, indent=2) + '\n')
    temp.chmod(0o600)
    os.replace(temp, path)


def read_json(path, default=None):
    return json.loads(path.read_text()) if path.exists() else default


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


def github_open(url, token, accept, timeout=45):
    # Credentials are sent only to our fixed GitHub API origin. Signed asset
    # redirects are followed without Authorization, including same-host ones.
    if not url.startswith(f'https://api.github.com/repos/{REPO}/releases/'):
        raise ValueError('Unexpected release API origin')
    headers = {'User-Agent': 'emby-cloud-release-manager', 'Accept': accept}
    if token:
        headers['Authorization'] = 'Bearer ' + token
    opener = urllib.request.build_opener(NoRedirect())
    for attempt in range(4):
        try:
            return opener.open(urllib.request.Request(url, headers=headers), timeout=timeout)
        except urllib.error.HTTPError as error:
            if error.code not in {301, 302, 303, 307, 308} or accept != 'application/octet-stream':
                raise ValueError('Release access failed; check repository access and token expiry') from None
            target = error.headers.get('Location', '')
            error.close()
            parsed = urlparse(target)
            if parsed.scheme != 'https' or parsed.username or parsed.port not in {None, 443} or not parsed.hostname or not parsed.hostname.endswith('.githubusercontent.com'):
                raise ValueError('Unexpected asset redirect')
            url = target
            headers = {'User-Agent': 'emby-cloud-release-manager', 'Accept': accept}
    raise ValueError('Too many asset redirects')


def fetch_json(url, token=''):
    request = urllib.request.Request(url, headers={'User-Agent': 'emby-cloud-release-manager', 'Accept': 'application/vnd.github+json'})
    response = github_open(url, token, 'application/vnd.github+json') if token else urllib.request.urlopen(request, timeout=45)
    with response:
        return json.load(response)


def release_info(tag=None, token=''):
    suffix = 'tags/' + tag if tag else 'latest'
    if tag:
        version_tuple(tag)
    result = fetch_json(f'https://api.github.com/repos/{REPO}/releases/{suffix}', token)
    version_tuple(result['tag_name'])
    if result.get('draft') or result.get('prerelease'):
        raise ValueError('The selected release is not a published stable release')
    return result


def download_asset(release, name, dest, token=''):
    asset = next((a for a in release['assets'] if a['name'] == name), None)
    if not asset or not re.fullmatch(r'sha256:[0-9a-f]{64}', asset.get('digest', '')):
        raise ValueError('Missing verified release asset: ' + name)
    expected_url = f'https://github.com/{REPO}/releases/download/{release["tag_name"]}/{name}'
    if asset['browser_download_url'] != expected_url:
        raise ValueError('Unexpected asset origin')
    digest = hashlib.sha256()
    size = 0
    request = urllib.request.Request(expected_url, headers={'User-Agent': 'emby-cloud-release-manager'})
    if token:
        asset_id = asset.get('id')
        if type(asset_id) is not int or asset_id <= 0:
            raise ValueError('Missing release asset identity')
        response = github_open(f'https://api.github.com/repos/{REPO}/releases/assets/{asset_id}', token, 'application/octet-stream', 120)
    else:
        response = urllib.request.urlopen(request, timeout=120)
    with response, dest.open('wb') as output:
        while chunk := response.read(1024 * 1024):
            output.write(chunk)
            digest.update(chunk)
            size += len(chunk)
            if size > asset['size']:
                raise ValueError('Asset exceeded declared size')
    if size != asset['size'] or 'sha256:' + digest.hexdigest() != asset['digest']:
        dest.unlink(missing_ok=True)
        raise ValueError('Release asset checksum mismatch: ' + name)


def unpack_source(archive, target):
    target.mkdir(parents=True, exist_ok=True)
    seen = set()
    total = 0
    with tarfile.open(archive, 'r:gz') as stream:
        for member in stream:
            parts = PurePosixPath(member.name).parts
            if not parts or parts[0] != 'emby-cloud' or '..' in parts or PurePosixPath(member.name).is_absolute():
                raise ValueError('Unsafe source archive path')
            rel = Path(*parts[1:])
            if not parts[1:]:
                continue
            if parts[1] in PROTECTED or rel.as_posix() in seen or not (member.isfile() or member.isdir()):
                raise ValueError('Archive contains protected, duplicate or linked paths')
            seen.add(rel.as_posix())
            total += member.size
            if total > 512 * 1024 * 1024 or len(seen) > 30000:
                raise ValueError('Source archive exceeds limits')
            path = target / rel
            if member.isdir():
                path.mkdir(parents=True, exist_ok=True)
            else:
                path.parent.mkdir(parents=True, exist_ok=True)
                with stream.extractfile(member) as source, path.open('wb') as output:
                    shutil.copyfileobj(source, output)
                path.chmod(0o755 if member.mode & 0o111 else 0o644)


class Manager:
    def __init__(self, root=ROOT, state=STATE):
        self.root, self.state = Path(root), Path(state)
        self.state.mkdir(parents=True, exist_ok=True)
        self.status_file = self.state / 'status.json'
        self.latest = None
        self.checked_at = 0.0
        self.guard = threading.Lock()
        self._network = None

    def network(self):
        with self.guard:
            if self._network is None:
                import sys
                sys.path.insert(0,str(self.root/'scripts'))
                from network import Network
                self._network=Network(self)
        return self._network

    def install_network_units(self):
        import shutil
        for name in ['emby-cloud-certificate.service','emby-cloud-certificate.timer']:
            source=self.root/'deploy'/name
            if source.exists():shutil.copy2(source,Path('/etc/systemd/system')/name)
        self.command(['systemctl','daemon-reload'])
        self.command(['systemctl','enable','--now','emby-cloud-certificate.timer'])

    def command(self, args, cwd=None, env=None, timeout=1800):
        with (self.state / 'operations.log').open('a') as log:
            result = subprocess.run(args, cwd=cwd or self.root, env=env, stdout=log, stderr=log, timeout=timeout)
        if result.returncode:
            raise RuntimeError('Command failed: ' + ' '.join(args[:3]) + '; see local operations.log')

    def dc(self, *args):
        return ['docker', 'compose', '-f', str(self.root/'compose.yaml'), '-f', str(self.root/'images.lock.yaml'), *args]

    def resume_stack(self):
        self.command(self.dc('up', '-d', 'emby'))
        readiness = self.root/'scripts/wait-emby.py'
        if readiness.exists():
            self.command(['python3', str(readiness)], timeout=100)
        self.command(self.dc('up', '-d', '--wait', '--wait-timeout', '240'))

    def status(self):
        marker = read_json(self.root/'.emby-cloud.json', {})
        current = (self.root/'VERSION').read_text().strip() if (self.root/'VERSION').exists() else None
        return {'installed_version': current, 'protocol': 1, 'source_repo': REPO,
                'latest': self.latest, 'job': read_json(self.status_file, {'phase': 'idle'}),
                'uninstalled': marker.get('uninstalled', False), 'credentials': self.credentials_status()}

    def release_credentials(self):
        return read_json(self.state/'release-credentials.json', {'revision': 0, 'token': ''})

    def credentials_status(self):
        value = self.release_credentials()
        return {'configured': bool(value.get('token')), 'revision': value['revision']}

    def configure_credentials(self, body):
        if set(body) != {'token', 'clear', 'expected_revision'} or type(body['clear']) is not bool or type(body['expected_revision']) is not int:
            raise ValueError('Invalid credential settings')
        token = body['token']
        if not isinstance(token, str) or (token and not re.fullmatch(r'[A-Za-z0-9_]{20,255}', token)) or (body['clear'] and token):
            raise ValueError('Invalid GitHub token')
        with self.guard:
            if read_json(self.status_file, {}).get('phase') in {'queued','download','backup','applying','checking','rollback'}:
                raise ValueError('Wait for the current upgrade to finish')
            value = self.release_credentials()
            if body['expected_revision'] != value['revision']:
                raise ValueError('Settings changed; refresh and retry')
            if not token and not body['clear']:
                raise ValueError('Enter a token or explicitly clear the existing token')
            write_json(self.state/'release-credentials.json', {'revision': value['revision']+1, 'token': token})
            self.latest = None
            self.checked_at = 0
        return self.status()

    def progress(self, phase, **fields):
        previous = read_json(self.status_file, {})
        previous.update(fields, phase=phase, updated_at=int(time.time()))
        write_json(self.status_file, previous)

    def failure_message(self, error):
        with contextlib.suppress(OSError):
            with (self.state/'operations.log').open('a') as log:
                log.write(type(error).__name__+': '+str(error)+'\n')
        return 'Upgrade could not finish; inspect the host upgrade log for details'

    def check(self):
        if self.latest is not None and time.monotonic() - self.checked_at < 60:
            return self.status()
        release = release_info(token=self.release_credentials().get('token', ''))
        current = self.status()['installed_version']
        self.latest = {'tag': release['tag_name'], 'version': release['tag_name'].lstrip('v'),
                       'url': release['html_url'], 'published_at': release['published_at'],
                       'available': current is None or version_tuple(release['tag_name']) > version_tuple(current.split('-')[0])}
        self.checked_at = time.monotonic()
        return self.status()

    def validate_root(self):
        if self.root != ROOT or self.root.is_symlink() or self.root.resolve() != ROOT:
            raise ValueError('Installation path must be the dedicated /srv/emby-cloud directory')
        if self.root.exists() and any(self.root.iterdir()):
            if not (self.root/'.emby-cloud.json').exists():
                # Adopt the earlier deployment only when its identifying compose files exist.
                compose = self.root/'compose.yaml'
                if not compose.is_file() or 'name: emby-cloud' not in compose.read_text() or not (self.root/'.env').is_file():
                    raise ValueError('Refusing to overwrite an unrelated directory')
            for folder in ['runtime', 'backups']:
                path = self.root/folder
                if path.is_symlink() or (path.exists() and path.resolve().parent != self.root):
                    raise ValueError('Runtime and backup paths cannot be symlinks')

    def start(self, tag, expected):
        version_tuple(tag)
        with self.guard:
            if read_json(self.status_file, {}).get('phase') in {'queued','download','backup','applying','checking','rollback'}:
                raise ValueError('An upgrade is already running')
            if self.status()['installed_version'] != expected:
                raise ValueError('Installed version changed; refresh before upgrading')
            self.progress('queued', job_id=secrets.token_hex(8), target=tag, error=None)
            threading.Thread(target=self._background, args=(tag,), daemon=True).start()
        return self.status()

    def _background(self, tag):
        try:
            self.install(tag)
            self.command(['systemd-run','--collect','--unit=emby-cloud-updater-refresh-'+secrets.token_hex(4),'--on-active=8s',
                          '/bin/systemctl','try-restart','emby-cloud-updater.service'],timeout=30)
        except Exception as exc:
            phase=read_json(self.status_file,{}).get('phase')
            self.progress(phase if phase in {'rolled_back','recovery_required'} else 'failed', error=self.failure_message(exc))

    def install(self, tag=None):
        self.validate_root()
        with (self.state/'update.lock').open('w') as update_lock:
            fcntl.flock(update_lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            self._install(tag)

    def _install(self, tag):
        token = self.release_credentials().get('token', '')
        release = release_info(tag, token=token)
        version = release['tag_name'].lstrip('v')
        old_version = self.status()['installed_version']
        if old_version and VERSION_RE.fullmatch(old_version) and version_tuple(version) < version_tuple(old_version):
            raise ValueError('Downgrades require a separate recovery procedure')
        if old_version == version and not self.status()['uninstalled']:
            self.progress('succeeded', target=release['tag_name'], message='Already up to date', error=None)
            return
        self.root.mkdir(parents=True, exist_ok=True)
        if shutil.disk_usage(self.root).free < 3 * 1024**3:
            raise ValueError('At least 3 GiB free disk is required')
        stamp = time.strftime('%Y%m%dT%H%M%SZ', time.gmtime()) + '-' + secrets.token_hex(3)
        stage = self.state/'staging'/stamp
        stage.mkdir(parents=True)
        self.progress('download', target=release['tag_name'], error=None)
        download_asset(release, 'release.json', stage/'release.json', token=token)
        metadata = read_json(stage/'release.json')
        old_marker = read_json(self.root/'.emby-cloud.json', {'database_schema': 1})
        if metadata.get('version') != version or metadata.get('protocol') != 1 or metadata.get('database_schema') != old_marker.get('database_schema',1) or metadata.get('rollback_safe') is not True:
            raise ValueError('Release requires an unsupported protocol or manual data migration')
        source_name = f'emby-cloud-{version}-source.tar.gz'
        download_asset(release, source_name, stage/source_name, token=token)
        source = stage/'source'
        unpack_source(stage/source_name, source)
        if (source/'VERSION').read_text().strip() != version:
            raise ValueError('Source version differs from release')
        for component in ['backend', 'webui']:
            name = f'{component}-{version}-amd64.tar.gz'
            download_asset(release, name, stage/name, token=token)
            self.command(['docker','load','--input',str(stage/name)])
        # Pull images before stopping any application service.
        registry = ['postgres:17-alpine','caddy:2-alpine','emby/embyserver:4.10.2.0']
        env_text = (self.root/'.env').read_text() if (self.root/'.env').exists() else ''
        if not old_version or 'cloud' in next((v.split('=',1)[1] for v in env_text.splitlines() if v.startswith('COMPOSE_PROFILES=')), ''):
            registry.append('indown/openstrm:v2.28.0')
        if 'source' in next((v.split('=',1)[1] for v in env_text.splitlines() if v.startswith('COMPOSE_PROFILES=')), ''):
            registry.append('openlistteam/openlist:v4.2.10')
        for image in registry:
            self.command(['docker','pull',image])
        runtime = self.root/'runtime'
        runtime.mkdir(exist_ok=True)
        with (runtime/'.maintenance.lock').open('w') as maintenance:
            fcntl.flock(maintenance, fcntl.LOCK_EX | fcntl.LOCK_NB)
            active = (self.root/'images.lock.yaml').exists() and not self.status()['uninstalled']
            if active:
                self.progress('backup')
                env = os.environ.copy()
                env['EMBY_CLOUD_LOCK_HELD'] = '1'
                env['EMBY_CLOUD_ROOT'] = str(self.root)
                self.command(['bash',str(source/'scripts/backup-full.sh')], env=env)
            files = sorted(p.relative_to(source).as_posix() for p in source.rglob('*') if p.is_file())
            old_files = old_marker.get('files', files)
            saved = self.state/'history'/stamp
            saved.mkdir(parents=True)
            originals = sorted(set(files + old_files + ['.env','images.lock.yaml','.emby-cloud.json']))
            for name in originals:
                current = self.root/name
                if current.is_symlink():
                    raise ValueError('Managed source path cannot be a symlink: '+name)
                if current.is_file():
                    dest = saved/name
                    dest.parent.mkdir(parents=True, exist_ok=True)
                    shutil.copy2(current, dest)
            if (self.root/'.git').exists():
                dirty = subprocess.check_output(['git','-C',str(self.root),'status','--porcelain','--untracked-files=no'],text=True)
                if dirty.strip():
                    raise ValueError('Tracked local modifications exist; preserve them before upgrading')
                shutil.move(str(self.root/'.git'), str(saved/'legacy-git'))
            self.progress('applying')
            try:
                if active:
                    self.command(self.dc('stop','-t','45','backend','webui','gateway','emby', *(['openstrm'] if 'cloud' in env_text else []), *(['openlist'] if 'source' in env_text else [])))
                for name in files:
                    dest = self.root/name
                    dest.parent.mkdir(parents=True, exist_ok=True)
                    shutil.copy2(source/name, dest)
                self.prepare_config(version, old_version is None)
                self.install_service()
                self.command(['python3',str(self.root/'scripts/lock_images.py')])
                self.resume_stack()
                self.progress('checking')
                self.command(['python3',str(self.root/'scripts/check.py')], timeout=90)
                if old_version is None:
                    self.initialize_cloud()
                write_json(self.root/'.emby-cloud.json', {'version':version,'database_schema':metadata['database_schema'],
                    'commit':metadata['commit'],'files':files,'uninstalled':False})
                self.progress('succeeded', target=release['tag_name'], message='Upgrade completed and health checks passed', error=None)
            except Exception:
                self.progress('rollback')
                try:
                    # Restore code and image selection only. Never overwrite live databases.
                    for name in originals:
                        dest, previous = self.root/name, saved/name
                        if previous.is_file():
                            dest.parent.mkdir(parents=True, exist_ok=True)
                            shutil.copy2(previous, dest)
                        elif dest.is_file():
                            dest.unlink()
                    if active:
                        self.resume_stack()
                    self.progress('rolled_back', message='Previous program and image selection restored; databases preserved')
                except Exception:
                    self.progress('recovery_required', message='Automatic program rollback failed; consult local operations.log')
                raise

    def prepare_config(self, version, fresh):
        env_path = self.root/'.env'
        if not env_path.exists():
            env = (self.root/'.env.example').read_text()
            for key, value in {'POSTGRES_PASSWORD':secrets.token_hex(32),'TWILIGHT_TWO_FACTOR_KEY':__import__('base64').b64encode(secrets.token_bytes(32)).decode(),'BOT_INTERNAL_SECRET':secrets.token_hex(32)}.items():
                env = env.replace(key+'=\n', key+'='+value+'\n')
            env += '\nCOMPOSE_PROFILES=cloud\n'
            env_path.write_text(env)
        lines = env_path.read_text().splitlines()
        for index, line in enumerate(lines):
            key, _, _value = line.partition('=')
            if key in {'BACKEND_IMAGE','WEBUI_IMAGE'}:
                lines[index] = key+'=emby-cloud-'+('backend' if key=='BACKEND_IMAGE' else 'webui')+':'+version
        env_path.write_text('\n'.join(lines)+'\n')
        env_path.chmod(0o600)
        for folder in ['twilight','postgres','emby','media','media/movies','media/tv','openstrm','openlist','backups']:
            path = self.root/(folder if folder=='backups' else 'runtime/'+folder)
            path.mkdir(parents=True,exist_ok=True)
        config = self.root/'runtime/twilight/config.toml'
        if not config.exists():
            shutil.copy2(self.root/'deploy/config.toml', config)
        for name, uid in [('twilight',10001),('emby',10002),('openstrm',10003),('media',10003),('media/movies',10003),('media/tv',10003)]:
            os.chown(self.root/'runtime'/name, uid, uid)
        os.chown(config,10001,10001)
        config.chmod(0o600)
        (self.root/'runtime/twilight').chmod(0o700)
        recipient = self.root/'runtime/backup-recipient.txt'
        if not recipient.exists():
            keydir = Path('/root/.config/emby-cloud')
            keydir.mkdir(parents=True,exist_ok=True,mode=0o700)
            identity = keydir/'backup-identity.agekey'
            if not identity.exists():
                self.command(['age-keygen','-o',str(identity)])
                identity.chmod(0o600)
            public = subprocess.check_output(['age-keygen','-y',str(identity)],text=True)
            recipient.write_text(public)

    def install_service(self):
        lib = Path('/usr/local/lib/emby-cloud')
        lib.mkdir(parents=True,exist_ok=True)
        shutil.copy2(self.root/'scripts/manager.py', lib/'manager.py')
        socketdir = SOCKET.parent
        socketdir.mkdir(parents=True,exist_ok=True)
        os.chown(socketdir,0,10001)
        socketdir.chmod(0o750)
        for name in ['emby-cloud-updater.service','emby-cloud-backup.service','emby-cloud-backup.timer','emby-cloud-certificate.service','emby-cloud-certificate.timer']:
            shutil.copy2(self.root/'deploy'/name, Path('/etc/systemd/system')/name)
        shutil.copy2(self.root/'deploy/emby-cloud-tmpfiles.conf', '/etc/tmpfiles.d/emby-cloud.conf')
        self.command(['systemd-tmpfiles','--create','/etc/tmpfiles.d/emby-cloud.conf'])
        self.command(['systemctl','daemon-reload'])
        self.command(['systemctl','enable','--now','emby-cloud-updater.service','emby-cloud-backup.timer'])

    def initialize_cloud(self):
        # Only fresh installations have the publicly known default account.
        def call(path, data, token=None):
            headers={'Content-Type':'application/json'}
            if token: headers['Authorization']='Bearer '+token
            request=urllib.request.Request('http://127.0.0.1:13001'+path,headers=headers,data=json.dumps(data).encode())
            with urllib.request.build_opener(urllib.request.ProxyHandler({})).open(request,timeout=20) as response:
                return json.load(response)
        token=call('/api/auth/login',{'username':'admin','password':'admin'})['token']
        password='Aa1-'+secrets.token_urlsafe(24)
        call('/api/auth/password',{'currentPassword':'admin','newPassword':password},token)
        write_json(self.root/'runtime/initial-credentials.json', {'openstrm_username':'admin','openstrm_password':password})

    def uninstall(self, purge=False, yes=False):
        self.validate_root()
        with (self.state/'update.lock').open('w') as lock:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            if purge and not yes:
                with open('/dev/tty','r+') as tty:
                    tty.write('Delete all project data and backups? Type DELETE /srv/emby-cloud: ')
                    tty.flush()
                    if tty.readline().strip() != 'DELETE /srv/emby-cloud':
                        raise ValueError('Purge cancelled')
            self.command(['systemctl','disable','--now','emby-cloud-updater.service','emby-cloud-backup.timer','emby-cloud-certificate.timer'],timeout=60)
            self.command(['systemctl','stop','emby-cloud-backup.service','emby-cloud-certificate.service'],timeout=120)
            if (self.root/'images.lock.yaml').exists():
                self.command(self.dc('--profile','cloud','--profile','source','--profile','tls','down','--remove-orphans'))
            for name in ['emby-cloud-updater.service','emby-cloud-backup.service','emby-cloud-backup.timer','emby-cloud-certificate.service','emby-cloud-certificate.timer']:
                (Path('/etc/systemd/system')/name).unlink(missing_ok=True)
            Path('/etc/tmpfiles.d/emby-cloud.conf').unlink(missing_ok=True)
            self.command(['systemctl','daemon-reload'])
            helper=Path('/usr/local/lib/emby-cloud/manager.py')
            helper.unlink(missing_ok=True)
            with contextlib.suppress(OSError):
                helper.parent.rmdir()
            marker = read_json(self.root/'.emby-cloud.json', {})
            marker['uninstalled'] = True
            write_json(self.root/'.emby-cloud.json', marker)
            if purge:
                if self.root != ROOT or self.root.resolve()!=ROOT or self.root.is_symlink():
                    raise ValueError('Purge path validation failed')
                shutil.rmtree(self.root)
                if self.state != STATE or self.state.resolve()!=STATE or self.state.is_symlink():
                    raise ValueError('Updater state path validation failed')
                shutil.rmtree(self.state)
            print('Uninstalled. '+('Project data removed.' if purge else 'Data and backups retained at /srv/emby-cloud; reinstall to resume.'))


class Server(socketserver.ThreadingMixIn, socketserver.UnixStreamServer):
    daemon_threads = True


class Handler(http.server.BaseHTTPRequestHandler):
    def log_message(self, *_args):
        pass

    def respond(self, status, data):
        encoded=json.dumps(data).encode()
        self.send_response(status)
        self.send_header('Content-Type','application/json')
        self.send_header('Content-Length',str(len(encoded)))
        self.end_headers()
        self.wfile.write(encoded)

    def dispatch(self):
        uid=struct.unpack('3i',self.connection.getsockopt(socket.SOL_SOCKET,socket.SO_PEERCRED,12))[1]
        if uid not in {0,10001}:
            return self.respond(403,{'error':'Forbidden peer'})
        try:
            if self.command=='GET' and self.path=='/network/status':
                return self.respond(200,self.server.manager.network().status())
            if self.command=='GET' and self.path=='/status':
                return self.respond(200,self.server.manager.status())
            length=int(self.headers.get('Content-Length','0'))
            if self.headers.get('Transfer-Encoding') or not 0 <= length <= 4096:
                return self.respond(400,{'error':'Invalid body'})
            body=json.loads(self.rfile.read(length) or b'{}')
            if self.command=='POST' and self.path=='/network/configure':
                return self.respond(200,self.server.manager.network().configure(body))
            if self.command=='POST' and self.path in {'/network/issue','/network/renew','/network/publish'} and set(body)=={'profile','expected_revision'}:
                return self.respond(202,self.server.manager.network().start(self.path.rsplit('/',1)[1],body['profile'],body['expected_revision']))
            if self.command=='POST' and self.path=='/check':
                return self.respond(200,self.server.manager.check())
            if self.command=='POST' and self.path=='/credentials':
                return self.respond(200,self.server.manager.configure_credentials(body))
            if self.command=='POST' and self.path=='/apply' and set(body)=={'tag','expected_version'}:
                return self.respond(202,self.server.manager.start(body['tag'],body['expected_version']))
            self.respond(404,{'error':'Unsupported operation'})
        except (ValueError,KeyError,TypeError) as error:
            self.respond(409,{'error':str(error)})
        except Exception:
            self.respond(503,{'error':'Updater unavailable; inspect local service logs'})

    do_GET=dispatch
    do_POST=dispatch


def main():
    parser=argparse.ArgumentParser()
    parser.add_argument('action',choices=['install','upgrade','uninstall','serve','status','certificate-renew'])
    parser.add_argument('--version')
    parser.add_argument('--purge',action='store_true')
    parser.add_argument('--yes-delete-data',action='store_true')
    args=parser.parse_args()
    if os.geteuid()!=0:
        raise SystemExit('Run as root')
    os.umask(0o077)
    manager=Manager()
    if args.action=='certificate-renew':
        succeeded=True
        for profile in ['account','shop','playback']:
            if manager.network().read('desired-'+profile+'.json') or manager.network().read('applied-'+profile+'.json'):
                succeeded=manager.network().run('renew',profile) and succeeded
        if not succeeded:raise SystemExit(1)
        return
    if args.action=='serve':
        if (manager.root/'deploy/emby-cloud-certificate.timer').exists():
            manager.install_network_units()
            manager.network().recover()
        if read_json(manager.status_file,{}).get('phase') in {'queued','download','backup','applying','checking','rollback'}:
            manager.progress('interrupted',error='Updater restarted before completion; inspect current health and retry')
        SOCKET.parent.mkdir(parents=True,exist_ok=True)
        os.chown(SOCKET.parent,0,10001)
        SOCKET.parent.chmod(0o750)
        SOCKET.unlink(missing_ok=True)
        with Server(str(SOCKET),Handler) as server:
            server.manager=manager
            os.chown(SOCKET,0,10001)
            SOCKET.chmod(0o660)
            server.serve_forever()
    elif args.action in {'install','upgrade'}:
        try:
            manager.install(args.version)
        except Exception as error:
            if read_json(manager.status_file,{}).get('phase') not in {'rolled_back','recovery_required'}:
                manager.progress('failed',error=manager.failure_message(error))
            raise
        print('Ready: forward SSH ports 13000 / 13001 / 18096 to localhost. Complete Owner setup and MFA in the panel.')
        manager.command(['systemctl','try-restart','emby-cloud-updater.service'],timeout=60)
    elif args.action=='uninstall':
        manager.uninstall(args.purge,args.yes_delete_data)
    else:
        print(json.dumps(manager.status(),indent=2))


if __name__=='__main__':
    main()
