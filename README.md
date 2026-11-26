# 见微 Emby · Emby Cloud Runtime

**v0.4.18：全量源码、正式安装包、一键安装/卸载和面板升级。**

当前程序提供独立管理后台、Emby、OpenStrm、PostgreSQL、入口隔离、加密备份和发布升级。依据 [emby-cloud-plan v8.1](https://github.com/czy-code-923/emby-cloud-plan) 持续开发；会员 A/B 保号、人工核款发卡与核心 Emby 管理已集成，24模块总控及高级事故/退款流程尚未全部实现，准确范围见 [逐项验收](docs/acceptance-2026-11-26.md) 和 [差距清单](docs/gaps.md)。

## 一键安装

在 **Debian/Ubuntu、Linux amd64** 的 VPS 上以 root 执行：

```bash
curl -fsSL https://raw.githubusercontent.com/czy-code-923/emby-cloud-runtime/main/install.sh | bash
```

自动安装系统仓库依赖和Docker，获取本仓库最新**正式 Release**，校验发布包SHA-256，安装到 `/srv/emby-cloud`，启动管理后台、Emby、OpenStrm和升级执行器，并配置每日加密备份。保留宿主机其它服务的80/443/8080端口。

首次安装通过SSH本地转发访问：管理后台 `http://127.0.0.1:13000`、OpenStrm `http://127.0.0.1:13001`、Emby `http://127.0.0.1:18096`。后台按向导创建Owner并绑定个人验证器；Emby按向导确认使用条款。OpenStrm默认口令自动替换为随机值，保存在服务器 `runtime/initial-credentials.json`，仅root可读。域名、网盘和Telegram可随后配置。

## 一键卸载

停止并移除本项目容器、网络、备份定时器和升级服务，**保留配置、数据库、媒体索引和备份**：

```bash
curl -fsSL https://raw.githubusercontent.com/czy-code-923/emby-cloud-runtime/main/uninstall.sh | bash
```

仓库改私有后也可直接使用已安装的本地执行器卸载，无需再下载脚本：

```bash
python3 /usr/local/lib/emby-cloud/manager.py uninstall
```

再次执行安装命令可使用保留的数据恢复服务。若明确要删除本项目所有运行数据、索引、备份和升级历史，使用下面的单行命令；执行后不可通过项目功能恢复：

```bash
curl -fsSL https://raw.githubusercontent.com/czy-code-923/emby-cloud-runtime/main/uninstall.sh | bash -s -- --purge --yes-delete-data
```

卸载保留宿主机Docker、其它容器/镜像和其它服务。单独保存的备份解密私钥仍保留供历史副本恢复。

## 面板一键升级

管理员进入 **系统运维 → 版本与升级**：检查最新版本，点击“一键拉取并升级”，输入当前管理员密码后确认。

升级执行器只接受本仓库已发布正式版本，通过Unix socket提供固定操作；Web容器不持有Docker socket。自动完成来源/摘要/兼容性检查、下载、停写加密备份、替换程序与镜像、服务健康检查和进度回执。失败时回退上一程序与镜像，保留数据库新数据；需要改变数据库兼容版本的发布会被拒绝并要求单独迁移。升级期间会短暂停止本项目服务，网页会自动重连。

也可在SSH中执行：

```bash
curl -fsSL https://raw.githubusercontent.com/czy-code-923/emby-cloud-runtime/main/install.sh | bash -s -- upgrade
```

仅手动确认升级，后台不会自动应用新版本。

## 后台与方案核对

当前后台尚未实现v8.1全部业务。最新生产状态与每项要求见 [2026-11-26 验收表](docs/acceptance-2026-11-26.md)；较早的24模块结构核对保留于 [2026-11-24历史报告](docs/panel-audit-2026-11-24.md)。账户与购买网站域名可在“系统运维 → 站点与入口”修改，支持 Cloudflare DNS-01 证书申请、续期与独立的 HTTPS 发布。配置和生效状态分别显示；管理后台继续通过 SSH 安全入口使用。Mini App 菜单在账户入口发布后由 Telegram 管理页发布。

## 完整源码与完整版本

- `upstream/twilight/`：实际构建后端和管理面板的完整源码，保留原AGPL许可与NOTICE；本仓库增加了发布升级API和页面。
- `upstream/openstrm/`：固定v2.28.0的完整源码和原MIT许可证；部署使用对应官方镜像。
- `scripts/`、`deploy/`、`compose.yaml`：全部安装、卸载、发布执行器、备份与资源配置源码。
- 每个正式Release包含全量源码压缩包、Linux amd64完整分发包、独立前后端镜像包、安装/卸载入口、版本/兼容元数据和SHA256SUMS。
- Emby为厂商闭源软件，使用官方镜像并遵循厂商条款，其专有源码不属于本仓库。

CI在临时环境验证源码/构建、鉴权、恶意源与损坏包拒绝、安装、保留数据升级、健康失败回退、卸载/重装、显式清理和其它容器保留后，才公开发布版本。版本与依赖清单见 [source-manifest.md](docs/source-manifest.md)，实机结果见 [verification.md](docs/verification.md)，使用与恢复说明见 [operations.md](docs/operations.md)。

商店与会员发现的操作说明见 [商店与会员发现](docs/v0.4.7-商店与会员发现.md)（最初随未发布的 v0.4.7 编写，相关功能已合入后续正式版）；当前范围以最新验收表为准。

## 私有仓库的新 VPS 安装

已有服务器在“版本与升级”保存本仓库 Contents: Read 的细粒度 Token 后继续使用同一升级流程。新 VPS 先通过官方 GitHub CLI 登录具有仓库读取权限的账户，再运行：

```bash
gh api -H 'Accept: application/vnd.github.raw+json' repos/czy-code-923/emby-cloud-runtime/contents/install.sh > /tmp/jianwei-install.sh && GITHUB_TOKEN="$(gh auth token)" bash /tmp/jianwei-install.sh
```

Token 仅在安装进程中使用；后续面板升级凭据通过管理页单独保存，不将凭据写进命令历史或源码。当前支持 Debian/Ubuntu、Linux amd64；ARM64 尚不支持。
