# wcplus bridge — 项目说明与接口使用手册

本文档汇总 **当前项目已实现的内容**、**全部 HTTP 接口**及 **调用方式**，供 ADP、运维与联调使用。

- **对接集成手册（推荐）**：[对接集成手册.md](./对接集成手册.md)
- 对接范围（红框）：[adp-scope.md](./adp-scope.md)
- 错误码与异常状态：[error-codes.md](./error-codes.md)
- 实现细节与目录：[project-overview.md](./project-overview.md)

---

## 1. 项目是什么、做了什么

**wcplus bridge** 是与 **wcplusPro Max** 同机部署的 Go 边车（默认 `127.0.0.1:19090`）。对外提供少量稳定 **HTTP RPC**，内部转发 wcplus 已有 API（默认 `http://127.0.0.1:5001`）。**不修改** wcplus / Django 源码。

### 1.1 已实现能力

| 类别 | 内容 |
|------|------|
| **ADP 主路径** | 公众号导入/删除、按 biz 导出最新 N 篇文章（含正文）、采集/微信通道状态查询 |
| **通知** | 可选 POST 到 ADP 提供的 `callback.base_url`；可选 `watchdog` 检测掉线需人工干预 |
| **运维** | 登录转发（Proxy Set/Clear）、详细登录状态、初始化指引、健康检查 |
| **运维扩展** | 同步公众号（多 step 建任务、等队列、同步后导出）— 非 ADP 日常必选 |
| **数据** | **不**自建文章仓库；**proxy 读** wcplus 已入库数据 |
| **定时抓取** | 由 **wcplus 主程序 + 定时任务** 负责，**不在 bridge** |

### 1.2 分工（与架构图一致）

| 系统 | 职责 |
|------|------|
| **wcplus** | 微信采集、定时更新本地库、Max 授权、`req_data` 抓包存储 |
| **bridge** | import / delete / export / status；错误码 404/409/503/502；出站 callback |
| **ADP** | 存 **biz**；定时 `export?limit=10`；写 dataflow；提供 **通知接收 URL**；批量 import = **循环单号** 调 bridge |

### 1.3 端到端（ADP 推荐）

```text
1. POST import-official-account → ADP 保存 data.candidate.biz
2. wcplus 内配置定时抓取（采集机）
3. ADP 定时 GET export-latest-articles?biz=&limit=10 → dataflow
4. 异常：404001 补 import；503001 / status 暂停拉取；callback 推运维/FUT
```

---

## 2. 运行与鉴权

### 2.1 启动

```bash
# 1. 先启动 wcplusPro（5001），Max 已激活
# 2. 配置
cp configs/config.example.yaml configs/config.yaml
# 3. 启动 bridge
go run ./cmd/bridge -config configs/config.yaml
```

Windows 交叉编译：`scripts/build-windows.sh` → `dist/wcplus-bridge/bridge.exe`。

### 2.2 配置要点（`configs/config.yaml`）

| 键 | 说明 |
|----|------|
| `listen` | 默认 `127.0.0.1:19090` |
| `auth_token` | 非空时所有路由需 `Authorization: Bearer <token>` |
| `wcplus.base_url` | wcplus 根 URL |
| `wcplus.timeout_sec` | 调 wcplus 超时，建议 ≥120 |
| `callback.base_url` | ADP 通知接收地址（bridge **出站 POST**） |
| `callback.token` | 可选，callback 请求 Bearer |
| `callback.notify_mode` | `manual_intervention`（推荐）或 `all` |
| `watchdog.enabled` / `interval_sec` | 采集机就绪→未就绪时 callback |
| `alert.webhook_url` | 可选企微/飞书 webhook |

环境变量：`WCPLUS_BRIDGE_AUTH_TOKEN`、`WCPLUS_BRIDGE_CALLBACK_URL`、`WCPLUS_BASE_URL` 等（见 README）。

### 2.3 统一响应约定

**成功（除 `/health`）：**

```json
{ "code": 0, "data": { ... } }
```

**失败：**

```json
{ "code": 404001, "error": "公众号不存在", "biz": "Mz..." }
```

完整错误码见 [error-codes.md](./error-codes.md)。

**鉴权失败：** HTTP 401，`code: 200001`。

---

## 3. 接口一览

