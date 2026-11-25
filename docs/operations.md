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

备份接收公钥在 `runtime/backup-recipient.txt`，本次dmit-pro私钥保存在管理员本机；全新一键安装若尚未指定接收方，会在服务器 `/root/.config/emby-cloud/backup-identity.agekey` 生成独立私钥，应由管理员另存到安全的离线位置。解密用 `age -d -i backup-identity.agekey -o snapshot.tar.gz full-....tar.gz.age`。先在隔离目录核对内容；数据库通过 `pg_restore` 恢复到新库，配置与 Emby/OpenStrm 状态在目标服务停止时恢复。不得把快照直接覆盖到仍在产生新数据的生产目录。当前验证包括临时SQL库恢复、本机解密和SQLite检查，未声称完成整机灾难恢复演练。

管理员可在“系统运维 → 版本与升级”检查正式Release，点击“一键拉取并升级”并验证当前密码；执行器自动备份和检查。未确认时不会自动升级。镜像摘要保存在不入库的 `images.lock.yaml`，重启使用 `docker compose -f compose.yaml -f images.lock.yaml ...`。固定源码、依赖版本和已发布构建可追踪；不同日期重新构建的系统包不保证得到相同二进制摘要。


# 发布升级执行器

`emby-cloud-updater.service` 仅监听 `/run/emby-cloud-updater/updater.sock`；socket只允许root与后台UID10001访问，网页通过原管理员鉴权与当前密码复核后发起固定版本操作。状态在 `/var/lib/emby-cloud-updater/status.json`，详细主机日志在同目录 `operations.log`，不通过普通用户接口分发。

升级只取本仓库的正式Release，验证GitHub资产摘要、来源、源码版本和数据库兼容标记。预下载和备份通过后才替换程序；失败回退程序与镜像，保留当前业务数据库。需要数据库不兼容迁移的发布不能直接点击应用。升级进程重启会标记中断，不会自动再次执行未知进度的操作。

从早期alpha部署迁移时，未改动的Git元数据保存在升级历史中；今后使用Release更新，无需在生产目录Git拉取。本地跟踪文件有改动时会拒绝覆盖，需管理员先处理。生产VPS不执行卸载演示；卸载、重装和彻底清理在独立CI环境验证。

v0.2.2新增的tmpfiles配置用于在开机时先创建socket目录，并在执行器重启时保留目录，使后台容器的只读挂载持续有效。首次从v0.2.1使用旧执行器升级后，应补装这份新增宿主机配置；本次dmit-pro已完成并验证执行器重启后面板状态接口正常。全新v0.2.2安装与以后升级会自动安装它：

```bash
cd /srv/emby-cloud
cp deploy/emby-cloud-tmpfiles.conf /etc/tmpfiles.d/emby-cloud.conf
systemd-tmpfiles --create /etc/tmpfiles.d/emby-cloud.conf
```


# 站点、卡码与Telegram入口

系统运维→站点与入口可记录网站HTTPS域名及Emby播放地址。仅记录地址，不会自动发布DNS/证书/反代；继续保留SSH救援通道，按页面步骤同步白名单和Cookie后验证。服务Bot在Telegram管理配置，人的TG身份在个人设置绑定，指令管理复用同一Bot；OpenStrm不要开启同Token轮询。卡码管理负责发卡与审查邀请码记录，个人邀请中心负责本人名下邀请，邀请系统管理负责全站历史关系维护。完整审核见panel-audit-2026-11-24.md。


## 见微 Emby 域名、证书与 Mini App（v0.4）

“站点与入口”中先保存不同的账户域名和独立购买网站域名。域名归属于管理员，DNS 的 A/AAAA 应指向实际可达地址。账户入口为用户面板和 Mini App；管理后台继续使用 SSH 13000 转发。

在证书管理选择入口，填写联系邮箱、仅限所需区域的 Cloudflare DNS 编辑 Token，并明确接受证书机构条款。Token 仅写入主机 root 可读文件，不从接口返回；留空表示保留该域名已有凭据。可先申请测试证书确认 DNS 权限，再保存生产配置并申请正式证书。测试证书不能发布。

证书签发与入口发布分开。发布只使用固定 HTTPS 模板并验证实际 TLS 信任链；首次发布要求 443 空闲，保留已有 80 服务。失败或执行器重启会恢复发布前入口。改变期望域名不会立即替换已发布入口；旧域名已发布证书仍按其原续期配置续期。每 12 小时的 systemd 定时器检查到期证书，页面显示定时器实际状态；操作失败请检查 root 可读的执行器日志。

账户 HTTPS 验证后进入 Telegram 管理页，验证 Bot 身份，再输入当前管理员密码发布 Mini App 菜单。用户私聊 /start 可建立普通身份；不会自动授予 Emby 服务。已有 Web 身份继续使用其签名绑定链接，不按同名账号合并。Mini App 会话独立于 Web 管理登录，15 分钟过期，Bot 密钥轮换撤销旧会话。便携数据迁移不导出 Mini App 登录会话，恢复后从 Bot 重新打开；整机加密备份保留运行数据库和域名凭据。

独立购买域名不会在 Bot/Mini App 中显示。v0.4 只提供购买入口的证书与隔离基础，实际商品、核款和履约尚待后续业务版本。

## 私有仓库与升级

版本与升级是本部署唯一更新入口。公开仓库无需 Token；改为私有前保存仅此仓库 Contents 只读权限的 GitHub fine-grained Token。凭据在主机 `/var/lib/emby-cloud-updater/release-credentials.json` 中以 0600 权限保存，不返回客户端，不进入源码包。Token 失效时替换并重新检查版本；升级过程中禁止修改凭据。安装后的命令行升级可使用 `sudo python3 /usr/local/lib/emby-cloud/manager.py upgrade`，卸载仍使用本机安装器。全新主机在仓库私有后需要通过有权限的 GitHub 账户下载安装器，公开 raw 链接不再可用。仓库私有不免除上游 AGPL 对网络用户提供对应源码的要求。

备份恢复先等待 Emby 的本机 HTTP 入口，最多 90 秒；即使未就绪也会恢复其他服务，并让备份任务返回失败，避免误报成功。日志同步仅对连接错误及 5xx 重试，不重试认证失败，也不伪造成功记录。
