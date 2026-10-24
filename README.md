# Footprint Serverful

使用 Go 1.27、Echo v5、Ent 和 PostgreSQL 18 实现的个人足迹后端. 提供位置上报、轨迹查询、项目管理及实时轨迹质量标注.

服务通过 Go embed 提供前端页面和 API, 运行时无需 Node.js 或单独的静态资源目录. 前端源码在独立仓库维护, 构建时下载固定版本的 Release 压缩包. 原始位置始终保留, 轨迹分析只更新独立的质量标记.

## Docker 部署

需要 Docker 和 Docker Compose.

Compose 使用 `ghcr.io/footprint-dev/footprint-serverful:latest` 镜像, 无需在部署机器上构建代码. 该镜像需要先由后端的发布 workflow 发布到 GHCR.

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

Compose 将当前目录的 `config.toml` 只读挂载到容器内的 `/etc/footprint/config.toml`, 将 `./postgres_data` 挂载到 PostgreSQL 的 `/var/lib/postgresql`. 默认 API 端口为 `8080`, 修改监听端口时同时调整 TOML 的 `server.addr` 和 Compose 的端口映射. PostgreSQL 仅通过 Compose 内部网络访问, 数据保存在当前目录的 `postgres_data/` 中. API 等待数据库健康后启动, 启动时由 Ent 自动创建表、外键和索引.

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

自行构建镜像可执行 `docker build -t footprint-serverful:local .`. 构建环境无法访问默认 Go 模块代理时, 增加 `--build-arg GOPROXY=https://goproxy.cn,direct`. 此参数只影响构建阶段.

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
internal/web/        内嵌前端资源和静态文件路由
scripts/             构建准备脚本
docs/                API 与检测行为说明
```

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

后端提供与 `footprint-serverless` 当前 `/api/*` 相同的业务协议. 原 Console Worker 使用 Cloudflare Service Binding, 需要另行将其服务端代理改为访问本服务的 HTTP 地址. 独立前端页面可直接打开, 在右上角设置中输入 `[server] ingest_token` 后, 浏览器为业务请求添加 `Authorization: Bearer <token>`, 代理透传凭据交由后端校验. 未认证请求返回 `401`.

地图配置由本服务的 `GET /api/console-config` 提供, 无需认证. 在 `config.toml` 中填写:

```toml
[mapbox]
public_token = "pk.your-public-token"
style = "mapbox://styles/mapbox/light-v11"
```

只有 `pk.` 开头的公开 Mapbox Token 可以配置并返回给浏览器; 留空时仍可使用后端 API. 该接口仅返回地图公开配置, 不返回业务 Token 或数据库信息. 内嵌页面直接请求同源 API; 独立开发前端使用 `API_BACKEND=serverful` 时会把请求转发给 Go. 地图配置在服务启动时加载, 修改后重启即可, 无需重新构建前端.

## 前端版本与镜像发布

前端仓库推送 `vX.Y.Z` 标签后, GitHub Actions 执行测试和构建, 并发布包含 `footprint-frontend.tar.gz` 的 Release. 本仓库在 `Makefile` 中固定 `FRONTEND_VERSION`; 升级前端时修改此版本号并重新构建. 压缩包包含 `dist/` 和外层的 `LICENSE`, Go 只嵌入 `dist/` 中的静态资源. 许可证位于最终镜像的 `/usr/share/licenses/footprint/LICENSE`, 不作为页面资源提供.

第一次接入时先提交并推送前端的发布 workflow 和相对路径修改, 发布前端 `v0.1.0`, 再构建后端. 在 Release 发布前进行本地联调, 可将本地同格式压缩包的地址传给 Make:

```bash
make build FRONTEND_URL=file:///absolute/path/footprint-frontend.tar.gz
```

两个仓库的 workflow 仅由 `v*` 标签推送或 `workflow_dispatch` 手动操作触发. 后端会运行 `make test`, 使用临时 PostgreSQL 18 执行集成测试, 并构建完整 Docker 镜像. 对 `vX.Y.Z` 标签执行时, 测试与构建成功后同时发布 `ghcr.io/<当前GitHub仓库>:<版本标签>` 和 `ghcr.io/<当前GitHub仓库>:latest`, 平台为 `linux/amd64`. 手动对分支执行时只测试和构建. 发布使用 GitHub 内置 Token; 需要仓库允许 Actions 发布 Release 和 Packages. 不自动更新运行中的服务.

已有 PostgreSQL 时, 可以直接使用发布的镜像:

```bash
docker run -d --name footprint-serverful \
  -p 8080:8080 \
  --mount type=bind,src="$(pwd)/config.toml",dst=/etc/footprint/config.toml,readonly \
  ghcr.io/footprint-dev/footprint-serverful:latest
```

若使用其他 GitHub 仓库发布, 将部署命令和 Compose 中的镜像路径替换为该仓库的全小写路径. `config.toml` 中的数据库地址必须能从容器内访问. GHCR 包的可见性由仓库所有者设置; 私有镜像需要先登录再拉取.

## 子路径部署

同一份前端产物可部署在 `/` 或 `/xxxx/`. 前端资源和 API 使用相对地址; 反向代理负责去掉 `/xxxx` 前缀, Go 保持根路径路由. Nginx 示例:

```nginx
location = /xxxx {
    return 308 /xxxx/$is_args$args;
}

location /xxxx/ {
    proxy_pass http://127.0.0.1:8080/;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-Proto $scheme;
}
```

`proxy_pass` 地址末尾的 `/` 用于移除路径前缀. 页面入口需保留尾斜杠, 例如 `/xxxx/`, 这样 `./api/me` 才会解析为 `/xxxx/api/me`. 静态资源支持 GET/HEAD; 首页使用重新验证缓存, 带哈希的资源长期缓存. 未知路径返回 `404`, 不回退为前端页面.

详细接口见 [API 文档](docs/api.md), 检测规则与限制见 [轨迹质量检测](docs/trajectory.md).

## 许可证

本项目采用 [GNU Affero General Public License v3.0](LICENSE), SPDX 标识为 `AGPL-3.0-only`.
