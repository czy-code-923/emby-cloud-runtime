#!/usr/bin/env python3
"""Synchronize member URLs after all three production TLS profiles are applied."""
import argparse
import importlib.util
import json
from pathlib import Path
import re
import time
import tomllib
import urllib.request

def main():
 p=argparse.ArgumentParser();p.add_argument('--root',default='/srv/emby-cloud');p.add_argument('--apply',action='store_true');a=p.parse_args()
 spec=importlib.util.spec_from_file_location('domains',Path(__file__).with_name('publish-domains.py'));module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)
 profiles=module.manager('/network/status')['profiles'];origins={k:v['applied'].get('origin','') for k,v in profiles.items()}
 if any(not origins.get(k,'').startswith('https://') for k in ('account','shop','playback')):raise RuntimeError('All production TLS profiles must be published first')
 path=Path(a.root)/'runtime/twilight/config.toml';text=path.read_text();cfg=tomllib.loads(text)
 changes={'Global':{'public_base_url':origins['account'],'commerce_store_url':origins['shop']},'Emby':{'emby_public_url':origins['playback']},'Telegram':{'mini_app_url':origins['account']+'/miniapp','shop_enabled':True}}
 print(json.dumps({'changes':changes,'apply':a.apply}),flush=True)
 if not a.apply:return
 for section,values in changes.items():
  match=re.search(r'(?ms)^\['+re.escape(section)+r'\]\s*\n(.*?)(?=^\[|\Z)',text)
  if not match:raise RuntimeError('Expected configuration section not found: '+section)
  body=match.group(1)
  for key,value in values.items():
   line=key+' = '+json.dumps(value)
   pattern=r'(?m)^'+re.escape(key)+r'\s*=.*$'
   body=re.sub(pattern,line,body,count=1) if re.search(pattern,body) else body+'\n'+line+'\n'
  text=text[:match.start(1)]+body+text[match.end(1):]
 tomllib.loads(text)
 backup=path.with_name('config.before-member-entrances-'+str(int(time.time()))+'.toml');backup.write_bytes(path.read_bytes());backup.chmod(0o600)
 path.write_text(text);path.chmod(0o600)
 token=cfg.get('Telegram',{}).get('bot_token','')
 if not token:raise RuntimeError('Saved Telegram bot token not found; configuration saved, publish menu in admin UI')
 request=urllib.request.Request('https://api.telegram.org/bot'+token+'/setChatMenuButton',data=json.dumps({'menu_button':{'type':'web_app','text':'见微面板','web_app':{'url':origins['account']+'/miniapp'}}}).encode(),headers={'Content-Type':'application/json'})
 try:
  with urllib.request.urlopen(request,timeout=30) as response:result=json.load(response)
 except Exception:raise RuntimeError('Configuration saved; Telegram menu update needs retry from administrator UI') from None
 if not result.get('ok'):raise RuntimeError('Telegram did not confirm the new menu; use administrator UI to retry')
 print('Member URLs saved and Telegram Mini App menu confirmed.',flush=True)

if __name__=='__main__':main()
