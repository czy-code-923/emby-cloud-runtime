#!/usr/bin/env bash
set -Eeuo pipefail
umask 077
cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.."
[[ $EUID == 0 ]] || { echo 'Run as root'; exit 1; }
[[ $(uname -m) == x86_64 ]] || { echo 'This release targets amd64 only'; exit 1; }
command -v docker >/dev/null || { echo 'Install Docker Engine and Compose first'; exit 1; }
docker compose version >/dev/null
available=$(df -Pk . | awk 'NR==2 {print $4}')
(( available > 4194304 )) || { echo 'At least 4 GiB free disk required'; exit 1; }
if [[ ! -f .env ]]; then
    cp .env.example .env
    python3 - <<'PY'
from pathlib import Path
import base64,secrets
p=Path('.env'); s=p.read_text()
for k,v in {'POSTGRES_PASSWORD':secrets.token_hex(32),'TWILIGHT_TWO_FACTOR_KEY':base64.b64encode(secrets.token_bytes(32)).decode(),'BOT_INTERNAL_SECRET':secrets.token_hex(32)}.items():
    s=s.replace(k+'=\n',k+'='+v+'\n')
p.write_text(s)
PY
fi
chmod 600 .env
mkdir -p runtime/{twilight,postgres,emby,media,openstrm,openlist} backups
if [[ ! -f runtime/twilight/config.toml ]]; then cp deploy/config.toml runtime/twilight/config.toml; fi
chown 10001:10001 runtime/twilight
chown 10001:10001 runtime/twilight/config.toml
chmod 700 runtime/twilight
chown 10002:10002 runtime/emby
chmod 755 runtime/media
docker compose config --quiet
version=$(cat VERSION)
python3 - "$version" <<'PY'
from pathlib import Path
import sys
p=Path('.env'); lines=p.read_text().splitlines()
images={'BACKEND_IMAGE':'emby-cloud-backend:','WEBUI_IMAGE':'emby-cloud-webui:'}
for i,line in enumerate(lines):
    key,sep,value=line.partition('=')
    if key in images and value.startswith(images[key]): lines[i]=key+'='+images[key]+sys.argv[1]
p.write_text('\n'.join(lines)+'\n')
PY
release="https://github.com/czy-code-923/emby-cloud-runtime/releases/download/v$version"
mkdir -p .validation
curl --fail --location --retry 3 "$release/SHA256SUMS" -o .validation/SHA256SUMS
for component in backend webui; do
    asset="$component-$version-amd64.tar.gz"
    curl --fail --location --retry 3 "$release/$asset" -o ".validation/$asset"
    (cd .validation && grep -F "  $asset" SHA256SUMS | sha256sum --check --strict -)
    gzip -dc ".validation/$asset" | docker load
    rm -- ".validation/$asset"
done
mapfile -t registry_services < <(docker compose config --services | grep -vE '^(backend|webui)$')
docker compose pull "${registry_services[@]}"
# Pin the exact downloaded images before starting. No floating tag is used for later restarts.
python3 scripts/lock_images.py
docker compose -f compose.yaml -f images.lock.yaml up -d --wait --wait-timeout 240
curl --fail --silent http://127.0.0.1:13000/api/v2/setup/status
echo
echo 'Ready on loopback port 13000. Forward it over SSH before creating the owner account.'
