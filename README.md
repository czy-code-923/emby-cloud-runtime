# Emby Cloud Runtime

**v0.2.0：全量源码、正式安装包、一键安装/卸载和面板升级。**

当前程序提供独立管理后台、Emby、OpenStrm、PostgreSQL、入口隔离、加密备份和发布升级。依据 [emby-cloud-plan v8.1](https://github.com/czy-code-923/emby-cloud-plan) 持续开发；该方案中的定制保号、交易和24模块总控尚未全部实现，准确范围见 [差距清单](docs/gaps.md)。

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

再次执行安装命令可使用保留的数据恢复服务。若明确要删除本项目所有运行数据、索引、备份和升级历史，使用下面的单行命令；执行后不可通过项目功能恢复：

```bash
curl -fsSL https://raw.githubusercontent.com/czy-code-923/emby-cloud-runtime/main/uninstall.sh | bash -s -- --purge --yes-delete-data
```

卸载保留宿主机Docker、其它容器/镜像和其它服务。单独保存的备份解密私钥仍保留供历史副本恢复。

## 面板一键升级

管理员进入 **系统 → 版本与升级**：检查最新版本，点击“一键拉取并升级”，输入当前管理员密码后确认。

升级执行器只接受本仓库已发布正式版本，通过Unix socket提供固定操作；Web容器不持有Docker socket。自动完成来源/摘要/兼容性检查、下载、停写加密备份、替换程序与镜像、服务健康检查和进度回执。失败时回退上一程序与镜像，保留数据库新数据；需要改变数据库兼容版本的发布会被拒绝并要求单独迁移。升级期间会短暂停止本项目服务，网页会自动重连。

也可在SSH中执行：

```bash
curl -fsSL https://raw.githubusercontent.com/czy-code-923/emby-cloud-runtime/main/install.sh | bash -s -- upgrade
```

仅手动确认升级，后台不会自动应用新版本。

## 完整源码与完整版本

- `upstream/twilight/`：实际构建后端和管理面板的完整源码，保留原AGPL许可与NOTICE；本仓库增加了发布升级API和页面。
- `upstream/openstrm/`：固定v2.28.0的完整源码和原MIT许可证；部署使用对应官方镜像。
- `scripts/`、`deploy/`、`compose.yaml`：全部安装、卸载、发布执行器、备份与资源配置源码。
- 每个正式Release包含全量源码压缩包、Linux amd64完整分发包、独立前后端镜像包、安装/卸载入口、版本/兼容元数据和SHA256SUMS。
- Emby为厂商闭源软件，使用官方镜像并遵循厂商条款，其专有源码不属于本仓库。

CI在临时环境验证源码/构建、鉴权、恶意源与损坏包拒绝、安装、保留数据升级、健康失败回退、卸载/重装、显式清理和其它容器保留后，才公开发布版本。版本与依赖清单见 [source-manifest.md](docs/source-manifest.md)，实机结果见 [verification.md](docs/verification.md)，使用与恢复说明见 [operations.md](docs/operations.md)。
