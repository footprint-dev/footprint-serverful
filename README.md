# Footprint Serverful

自托管的个人足迹应用, 在自己的地图上记录和回看每次出行.

接收设备上报的位置, 在地图和列表中查看历史轨迹, 用项目整理旅行或日常通勤. 数据保存在你自己的 PostgreSQL 中, 网页和 API 随服务一起提供, 使用 Docker Compose 即可部署.

[快速开始](#快速开始) | [接入位置上报](#接入位置上报) | [API 文档](docs/api.md)

## 功能

- **回看足迹**. 在地图上查看轨迹, 按日期或项目筛选, 查看每个位置的时间和设备.
- **整理行程**. 为一次旅行或一段通勤创建项目, 用起止时间保存对应的行程范围.
- **过滤异常点**. 自动标记疑似定位跳变, 支持切换原始轨迹. 检测属于实验性功能, 原始位置始终保留.
- **接入自己的设备**. 通过 HTTP API 上报 JSON 或表单数据, 按设备和时间查询历史位置.
- **自行托管**. 应用与数据库通过 Docker Compose 一起启动, 无需单独部署前端.

## 快速开始

需要 Docker 和 Docker Compose. 网页地图还需要一个 [Mapbox 公开 Token](https://docs.mapbox.com/help/dive-deeper/access-tokens/), 仅使用 API 时可以不配置地图.

### 1. 保存 Compose 模板

创建一个部署目录, 将以下内容保存为 `compose.yaml`:

```yaml
services:
  footprint:
    image: ghcr.io/footprint-dev/footprint-serverful:latest
    restart: unless-stopped
    volumes:
      - type: bind
        source: ./config.toml
        target: /etc/footprint/config.toml
        read_only: true
        bind:
          create_host_path: false
    ports:
      - "8080:8080"
    depends_on:
      db:
        condition: service_healthy
    stop_grace_period: 15s

  db:
    image: postgres:18-alpine
    restart: unless-stopped
    environment:
      POSTGRES_USER: footprint
      POSTGRES_PASSWORD: footprint
      POSTGRES_DB: footprint
    volumes:
      - ./postgres_data:/var/lib/postgresql
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U footprint -d footprint"]
      interval: 5s
      timeout: 3s
      retries: 10
```

### 2. 保存应用配置

在同一目录创建 `config.toml`:

```toml
[server]
addr = ":8080"
database_url = "postgres://footprint:footprint@db:5432/footprint?sslmode=disable"
ingest_token = "REPLACE_WITH_YOUR_TOKEN"

[mapbox]
public_token = ""
style = "mapbox://styles/mapbox/light-v11"
```

填写以下两项, 其余配置可保持默认:

| 配置项 | 填写内容 |
| --- | --- |
| `server.ingest_token` | 将 `REPLACE_WITH_YOUR_TOKEN` 替换为访问 Token, 可用 `openssl rand -hex 32` 生成; 网页和上报设备都使用它 |
| `mapbox.public_token` | 以 `pk.` 开头的 Mapbox 公开 Token; 留空时不加载地图 |

### 3. 启动服务

在部署目录中执行:

```bash
docker compose up -d --wait
```

应用会等待数据库就绪并自动创建所需的表.

### 4. 打开网页

访问 <http://localhost:8080>, 在右上角设置中输入 `server.ingest_token`. 部署在远程服务器时, 将 `localhost` 换成服务器地址.

首次使用还没有足迹数据, 可以先按下一节上报一个位置, 再刷新网页查看. 服务使用一个共享 Token 访问全部业务数据, 无需注册账号.

## 接入位置上报

在设备端或脚本中向 `PUT /api/location` 发送坐标和观测时间, 并通过 `Authorization: Bearer <Token>` 请求头携带访问 Token.

下面的示例会以当前时间写入一个演示坐标. 将 `FOOTPRINT_TOKEN` 的值替换为 `config.toml` 中的 `server.ingest_token`:

```bash
export FOOTPRINT_TOKEN='replace-with-your-ingest-token'

curl --fail-with-body -X PUT http://localhost:8080/api/location \
  -H "Authorization: Bearer $FOOTPRINT_TOKEN" \
  --data-urlencode 'longitude=121.4737' \
  --data-urlencode 'latitude=31.2304' \
  --data-urlencode "datetime=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  --data-urlencode 'timezone=8' \
  --data-urlencode 'device=example-device'
```

成功时返回 HTTP `201` 和包含 `location` 的 JSON. 查询这个设备上报的位置:

```bash
curl --fail-with-body 'http://localhost:8080/api/locations?device=example-device' \
  -H "Authorization: Bearer $FOOTPRINT_TOKEN"
```

正式接入时, 使用设备实际采集的 WGS84 经纬度, 观测时间和设备标识. 完整的上报字段, 查询参数和响应格式见 [API 文档](docs/api.md). 每次上报都会新增记录, 重复发送同一个点不会自动去重.

## 文档与反馈

| 我想要 | 查看 |
| --- | --- |
| 接入设备或编写脚本 | [API 文档](docs/api.md) |
| 了解异常点如何判定 | [轨迹质量检测](docs/trajectory.md) |
| 从源码构建或参与开发 | [开发指南](docs/development.md) |
| 修改网页界面 | [前端仓库](https://github.com/footprint-dev/footprint-frontend) |
| 报告问题或提出建议 | [GitHub Issues](https://github.com/footprint-dev/footprint-serverful/issues) |

## 许可证

[GNU Affero General Public License v3.0](LICENSE), SPDX 标识为 `AGPL-3.0-only`.
