#!/usr/bin/env python3
"""Enable the requested admin-only Web login and role media quotas.

Reads and modifies only these two keys, preserving every other existing value.
Defaults to a plan; --apply stores a root-only pre-change backup.
"""
import argparse
import os
from pathlib import Path
import re
import time
import tomllib

def main():
 p=argparse.ArgumentParser();p.add_argument('--root',default='/srv/emby-cloud');p.add_argument('--apply',action='store_true');a=p.parse_args()
 path=Path(a.root)/'runtime/twilight/config.toml';metadata=path.stat();text=path.read_text();cfg=tomllib.loads(text)
 changes={'Global':{'admin_only':True},'DeviceLimit':{'media_limits_enabled':True}}
 for section,values in changes.items():
  match=re.search(r'(?ms)^\['+re.escape(section)+r'\]\s*\n(.*?)(?=^\[|\Z)',text)
  if not match:raise RuntimeError('Missing expected configuration section: '+section)
  body=match.group(1)
  for key,value in values.items():
   print(section+'.'+key+': '+str(cfg.get(section,{}).get(key))+' -> '+str(value),flush=True)
   pattern=r'(?m)^'+re.escape(key)+r'\s*=.*$';line=key+' = true'
   body=re.sub(pattern,line,body,count=1) if re.search(pattern,body) else body+'\n'+line+'\n'
  text=text[:match.start(1)]+body+text[match.end(1):]
 tomllib.loads(text)
 if a.apply:
  backup=path.with_name('config.before-member-boundary-'+str(int(time.time()))+'.toml');backup.write_bytes(path.read_bytes());backup.chmod(0o600)
  temporary=path.with_name('config.member-boundary.tmp');temporary.write_text(text);os.chown(temporary,metadata.st_uid,metadata.st_gid);temporary.chmod(0o600);temporary.replace(path)
  print('Saved. The backend hot reloads this configuration.',flush=True)
 else:print('Plan only. Pass --apply after the compatible release is installed.',flush=True)

if __name__=='__main__':main()
