#!/usr/bin/env python3
"""Root maintenance helper. Reuses protected DNS credentials; never prints them.

Only explicitly supplied account/shop/playback hosts are changed. Default is a
read-only plan. --apply configures DNS, issues DNS-01 TLS and publishes ingress.
"""
import argparse
import http.client
import json
import os
from pathlib import Path
import re
import socket
import time
import urllib.error
import urllib.request

class UnixHTTP(http.client.HTTPConnection):
    def connect(self):
        self.sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        self.sock.settimeout(30)
        self.sock.connect('/run/emby-cloud-updater/updater.sock')

def manager(path, body=None):
    c=UnixHTTP('localhost');c.request('GET' if body is None else 'POST',path,None if body is None else json.dumps(body),{'Content-Type':'application/json'})
    r=c.getresponse();data=json.loads(r.read());c.close()
    if r.status not in (200,202):raise RuntimeError('Deployment manager rejected '+path+': '+str(data.get('error','operation')))
    return data

def cloudflare(token, zone, path, body=None, method=None):
    req=urllib.request.Request('https://api.cloudflare.com/client/v4/zones/'+zone+path,data=None if body is None else json.dumps(body).encode(),headers={'Authorization':'Bearer '+token,'Content-Type':'application/json'},method=method or ('GET' if body is None else 'POST'))
    try:
        with urllib.request.urlopen(req,timeout=30) as r:data=json.load(r)
    except urllib.error.HTTPError as e:raise RuntimeError('Cloudflare request failed, HTTP '+str(e.code)) from None
    if not data.get('success'):raise RuntimeError('Cloudflare DNS operation rejected')
    return data['result']

def wait_job(profile):
    for _ in range(180):
        job=manager('/network/status')['job']
        if job.get('profile')==profile and job.get('phase') in ('failed','succeeded'):
            if job['phase']=='failed':raise RuntimeError('Certificate/ingress operation failed; inspect protected administrator diagnostics')
            return
        time.sleep(5)
    raise RuntimeError('Certificate operation still running; inspect status before retry')

def main():
    p=argparse.ArgumentParser(description=__doc__)
    p.add_argument('--root',default='/srv/emby-cloud');p.add_argument('--zone',required=True);p.add_argument('--ip',required=True)
    for name in ('account','shop','playback'):p.add_argument('--'+name,required=True)
    p.add_argument('--apply',action='store_true');args=p.parse_args()
    if not re.fullmatch('[0-9a-f]{32}',args.zone):raise RuntimeError('Invalid zone ID')
    import ipaddress
    ipaddress.IPv4Address(args.ip)
    hosts={key:getattr(args,key).lower() for key in ('account','shop','playback')}
    if len(set(hosts.values()))!=3 or any(not re.fullmatch(r'[a-z0-9.-]{3,253}',x) for x in hosts.values()):raise RuntimeError('Three distinct DNS hostnames required')
    print(json.dumps({'hosts':hosts,'proxy':{'account':True,'shop':True,'playback':False},'apply':args.apply}),flush=True)
    if not args.apply:return
    if os.geteuid()!=0:raise RuntimeError('Run through the existing root maintenance session')
    root=Path(args.root);net=root/'runtime/network'
    base=manager('/network/status')['profiles']['account']['desired']
    if not base.get('accept_terms') or base.get('staging'):raise RuntimeError('Production ACME consent must already be configured')
    cred=(net/'credentials'/(base['domain']+'.ini')).read_text()
    token=cred.split('=',1)[1].strip()
    # Configure exact-origin mutations before changing the live entrance.
    cfg=root/'runtime/twilight/config.toml'
    text=cfg.read_text();origins=['http://localhost:13000','http://127.0.0.1:13000']+['https://'+hosts[k] for k in ('account','shop')]
    import tomllib
    current=tomllib.loads(text);origins=list(dict.fromkeys(current.get('API',{}).get('cors_origins',[])+origins))
    pattern=r'(?m)^cors_origins\s*=.*$'
    if not re.search(pattern,text):raise RuntimeError('Expected configured CORS list not found; refusing broad configuration rewrite')
    backup=cfg.with_name('config.before-domain-'+str(int(time.time()))+'.toml');backup.write_text(text);backup.chmod(0o600)
    cfg.write_text(re.sub(pattern,'cors_origins = '+json.dumps(origins),text,count=1));cfg.chmod(0o600)
    for profile,host in hosts.items():
        records=cloudflare(token,args.zone,'/dns_records?name='+host)
        if any(r['type'] not in ('A','AAAA') for r in records):raise RuntimeError('Existing non-address record requires manual review: '+host)
        for r in records:
            if r['type']=='AAAA':raise RuntimeError('Existing IPv6 record requires matching origin configuration: '+host)
        if len(records)>1:raise RuntimeError('Multiple address records require manual review: '+host)
        record={'type':'A','name':host,'content':args.ip,'ttl':1,'proxied':profile!='playback'}
        cloudflare(token,args.zone,'/dns_records'+('/'+records[0]['id'] if records else ''),record,'PUT' if records else 'POST')
        dest=net/'credentials'/(host+'.ini')
        if not dest.exists():dest.write_text(cred);dest.chmod(0o600)
        state=manager('/network/status')['profiles'][profile]
        desired=manager('/network/configure',{'profile':profile,'domain':host,'email':base['email'],'accept_terms':True,'staging':False,'expected_revision':state['desired'].get('revision',0)})['profiles'][profile]['desired']
        for operation in ('issue','publish'):
            manager('/network/'+operation,{'profile':profile,'expected_revision':desired['revision']});wait_job(profile)
            print(profile+' '+operation+' succeeded',flush=True)
    print('All three HTTPS entrances published; administration remains private.',flush=True)

if __name__=='__main__':main()