| 方法 | 路径 | ADP 主路径 | 说明 |
|------|------|:----------:|------|
| GET | `/health` | | bridge 与 wcplus 连通 |
| POST | `/v1/rpc/import-official-account` | ✅ | 导入公众号 |
| POST | `/v1/rpc/delete-official-account` | ✅ | 从 wcplus 删除公众号及本地数据 |
| GET | `/v1/rpc/export-latest-articles` | ✅ | 最新 N 篇文章 |
| GET | `/v1/rpc/status` | ✅ | 采集/微信通道是否就绪 |
| POST | （无） | | ADP 提供 URL，bridge **callback 出站** |
| POST | `/v1/rpc/remediate-wechat-content-restriction` | | 正文受限：Set Proxy + 指引 |
| POST | `/v1/rpc/sync-official-account` | | 运维：补采 + 可选等队列/导出 |
| GET | `/v1/rpc/login-status` | | 运维：Max、req_data、是否需登录 |
| POST | `/v1/rpc/login/prepare` | | 运维：Set Proxy |
| POST | `/v1/rpc/login/finish` | | 运维：Clear Proxy |
| POST | `/v1/rpc/initialize` | | 运维：授权 + 流程说明 |

基址示例：`http://127.0.0.1:19090`（远程需隧道或改 `listen` + 强 token）。

---

## 4. 接口详细说明

### 4.1 健康检查

```http
GET /health
```

**响应（无 `code` 字段）：**

```json
{ "ok": true, "wcplus": "up" }
```

---

### 4.2 导入公众号（ADP）

```http
POST /v1/rpc/import-official-account
Content-Type: application/json
Authorization: Bearer <token>   # 若配置了 auth_token
```

**请求体（三选一或组合）：**

方式 A — 文章链接：

```json
{
  "articleLink": "https://mp.weixin.qq.com/s/xxxxxxxx",
  "runQueue": true
}
```

方式 B — 昵称 / biz / 主页 link：

```json
{
  "nickname": "36氪",
  "link": "https://mp.weixin.qq.com/mp/profile_ext?action=home&__biz=Mzxxxx==",
  "runQueue": true
}
```

| 字段 | 说明 |
|------|------|
| `articleLink` | `mp.weixin.qq.com/s/...` |
| `nickname` / `biz` / `link` | 搜号或带 `__biz` 的主页 URL |
| `runQueue` | 省略时默认 `true` 并执行 `task/control run`；显式传 `false` 时只创建或复用任务，不启动队列 |

**成功：** HTTP 200，`code: 0`。ADP **必须持久化** `data.candidate.biz`。若同一 `biz` 已有未完成的文章链接任务，返回已有任务，并在 `data.taskReused=true` 标记复用。

**常见错误：**

| HTTP | code | 含义 |
|------|------|------|
| 409 | 409001 | 号已在 wcplus |
| 503 | 503001 | 采集机未就绪 |
| 502 | 502001 | wcplus/搜号失败等 |
| 400 | 100001 | JSON 无效 |

**示例：**

```bash
curl -sS -X POST "http://127.0.0.1:19090/v1/rpc/import-official-account" \
  -H "Content-Type: application/json" \
  -d '{"nickname":"36氪","runQueue":true}'
```

**批量：** 无批量 RPC；ADP 对多个链接 **循环** 调用本接口，409 当已存在跳过。

**注意：** `articleLink` 依赖 wcplus 真实 API，需在 Windows F12 核对；失败时见 502001。

---

### 4.3 删除公众号（ADP）

用户取消订阅或 ADP 侧下线 biz 时，从 **wcplus 本地库** 删除该号（与 wcplus UI「删除」一致）。**不**删除 ADP/FUT 业务记录，由 ADP 自行维护。

```http
POST /v1/rpc/delete-official-account
Content-Type: application/json
```

```json
{ "biz": "Mzxxxx==", "nickname": "36氪" }
```

| 字段 | 说明 |
|------|------|
| `biz` | 必填，与 import 保存的 biz 一致 |
| `nickname` | 可选；缺省时 bridge 从 wcplus 列表解析 |

| HTTP | code | 含义 |
|------|------|------|
| 200 | 0 | 已删除 |
| 404 | 404001 | wcplus 中不存在该 biz |
| 502 | 502001 | wcplus 删除 API 失败或路径未匹配（F12 核对真实 POST） |

删除成功后，同一 biz 再 `export` 应返回 **404001**；ADP 可再次 `import` 重新入库。

```bash
curl -sS -X POST "http://127.0.0.1:19090/v1/rpc/delete-official-account" \
  -H "Content-Type: application/json" \
  -d '{"biz":"Mzxxxx=="}'
```

---

### 4.4 导出最新文章（ADP）

