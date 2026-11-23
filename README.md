# Emby Cloud Runtime

根据 [emby-cloud-plan v8.1](https://github.com/czy-code-923/emby-cloud-plan) 制作的分阶段部署仓库。当前版本 **0.1.0-alpha.3：隔离管理基础版**。

**这是基础服务部署版本，不是方案全部功能的完成声明。** 账户与管理使用固定版本 Twilight；媒体服务使用 Emby；网盘工具按需启用。域名、Telegram、网盘凭据在首次登录后配置。签到/自动清退/公开注册/销售默认关闭，v8.1 定制保号规则尚未开发和生产验收，不用上游近似规则代替。

## 源码与许可

`upstream/twilight` 保存部署所用的完整 Twilight 源码，固定提交 `7e9de1430a24ae8a01bc21d03168a4b8b459ba59`。上游项目为 [Prejudice Studio / Twilight](https://github.com/Prejudice-Studio/Twilight)，遵循 GNU AGPL-3.0-or-later 和原 NOTICE 署名要求。本仓库保留完整 LICENSE、NOTICE 与历史许可材料；没有修改上游业务代码。Emby 为独立的第三方专有软件，其镜像不属于本仓库授权范围。

## 部署

首次部署在独立目录执行 `bash scripts/install.sh`。必须先安装 Docker Engine、Compose、Python 3 和 curl。脚本只管理本项目；不清理其它容器、不覆盖已有数据库、不占用宿主机 80/443/8080。

入口仅监听服务器回环地址，先通过 SSH 本地转发：本机 13000 → 服务器 127.0.0.1:13000。然后访问 `http://localhost:13000` 创建 Owner，启用双因素认证。Emby 初始化入口为服务器回环 `18096`，同样通过 SSH 转发访问。禁止在未完成 Owner 初始化前把这些端口改为公网监听。

后台的系统配置可绑定 Emby API、Telegram Token、TMDB、邮件及实际播放线路。Cloud profile 的 OpenStrm 用于随后选择的网盘；需要可读稳定来源时启用 source profile 的 OpenList。两者各有自己的管理界面。域名的 TLS/反代发布仍需部署侧配置，当前没有统一网页域名发布器。

## 运维

- `.env`、`runtime/`、密钥和备份均不提交。
- 初次拉取生成 `images.lock.yaml`，后续用 `docker compose -f compose.yaml -f images.lock.yaml ...` 保持镜像摘要固定。
- `scripts/backup.sh` 提供控制器数据库和配置的 age 加密备份；需填写异地保存的 age 公钥。它不声称完成全服务一致备份或异地恢复验收。
- 不启用自动升级。升级前备份、核对兼容性；禁止通过旧数据库覆盖方式回滚新业务数据。
- 1 核 2GB 只适合小规模验证；网盘 profile 启用前重新测量内存。无域名/网盘授权时不能声称真实播放或 302 已验收。

访问、配置与备份操作见 [使用说明](docs/operations.md)，部署来源见 [版本清单](docs/source-manifest.md)。完整范围与未实现内容见 [差距清单](docs/gaps.md)，实机证据见 [验证记录](docs/verification.md)。
