#!/usr/bin/env python3
"""Check saved group IDs; repair a missing minus sign only after Telegram confirms it."""
import argparse
import json
import os
from pathlib import Path
import re
import time
import tomllib
import urllib.error
import urllib.request


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--root', default='/srv/emby-cloud')
    parser.add_argument('--apply', action='store_true')
    args = parser.parse_args()
    path = Path(args.root) / 'runtime/twilight/config.toml'
    text = path.read_text(encoding='utf-8')
    metadata = path.stat()
    cfg = tomllib.loads(text).get('Telegram', {})
    token = cfg.get('bot_token', '')
    if not token:
        raise SystemExit('No saved Bot credential; configure the Bot in the administrator panel.')
    base = cfg.get('telegram_api_url', 'https://api.telegram.org').rstrip('/')
    if base.endswith('/bot' + token):
        prefix = base
    elif base.endswith('/bot'):
        prefix = base + token
    else:
        prefix = base + '/bot' + token

    def call(method, payload):
        request = urllib.request.Request(prefix + '/' + method,
            data=json.dumps(payload).encode(), headers={'Content-Type': 'application/json'})
        try:
            with urllib.request.urlopen(request, timeout=20) as response:
                result = json.load(response)
        except urllib.error.HTTPError as error:
            if error.code in (400, 403):
                return None
            raise RuntimeError('Telegram service unavailable; no configuration changed.') from None
        except Exception:
            raise RuntimeError('Telegram connection unavailable; no configuration changed.') from None
        return result.get('result') if result.get('ok') else None

    me = call('getMe', {})
    if not me:
        raise SystemExit('Bot identity check failed; no configuration changed.')
    original = cfg.get('group_id', [])
    if not isinstance(original, list):
        raise SystemExit('Unexpected group list format; use the administrator configuration page.')
    revised = list(original)
    for index, value in enumerate(original):
        value = str(value).strip()
        actual = call('getChat', {'chat_id': value})
        candidate = '-' + value if re.fullmatch(r'100\d{6,}', value) else ''
        if actual is None and candidate:
            actual = call('getChat', {'chat_id': candidate})
            if actual and str(actual.get('id')) == candidate and actual.get('type') in ('group', 'supergroup'):
                membership = call('getChatMember', {'chat_id': candidate, 'user_id': me['id']})
                if membership and membership.get('status') in ('creator', 'administrator'):
                    revised[index] = candidate
                    print('Verified missing minus sign: ' + value + ' -> ' + candidate, flush=True)
                else:
                    print('Target group exists, but Bot administrator status is not confirmed; unchanged.', flush=True)
            else:
                print('Group could not be verified; unchanged.', flush=True)
        elif actual:
            print('Saved group is reachable: ' + str(actual.get('id')), flush=True)
        else:
            print('Saved group is unreachable; no automatic repair is safe.', flush=True)
    if revised == original:
        print('No configuration changes.', flush=True)
        return
    if not args.apply:
        print('Plan only. Use --apply to save verified group IDs.', flush=True)
        return
    match = re.search(r'(?ms)^\[Telegram\]\s*\n(.*?)(?=^\[|\Z)', text)
    if not match:
        raise SystemExit('Telegram section missing; no configuration changed.')
    body, count = re.subn(r'(?m)^group_id\s*=.*$', 'group_id = ' + json.dumps(revised), match.group(1), count=1)
    if count != 1:
        raise SystemExit('Group field is not a single-line list; use the configuration page.')
    updated = text[:match.start(1)] + body + text[match.end(1):]
    assert tomllib.loads(updated)['Telegram']['group_id'] == revised
    backup = path.with_name('config.before-group-repair-' + str(int(time.time())) + '.toml')
    backup.write_bytes(path.read_bytes())
    backup.chmod(0o600)
    temporary = path.with_name('config.group-repair.tmp')
    temporary.write_text(updated, encoding='utf-8')
    os.chown(temporary, metadata.st_uid, metadata.st_gid)
    temporary.chmod(0o600)
    temporary.replace(path)
    print('Verified group IDs saved; all enforcement switches preserved.', flush=True)


if __name__ == '__main__':
    main()
