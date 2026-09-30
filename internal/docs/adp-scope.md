# ADP 对接范围与实施指南






**wcplus bridge** 代理 **wcplusPro Max**：产品范围 = 架构图红框（链接导入、文章 proxy 读、登出/异常通知）+ **异常状态三条** + **删除公众号**；定时抓取在 **wcplus**，不在 bridge。


| 产品项 | bridge |
|--------|--------|
| 链接导入 | `POST import-official-account` |
| 文章仓库（proxy） | `GET export-latest-articles?limit=10` |
| 异常：不存在 / 最新 10 篇 / 整体状态 | `404001` / 200+`articles[]` / `GET status` |
| 登出与人工干预 | `503001` + callback（`status` + `watchdog`） |
| 删除公众号 | `POST delete-official-account` |

完整参数与流程：[对接集成手册.md](./对接集成手册.md) §0。

## 分工

| 系统 | 职责 |
|------|------|
| ADP | 存 **biz**；定时 `export`；写 dataflow；提供 **callback URL** |
| bridge | `import` / `delete` / `export` / `status`；503/404/409；失败与 `collector.manual_required` 回调 |
| wcplus | 微信采集、**自带定时**更新库；Max / 登录转发在采集机 |

## 端到端流程

```text
【新号】ADP → POST import → 保存 data.candidate.Biz
【日常】wcplus 定时采数 → ADP 定时 GET export?biz=&limit=10 → dataflow
【异常】404 补 import；503 运维；callback 告警
```

---

## 1. 导入公众号

```http
POST /v1/rpc/import-official-account
Content-Type: application/json
```

**方式 A（与 wcplus 弹窗一致）**

```json
{
  "articleLink": "https://mp.weixin.qq.com/s/xxxxxxxx",
  "runQueue": true
}
```

**方式 B（昵称 / 主页 `__biz`）**

```json
{
  "nickname": "36氪",
  "link": "https://mp.weixin.qq.com/mp/profile_ext?action=home&__biz=Mzxxxx==",
  "runQueue": true
}
```

| 字段 | 说明 |
|------|------|
| `articleLink` | `mp.weixin.qq.com/s/...` 文章链接 |
| `nickname` / `biz` / `link` | 见方式 B |
| `runQueue` | 省略时默认 true 并启动队列；显式 false 时只创建或复用任务 |

同一 `biz` 若已有未完成的文章链接任务，接口返回该任务并在 `data.taskReused=true` 标记复用，避免重试产生重复任务。

若 A 返回 API 不可用：在 Windows 打开 wcplus，F12 网络里点一次「通过文章链接导入」，把真实 POST 路径发给开发补到 bridge。

| HTTP | code | 含义 |
|------|------|------|
| 200 | 0 | 成功，`data.candidate.Biz` 必须存库 |
| 409 | 409001 | 公众号已存在 |
| 503 | 503001 | 采集机未就绪（Max/微信参数） |
| 502 | 502001 | wcplus 失败 |

---

## 2. 取最新文章（按 biz）

```http
GET /v1/rpc/export-latest-articles?biz=Mzxxxx==&limit=10
```

默认 `limit=10`、默认带正文（`withContent=false` 可关）。

| HTTP | code | 含义 |
|------|------|------|
| 200 | 0 | 见下文字段 |
| 404 | 404001 | 公众号不存在 |
| 503 | 503001 | 采集机未就绪（**import**；export 只读库不返此码） |
| 502 | 502001 | 读 wcplus 失败 |

**`data.articles[]`：** `name`、`link`、`time`（Unix 秒）、`imageUrl`、`content`（HTML）、`id`

**`data` 顶层：** `biz`、`officialAccountName`、`total`

ADP/网关建议超时 **≥120s**（10 篇正文）。

---

## 2.1 删除公众号（取消订阅 / 下线采集）

```http
POST /v1/rpc/delete-official-account
Content-Type: application/json
```

```json
{ "biz": "Mzxxxx==" }
```

| HTTP | code | 含义 |
|------|------|------|
| 200 | 0 | wcplus 侧已删除 |
| 404 | 404001 | biz 不在 wcplus |
| 502 | 502001 | wcplus 删除 API 不可用（Windows F12 核对路径） |

