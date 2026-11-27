#!/usr/bin/env python3
"""Refresh existing HTTPS route templates after a program upgrade, no DNS or certificate changes."""
from pathlib import Path
import sys
sys.path.insert(0,str(Path(__file__).resolve().parent))
from manager import Manager

def refresh(root=Path('/srv/emby-cloud')):
 manager=Manager(root);network=manager.network()
 sites={key:network.read('applied-'+key+'.json') for key in network.profile_ids()}
 if not any(sites.values()):return
 path=network.root/'Caddyfile'
 previous=path.read_text();candidate=network.render_ingress(sites)
 if previous==candidate:return
 try:
  path.write_text(candidate);path.chmod(0o600)
  manager.command(network.edge_command('exec','-T','edge','caddy','validate','--config','/etc/caddy/Caddyfile'),timeout=45)
  network.reload_edge()
  for profile,site in sites.items():
   if site:network.probe_origin(site['domain'],profile)
 except Exception:
  path.write_text(previous);path.chmod(0o600);network.reload_edge();raise
 print('Refreshed existing HTTPS route templates; pending domain changes preserved')
if __name__=='__main__':refresh(Path(sys.argv[1]) if len(sys.argv)>1 else Path('/srv/emby-cloud'))
