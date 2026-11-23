# Emby Cloud 正式版本升级

本发行版在上游基础上新增“系统 → 版本与升级”。程序源固定为 `czy-code-923/emby-cloud-runtime`，管理员不能通过HTTP请求替换仓库URL、任意分支或主机命令。

| 方法 | V2 路由 | 权限与副作用 |
| --- | --- | --- |
| GET | `/api/v2/admin/deployment/status` | 管理员，private/no-store状态读取 |
| POST | `/api/v2/admin/deployment/check` | 管理员、显式WebUI意图，检查最新正式Release，执行器缓存结果60秒 |
| POST | `/api/v2/admin/deployment/apply` | 管理员、显式WebUI意图、当前密码复核、限流与审计；请求tag和expected_version，后台执行升级 |

写请求必须带 `X-Twilight-Client: webui`、`X-Twilight-Intent: deploy-release`。apply请求包含 `{tag, expected_version, password}`，密码只用于当前用户复核，不转发给主机、不进入日志或审计。审核记录为 `deployment_release_apply`，只保存版本号。

Go层保持原有登录/MFA会话和管理员鉴权，经仅限后台UID/root访问的Unix socket转发固定操作。Web容器没有Docker socket，也不持有执行器安装目录写权限。执行器验证资产来源/摘要/版本与数据库兼容标记，禁止并行升级；升级前停写加密备份，失败时恢复上一程序和镜像，保留业务数据库。

前端通过共享api.ts/api-request.ts和useAsyncResource读取状态，取消过期请求；只在有运行任务且页面可见时每4秒刷新。简中、繁中、英文文案均由统一catalog提供。安装、卸载、升级的完整源码与发行包由外层仓库发布，部署事实与业务差距以外层docs/verification.md和docs/gaps.md为准。
