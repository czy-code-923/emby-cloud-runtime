#!/usr/bin/env python3
"""Read-only live deployment audit; explicit flags refresh ingress or Emby name."""
import argparse
import importlib.util
import json
from pathlib import Path
import socket
import subprocess
import sys
import tomllib
import urllib.error
import urllib.request

def http(url,token='',body=None):
 headers={'User-Agent':'jianwei-deployment-audit'}
 if token:headers['X-Emby-Token']=token
 if body is not None:headers['Content-Type']='application/json'
 req=urllib.request.Request(url,headers=headers,data=None if body is None else json.dumps(body).encode())
 try:
  with urllib.request.urlopen(req,timeout=15) as r:return r.status,r.read()
 except urllib.error.HTTPError as e:return e.code,b''

def main():
 p=argparse.ArgumentParser();p.add_argument('--root',default='/srv/emby-cloud');p.add_argument('--refresh-ingress',action='store_true');p.add_argument('--brand-emby',action='store_true');a=p.parse_args()
 root=Path(a.root);cfg=tomllib.loads((root/'runtime/twilight/config.toml').read_text())
 sys.path.insert(0,str(root/'scripts'));from manager import Manager
 manager=Manager(root);network=manager.network()
 if a.refresh_ingress:
  for profile in ('account','shop','playback'):
   state=network.profile_status(profile)
   if not state['applied']:continue
   if state['desired'].get('domain')!=state['applied'].get('domain'):raise RuntimeError('Unapplied domain change; refusing automatic publication')
   if not network.run('publish',profile,state['desired']['revision']):raise RuntimeError('Ingress refresh failed; protected host log contains details')
  print('Existing HTTPS entrances refreshed with the installed allowlists.',flush=True)
 token=cfg.get('Emby',{}).get('emby_token','')
 if a.brand_emby:
  status,body=http('http://127.0.0.1:18096/System/Configuration',token)
  if status!=200:raise RuntimeError('Unable to read Emby configuration')
  configuration=json.loads(body);configuration['ServerName']=cfg.get('Global',{}).get('server_name','见微 Emby')
  status,_=http('http://127.0.0.1:18096/System/Configuration',token,configuration)
  if status not in (200,204):raise RuntimeError('Emby name update was not confirmed')
 info=manager.status();state=network.status()
 report={'version':info['installed_version'],'job_phase':info['job'].get('phase'),'configuration':{},'entrances':{},'private_ports':{}}
 for section,keys in {'Global':['admin_only','public_base_url','commerce_store_url'],'Telegram':['mini_app_url','shop_enabled','banner_enabled','auto_provision'],'Emby':['emby_public_url'],'SAR':['signin_enabled','signin_auto_renewal_enabled','emby_user_limit'],'MediaRequest':['enabled'],'Database':['migration_panel_enabled']}.items():
  report['configuration'][section]={k:cfg.get(section,{}).get(k) for k in keys}
 for profile,s in state['profiles'].items():
  host=s['applied'].get('domain');entry={'host':host,'certificate_days':(s.get('active_certificate') or {}).get('days_remaining'),'admin_exposed':s['applied'].get('admin_exposed')}
  if host:
   network.probe_origin(host,profile)
   path='/System/Info/Public' if profile=='playback' else '/api/v2/system/health'
   entry['https_health']=http('https://'+host+path)[0]
   entry['management_blocked']=http('https://'+host+('/web/index.html' if profile=='playback' else '/admin'))[0]==403
  report['entrances'][profile]=entry
 for port in (13000,13001,18096):
  result=subprocess.run(['ss','-ltnH','sport = :'+str(port)],capture_output=True,text=True,timeout=10)
  lines=result.stdout.splitlines();report['private_ports'][port]=bool(lines) and all('127.0.0.1:'+str(port) in line for line in lines)
 status,body=http('http://127.0.0.1:18096/System/Info/Public');report['emby']={'http':status,'name':json.loads(body).get('ServerName') if status==200 else ''}
 report['automatic_renewal']=state['automatic_renewal']
 print(json.dumps(report,ensure_ascii=False,indent=2),flush=True)

if __name__=='__main__':main()
