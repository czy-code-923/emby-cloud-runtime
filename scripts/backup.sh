#!/usr/bin/env bash
set -Eeuo pipefail
umask 077
cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.."
command -v age >/dev/null || { echo 'Install age and provide AGE_RECIPIENT first'; exit 1; }
: "${AGE_RECIPIENT:?Set your off-server age public recipient}"
stamp=$(date -u +%Y%m%dT%H%M%SZ)
mkdir -p backups
tmp=$(mktemp -d)
trap 'rm -rf -- "$tmp"' EXIT
docker compose exec -T postgres pg_dump -U twilight -d twilight -Fc > "$tmp/twilight.dump"
docker compose exec -T postgres pg_restore --list < "$tmp/twilight.dump" >/dev/null
tar -czf - .env runtime/twilight images.lock.yaml -C "$tmp" twilight.dump | age -r "$AGE_RECIPIENT" -o "backups/controller-$stamp.tar.gz.age"
sha256sum "backups/controller-$stamp.tar.gz.age" > "backups/controller-$stamp.sha256"
echo 'Controller backup only. Emby/adapter databases require a coordinated stopped-service backup.'
