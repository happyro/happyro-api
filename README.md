# HappyRO API

独立 Go API 服务，为 HappyRO 管理后台和玩家工具提供 HTTP 接口。管理后台前端位于独立的 happyro-admin 仓库；游戏核心位于 happyro-server。

## 开发

```sh
go test ./...
go vet ./...
go build -o bin/happyro-api ./cmd/happyro-api
```

直接运行 `happyro-api` 只显示帮助；使用 `serve` 启动服务。程序不自动读取 .env，由运行环境显式传入配置，示例见 `.env.example`。

`internal/app` 组装依赖，`internal/cli` 适配命令行，`internal/transport/http` 组装 HTTP 服务，`admin` 与 `player` 子包分别处理管理员和玩家入口，`response` 统一协议解析与错误响应；业务模块复用同一组服务。管理库迁移位于 `migrations/management` 并嵌入二进制。单元测试与实现同目录，数据库集成测试位于 `tests/integration`，通过 `TEST_ADMIN_MYSQL_DSN` 显式指定隔离数据库。

开发与验收见 [开发说明](docs/development.md)，原生和容器部署见 [部署说明](docs/deployment.md)。本仓库尚未发布，版本发布由根仓库统一编排。

游戏库结构由 happyro-server 的 `sql-files/` 维护，API 仅检查结构，不提供建表命令。启用数据库集成测试时，按标准并列仓库布局准备 happyro-server，测试会读取其权威 SQL。
