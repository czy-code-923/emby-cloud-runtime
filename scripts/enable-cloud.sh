#!/usr/bin/env bash
set -Eeuo pipefail
umask 077
cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.."
[[ $EUID == 0 ]] || { echo 'Run as root'; exit 1; }
mkdir -p runtime/media/movies runtime/media/tv runtime/openstrm
chown 10003:10003 runtime/media runtime/media/movies runtime/media/tv runtime/openstrm
chmod 755 runtime/media runtime/media/movies runtime/media/tv
chmod 700 runtime/openstrm
python3 - <<'PY'
from pathlib import Path
p=Path('.env'); lines=p.read_text().splitlines()
for i,line in enumerate(lines):
    if line.startswith('COMPOSE_PROFILES='):
        values=[v for v in line.split('=',1)[1].split(',') if v]
        if 'cloud' not in values: values.append('cloud')
        lines[i]='COMPOSE_PROFILES='+','.join(values)
        break
else: lines.append('COMPOSE_PROFILES=cloud')
p.write_text('\n'.join(lines)+'\n')
PY
docker compose pull openstrm
python3 scripts/lock_images.py
docker compose -f compose.yaml -f images.lock.yaml up -d openstrm
echo 'OpenStrm is loopback-only on 13001; change its default password before adding cloud accounts.'
