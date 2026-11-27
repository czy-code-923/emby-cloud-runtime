"""Fixed Cloudflare DNS-01 certificate and HTTPS-account ingress operations."""
from contextlib import contextmanager
from datetime import datetime, timezone
import fcntl
import http.client
import json
import os
from pathlib import Path
import re
import socket
import ssl
import subprocess
import threading
import time

DOMAIN=re.compile(r'(?=.{1,253}$)(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z](?:[a-z0-9-]{0,61}[a-z0-9])?$')

def validate_domain(value):
    value=str(value).lower().strip()
    if not DOMAIN.fullmatch(value):
        raise ValueError('Use an ASCII DNS hostname, without a scheme, port, wildcard or path')
    return value

def valid_profile(value):
    return value in {'account','shop','playback'} or bool(re.fullmatch(r'playback-[0-9a-f]{12}',str(value)))

class Network:
    def __init__(self,manager):
        self.manager=manager
        self.root=manager.root/'runtime/network'
        self.root.mkdir(parents=True,exist_ok=True,mode=0o700)
        self.guard=threading.Lock()
        self.thread=None

    def profile_ids(self):
        extra=set()
        for prefix in ('desired-','applied-'):
            for path in self.root.glob(prefix+'playback-*.json'):
                name=path.name[len(prefix):-5]
                if valid_profile(name): extra.add(name)
        return ['account','shop','playback']+sorted(extra)[:16]

    def read(self,name,default=None):
        try:return json.loads((self.root/name).read_text())
        except FileNotFoundError:return default if default is not None else {}

    def write(self,name,data):
        dest=self.root/name
        temp=dest.with_suffix('.tmp')
        temp.write_text(json.dumps(data,indent=2)+'\n')
        temp.chmod(0o600)
        temp.replace(dest)

    @contextmanager
    def exclusive(self):
        with (self.manager.state/'update.lock').open('a') as update, (self.manager.root/'runtime/.maintenance.lock').open('a') as maintenance:
            fcntl.flock(update,fcntl.LOCK_EX|fcntl.LOCK_NB)
            fcntl.flock(maintenance,fcntl.LOCK_EX|fcntl.LOCK_NB)
            yield

    def configure(self,body):
        profile=body.get('profile','account')
        if not valid_profile(profile):raise ValueError('Unsupported ingress profile')
        allowed={'domain','email','cloudflare_token','accept_terms','staging','profile','expected_revision'}
        if set(body)-allowed:raise ValueError('Unsupported configuration field')
        if profile.startswith('playback-') and profile not in self.profile_ids() and len(self.profile_ids())>=19:raise ValueError('At most 16 additional playback entrances are supported')
        domain=validate_domain(body.get('domain',''))
        email=str(body.get('email','')).strip()
        if not re.fullmatch(r'[^\s@]{1,64}@[^\s@]{1,190}\.[^\s@]{2,63}',email):raise ValueError('A valid certificate contact email is required')
        if type(body.get('accept_terms')) is not bool or type(body.get('staging')) is not bool:raise ValueError('Explicit terms and environment choices are required')
        token=body.get('cloudflare_token','')
        if token and not re.fullmatch(r'[A-Za-z0-9_-]{20,200}',token):raise ValueError('Invalid Cloudflare scoped API token')
        with self.exclusive():
            current=self.read('desired-'+profile+'.json')
            if type(body.get('expected_revision')) is not int or body['expected_revision']!=current.get('revision',0):raise ValueError('Configuration changed; reload before saving')
            credential_dir=self.root/'credentials';credential_dir.mkdir(exist_ok=True,mode=0o700)
            credentials=credential_dir/(domain+'.ini')
            if token:
                temp=credentials.with_suffix('.tmp')
                temp.write_text('dns_cloudflare_api_token = '+token+'\n')
                temp.chmod(0o600);temp.replace(credentials)
            if not credentials.exists() and not token and current.get('domain'):
                previous=credential_dir/(validate_domain(current['domain'])+'.ini')
                if previous.exists():
                    credentials.write_bytes(previous.read_bytes());credentials.chmod(0o600)
            if not credentials.exists() and profile.startswith('playback-'):
                parent=self.read('desired-playback.json') or self.read('desired-account.json')
                if parent.get('domain'):
                    previous=credential_dir/(validate_domain(parent['domain'])+'.ini')
                    if previous.exists():credentials.write_bytes(previous.read_bytes());credentials.chmod(0o600)
            if not credentials.exists():raise ValueError('Save a Cloudflare scoped DNS API token first')
            self.write('desired-'+profile+'.json',{'revision':current.get('revision',0)+1,'domain':domain,'email':email,'accept_terms':body['accept_terms'],'staging':body['staging'],'updated_at':int(time.time())})
        return self.status()

    def certificate(self,domain,staging=False):
        if not domain:return None
        path=self.root/'letsencrypt/live'/(('jw-staging-' if staging else 'jw-')+domain)/'fullchain.pem'
        if not path.exists():return None
        result=subprocess.run(['/usr/bin/openssl','x509','-in',str(path),'-noout','-enddate','-issuer','-serial'],capture_output=True,text=True,timeout=10)
        if result.returncode:return None
        fields=dict(line.split('=',1) for line in result.stdout.splitlines() if '=' in line)
        expiry=int(datetime.strptime(fields['notAfter'],'%b %d %H:%M:%S %Y %Z').replace(tzinfo=timezone.utc).timestamp())
        return {'domain':domain,'expires_at':expiry,'days_remaining':max(0,(expiry-int(time.time()))//86400),'issuer':fields.get('issuer',''),'serial':fields.get('serial',''),'staging':'STAGING' in fields.get('issuer','').upper()}

    def profile_status(self,profile):
        desired=self.read('desired-'+profile+'.json')
        applied=self.read('applied-'+profile+'.json')
        return {'configured':bool(desired),'credential_saved':bool(desired.get('domain') and (self.root/'credentials'/(desired['domain']+'.ini')).exists()),'desired':desired,'applied':applied,
                'certificate':self.certificate(desired.get('domain'),desired.get('staging',False)),'active_certificate':self.certificate(applied.get('domain'))}

    def status(self):
        profiles={profile:self.profile_status(profile) for profile in self.profile_ids()}
        renewal={'interval_hours':12,'enabled':False,'active':False}
        if Path('/run/systemd/system').exists():
            try:
                result=subprocess.run(['systemctl','show','emby-cloud-certificate.timer','--property=UnitFileState,ActiveState'],capture_output=True,text=True,timeout=3)
                fields=dict(line.split('=',1) for line in result.stdout.splitlines() if '=' in line)
                renewal.update(enabled=fields.get('UnitFileState')=='enabled',active=fields.get('ActiveState')=='active')
            except (OSError,subprocess.TimeoutExpired):pass
        return {**profiles['account'],'profiles':profiles,'job':self.read('job.json',{'phase':'idle'}),'automatic_renewal':renewal}

    def start(self,operation,profile,revision):
        if not valid_profile(profile):raise ValueError("Unsupported ingress profile")
        if type(revision) is not int or revision<0:raise ValueError('A configuration revision is required')
        if operation not in {'issue','renew','publish'}:raise ValueError('Unsupported certificate operation')
        with self.guard:
            if self.thread and self.thread.is_alive():raise ValueError('A network operation is already running')
            self.write('job.json',{'phase':'queued','operation':operation,'profile':profile,'updated_at':int(time.time())})
            self.thread=threading.Thread(target=self.run,args=(operation,profile,revision),daemon=True)
            self.thread.start()
        return self.status()

    def run(self,operation,profile="account",revision=None):
        try:
            with self.exclusive():
                if revision is not None and self.read('desired-'+profile+'.json').get('revision',0)!=revision:raise ValueError('Configuration revision changed')
                self.write('job.json',{'phase':'running','operation':operation,'profile':profile,'updated_at':int(time.time())})
                if operation=='publish':self.publish(profile)
                else:self.issue(renew=operation=='renew',profile=profile)
                self.write('job.json',{'phase':'succeeded','operation':operation,'profile':profile,'updated_at':int(time.time())})
                return True
        except BlockingIOError:
            self.write('job.json',{'phase':'failed','operation':operation,'error':'Another upgrade, backup or network operation is active; retry later','updated_at':int(time.time())})
        except Exception:
            import traceback
            with (self.manager.state/'operations.log').open('a') as log:traceback.print_exc(file=log)
            self.write('job.json',{'phase':'failed','operation':operation,'error':'Certificate or ingress operation failed; check the protected host log','updated_at':int(time.time())})

        return False

    def recover(self):
        try:
            with self.exclusive():
                pending=self.read('pending-publish.json')
                if pending:
                    self.restore_ingress(pending)
                    (self.root/'pending-publish.json').unlink(missing_ok=True)
                job=self.read('job.json')
                if pending or job.get('phase') in {'running','queued'}:
                    self.write('job.json',{'phase':'interrupted','error':'Executor restarted; previous verified ingress restored. Review and retry.','updated_at':int(time.time())})
        except BlockingIOError:pass

    def restore_ingress(self,snapshot):
        env=self.manager.root/'.env';env.write_text(snapshot['env']);env.chmod(0o600)
        config=self.root/'Caddyfile'
        if snapshot['config'] is None:
            self.manager.command(self.edge_command('stop','edge'),timeout=30)
        else:
            config.write_text(snapshot['config']);config.chmod(0o600)
            self.manager.command(self.edge_command('up','-d','--no-deps','edge'),timeout=60)
            self.reload_edge()
        for profile,applied in snapshot.get('applied',{}).items():self.write('applied-'+profile+'.json',applied)
        self.manager.command(self.manager.dc('up','-d','--no-deps','--force-recreate','gateway'),timeout=60)

    def issue(self,renew=False,profile="account"):
        desired=self.read('desired-'+profile+'.json')
        if renew and self.read('applied-'+profile+'.json').get('domain'):
            desired={**desired, 'domain':self.read('applied-'+profile+'.json')['domain'], 'staging':False, 'accept_terms':True}
        if not desired or not desired.get('accept_terms'):raise ValueError('Certificate terms must be accepted')
        domain=validate_domain(desired['domain'])
        cert_name=('jw-staging-' if desired['staging'] else 'jw-')+domain
        if not Path('/usr/bin/certbot').exists():
            self.manager.command(['apt-get','update','-qq'],timeout=900)
            self.manager.command(['apt-get','install','-y','certbot','python3-certbot-dns-cloudflare','openssl'],timeout=900)
        args=['/usr/bin/certbot']
        directories=['--config-dir',str(self.root/'letsencrypt'),'--work-dir',str(self.root/'work'),'--logs-dir',str(self.root/'logs')]
        if renew:
            if not self.certificate(domain,desired['staging']):raise ValueError('Issue the certificate before renewing it')
            args+=['renew','--cert-name',cert_name,'--non-interactive','--no-random-sleep-on-renew']+directories
        else:
            args+=['certonly','--non-interactive','--agree-tos','--email',desired['email'],'--dns-cloudflare','--dns-cloudflare-credentials',str(self.root/'credentials'/(domain+'.ini')),'--dns-cloudflare-propagation-seconds','30','--cert-name',cert_name,'--keep-until-expiring','-d',domain]+directories
            if desired['staging']:args+=['--staging']
        self.manager.command(args,timeout=900)
        certificate=self.certificate(domain,desired.get('staging',False))
        if not certificate:raise ValueError('Certificate output validation failed')
        if self.read('applied-'+profile+'.json').get('domain')==domain:
            self.reload_edge()

    def edge_command(self,*args):
        return ['docker','compose','--profile','tls','-f',str(self.manager.root/'compose.yaml'),'-f',str(self.manager.root/'images.lock.yaml'),*args]

    def reload_edge(self):
        self.manager.command(self.edge_command('exec','-T','edge','caddy','reload','--config','/etc/caddy/Caddyfile'),timeout=45)

    def publish(self,profile="account"):
        desired=self.read('desired-'+profile+'.json');domain=validate_domain(desired.get('domain',''))
        if desired.get('staging',False):raise ValueError('A staging configuration cannot be published')
        certificate=self.certificate(domain,desired['staging'])
        if not certificate or certificate['staging'] or certificate['expires_at']<=time.time()+3600:raise ValueError('A valid production certificate is required')
        applied=self.read('applied-'+profile+'.json')
        if not any(self.read('applied-'+key+'.json') for key in self.profile_ids()):
            with socket.socket() as listener:listener.bind(('0.0.0.0',443))
        sites={key:self.read('applied-'+key+'.json') for key in self.profile_ids()}
        sites[profile]={'domain':domain,'origin':'https://'+domain}
        if len({item['domain'] for item in sites.values() if item})!=sum(bool(item) for item in sites.values()):raise ValueError('Account and shop require separate hostnames')
        config=self.render_ingress(sites)
        compose=json.loads(subprocess.check_output(self.manager.dc('config','--format','json')))
        edge_image=compose['services'].get('edge',compose['services']['gateway'])['image']
        if not re.fullmatch(r'(?:docker\.io/(?:library/)?)?caddy@sha256:[0-9a-f]{64}',edge_image):raise ValueError('A pinned gateway image is required')
        path=self.root/'Caddyfile';previous=path.read_text() if path.exists() else None
        env_path=self.manager.root/'.env';previous_env=env_path.read_text()
        snapshot={'env':previous_env,'config':previous,'applied':{key:self.read('applied-'+key+'.json') for key in self.profile_ids()}}
        self.write('pending-publish.json',snapshot)
        try:
            path.write_text(config);path.chmod(0o600)
            # Owned template validation before opening the listener.
            self.manager.command(['docker','run','--rm','--network','none','-e','BOT_INTERNAL_SECRET=validation-placeholder-not-a-secret','-v',str(path)+':/etc/caddy/Caddyfile:ro','-v',str(self.root/'letsencrypt')+':/etc/letsencrypt:ro',edge_image,'caddy','validate','--config','/etc/caddy/Caddyfile'],timeout=60)
            lines=[line for line in previous_env.splitlines() if not line.startswith(('PUBLIC_SITE_ORIGIN=','PUBLIC_SHOP_ORIGIN=','EDGE_IMAGE=','COMPOSE_PROFILES='))]
            profiles=next((line.split('=',1)[1].split(',') for line in previous_env.splitlines() if line.startswith('COMPOSE_PROFILES=')),['cloud'])
            if 'tls' not in profiles:profiles.append('tls')
            lines+=['EDGE_IMAGE='+edge_image,'PUBLIC_SITE_ORIGIN='+sites.get('account',{}).get('origin','https://disabled.invalid'),'PUBLIC_SHOP_ORIGIN='+sites.get('shop',{}).get('origin','https://disabled.invalid'),'COMPOSE_PROFILES='+','.join(profiles)]
            env_path.write_text('\n'.join(lines)+'\n');env_path.chmod(0o600)
            self.manager.command(self.manager.dc('up','-d','--no-deps','--force-recreate','gateway'),timeout=60)
            self.manager.command(self.edge_command('up','-d','--no-deps','edge'),timeout=60)
            self.reload_edge()
            self.probe_origin(domain,profile)
            self.write('applied-'+profile+'.json',{'domain':domain,'origin':'https://'+domain,'admin_exposed':False,'published_at':int(time.time())})
            self.write('previous-publish.json',snapshot)
            (self.root/'pending-publish.json').unlink(missing_ok=True)
        except Exception:
            self.restore_ingress(snapshot)
            (self.root/'pending-publish.json').unlink(missing_ok=True)
            raise

    @staticmethod
    def render_ingress(sites):
        config="{\n admin 127.0.0.1:20191\n auto_https off\n}\n"
        for profile,site in sites.items():
            if not site:continue
            domain=validate_domain(site['domain'])
            config+='https://'+domain+' {\n tls /etc/letsencrypt/live/jw-'+domain+'/fullchain.pem /etc/letsencrypt/live/jw-'+domain+'/privkey.pem\n'
            if profile.startswith('playback'):
                # Fixed upstreams and a secret injected by Compose; the public client
                # can never choose the gateway identity or authentication target.
                config+=''' route {
  @management path /web /web/* /emby/web /emby/web/* /Users /Users/Public /emby/Users /emby/Users/Public /System/Configuration* /emby/System/Configuration* /Plugins* /emby/Plugins* /ScheduledTasks* /emby/ScheduledTasks* /swagger* /emby/swagger*
  respond @management "Private administration entrance required" 403
  forward_auth 127.0.0.1:13000 {
   uri /api/v2/internal/emby-access
   header_up -Origin
   header_up X-JW-Gateway {$BOT_INTERNAL_SECRET}
   header_up X-JW-Host {http.request.host}
   header_up X-JW-URI {http.request.uri}
   header_up X-JW-Method {http.request.method}
   header_up X-JW-Client-IP {http.request.remote.host}
  }
  @client_login path /Users/AuthenticateByName /emby/Users/AuthenticateByName
  handle @client_login {
   rewrite * /api/v2/internal/emby-login
   reverse_proxy 127.0.0.1:13000 {
    header_up X-JW-Gateway {$BOT_INTERNAL_SECRET}
    header_up X-JW-Client-IP {http.request.remote.host}
   }
  }
  handle {
   reverse_proxy 127.0.0.1:18096
  }
 }
'''
            else:
                routes='/miniapp /miniapp/* /api/v2/miniapp/*' if profile=='account' else '/shop /shop/* /api/v2/commerce/*'
                config+=' @public path '+routes+' /_next/* /brand/* /favicon* /api/v2/system/server-icon /api/v2/system/info /api/v2/system/health /api/v2/system/capabilities\n'
                config+=' handle @public {\n  reverse_proxy 127.0.0.1:13000 {\n   header_up Host {host}\n   header_up X-Forwarded-Proto https\n   header_down Set-Cookie "^(.+)$" "$1; Secure"\n  }\n }\n'
                config+=' redir / '+('/miniapp' if profile=='account' else '/shop')+' 302\n respond "Private or separate entrance required" 403\n'
            config+='}\n'
        return config

    @staticmethod
    def probe_origin(domain,profile='account'):
        # Validate the real local TLS chain and HTTP host; no certificate bypass.
        context=ssl.create_default_context()
        with socket.create_connection(('127.0.0.1',443),timeout=10) as raw:
            with context.wrap_socket(raw,server_hostname=domain) as tls:
                health_path='/System/Info/Public' if profile.startswith('playback') else '/api/v2/system/health'
                tls.sendall(('GET '+health_path+' HTTP/1.1\r\nHost: '+domain+'\r\nConnection: close\r\n\r\n').encode())
                response=http.client.HTTPResponse(tls);response.begin()
                if response.status!=200:raise ValueError('Origin health check failed')
