# 部署与升级

## Docker 交付

API 是 HappyRO 五类镜像之一，和 Gateway、Server、Admin、Database 一起由[根仓库发布流程](https://github.com/happyro/happyro/blob/main/docs/operations/docker-release.md)全量构建。应用版本统一来自根仓库 `deploy/docker/VERSION`。离线包的安装与升级见[公开部署说明](https://happyro.kugarocks.com/deploy/docker)。

根仓库 Compose 将 API 与 Admin 接入专用代理网络，`HTTP_TRUSTED_PROXIES` 仅信任 `ADMIN_PROXY_IP` 指定的 Admin 地址；原生部署需按实际代理地址单独配置该变量，不能信任任意来源。

API 镜像仅提供 Go 接口、迁移与资料初始化；独立 Admin Nginx 镜像提供页面并同源代理 `/api` 到 `api:8080`。API 不映射宿主机端口，健康检查使用 `/api/health`；Admin 默认通过宿主机 8000 端口访问，健康检查使用 `/healthz`。

Docker 中 Go API 通过内部 web-api 服务访问 Game Control，共享配置文件用于倍率等设置的持久化；不要把本机 loopback 地址直接用于跨容器连接。数据库、图片资源及配置挂载由根仓库模板定义。

## 管理库升级与访问来源

升级前备份管理库与游戏库，使用当前程序执行 `happyro-api migrate --no-color`，再启动服务。Docker 由 `api-init` 执行管理库迁移；检查其退出状态及日志。不要用首次空库初始化替代现有库升级。

`00010_account_gm.sql` 为 administrator 添加 `players.gm`，允许设置游戏账号 GM；super_admin 沿用通配权限。账号 GM 与后台管理员角色互不等价，保存后需完整退出游戏并重新登录。

`ADMIN_CSRF_ORIGIN_CHECK_ENABLED` 默认 `false`，关闭后台 Origin 和 Sec-Fetch-Site 来源限制，允许通过不同 IP 或域名访问；两种模式均要求有效会话及 CSRF Token。设为 `true` 后实际浏览器来源须与 `APP_URL` 一致；Docker 的 `ADMIN_PUBLIC_URL` 映射到 `APP_URL`。修改环境后重新创建 API 容器或重启原生服务。

## 原生开发环境

首次部署需要独立的空管理库。首次部署按顺序执行 `migrate`、`init`、`catalog import`。游戏库由具备结构修改权限的账号应用 happyro-server 的 `sql-files/happyro-accounts.sql`、`happyro-mail.sql` 和 `happyro-admin.sql`，建立账号唯一约束、删除队列及邮件回执表；运行账号只需要游戏库读写权限。重复账号或非 InnoDB 邮件表会使准备失败，程序不会自动删除账号或转换表引擎。

构建后安装 `happyro-api` 至服务配置的绝对路径。`.env.example` 是配置模板，Go 不自动读取 `.env`；使用 systemd `EnvironmentFile` 或显式导出环境变量。时长带单位，例如 `GAME_CONTROL_TIMEOUT=3s`、`SESSION_LIFETIME=2h`。

`GAME_SERVER_BATTLE_CONFIG_PATH` 必须是绝对路径，父目录须可由服务账号写入，并与游戏服务器共享。配置采用临时文件加原子替换，Docker 必须挂载整个目录，不能只绑定单个文件。

Linux 模板位于 `deploy/systemd/`；安装前核对 User、Group、绝对路径和监听地址。模板后端监听 `0.0.0.0:18081`，前端监听 8000；根据实际访问边界限制网络入口。macOS 混合环境由根仓库的 launchd 服务脚本管理。

Go API 经 HTTP 调用 web-server，不直接连接 map-server Socket；共享组权限用于读写受控服务配置。Socket 连接由 web-server 负责。不要根据模板推断当前机器已安装同名服务，验收步骤见根 AGENTS。

## 配置与更新

- 管理库与玩家库分离，核对 API 的数据库连接、APP_URL、会话时长和前端代理；HTTPS、HTTP Cookie 配置与游戏「保持登录」不是同一套设置。
- `GAME_CONTROL_BASE_URL`、`GAME_CONTROL_TOKEN` 和 `GAME_SERVER_BATTLE_CONFIG_PATH` 由 `internal/config` 从进程环境读取；保持与服务端密钥、实际共享文件位置一致。
- Docker 初始化已生成并注入 Game Control 密钥；不再将基础密钥生成列为未实现。轮换或跨主机传输需要按部署环境验证，不把已有本机连接当作跨主机方案。
- 更新前备份数据库与运行配置，迁移在正确环境执行 `happyro-api migrate`；Docker 由配套入口执行迁移，原生部署须自行完成。
- 资料生成后运行 `happyro-api catalog import --client-items <客户端物品.json> --server-items <Renewal物品.json> --monsters <魔物.json> --npcs <NPC.json>`，四份资料校验通过后原子发布。Docker 中使用 `refresh-custom-catalogs` 先刷新自定义资料再导入。
- 更新后核对健康检查、管理员登录、资料查询、能力发现与配置回读；测试角色操作和有消耗业务单独验收。

不在此维护特定机器的账号、旧包目录、测试角色 ID 或“当前进程已升级”记录。

### 登录页初始凭据提示

`SHOW_DEFAULT_CREDENTIALS=true` 显示“账号密码：admin / admin”，默认关闭；设置 `false` 隐藏。开关仅控制文案，不修改实际账号或密码。Docker 部署在部署目录 `.env` 修改后运行 `docker compose up -d --no-deps --force-recreate api`，无需重新构建镜像。原生部署修改 EnvironmentFile 后重启 Go 服务。

### 初始管理员账号

部署目录 `.env` 中的 `ADMIN_INITIAL_USERNAME` 和 `ADMIN_INITIAL_PASSWORD` 指定初始管理员，默认均为 `admin`。首次初始化只在管理库没有任何账号时创建超级管理员；已有账号时直接跳过，不覆盖密码、角色或状态。修改变量不会重置已有账号。用户名沿用小写字母、数字、下划线、点及短横线规则，密码为 5–255 个 UTF-8 字节。

使用自定义账号或密码时，登录页隐藏默认凭据提示，即使 `SHOW_DEFAULT_CREDENTIALS=true`；公开配置接口不返回自定义凭据。直接部署使用 `happyro-api init --no-color`。

提示是固定的 `admin/admin` 文案，不读取真实密码。初始化配置仍为默认值但实际密码已经修改时，应设置 `SHOW_DEFAULT_CREDENTIALS=false` 隐藏提示。

客户端刷新脚本使用 `/usr/local/bin/happyro-api` 导入完整资料快照，可通过 `HAPPYRO_API_BINARY` 指定其他已构建的 Go 二进制。脚本以后台服务账号通过 Bash 显式加载 `happyro-api/.env`，因此环境文件须使用可被 Bash 加载的赋值格式。物品子类生成脚本使用 Go 工具链执行源码中的共用分类规则，不依赖数据库。

## macOS Go 服务切换

先使用 Go 环境模板创建独立的 `work/runtime/native/api.env`，明确新管理库、游戏库连接、共享设置目录和 `HTTP_LISTEN=127.0.0.1:18081`。Admin 前端由独立服务提供，并将 `/api` 代理到 Go API；API 不配置前端产物目录。环境文件须是可被 Bash 加载的受信任赋值文件；限制文件权限，禁止提交凭据。

在根仓库执行构建与配置生成：

```bash
mkdir -p work/build/api
(cd repos/happyro-api && go build -o ../../work/build/api/happyro-api ./cmd/happyro-api)
python3 tools/operations/native_api.py prepare --workspace . \
  --binary work/build/api/happyro-api \
  --environment work/runtime/native/api.env \
  --output work/runtime/native/launchd/local.happyro.api.go.plist
```

生成工具只创建 plist，不启动服务，不读取或复制环境中的密钥，也不覆盖已有输出。先在显式加载环境后执行新管理库迁移、初始化与完整资料导入；游戏库由操作账号完成上述游戏库 SQL 初始化。上述示例让 plist 明确使用暂存构建路径；长期安装可先停止服务，将二进制复制到实际安装路径后用该绝对路径生成配置。确认 Go 程序可启动后，将生成配置安装为 `local.happyro.api.plist` 并加载；更新已有配置前先停止对应服务。不要覆盖正在运行的二进制；更新时先停止对应服务。

## 游戏设置与部署覆盖

地图服通过 `battle_config.read` 返回 `environment_overrides`。API 在写入受管理文件之前拒绝涉及非空环境覆盖的修改，并返回变量名；不需要将地图服的 HAPPYRO_NAVIGATION_* / HAPPYRO_GAME_TOOLS_* 复制到 API 环境。两端应使用同一套控制协议。

完整变量见[环境变量参考](https://happyro.kugarocks.com/deploy/environment-variables)。Go API 的 `.env.example` 只是模板，不由程序自动读取；Docker `.env` 由 Compose 注入，修改后重新创建相应容器，单独 restart 不更新环境。

## 后台账号改密

`ADMIN_PASSWORD_CHANGE_ENABLED` 是后台自助改密的唯一开关，默认 `true`。共享 admin 账号的演示环境应设置为 `false`：隐藏头像菜单中的改密入口，API 同时拒绝改密请求，后台用户无法重新开启。原生部署修改环境变量后重启 API；Docker 部署修改 `.env` 后运行 `docker compose up -d --no-deps --force-recreate api`。

改密入口在头像菜单，要求当前密码、新密码及确认新密码；成功后所有旧登录会话在下次请求时失效。该环境变量不限制服务器 CLI `users password`。新密码沿用现有 5～255 字节规则。

## 后台登录限流

后台密码登录按「规范化用户名 + 来源 IP」计数：固定 60 秒窗口内允许 5 次尝试，第 6 次起返回 HTTP 429 和 `Retry-After`；登录成功清除对应计数。计数保存在管理库 `login_attempts`，使用数据库时钟和事务更新，并发请求与多个 API 实例共用限制。失败及限流写入审计日志。

当前没有跨 IP 的账号总限流或全局 IP 总限流，不应将其视为完整的分布式爆破防护。反向代理部署需按实际代理配置 `HTTP_TRUSTED_PROXIES`，确保来源 IP 识别正确。此处描述的是后台账号登录，游戏账号认证使用独立流程。
