#!/usr/bin/env bash
# Consistent application snapshot. Does not copy PostgreSQL's live data directory.
set -Eeuo pipefail
umask 077
cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.."
[[ $EUID == 0 ]] || { echo 'Run as root'; exit 1; }
command -v age >/dev/null
mkdir -p backups
exec 9>runtime/.maintenance.lock
if [[ ${EMBY_CLOUD_LOCK_HELD:-0} != 1 ]]; then
  flock -n 9 || { echo 'Another maintenance operation is active'; exit 1; }
fi
recipient=${AGE_RECIPIENT:-$(cat runtime/backup-recipient.txt 2>/dev/null || true)}
[[ $recipient == age1* && $recipient != *[[:space:]]* ]] || { echo 'Set AGE_RECIPIENT or runtime/backup-recipient.txt'; exit 1; }
dc=(docker compose --profile cloud --profile source -f compose.yaml -f images.lock.yaml)
stamp=$(date -u +%Y%m%dT%H%M%SZ)
stage=$(mktemp -d)
writers=()
restore_db="restorecheck_$(date +%s)_$$"
restore_created=false
cleanup() {
  rc=$?
  trap - EXIT
  if $restore_created; then "${dc[@]}" exec -T postgres dropdb -U twilight --if-exists "$restore_db" >/dev/null || rc=1; fi
  if ((${#writers[@]})); then "${dc[@]}" start "${writers[@]}" || rc=1; fi
  rm -rf -- "$stage"
  exit "$rc"
}
trap cleanup EXIT
while read -r service; do
  case "$service" in backend|webui|gateway|emby|openstrm|openlist) writers+=("$service");; esac
done < <("${dc[@]}" ps --services --filter status=running)
if ((${#writers[@]})); then "${dc[@]}" stop -t 45 "${writers[@]}"; fi
"${dc[@]}" exec -T postgres pg_dump -U twilight -d twilight -Fc > "$stage/twilight.dump"
"${dc[@]}" exec -T postgres pg_restore --list < "$stage/twilight.dump" >/dev/null
# Exercise the SQL restore without touching the application database.
"${dc[@]}" exec -T postgres createdb -U twilight "$restore_db"
restore_created=true
"${dc[@]}" exec -T postgres pg_restore --exit-on-error --no-owner -U twilight -d "$restore_db" < "$stage/twilight.dump"
"${dc[@]}" exec -T postgres dropdb -U twilight "$restore_db"
restore_created=false
python3 - <<'PY'
from pathlib import Path
import sqlite3
checked=0
for folder in ['emby','openstrm','openlist']:
    for path in (Path('runtime')/folder).rglob('*.db'):
        with path.open('rb') as stream:
            if stream.read(16) != b'SQLite format 3\x00': continue
        with sqlite3.connect(path.resolve().as_uri()+'?mode=ro',uri=True) as db:
            result=db.execute('PRAGMA quick_check').fetchall()
            if result != [('ok',)]: raise SystemExit('SQLite check failed: '+str(path))
        checked+=1
print('SQLite quick_check passed:', checked)
PY
printf 'Database restore smoke check: PASS\nStopped writers: %s\n' "${writers[*]}" > "$stage/verification.txt"
target="backups/full-$stamp.tar.gz.age"
tar --exclude=runtime/postgres --exclude=runtime/.maintenance.lock -czf - \
  .env compose.yaml images.lock.yaml VERSION deploy runtime \
  -C "$stage" twilight.dump verification.txt | age -r "$recipient" -o "$target.partial"
mv -- "$target.partial" "$target"
sha256sum "$target" > "$target.sha256"
echo "Encrypted snapshot ready: $target"
echo 'SQL restore smoke check passed. Keep an off-server copy and the private identity separately.'
