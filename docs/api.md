# API

`GET /health` 和 `GET /api/console-config` 可公开访问. 其他 `/api/*` 接口要求 `Authorization: Bearer <ingest_token>`, Token 配置在 `config.toml` 的 `[server] ingest_token` 中. 错误凭据返回 `401`. 全部响应使用 JSON 和 `Cache-Control: no-store`.

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| GET | `/health` | 存活检查, 不查询数据库 |
| GET | `/api/console-config` | 公开的地图配置, 不查询数据库 |
| GET | `/api/me` | 校验 Token |
| PUT | `/api/location` | 上报一个位置 |
| GET | `/api/location` | 查询观测时间最新的位置 |
| GET | `/api/locations` | 分页查询位置 |
| GET | `/api/projects` | 分页查询项目 |
| POST | `/api/projects` | 创建项目 |
| GET | `/api/projects/:name` | 按名称查询项目 |
| PATCH | `/api/projects/:id` | 按 ID 修改项目 |

## 地图配置

`GET /api/console-config` 从服务启动时读取的 `config.toml` 返回地图公开配置, 无需认证:

```json
{
  "mapboxToken": "pk.your-public-token",
  "mapboxStyle": "mapbox://styles/mapbox/light-v11",
  "localPreview": false
}
```

`mapboxToken` 对应 `[mapbox] public_token`, 未配置时为空字符串. `mapboxStyle` 对应 `[mapbox] style`, 默认使用 `mapbox://styles/mapbox/light-v11`. 该接口不返回 `server.ingest_token` 或数据库连接信息. 修改配置后重启 Go 服务即可生效.

## 位置上报

支持 `application/json`、`application/x-www-form-urlencoded` 和 `multipart/form-data`. 字段必须是标量, 文件不能用作坐标或文本.

| 字段 | 说明 |
| --- | --- |
| `longitude` / `lng` | 必填, WGS84 经度, -180 到 180 |
| `latitude` / `lat` | 必填, WGS84 纬度, -90 到 90 |
| `timestamp` / `datetime` | 必填, 非负整数毫秒时间戳, 或带时区的 ISO 8601 日期时间 |
| `timezone` | 可选, UTC 小时偏移, -12 到 14, 精确到整分钟, 如 `8`、`5.5`、`5.75` |
| `device` | 必填, 非空字符串, 最长 200 个 UTF-16 代码单元 |
| `city` | 可选字符串, 同样最长 200, 默认为空字符串 |

主字段非 `null` 时优先于别名. `device` 和 `city` 去除首尾空白. 时间戳最大为 `8640000000000000`; ISO 时间允许最多三位小数秒. 省略 `timezone` 或提交 `null` 时保存为空, 不从坐标或日期字符串推断偏移.

每次上报新增记录, 重试不会去重. 原始位置、设备版本与初始质量标记一起提交. 成功返回 `201`:

```json
{
  "location": {
    "id": 1,
    "longitude": 0,
    "latitude": 0,
    "city": "",
    "device": "phone",
    "timestamp": 1704067200000,
    "timezone": 0,
    "quality": {
      "status": "pending",
      "reason": "awaiting_context",
      "version": "excursion-v3-experimental",
      "checkedAt": 1704067200000,
      "evidence": {}
    }
  },
  "analysis": {
    "state": "updated",
    "windowPoints": 1,
    "updatedPoints": 0
  }
}
```

`checkedAt` 是服务端最近一次保存标记的时间, 与设备观测时间不同. `updatedPoints` 不包括初始标记. 分析失败或并发版本校验两次都未通过时, 已提交的录入仍返回 `201`, `analysis.state` 为 `deferred`. 不要因为该状态重复上报同一个位置.

## 位置查询

最新位置和列表均支持以下参数, 条件取交集:

| 参数 | 说明 |
| --- | --- |
| `device` | 精确匹配设备 |
| `since` / `until` | 毫秒时间戳或带时区的 ISO 时间, 包含两端 |
| `project` | 按项目名称对应的时间窗口筛选 |
| `view` | `raw` 为默认值; `filtered` 仅排除 `outlier` |

最新位置返回 `{"location": ...}`, 无记录返回 `{"location": null}`. 按观测时间倒序查询, 相同时间按 ID 倒序. 补报历史位置不会取代时间更新的位置.

列表另支持:

| 参数 | 默认值 | 约束 |
| --- | --- | --- |
| `limit` | 500 | 1 到 5000 |
| `offset` | 0 | 非负安全整数 |
| `order` | `asc` | `asc` 或 `desc` |

相同时间按 ID 同方向排序. 过滤发生在分页之前, 同一请求中的位置筛选和质量标记使用同一个数据库快照.

```json
{
  "locations": [],
  "pagination": {"limit": 500, "offset": 0, "hasMore": false}
}
```

每页增加 `offset`, 直到 `hasMore` 为 `false`. 每条位置都包含质量标记. 没有标记的记录返回 `unassessed`, 其版本、检查时间和证据为 `null`.

## 项目

项目只定义时间窗口, 位置不持有项目外键. 创建接受 `name`、`startTime`、`endTime`, 时间格式与位置一致, 返回 `201`:

```json
{"project":{"id":1,"name":"trip","startTime":1000,"endTime":3000}}
```

名称去除首尾空白后不能为空, 最长 200 个 UTF-16 代码单元. 允许起止时间相同、项目重叠及同名, 结束时间不得早于开始.

`PATCH /api/projects/:id` 至少提供上述一个字段, 未提供的字段保留, `null` 不表示省略. 更新按 ID 定位并返回 `200`. 不提供删除接口.

`GET /api/projects/:name` 中名称需要 URL 编码, 同名时取 ID 最小的一条. 纯数字路径也按名称查询. 查询不存在的项目返回 `404`.

项目列表按 `startTime DESC, id DESC` 排序, `limit` 默认为 100、最大为 500, `offset` 默认为 0:

```json
{"projects":[],"pagination":{"limit":100,"offset":0,"hasMore":false}}
```

## 错误

```json
{"error":{"code":"bad_request","message":"latitude must be between -90 and 90","field":"latitude"}}
```

参数或请求体无效返回 `400`, 凭据错误返回 `401`, 路径或项目不存在返回 `404`, 方法不支持返回 `405`, 内容类型不支持返回 `415`, 内部错误返回 `500`. 内部错误不会向客户端暴露数据库信息. 进程缺少必填配置时直接启动失败.

旧 `/location`、`/locations`、`/data` 路径及 `Token` 请求头不受支持.
