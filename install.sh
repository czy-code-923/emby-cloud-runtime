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
    apt-get install -y ca-certificates curl python3 git age docker.io
    if apt-cache show docker-cli >/dev/null 2>&1; then apt-get install -y docker-cli; fi
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
import hashlib,json,pathlib,urllib.request,sys
repo='czy-code-923/emby-cloud-runtime'
req=urllib.request.Request('https://api.github.com/repos/'+repo+'/releases/latest',headers={'User-Agent':'emby-cloud-bootstrap'})
with urllib.request.urlopen(req,timeout=45) as response: release=json.load(response)
if release['draft'] or release['prerelease']: raise SystemExit('没有可安装的正式版本')
asset=next(a for a in release['assets'] if a['name']=='manager.py')
url='https://github.com/'+repo+'/releases/download/'+release['tag_name']+'/manager.py'
if asset['browser_download_url']!=url: raise SystemExit('发布来源不匹配')
with urllib.request.urlopen(url,timeout=45) as response: raw=response.read()
if len(raw)!=asset['size'] or 'sha256:'+hashlib.sha256(raw).hexdigest()!=asset.get('digest'): raise SystemExit('安装器校验失败')
(pathlib.Path(sys.argv[1])/'manager.py').write_bytes(raw)
PY
python3 "$stage/manager.py" "$action" "$@"