```http
GET /v1/rpc/export-latest-articles?biz=Mzxxxx==&limit=10&withContent=true
```

| Query | 默认 | 说明 |
|-------|------|------|
| `biz` | 必填 | 已 import 的 biz |
| `nickname` | 空 | 可选，辅助拉正文 |
| `limit` | 10 | 1～100 |
| `withContent` | true | `false`/`0` 不拉 HTML 正文 |

**成功：** HTTP 200，`code: 0`。

**`data.articles[]` 字段：**

| 字段 | 说明 |
|------|------|
| `name` | 标题 |
| `link` | 文章 URL |
| `time` | 发布时间 Unix 秒 |
| `id` | 文章 id |
| `imageUrl` | 封面 |
| `content` | 正文 HTML |

**异常（架构图「异常状态」）：**

| 情况 | HTTP | code |
|------|------|------|
| biz 未在 wcplus 入库 | 404 | 404001 |
| 号在库、暂无文章 | 200 | 0，`articles:[]` |
| 采集机整体异常（export 不拦） | — | 用 **GET /status**；export 仍可能 200+`[]` 或返回已有本地数据 |
| 读 wcplus 失败 | 502 | 502001 |
| 有文章列表但正文全空（微信受限） | 503 | **503002**，带 `data` 摘要 + `remediation` |

**503002 响应示例（wcplus 日志「采集文章内容受限」时 ADP 可对齐）：**

```json
{
  "code": 503002,
  "error": "微信正文采集受限",
  "message": "微信正文采集受限，需采集机 Set Proxy 并在 PC 微信内打开公众号文章",
  "data": { "biz": "...", "articles": [ { "name": "...", "link": "...", "content": "" } ] },
  "remediation": {
    "recommendedArticleURL": "https://mp.weixin.qq.com/s/...",
    "steps": ["..."],
    "prepare": "POST /v1/rpc/login/prepare",
    "finish": "POST /v1/rpc/login/finish",
    "remediate": "POST /v1/rpc/remediate-wechat-content-restriction"
  }
}
```

同时 callback（manual 模式）：`alertType: wechat.content_restricted`。

**示例：**

```bash
curl -sS "http://127.0.0.1:19090/v1/rpc/export-latest-articles?biz=Mzxxxx==&limit=10"
```

网关/客户端超时建议 **≥120s**（10 篇正文）。

**说明：** 只读 wcplus **已有库**；新数据依赖 wcplus 定时/任务，bridge 不触发全量重采。

---

### 4.5 状态查询（ADP / FUT）

```http
GET /v1/rpc/status
```

**成功：**

```json
{
  "code": 0,
  "data": {
    "ok": true,
    "wcplusReachable": true,
    "maxActive": true,
    "wechatReady": true,
    "message": "正常"
  }
}
```

| 字段 | 含义 |
|------|------|
| `ok` | 可否正常 import/export |
| `wechatReady` | Max 且微信 `req_data` 就绪 |
| `message` | 未就绪时的原因说明 |

ADP 可定时 poll；FUT 状态灯由 ADP 映射 `ok` / `wechatReady`。

---

### 4.6 通知 callback（ADP 被动接收）

bridge **不**提供入站「ADP 调 bridge 通知」接口。ADP 提供 **HTTP POST 接收地址**，写入 `callback.base_url`。

**请求（bridge → ADP）：**

```json
{
  "type": "alert.triggered",
  "occurredAt": "2026-09-17T12:00:00Z",
  "payload": {
    "alertType": "collector.manual_required",
    "message": "微信参数未就绪...",
    "maxActive": true,
    "wechatReady": false
  }
}
```

`notify_mode: manual_intervention`（推荐）时，主要包含：

| alertType | 含义 |
|-----------|------|
| `collector.not_ready` | import 时 503 |
| `collector.manual_required` | watchdog：曾就绪→未就绪 |
| `login.still_required` | login/finish 后仍缺参数 |

`notify_mode: all` 时另含 `import_failed`、`import.succeeded` 等。

ADP 应 **快速返回 2xx**；失败不会改变 RPC 结果。

---

### 4.7 解除微信正文受限（运维 / ADP 一键入口）

对应 wcplus WARN：**Set Proxy → PC 微信打开文章 → Clear Proxy**。

```http
POST /v1/rpc/remediate-wechat-content-restriction
Content-Type: application/json

{ "biz": "Mzxxxx==", "articleURL": "https://mp.weixin.qq.com/s/..." }
```

