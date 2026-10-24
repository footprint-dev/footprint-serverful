# Footprint Serverful

使用 Go 1.27、Echo v5、Ent 和 PostgreSQL 18 实现的个人足迹后端. 提供位置上报、轨迹查询、项目管理及实时轨迹质量标注.

服务通过 Go embed 提供前端页面和 API, 运行时无需 Node.js 或单独的静态资源目录. 前端源码在独立仓库维护, 构建时下载固定版本的 Release 压缩包. 原始位置始终保留, 轨迹分析只更新独立的质量标记.

## Docker 部署

需要 Docker 和 Docker Compose.

Compose 中的 `footprint` 服务提供页面和 API, 使用 `ghcr.io/footprint-dev/footprint-serverful:latest` 镜像, 无需在部署机器上构建代码. 该镜像需要先由后端的发布 workflow 发布到 GHCR.

```bash
cp config.example.toml config.toml
openssl rand -hex 32
openssl rand -hex 32
```

在 `config.toml` 中填写数据库连接地址 `server.database_url` 的密码和 `server.ingest_token`. Docker 内的数据库主机名使用 `db`. PostgreSQL 官方镜像的初始化密码通过启动参数提供, 与 `server.database_url` 中的密码保持一致:

```bash
export POSTGRES_PASSWORD='填写 config.toml 中的数据库密码'
docker compose pull
docker compose up -d
docker compose ps
curl http://localhost:8080/health
```

打开 `http://localhost:8080/` 即可访问页面, 在右上角设置中输入 `[server] ingest_token` 后加载业务数据.

Compose 将当前目录的 `config.toml` 只读挂载到容器内的 `/etc/footprint/config.toml`, 将 `./postgres_data` 挂载到 PostgreSQL 的 `/var/lib/postgresql`. 默认服务端口为 `8080`, 修改监听端口时同时调整 TOML 的 `server.addr` 和 Compose 的端口映射. PostgreSQL 仅通过 Compose 内部网络访问, 数据保存在当前目录的 `postgres_data/` 中. `footprint` 等待数据库健康后启动, 启动时由 Ent 自动创建表, 外键和索引.

升级时执行 `docker compose pull` 和 `docker compose up -d`, 拉取并使用最新发布的镜像.

```bash
export FOOTPRINT_TOKEN='填写 config.toml 中的 server.ingest_token'

curl -X PUT http://localhost:8080/api/location \
  -H "Authorization: Bearer $FOOTPRINT_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"longitude":0,"latitude":0,"datetime":"2024-01-01T00:00:00Z","timezone":0,"device":"example-device"}'

curl 'http://localhost:8080/api/locations?device=example-device' \
  -H "Authorization: Bearer $FOOTPRINT_TOKEN"
```

`docker compose down` 停止服务, 数据仍保留在 `postgres_data/` 中. 删除该目录会删除数据库数据. 配置文件和数据库目录均已从 Git 和 Docker 构建上下文中排除. 服务对外使用 HTTP, 公网部署时由反向代理提供 HTTPS.

## 连接已有 PostgreSQL

在 `config.toml` 中设置外部数据库的 `server.database_url`, 如 `postgres://user:password@database.example:5432/footprint?sslmode=require`, 然后挂载该文件:

```bash
docker pull ghcr.io/footprint-dev/footprint-serverful:latest
docker run -d --name footprint-serverful \
  -p 8080:8080 \
  --mount type=bind,src="$(pwd)/config.toml",dst=/etc/footprint/config.toml,readonly \
  ghcr.io/footprint-dev/footprint-serverful:latest
```

`server.database_url` 支持 pgx 连接参数. URL 中的用户名和密码包含保留字符时需要 URL 编码.

| 配置 | 用途 |
| --- | --- |
| `[server] addr` | Go 服务监听地址, 默认 `:8080` |
| `[server] database_url` | 必填, PostgreSQL 连接地址 |
| `[server] ingest_token` | 必填, 全部业务接口使用的 Bearer Token, 不含空白 |
| `[mapbox] public_token` | 可选, 前端地图使用的 `pk.` 公开 Token, 未配置时不加载地图 |
| `[mapbox] style` | 地图样式, 默认 `mapbox://styles/mapbox/light-v11` |

