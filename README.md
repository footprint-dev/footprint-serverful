# Footprint Serverful

使用 Go 1.27、Echo v5、Ent 和 PostgreSQL 18 实现的个人足迹后端. 提供位置上报、轨迹查询、项目管理及实时轨迹质量标注.

本仓库只包含后端. 原始位置始终保留, 轨迹分析只更新独立的质量标记.

## Docker 部署

需要 Docker 和 Docker Compose.

```bash
cp config.example.toml config.toml
openssl rand -hex 32
openssl rand -hex 32
```

在 `config.toml` 中填写数据库连接地址 `database_url` 的密码和 `ingest_token`. Docker 内的数据库主机名使用 `db`. PostgreSQL 官方镜像的初始化密码通过启动参数提供, 与 `database_url` 中的密码保持一致:

```bash
export POSTGRES_PASSWORD='填写 config.toml 中的数据库密码'
docker compose up -d --build
docker compose ps
curl http://localhost:8080/health
```

Compose 将当前目录的 `config.toml` 只读挂载到容器内的 `/etc/footprint/config.toml`, 将 `./postgres_data` 挂载到 PostgreSQL 的 `/var/lib/postgresql`. 默认 API 端口为 `8080`, 修改监听端口时同时调整 TOML 的 `http_addr` 和 Compose 的端口映射. PostgreSQL 仅通过 Compose 内部网络访问, 数据保存在当前目录的 `postgres_data/` 中. API 等待数据库健康后启动, 启动时由 Ent 自动创建表、外键和索引.

构建环境无法访问默认 Go 模块代理时, 使用 `docker compose build --build-arg GOPROXY=https://goproxy.cn,direct` 构建后再启动服务. 单独使用 `docker build` 时同样通过 `--build-arg GOPROXY=https://goproxy.cn,direct` 传入. 此参数只影响构建阶段.

```bash
export FOOTPRINT_TOKEN='填写 config.toml 中的 ingest_token'

curl -X PUT http://localhost:8080/api/location \
  -H "Authorization: Bearer $FOOTPRINT_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"longitude":0,"latitude":0,"datetime":"2024-01-01T00:00:00Z","timezone":0,"device":"example-device"}'

curl 'http://localhost:8080/api/locations?device=example-device' \
  -H "Authorization: Bearer $FOOTPRINT_TOKEN"
```

`docker compose down` 停止服务, 数据仍保留在 `postgres_data/` 中. 删除该目录会删除数据库数据. 配置文件和数据库目录均已从 Git 和 Docker 构建上下文中排除. 服务对外使用 HTTP, 公网部署时由反向代理提供 HTTPS.

## 连接已有 PostgreSQL

在 `config.toml` 中设置外部数据库的 `database_url`, 如 `postgres://user:password@database.example:5432/footprint?sslmode=require`, 然后挂载该文件:

```bash
docker build -t footprint-serverful:local .
docker run -d --name footprint-serverful \
  -p 8080:8080 \
  --mount type=bind,src="$(pwd)/config.toml",dst=/etc/footprint/config.toml,readonly \
  footprint-serverful:local
```

`database_url` 支持 pgx 连接参数. URL 中的用户名和密码包含保留字符时需要 URL 编码.

| 配置 | 用途 |
| --- | --- |
| `database_url` | 必填, PostgreSQL 连接地址 |
| `ingest_token` | 必填, 全部业务接口使用的 Bearer Token, 不含空白 |
| `http_addr` | Go 服务监听地址, 默认 `:8080` |

Go 服务默认读取当前目录的 `config.toml`, 可通过 `-config /path/to/config.toml` 指定其他文件. 实际配置文件不进入 Git 或 Docker 构建上下文, 只提交不含凭据的 `config.example.toml`. Docker 健康检查也读取挂载的 TOML, 使用其中的监听地址访问 `/health`.

数据库账号需要建表和更新结构的权限. 默认部署一个 API 实例; 本项目没有多实例同时初始化数据库的协调机制. 自动初始化失败时 API 退出, 不开始接收请求.

## 本地开发

需要 Go 1.27.3+、Make 和 PostgreSQL 18.

```bash
go mod download
make build
cp config.example.toml config.toml
```

在 `config.toml` 中填写 Token, 并把 `database_url` 改为本机数据库地址, 例如 `postgres://footprint:password@localhost:5432/footprint?sslmode=disable`, 然后启动:

```bash
./bin/footprint
```

`make build` 和 Docker 构建都会先生成 Ent 代码, 再编译服务. 单独刷新本地生成代码可以执行:

```bash
go generate ./ent
```

Ent 目录只提交 `ent/schema/` 下的手写模型和 `ent/generate.go` 生成入口. 其余生成代码由 `.gitignore` 和 `.dockerignore` 排除, 不进入 Git 或 Docker 构建上下文. 生成工具与运行库版本由 `go.mod` 和 `go.sum` 固定. 数据访问使用 Ent, 不手写 SQL.

```text
cmd/footprint/        启动、配置和优雅退出
ent/schema/          手写 Ent Schema
ent/generate.go      手写代码生成入口
ent/                 本地或构建时生成的数据访问代码, 不提交
internal/config/     TOML 配置读取与校验
internal/api/        Echo 接口、鉴权、解析和错误响应
internal/model/      API 数据结构
internal/store/      Ent 查询、事务和质量标注流程
internal/trajectory/ 独立的轨迹检测算法
docs/                API 与检测行为说明
```

## 测试

不连接数据库的测试:

```bash
go generate ./ent
go test ./...
go vet ./...
```

未设置 `TEST_DATABASE_URL` 时, PostgreSQL 集成测试会明确跳过. 完整验证需要专用的临时数据库:

```bash
docker run -d --rm --name footprint-serverful-test \
  -e POSTGRES_USER=footprint \
  -e POSTGRES_PASSWORD=footprint-test \
  -e POSTGRES_DB=footprint_test \
  -p 127.0.0.1:55432:5432 \
  postgres:18-alpine

docker exec footprint-serverful-test pg_isready -U footprint -d footprint_test

go generate ./ent
TEST_DATABASE_URL='postgres://footprint:footprint-test@127.0.0.1:55432/footprint_test?sslmode=disable' \
  go test -race ./...

docker stop footprint-serverful-test
```

集成测试会清理所连接数据库中的位置、质量标记、项目及设备版本记录. 只能使用专用测试数据库. 测试覆盖真实 PostgreSQL 事务、并发上报、故障降级、历史匹配、HTTP 协议与自动建表.

## 前端接入

后端提供与 `footprint-serverless` 当前 `/api/*` 相同的业务协议. 原 Console Worker 使用 Cloudflare Service Binding, 需要另行将其服务端代理改为访问本服务的 HTTP 地址. 独立前端在 Serverful 模式下透传客户端认证凭据, 不配置或注入共享 API Token. 本服务仍要求客户端提供合法的 `Authorization: Bearer <ingest_token>`, 未认证请求返回 `401`. `/api/console-config` 和 Cloudflare Access 登录属于控制台, 不由本服务提供.

详细接口见 [API 文档](docs/api.md), 检测规则与限制见 [轨迹质量检测](docs/trajectory.md).
