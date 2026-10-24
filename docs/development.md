# 开发指南

后端使用 Go, Echo v5, Ent 和 PostgreSQL. 网页源码在[前端仓库](https://github.com/footprint-dev/footprint-frontend)维护, 后端构建时下载固定版本的前端 Release 并嵌入可执行文件, 无需安装 Node.js 或 pnpm.

## 本地运行

需要 Go 1.27.3+, PostgreSQL 18, Make, curl 和 tar. 以下命令在仓库根目录执行.

```bash
make build
```

首次运行时复制配置模板. 如果已有 `config.toml`, 直接编辑现有文件即可:

```bash
cp config.example.toml config.toml
```

设置 `server.ingest_token`, 将 `server.database_url` 改为本机数据库地址, 例如 `postgres://footprint:password@localhost:5432/footprint?sslmode=disable`. 先创建数据库和相应账号, 应用启动时会自动创建表.

```bash
./bin/footprint
```

默认读取当前目录的 `config.toml`, 也可以通过 `-config /path/to/config.toml` 指定文件. 启动后访问 <http://localhost:8080>.

## 构建与调试

| 命令 | 用途 |
| --- | --- |
| `make prepare` | 下载前端 Release 到 `internal/web/dist`, 并生成 Ent 代码 |
| `make build` | 执行准备步骤, 编译生成 `bin/footprint` |
| `make debug` | 使用已有前端产物和 Ent 代码, 仅重新编译服务 |
| `make generate` | 根据 `ent/schema/` 重新生成 Ent 代码 |
| `make test` | 执行准备步骤, 运行 `go test -race ./...` 和 `go vet ./...` |

初次构建使用 `make build`; 之后只修改 Go 代码时可用 `make debug`. 修改 `ent/schema/` 后先运行 `make generate`. Ent 生成代码和前端构建产物不提交到 Git.

前端版本固定在 [Makefile](../Makefile) 的 `FRONTEND_VERSION` 中. 下载或解压前端失败会中止构建. 独立开发前端时, 使用前端的 `API_BACKEND=serverful` 配置将请求和认证头转发给 Go 服务.

## 测试

运行不依赖数据库的测试:

```bash
make test
```

未设置 `TEST_DATABASE_URL` 时, PostgreSQL 集成测试会跳过. 要运行完整测试, 先启动专用的临时数据库:

```bash
docker run -d --rm --name footprint-serverful-test \
  -e POSTGRES_USER=footprint \
  -e POSTGRES_PASSWORD=footprint-test \
  -e POSTGRES_DB=footprint_test \
  -p 127.0.0.1:55432:5432 \
  postgres:18-alpine

docker exec footprint-serverful-test pg_isready -U footprint -d footprint_test
```

等 `pg_isready` 返回 `accepting connections` 后执行测试. 集成测试会清理位置, 质量标记, 项目和设备版本记录, 必须连接专用测试数据库:

```bash
TEST_DATABASE_URL='postgres://footprint:footprint-test@127.0.0.1:55432/footprint_test?sslmode=disable' \
  make test
```

测试结束后停止临时数据库:

```bash
docker stop footprint-serverful-test
```

## 构建 Docker 镜像

```bash
docker build -t footprint-serverful:local .
```

使用本地镜像部署时, 将 `compose.yaml` 中 `footprint` 服务的 `image` 改为 `footprint-serverful:local`, 然后按 [README](../README.md#快速开始)填写配置并启动.

构建环境无法访问默认 Go 模块代理时, 可在命令中增加 `--build-arg GOPROXY=https://goproxy.cn,direct`. 此参数仅影响构建阶段.