Go 服务默认读取当前目录的 `config.toml`, 可通过 `-config /path/to/config.toml` 指定其他文件. 实际配置文件不进入 Git 或 Docker 构建上下文, 只提交不含凭据的 `config.example.toml`. Docker 健康检查也读取挂载的 TOML, 使用其中的监听地址访问 `/health`.

数据库账号需要建表和更新结构的权限. 默认部署一个 API 实例; 本项目没有多实例同时初始化数据库的协调机制. 自动初始化失败时 API 退出, 不开始接收请求.

## 本地开发

需要 Go 1.27.3+, Make, curl, tar 和 PostgreSQL 18. 后端构建不需要 Node.js 或 pnpm.

```bash
go mod download
make build
cp config.example.toml config.toml
```

在 `config.toml` 中填写 `server.ingest_token`, 并把 `server.database_url` 改为本机数据库地址, 例如 `postgres://footprint:password@localhost:5432/footprint?sslmode=disable`, 然后启动:

```bash
./bin/footprint
```

`make prepare` 下载前端 Release, 解压到 `internal/web/dist`, 并生成 Ent 代码. `make build` 自动执行准备步骤后编译服务; Docker 构建复用相同步骤. 下载或解压失败会停止构建. 单独刷新 Ent 代码可以执行:

```bash
go generate ./ent
```

本地调试时, 使用已有的前端产物和 Ent 生成代码, 只编译二进制:

```bash
make debug
./bin/footprint
```

`make debug` 不下载前端, 也不生成 Ent 代码. 需要本地已存在 `internal/web/dist` 和 Ent 生成代码; 修改 Ent Schema 后先执行 `make generate`.

自行构建镜像可执行 `docker build -t footprint-serverful:local .`. 构建环境无法访问默认 Go 模块代理时, 增加 `--build-arg GOPROXY=https://goproxy.cn,direct`. 此参数只影响构建阶段.

Ent 目录只提交 `ent/schema/` 下的手写模型和 `ent/generate.go` 生成入口. 其余生成代码由 `.gitignore` 和 `.dockerignore` 排除, 不进入 Git 或 Docker 构建上下文. 生成工具与运行库版本由 `go.mod` 和 `go.sum` 固定. 数据访问使用 Ent, 不手写 SQL.

## 测试

不连接数据库的测试:

```bash
make test
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

TEST_DATABASE_URL='postgres://footprint:footprint-test@127.0.0.1:55432/footprint_test?sslmode=disable' \
  make test

docker stop footprint-serverful-test
```

集成测试会清理所连接数据库中的位置、质量标记、项目及设备版本记录. 只能使用专用测试数据库. 测试覆盖真实 PostgreSQL 事务、并发上报、故障降级、历史匹配、HTTP 协议与自动建表.

## 前端接入

后端提供与 `footprint-serverless` 当前 `/api/*` 相同的业务协议.

地图配置由本服务的 `GET /api/console-config` 提供, 需要与其他业务接口相同的 Bearer Token. 页面在用户输入有效 Token 后才请求地图配置. 在 `config.toml` 中填写:

```toml
[mapbox]
public_token = "pk.your-public-token"
style = "mapbox://styles/mapbox/light-v11"
```

只有 `pk.` 开头的公开 Mapbox Token 可以配置并返回给通过认证的浏览器; 留空时仍可使用后端 API. 该接口仅返回地图配置, 不返回业务 Token 或数据库信息. 内嵌页面直接请求同源 API; 独立开发前端使用 `API_BACKEND=serverful` 时会把请求和认证头转发给 Go. 地图配置在服务启动时加载, 修改后重启即可, 无需重新构建前端.

## 许可证

本项目采用 [GNU Affero General Public License v3.0](LICENSE), SPDX 标识为 `AGPL-3.0-only`.
