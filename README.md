# HappyRO API

HappyRO 的 Go 后端，为管理后台和游戏内冒险工具提供 HTTP API。后台与玩家入口使用独立的认证和权限校验，共用业务服务，以一个程序部署。

源码仓库：[happyro/happyro-api](https://github.com/happyro/happyro-api)。本仓库随 HappyRO 统一版本交付。

## 职责与边界

| 模块 | 提供的能力 |
| --- | --- |
| 管理后台 API | 管理员账号、角色权限、审计、玩家与角色管理、游戏资料、物品与邮件发放、运营设置 |
| 玩家工具 API | 冒险工具资料查询、角色操作、物品与金币发放、游戏设置；按游戏身份和功能策略授权 |
| 运维 CLI | 管理库迁移、初始管理员创建、管理员密码管理、资料目录原子导入 |
| HTTP 服务 | 健康检查，以及构建后的 Admin 前端静态文件 |

[happyro-admin](https://github.com/happyro/happyro-admin) 维护 React / Umi 管理前端；[happyro-gateway](https://github.com/happyro/happyro-gateway) 为游戏浏览器代理玩家 API；[happyro-server](https://github.com/happyro/happyro-server) 运行 rAthena 登录、角色、地图和 Web 服务。API 经 Game Control 调用游戏服务，在线角色与世界状态由地图服最终裁决。

管理员会话使用 SCS、CSRF 和角色权限保护。玩家接口使用独立的游戏身份认证、角色归属检查和限流，不共用管理员登录凭据。

## 开发入门

准备 Git 和符合 [go.mod](go.mod) 要求的 Go 工具链；运行服务还需要 MariaDB、游戏服务和对应游戏资料。以下命令只下载依赖、构建和显示帮助：

```bash
git clone https://github.com/happyro/happyro-api.git
cd happyro-api
go mod download
go build -mod=readonly -o bin/happyro-api ./cmd/happyro-api
./bin/happyro-api --help --no-color
```

程序不自动加载 `.env`。根据 [.env.example](.env.example) 配置管理库、游戏库、Game Control 与资源路径，由启动环境显式传入变量；Linux 可使用 systemd 的 `EnvironmentFile`。直接运行程序不带参数只显示帮助。

首次运行须准备全新的管理库，按顺序执行 `migrate`、`init`、`catalog import`，完成后使用 `serve` 启动 HTTP 服务。命令参数、数据库权限和部署步骤见[部署说明](docs/deployment.md)。

| 配置 | 用途 |
| --- | --- |
| `HTTP_LISTEN`、`APP_URL` | 监听地址与后台公开地址；开启来源校验时，浏览器来源必须与 APP_URL 一致 |
| `ADMIN_CSRF_ORIGIN_CHECK_ENABLED` | 默认 `false`，不校验 Origin 和 Sec-Fetch-Site；设为 `true` 启用来源校验。两种模式均要求有效 CSRF Token |
| `ADMIN_PASSWORD_CHANGE_ENABLED` | 默认 `true`，允许后台自助改密；演示环境设置 `false`，隐藏入口并拒绝改密请求，CLI 改密不受影响 |
| `FRONTEND_DIR` | Admin 构建产物目录；前端开发代理由 Admin 仓库维护 |
| `DB_*`、`GAME_DB_*` | 独立的管理库与游戏库连接 |
| `GAME_CONTROL_BASE_URL`、`GAME_CONTROL_TOKEN` | 与游戏服通信的地址和密钥 |
| `ADMIN_INITIAL_USERNAME`、`ADMIN_INITIAL_PASSWORD` | 空管理库初始化时创建的管理员，不覆盖已有账号 |

完整配置以环境模板为准。游戏库结构由 Server 的 `sql-files/` 维护，API 检查所需结构，不执行游戏库建表或迁移。

## 项目结构

```text
cmd/happyro-api/       程序入口
internal/
├── app/              依赖组装与应用启动
├── cli/              Cobra 命令
├── transport/http/   HTTP 服务、管理与玩家路由、协议适配
├── identity/         管理员、角色与权限
├── session/          会话管理
├── accounts/         游戏账号
├── characters/       游戏角色
├── catalog/          资料导入与查询
├── rewards/          发放业务
├── settings/         游戏设置
├── audit/            审计
├── game/             游戏控制适配
├── config/           环境配置
└── platform/         数据库等基础设施
migrations/management/ 嵌入程序的管理库迁移
resources/game-data/   根仓库生成器维护的资料快照
tests/integration/    数据库集成测试
deploy/               Docker 与 systemd 部署文件
```

HTTP 与 CLI 负责输入、输出转换，业务逻辑在领域模块中复用；数据库、文件和游戏服务依赖通过接口注入。目录快照由根仓库工具生成，导入时同时校验客户端物品、服务端物品、魔物和 NPC 四份资料，再原子发布。

## 游戏账号权限

`PUT /api/players/accounts/{accountId}/gm` 设置游戏账号组，要求管理员具备 `players.view` 和 `players.gm`。请求必须包含目标 `group_id`（普通玩家 0 或 GM 99）及读取时的 `expected_group_id`。普通账号 PATCH 接口不接受此字段。

权限作用于账号下所有角色，不改变后台管理员身份。登录服在完整退出并重新登录后加载新组；现有在线角色保持当前会话权限。旧组不匹配返回 409，不能静默覆盖其他管理员的修改。写入复用账号锁，保护服务器账号和删除中的账号，并记录 `player.gm_changed` 审计事件。

管理库迁移 `00010_account_gm.sql` 为 administrator 添加 `players.gm`；super_admin 通过通配权限访问。部署升级须执行管理库迁移，见[部署说明](docs/deployment.md)。

## 技术栈与检查

HTTP 使用 Gin，配置使用 caarlos0/env，日志使用 Zap，数据库访问使用 sqlx 与 MySQL 驱动，迁移使用 Goose，会话使用 SCS，CLI 使用 Cobra，密码使用 Argon2id。依赖版本由 [go.mod](go.mod) 和 [go.sum](go.sum) 锁定。

```bash
go test -mod=readonly ./...
go vet -mod=readonly ./...
```

默认测试不连接外部数据库。数据库集成测试通过 `TEST_ADMIN_MYSQL_DSN` 显式启用，会创建并删除临时数据库，必须使用隔离实例；并在同级检出 `happyro-server`，供测试读取权威 SQL。真实游戏进程与浏览器验收另行执行。

## 文档入口

- [开发说明](docs/development.md)：模块边界、认证与测试方式。
- [部署说明](docs/deployment.md)：初始化、环境配置、原生服务与容器部署。
- [变更记录](changelog/)：本仓库改动。
- [HappyRO 发布流程](https://github.com/happyro/happyro/blob/main/docs/operations/docker-release.md)：跨仓库构建、双架构镜像和离线交付。
