# 后端开发

Go 应用位于独立的 happyro-api 仓库，依赖版本以 `go.mod` 和 `go.sum` 为准。HTTP 使用 Gin，配置使用 caarlos0/env，日志使用 Zap，数据库访问使用 sqlx，管理库迁移使用 Goose，会话使用 SCS，CLI 使用 Cobra，密码使用 Argon2id。

在仓库根目录执行：

```bash
go mod download
go test -mod=readonly ./...
go build -mod=readonly -o bin/happyro-api ./cmd/happyro-api
./bin/happyro-api --help
```

`cmd/happyro-api` 是进程入口，`internal/cli` 适配命令行，`internal/app` 组装 HTTP 服务依赖，`internal/transport/http` 负责路由、请求校验和响应转换；业务组件通过接口注入数据库、文件和 Game Control 依赖。管理后台以数据库会话、CSRF 和权限校验保护；冒险工具独立验证游戏令牌、在线角色归属、功能策略及限流，不接受管理员会话替代游戏身份。

原生进程不自动读取 `.env`。导出配置或使用 systemd `EnvironmentFile` 后执行 CLI；初始管理员通过 `init` 创建，其他账号通过 `users create <username> --role <role> --password-stdin` 创建。部署和迁移步骤见[部署](deployment.md)。首次部署使用独立空管理库。

根仓库生成器写入 `resources/game-data/`；`catalog import` 同时校验客户端物品、服务端物品、魔物和 NPC 四份资料，再原子发布。查询直接使用已发布的数据库快照。

测试默认不访问外部数据库。MariaDB 集成测试通过 `TEST_ADMIN_MYSQL_DSN` 显式启用，测试创建随机数据库并在结束时删除；只能提供隔离测试数据库。真实游戏服务进程验收与普通单元测试分开进行。

## HTTP 模块边界

保持一个 Go 项目和一个部署进程。`internal/transport/http/admin/routes.go` 注册后台接口，`internal/transport/http/player/routes.go` 注册玩家冒险工具接口，共用领域服务。后台路由独立加载 SCS 会话并执行 CSRF 与权限检查；玩家路由只使用游戏身份认证和业务限流，不加载管理员会话。健康检查和前端静态文件也不依赖管理员会话存储。通用请求日志、安全响应头和请求体限制由入口统一处理。当前不引入服务间 HTTP 调用或额外框架。

## 游戏设置一致性

`internal/settings.Service` 负责注册表校验、环境覆盖检查、修订锁、文件发布、命令提交及回读；`Recovery` 处理管理员确认，`internal/game.SettingsState` 保留地图服实例、修订和命令回执。受控参数测试必须覆盖整批拒绝且没有文件写入；运行结果不确定时不得用重试代替幂等和版本屏障。

## 后台自助改密

`POST /api/auth/password` 使用管理员会话、CSRF 和 `ADMIN_PASSWORD_CHANGE_ENABLED` 校验，只操作当前会话账号。`SelfPasswordService` 校验当前密码、新密码和确认值，按账号限制尝试频率；仓库以旧密码哈希和启用状态作原子条件，密码更新与审计在同一事务提交。审计失败回滚改密，并发请求不能覆盖已经改变的密码。

`GET /api/auth/user` 返回部署能力 `passwordChangeEnabled`，无后台开关或专用配置表。改密后销毁当前会话，其他会话通过已有 `password_version` 校验失效；CLI 管理员维护不受网页入口开关限制。