| 字段 | 说明 |
|------|------|
| `biz` | 可选；无 `articleURL` 时用该号最近文章 link |
| `articleURL` | 推荐打开的文章（可与 wcplus 日志一致） |

**成功：** `code: 0`，`data.prepare`（已 Set Proxy）、`data.remediationSteps`。运维在微信内开文后调 **`POST /login/finish`**。

---

### 4.8 同步公众号（运维，非 ADP 日常）

```http
POST /v1/rpc/sync-official-account
Content-Type: application/json
```

**请求体示例：**

```json
{
  "biz": "Mzxxxx==",
  "nickname": "公众号昵称",
  "steps": ["link", "article"],
  "runQueue": true,
  "waitQueue": true,
  "waitTimeoutSec": 600,
  "exportAfter": true,
  "exportLimit": 20,
  "withContent": true
}
```

| 字段 | 说明 |
|------|------|
| `steps` | `link` / `article` / `reading`；省略默认 link+article |
| `waitQueue` | 轮询任务队列直到空闲或超时 |
| `exportAfter` | 同步（及 wait）成功后调用 export |

**注意：** sync **不**强制 `EnsureCollectorReady`（与 import/export 不同）。队列超时：**502002**。

---

### 4.9 登录状态（运维）

```http
GET /v1/rpc/login-status
```

返回 Max 状态、`reqData`（wcplus `GET /api/req_data/get_gzh` 透传）、`needsManualLogin`、`recommendedAction`。

---

### 4.10 登录转发（运维）

```http
POST /v1/rpc/login/prepare
Content-Type: application/json
{}
```

可选：`{"articleURL":"https://mp.weixin.qq.com/s/..."}` 写入操作指引。

用户在 **PC 微信** 打开目标号图文 → 然后：

```http
POST /v1/rpc/login/finish
```

finish 后若仍 `needsManualLogin`，可能 callback `login.still_required`。

微信参数来源：wcplus 在 Proxy 抓包后写入；见 [project-overview.md](./project-overview.md) §5.4。

---

### 4.11 初始化（运维）

```http
POST /v1/rpc/initialize
```

返回 license 摘要、`loginStatus`、`forwardFlow`、`wcplusTaskURL`、文档链接、`nextActions`。

---

## 5. 错误码速查

| code | HTTP | 说明 |
|------|------|------|
| 0 | 200 | 成功 |
| 100001 | 400 | 参数错误 |
| 200001 | 401 | 未授权 |
| 404001 | 404 | export：公众号不存在 |
| 409001 | 409 | import：公众号已存在 |
| 503001 | 503 | 采集机未就绪 |
| 503002 | 503 | 微信正文采集受限 |
| 502001 | 502 | wcplus/业务失败 |
| 502002 | 502 | sync 等队列超时 |

未单独成码的场景（列表不一致、搜号失败、链接导入不可用等）目前多为 **502001** + 动态 `error`，见 [error-codes.md](./error-codes.md) §7。

---

## 6. 联调检查清单

- [ ] wcplus + bridge 同机；wcplus **定时抓取** 已开  
- [ ] `callback.base_url`、`watchdog.enabled`、生产 `auth_token`  
- [ ] import 成功并存 **biz**  
- [ ] export 未 import → **404001**  
- [ ] export 已 import 无文 → **200** `articles:[]`  
- [ ] 参数空 → **503001** 或 status `ok=false`  
- [ ] articleLink 在 Windows F12 验证（若使用链接导入）  

---

## 7. 刻意未提供 / 范围外

- bridge 内 **cron** 定时 export  
- bridge **本地文章仓库**（仅 proxy wcplus）  
- **批量 import** 单 RPC  
- wcplus **弹窗 → bridge** push（登出靠 status/watchdog/503）  
- 低层 wcplus API 全量透传（见 [remaining-features.md](./remaining-features.md)）  
- 可选 **MCP** 见 [mcp.md](./mcp.md)，业务默认只调 HTTP  

---

## 8. 相关仓库路径

| 路径 | 说明 |
|------|------|
| `cmd/bridge` | 主程序 |
| `internal/handler/rpc.go` | RPC 路由与入参 |
| `internal/handler/errors.go` | 404/409/503 |
| `internal/service/export_latest.go` | 导出逻辑 |
| `internal/service/import_account.go` | 导入逻辑 |
| `internal/service/adp_status.go` | `/status` |
| `internal/alert/notifier.go` | callback |
| `configs/config.example.yaml` | 配置模板 |
