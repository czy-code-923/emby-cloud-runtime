#!/usr/bin/env bash
# Public entry point: fixed GitHub repository, published stable assets only.
set -Eeuo pipefail
[[ $EUID == 0 ]] || { echo '请以 root 运行，或使用 sudo bash'; exit 1; }
[[ $(uname -s) == Linux && $(uname -m) == x86_64 ]] || { echo '目前支持 Linux amd64'; exit 1; }
action=${1:-install}
[[ $# == 0 ]] || shift
[[ $action == install || $action == upgrade || $action == uninstall ]] || { echo '用法：install | upgrade | uninstall [--purge]'; exit 1; }
if [[ $action != uninstall ]]; then
    [[ -f /etc/debian_version ]] || { echo '目前支持 Debian / Ubuntu'; exit 1; }
    apt-get update
    export DEBIAN_FRONTEND=noninteractive
    apt-get install -y ca-certificates curl python3 git age
    if ! command -v docker >/dev/null 2>&1; then
        apt-get install -y docker.io
        if apt-cache show docker-cli >/dev/null 2>&1; then apt-get install -y docker-cli; fi
    fi
    if ! docker compose version >/dev/null 2>&1; then
        if apt-cache show docker-compose-v2 >/dev/null 2>&1; then
            apt-get install -y docker-compose-v2
        else
            apt-get install -y docker-compose
        fi
    fi
    docker compose version | grep -Eq '(v?2\.|v?5\.)' || { echo '需要 Docker Compose v2 或更新版本'; exit 1; }
    systemctl enable --now docker
fi
if [[ $action == uninstall && -f /usr/local/lib/emby-cloud/manager.py ]]; then
    exec python3 /usr/local/lib/emby-cloud/manager.py uninstall "$@"
fi
stage=$(mktemp -d)
trap 'rm -rf -- "$stage"' EXIT
python3 - "$stage" <<'PY'
import hashlib,json,os,pathlib,urllib.request,urllib.error,sys
repo='czy-code-923/emby-cloud-runtime'
token=os.environ.get('GITHUB_TOKEN','')
class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self,*args,**kwargs):return None
opener=urllib.request.build_opener(NoRedirect())
def github(url,accept):
    headers={'User-Agent':'emby-cloud-bootstrap','Accept':accept}
    if token:headers['Authorization']='Bearer '+token
    for _ in range(5):
        try:return opener.open(urllib.request.Request(url,headers=headers),timeout=120)
        except urllib.error.HTTPError as error:
            if error.code not in (301,302,303,307,308):raise SystemExit('无法获取正式版本；私有仓库请提供有效的只读 GITHUB_TOKEN') from None
            target=error.headers.get('Location','')
            from urllib.parse import urlparse
            parsed=urlparse(target)
            if parsed.scheme!='https' or parsed.hostname not in ('release-assets.githubusercontent.com','objects.githubusercontent.com'):raise SystemExit('安装资产跳转来源不正确')
            url=target;headers={'User-Agent':'emby-cloud-bootstrap','Accept':accept}
    raise SystemExit('安装资产跳转次数过多')
with github('https://api.github.com/repos/'+repo+'/releases/latest','application/vnd.github+json') as response:release=json.load(response)
if release['draft'] or release['prerelease']:raise SystemExit('没有可安装的正式版本')
asset=next(a for a in release['assets'] if a['name']=='manager.py')
url='https://github.com/'+repo+'/releases/download/'+release['tag_name']+'/manager.py'
if asset['browser_download_url']!=url:raise SystemExit('发布来源不匹配')
request_url='https://api.github.com/repos/'+repo+'/releases/assets/'+str(asset['id']) if token else url
response=github(request_url,'application/octet-stream') if token else urllib.request.urlopen(url,timeout=120)
with response:raw=response.read()
if len(raw)!=asset['size'] or 'sha256:'+hashlib.sha256(raw).hexdigest()!=asset.get('digest'):raise SystemExit('安装器校验失败')
(pathlib.Path(sys.argv[1])/'manager.py').write_bytes(raw)
PY
python3 "$stage/manager.py" "$action" "$@"
