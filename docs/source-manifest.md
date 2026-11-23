# 部署来源

| 组件 | 版本 / 源码固定点 | 分发方式 |
| --- | --- | --- |
| Twilight | `7e9de1430a24ae8a01bc21d03168a4b8b459ba59` | 完整源码位于 upstream/twilight，基于该固定提交增加面板升级，保留 AGPL、LICENSE 和 NOTICE |
| OpenStrm | v2.28.0，`dde2b9889d8b9ba1194797be70e6fbf7472d9f5b` | 官方 indown/openstrm 镜像；完整对应源码与MIT许可位于 upstream/openstrm |
| Emby Server | 4.10.2.0 | 官方 emby/embyserver 镜像；独立专有软件，遵循厂商条款 |
| PostgreSQL | 17-alpine | 官方镜像，实机镜像摘要写入本地 images.lock.yaml |
| Caddy | 2-alpine | 官方镜像，实机镜像摘要写入本地 images.lock.yaml |
| OpenList | v4.2.10，`7c26f1012c2951441233dce4ef309753fc76165d` | 可选来源适配器，未启用；源码 https://github.com/OpenListTeam/OpenList/tree/v4.2.10 |

本仓库的安装、资源限制、备份和入口配置位于 deploy/、scripts/、compose.yaml。第三方镜像不是由本仓库重新授权。运行时密钥、账号、快照和镜像锁不进入 public 仓库。
