#!/usr/bin/env python3
"""Wait for this deployment's loopback Emby listener without exposing credentials."""
import json
import time
import urllib.request


def wait(timeout=90):
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        try:
            with opener.open('http://127.0.0.1:18096/System/Info/Public', timeout=3) as response:
                if response.status == 200 and json.loads(response.read(65536)).get('Version'):
                    return True
        except (OSError, ValueError):
            pass
        time.sleep(min(2, max(0, deadline - time.monotonic())))
    return False


if __name__ == '__main__':
    if not wait():
        raise SystemExit('Emby did not become ready within 90 seconds; other services will still resume')
    print('Emby HTTP listener is ready')
