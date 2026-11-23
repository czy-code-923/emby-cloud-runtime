"""Read-only deployment checks; output excludes environments and credentials."""
import json
import socket
import subprocess
import urllib.request

def run(*args):
    return subprocess.check_output(args, text=True).strip()

opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
services = json.loads(run('docker','compose','-f','compose.yaml','-f','images.lock.yaml','config','--format','json'))['services']
report = {'services': {}, 'checks': {}}
passed = True
for name in services:
    cid = run('docker','compose','-f','compose.yaml','-f','images.lock.yaml','ps','-q',name)
    if not cid:
        report['services'][name] = {'running': False}
        passed = False
        continue
    data = json.loads(run('docker','inspect',cid))[0]
    state = data['State']
    ports = [p for values in data['HostConfig']['PortBindings'].values() for p in values] if data['HostConfig'].get('PortBindings') else []
    mounts = data['Mounts']
    item = {'running': state['Running'], 'health': state.get('Health',{}).get('Status','not-configured'),
            'oom_killed':state['OOMKilled'], 'restarts':data['RestartCount'],
            'image_id':data['Image'], 'ports':ports, 'memory_limit_mib':data['HostConfig']['Memory']//1048576}
    if name == 'emby':
        item['media_read_only'] = any(m['Destination']=='/media' and not m['RW'] for m in mounts)
        passed = passed and item['media_read_only']
    report['services'][name] = item
    passed = passed and item['running'] and not item['oom_killed'] and item['health'] != 'unhealthy'
    passed = passed and all(p['HostIp']=='127.0.0.1' for p in ports)
for name,url in {'controller':'http://127.0.0.1:13000/api/v2/system/health',
                 'emby':'http://127.0.0.1:18096/emby/System/Info/Public',
                 'cloud_settings':'http://127.0.0.1:13001/'}.items():
    if name=='cloud_settings' and 'openstrm' not in services: continue
    try:
        with opener.open(url, timeout=10) as response:
            report['checks'][name] = response.status
            passed = passed and response.status == 200
    except Exception as exc:
        report['checks'][name] = type(exc).__name__
        passed = False
report['checks']['docker_enabled'] = run('systemctl','is-enabled','docker') == 'enabled'
report['checks']['docker_active'] = run('systemctl','is-active','docker') == 'active'
report['passed'] = passed and all(report['checks'][k] for k in ['docker_enabled','docker_active'])
print(json.dumps(report, indent=2))
raise SystemExit(0 if report['passed'] else 1)
