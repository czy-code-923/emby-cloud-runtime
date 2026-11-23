#!/usr/bin/env bash
set -Eeuo pipefail
[[ $EUID == 0 ]] || { echo '请以 root 运行，或使用 sudo bash'; exit 1; }
if [[ -f /usr/local/lib/emby-cloud/manager.py ]]; then
    exec python3 /usr/local/lib/emby-cloud/manager.py uninstall "$@"
fi
echo '未发现 Emby Cloud 升级执行器；保留原有目录，不删除其它服务。'
