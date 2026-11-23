# 访问与配置

部署目录 `/srv/emby-cloud`。通过 ServerBox 保持 dmit-pro SSH 连接，开启仅绑定本机 `127.0.0.1` 的三条本地转发：13000 管理后台，13001 OpenStrm，18096 Emby。关闭 SSH 转发后，这些入口不会继续在本机可用。

管理后台 `http://127.0.0.1:13000/admin`，系统配置可绑定 Telegram、Emby、TMDB、邮件。Owner 首次登录后在账户设置中绑定个人验证器，离线保存恢复码。随机初始密码通过受限的本机使用说明交付，仓库不保存密码。

OpenStrm `http://127.0.0.1:13001`，在“账户”添加网盘。电影任务的输出目录为 `/app/data/movies`，剧集为 `/app/data/tv`；同一目录在 Emby 中分别映射为 `/media/movies`、`/media/tv`。网盘账号、目录与实际播放未经验证前，不开启删除同步。匿名 302、媒体预热、自动网盘轮询默认关闭。网盘请求预算为每秒1次、1个并行请求、1个并行下载，可在实测后调整。

Emby `http://127.0.0.1:18096`，已经建立电影和剧集空库。管理员默认禁用音视频转码，适合本机资源下的直接播放验证；新增用户要独立检查其转码权限。未绑定真实网盘时没有可播放媒体。

Telegram 由 Twilight 单独收件；不要把同一个 Bot Token 同时交给 OpenStrm 开启轮询。Telegram 购买外链、现金结账不在本版本中启用。

域名尚未发布。管理后台中的播放地址字段不等于 DNS/TLS 已生效；实际域名发布必须验证 DNS、证书和反代入口，并同步 CORS、Cookie 与入口 Origin 白名单。当前入口只接受本机13000的浏览器写请求。

# 检查、备份与恢复

`python3 scripts/check.py` 检查已启用服务、容器重启/OOM、回环监听、只读媒体挂载和HTTP入口，输出不含环境变量或凭据。

`bash scripts/backup-full.sh` 停止本项目写服务，导出 PostgreSQL、在独立临时库中验证恢复，并检查 Emby/OpenStrm SQLite，随后打包配置、状态和镜像锁并用 age 加密。退出时恢复原来运行的服务。媒体原文件位于网盘，不包含在这个备份保证中。

目标机的 `emby-cloud-backup.timer` 每天北京时间04:20后随机0～5分钟执行本机快照。该操作会短暂停止本项目；它不影响宿主机其它服务。`systemctl status emby-cloud-backup.timer` 查看计划，`journalctl -u emby-cloud-backup.service` 查看结果。快照不自动清理，需结合可用磁盘按明确的保留策略整理；当前没有异地自动同步。

备份接收公钥在 `runtime/backup-recipient.txt`，私钥仅在管理员本机。解密用 `age -d -i backup-identity.agekey -o snapshot.tar.gz full-....tar.gz.age`。先在隔离目录核对内容；数据库通过 `pg_restore` 恢复到新库，配置与 Emby/OpenStrm 状态在目标服务停止时恢复。不得把快照直接覆盖到仍在产生新数据的生产目录。当前验证包括临时SQL库恢复、本机解密和SQLite检查，未声称完成整机灾难恢复演练。

不启用自动升级。升级前先备份，确认新版本发布检查通过，再运行安装脚本。镜像摘要保存在不入库的 `images.lock.yaml`，重启使用 `docker compose -f compose.yaml -f images.lock.yaml ...`。固定源码、依赖版本和已发布构建可追踪；不同日期重新构建的系统包不保证得到相同二进制摘要。