ADP 删 FUT 订阅记录与 dataflow 策略由 **ADP 自行处理**；本接口只清理采集机 wcplus 本地数据。

---

## 3. 异常状态（架构图三条）

| 要求 | bridge | ADP |
|------|--------|-----|
| 未添加 → 提示不存在 | export → **404001** | FUT 文案；触发 import |
| 否则最新 10 篇 | export `limit=10` → **200**；无文 → `articles:[]` | 定时 export → dataflow |
| 微信/采集整体是否正常 | **GET /v1/rpc/status** | poll + 状态灯；见 [error-codes.md](./error-codes.md) |

```http
GET /v1/rpc/status
```

`data.ok` / `wechatReady` / `message` — ADP 可选调；掉线配合 **watchdog callback** 与 import **503001**（export 只读库不返 503001）。

---

## 4. 通知（ADP 被动）

配置 `callback.base_url`（+ `token`）。bridge **出站 POST** 到 ADP 提供的通知 URL；批量 import 由 **ADP 循环单号 import**，无 bridge 批量 RPC。

`callback.notify_mode`:

| 值 | 行为 |
|----|------|
| `manual_intervention`（**推荐**） | 仅 POST：`collector.not_ready`、`collector.manual_required`、`login.still_required` |
| `all`（默认兼容） | 另含 RPC 失败 `alert.triggered`、`import.succeeded` 等 |

| 事件 | 何时 | manual 模式 |
|------|------|-------------|
| `alert.triggered` + `collector.not_ready` | import/export 503 | ✅ |
| `alert.triggered` + `collector.manual_required` | watchdog 就绪→未就绪 | ✅ |
| `alert.triggered` + `login.still_required` | login/finish 仍缺参数 | ✅ |
| `alert.triggered` + `wechat.content_restricted` | export 503002 正文受限 | ✅ |
| `alert.triggered` + `import_failed` 等 | RPC 502 | ❌ |
| `import.succeeded` / `sync.succeeded` | 成功 | ❌ |

启用后台巡检：`watchdog.enabled: true`（见 `config.example.yaml`）。

---

## 错误码汇总

完整表与 FUT 映射见 **[error-codes.md](./error-codes.md)**。

| code | HTTP | error |
|------|------|-------|
| 0 | 200 | 成功 |
| 100001 | 400 | 参数错误 |
| 200001 | 401 | 未授权 |
| 404001 | 404 | 公众号不存在 |
| 409001 | 409 | 公众号已存在 |
| 503001 | 503 | 采集机未就绪 |
| 503002 | 503 | 微信正文采集受限（export 正文全空等） |
| 502001/502002 | 502 | wcplus/队列 |

---

## 运维清单

**采集机**

- wcplus + `bridge.exe` 同机自启；生产 `auth_token`
- wcplus 内配置 **定时抓取**
- `callback.base_url` = ADP 通知 URL；`notify_mode: manual_intervention`；`watchdog.enabled: true`
- Windows 验证 **articleLink** 导入（F12 → 不对则改 `internal/wcplus/import_article.go`）

**ADP（业务侧完成对接）**

- 提供 **通知 POST 接收**（2xx ACK）；映射 FUT 见 [error-codes.md](./error-codes.md)
- import 存 **biz**；定时 **export?limit=10** → dataflow
- 定时 **GET /status**；处理 **404001 / 503001**
- 批量新号：**循环** `POST import`（无需 bridge 批量接口）

**联调用例**

- 未 import 的 biz export → 404001
- 已 import 无文 → 200 `articles:[]`
- 参数空 → 503001 或 status `ok=false`
- watchdog：就绪→未就绪 → callback `collector.manual_required`

## 其它文档

- [对接集成手册.md](./对接集成手册.md) — **ADP/FUT 对接（做什么、怎么做、参数）**
- [bridge-api-guide.md](./bridge-api-guide.md) — 项目与接口完整手册（含 cURL）
- [project-overview.md](./project-overview.md)
- [remaining-features.md](./remaining-features.md)
