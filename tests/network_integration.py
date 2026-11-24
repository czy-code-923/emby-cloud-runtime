"""Real Caddy/TLS test on a disposable CI host, with a local test CA only."""
from pathlib import Path
import importlib.util
import os
import socket
import ssl
import subprocess
import tempfile
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from http.client import HTTPResponse

assert os.environ.get('GITHUB_ACTIONS') == 'true', 'Disposable CI runner only'
repo = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('network_module', repo/'scripts/network.py')
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)

class Gateway(BaseHTTPRequestHandler):
    def do_GET(self):
        self.send_response(200)
        self.send_header('Set-Cookie', 'session=disposable-fixture; HttpOnly; Path=/')
        self.end_headers()
        self.wfile.write(b'fixture-health')
    def log_message(self, *args): pass

def command(*args):
    subprocess.run(list(args), check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)

with tempfile.TemporaryDirectory() as temporary:
    root = Path(temporary)
    command('openssl','req','-x509','-newkey','rsa:2048','-nodes','-days','1',
            '-subj','/CN=Disposable CI Root','-keyout',str(root/'ca.key'),'-out',str(root/'ca.pem'),
            '-addext','basicConstraints=critical,CA:TRUE','-addext','keyUsage=critical,keyCertSign,cRLSign')
    command('openssl','req','-newkey','rsa:2048','-nodes','-subj','/CN=jw.example.test',
            '-keyout',str(root/'key.pem'),'-out',str(root/'request.pem'))
    (root/'extensions').write_text('subjectAltName=DNS:jw.example.test,DNS:shop.example.test\nextendedKeyUsage=serverAuth\nbasicConstraints=critical,CA:FALSE\nkeyUsage=critical,digitalSignature,keyEncipherment\n')
    command('openssl','x509','-req','-in',str(root/'request.pem'),'-CA',str(root/'ca.pem'),
            '-CAkey',str(root/'ca.key'),'-CAcreateserial','-days','1','-out',str(root/'cert.pem'),
            '-extfile',str(root/'extensions'))
    for name in ['jw.example.test','shop.example.test']:
        certificate=root/'letsencrypt/live'/('jw-'+name)
        certificate.mkdir(parents=True)
        (certificate/'fullchain.pem').write_bytes((root/'cert.pem').read_bytes()+(root/'ca.pem').read_bytes())
        (certificate/'privkey.pem').write_bytes((root/'key.pem').read_bytes())
    (root/'Caddyfile').write_text(module.Network.render_ingress({
        'account':{'domain':'jw.example.test'},'shop':{'domain':'shop.example.test'}}))
    server=ThreadingHTTPServer(('127.0.0.1',13000),Gateway)
    threading.Thread(target=server.serve_forever,daemon=True).start()
    try:
        command('docker','run','-d','--name','jw-tls-check','--network','host',
                '-v',str(root/'Caddyfile')+':/etc/caddy/Caddyfile:ro',
                '-v',str(root/'letsencrypt')+':/etc/letsencrypt:ro','caddy:2-alpine')
        context=ssl.create_default_context(cafile=str(root/'ca.pem'))
        def request(host,path):
            with socket.create_connection(('127.0.0.1',443),timeout=5) as raw:
                with context.wrap_socket(raw,server_hostname=host) as tls:
                    tls.sendall(('GET '+path+' HTTP/1.1\r\nHost: '+host+'\r\nConnection: close\r\n\r\n').encode())
                    response=HTTPResponse(tls);response.begin()
                    return response.status, response.getheader('Set-Cookie','')
        for attempt in range(30):
            try:
                assert request('jw.example.test','/api/v2/system/health')[0]==200
                break
            except (OSError,AssertionError):
                if attempt==29:raise
                time.sleep(1)
        for path in ['/admin','/admin/telegram','/api/v2/admin/stats','/api/v2/system/stats',
                     '/api/v2/system/health/database','/api/v1/system/stats','/deployment/index.html',
                     '/shop','/api/v2/commerce/orders']:
            assert request('jw.example.test',path)[0]==403, path
        assert request('jw.example.test','/miniapp')[0]==200
        for path in ['/miniapp','/admin','/api/v2/admin/stats','/api/v2/miniapp/account']:
            assert request('shop.example.test',path)[0]==403, path
        for path in ['/shop','/login','/api/v2/auth/me','/api/v2/commerce/orders']:
            status,cookie=request('shop.example.test',path)
            assert status==200 and 'Secure' in cookie, (path,status)
        # The production probe uses the platform trust chain; only this process
        # installs the fixture CA, without any certificate verification bypass.
        os.environ['SSL_CERT_FILE']=str(root/'ca.pem')
        module.Network.probe_origin('jw.example.test')
        print('Verified TLS chain, account/shop/admin isolation and Secure cookies')
    finally:
        subprocess.run(['docker','rm','-f','jw-tls-check'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
        server.shutdown();server.server_close()
