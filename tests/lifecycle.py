"""Destructive only on a fresh disposable CI runner; never run on the VPS."""
from pathlib import Path
import contextlib
import http.cookiejar
import importlib.util
import json
import os
import shutil
import subprocess
import tarfile
import tempfile
import urllib.error
import urllib.request
from unittest.mock import patch

repo=Path(__file__).resolve().parents[1]
spec=importlib.util.spec_from_file_location('manager',repo/'scripts/manager.py')
m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
assert os.environ.get('GITHUB_ACTIONS')=='true' and os.geteuid()==0, 'Disposable GitHub runner only'
assert not m.ROOT.exists(), 'Refusing to touch an existing deployment'
base=(repo/'VERSION').read_text().strip()
temp=Path(tempfile.mkdtemp(prefix='emby-cloud-ci-'))
fixture=temp/'assets';fixture.mkdir()
source=temp/'source'
m.unpack_source(repo/'dist'/f'emby-cloud-{base}-source.tar.gz',source)
release={'tag_name':'v'+base}

def prepare(version):
    release['tag_name']='v'+version
    (source/'VERSION').write_text(version+'\n')
    for component in ['backend','webui']:
        subprocess.run(['docker','tag',f'emby-cloud-{component}:{base}',f'emby-cloud-{component}:{version}'],check=True)
        # docker load already happened during the real first installation.
        shutil.copy2(repo/'dist'/f'{component}-{base}-amd64.tar.gz',fixture/f'{component}-{version}-amd64.tar.gz')
    with tarfile.open(fixture/f'emby-cloud-{version}-source.tar.gz','w:gz') as archive:
        archive.add(source,arcname='emby-cloud')
    (fixture/'release.json').write_text(json.dumps({'version':version,'commit':'ci-fixture','protocol':1,'database_schema':1,'rollback_safe':True}))

def copy_asset(_release,name,dest):shutil.copy2(fixture/name,dest)
manager=m.Manager()
jar=http.cookiejar.CookieJar()
op=urllib.request.build_opener(urllib.request.ProxyHandler({}),urllib.request.HTTPCookieProcessor(jar))
def api(path,data=None):
    req=urllib.request.Request('http://127.0.0.1:13000'+path,
        headers={'Content-Type':'application/json','X-Twilight-Client':'webui','X-Twilight-Intent':'complete-setup'},
        data=json.dumps(data).encode() if data is not None else None)
    with op.open(req,timeout=30) as response:return json.load(response)

with patch.object(m,'release_info',side_effect=lambda *_args:dict(release)),patch.object(m,'download_asset',side_effect=copy_asset):
    prepare(base)
    manager.install('v'+base)
    assert manager.status()['installed_version']==base
    api('/api/v2/setup/complete',{'admin':{'username':'ci-owner','password':'CI-Disposable-Password123!'}})
    assert api('/api/v2/auth/me')['data']['username']=='ci-owner'
    # Check the actual container-to-host Unix socket route.
    assert api('/api/v2/admin/deployment/status')['data']['installed_version']==base
    env_before=(m.ROOT/'.env').read_bytes()
    sentinel=m.ROOT/'runtime/media/movies/ci-preserve.txt';sentinel.write_text('keep media and configuration')
    prepare('0.2.1')
    manager.install('v0.2.1')
    assert (m.ROOT/'.env').read_bytes().split(b'POSTGRES_PASSWORD=',1)[1].splitlines()[0]==env_before.split(b'POSTGRES_PASSWORD=',1)[1].splitlines()[0]
    assert sentinel.read_text()=='keep media and configuration'
    api('/api/v2/auth/login',{'username':'ci-owner','password':'CI-Disposable-Password123!'})
    assert api('/api/v2/admin/health/database')['data']['user_count']==1
    assert manager.status()['installed_version']=='0.2.1'
    prepare('0.2.2')
    original=manager.command
    def injected(args,**kwargs):
        if any(str(arg).endswith('/scripts/check.py') for arg in args):raise RuntimeError('Injected post-start health failure')
        return original(args,**kwargs)
    with patch.object(manager,'command',side_effect=injected):
        try:manager.install('v0.2.2')
        except RuntimeError:pass
        else:raise AssertionError('Health failure must abort the upgrade')
    assert manager.status()['installed_version']=='0.2.1'
    assert manager.status()['job']['phase']=='rolled_back'
    assert sentinel.exists()
    api('/api/v2/auth/login',{'username':'ci-owner','password':'CI-Disposable-Password123!'})
    assert api('/api/v2/admin/health/database')['data']['user_count']==1
    subprocess.run(['docker','run','-d','--name','unrelated-canary','busybox:1.37','sleep','900'],check=True)
    manager.uninstall()
    assert sentinel.exists() and (m.ROOT/'.env').exists()
    assert json.loads(subprocess.check_output(['docker','inspect','unrelated-canary']))[0]['State']['Running']
    manager.install('v0.2.1')
    api('/api/v2/auth/login',{'username':'ci-owner','password':'CI-Disposable-Password123!'})
    assert api('/api/v2/admin/health/database')['data']['user_count']==1
    assert sentinel.exists()
    manager.uninstall(purge=True,yes=True)
    assert not m.ROOT.exists()
print('PASS: fresh install, Unix proxy, data-preserving upgrade, health-failure rollback, uninstall, reinstall, explicit purge and unrelated-service preservation')
