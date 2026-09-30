# ADP 错误码与异常状态（bridge 侧）

bridge 返回 **HTTP 状态 + 数字 `code` + `error` 文案**。ADP 接收后映射 FUT 展示；callback 与 HTTP 独立（见 [adp-scope.md](./adp-scope.md) §4）。

**维护**：业务/契约侧可在此表扩展「列表不一致」等 code；bridge 未实现的 code 需开发补映射后再启用。

---

## 1. 架构图「异常状态」三条

| 产品要求 | bridge 行为 | ADP / FUT |
|----------|-------------|-----------|
| 公众号未添加 → 提示不存在 | `GET export` → **404** `404001` `error: 公众号不存在` | 映射文案，引导 **import** |
| 否则返回最新 10 篇 | `GET export?limit=10` → **200** `code:0`，`articles[]` | 写 dataflow；空数组 = 暂无内容 |
| 微信/采集整体是否正常 | `GET /v1/rpc/status` → `ok` / `wechatReady` / `message` | 定时 poll + 状态灯；可选接 callback |

无文章：**200 + `articles:[]`**（不是 404）。  
未 import：**404001**（不是 200 空）。

---

## 2. 错误码表

| code | HTTP | error（稳定键） | 触发场景 | ADP 建议 |
|------|------|-----------------|----------|----------|
| 0 | 200 | — | RPC 成功 | 正常处理 `data` |
| 100001 | 400 | （参数原文） | JSON 绑定失败、缺必填字段 | 修请求 |
| 200001 | 401 | unauthorized | 配置了 `auth_token` 且 Bearer 不对 | 修鉴权 |
| 404001 | 404 | 公众号不存在 | export：biz 不在 wcplus 已入库列表 | 补 import；FUT：「公众号不存在」 |
| 409001 | 409 | 公众号已存在 | import：biz/昵称已存在 | 当成功，继续用已有 biz |
| 503001 | 503 | 采集机未就绪 | **import** 前 Max 或微信 `req_data` 不可用 | 暂停 import；运维/登录转发；**export 不返回 503001**（只读本地库，无文则 200+`[]`） |
| 503002 | 503 | 微信正文采集受限 | export 要正文但全部为空，或 wcplus 返回受限类错误 | 见 `remediation`；调 remediate 或 login/prepare→finish |
| 503004 | 503 | 模拟取链服务未启用 | `simulator.enabled=false` 或服务未初始化 | 启用 simulator 并配置 Windows helper |
| 502001 | 502 | （wcplus/业务原文） | import 失败、export 读库失败、login 失败等 | 重试 + 日志；可选告警 |
| 502002 | 502 | （队列超时原文） | sync + `waitQueue` 超时 | 运维；响应可带部分 `data` |
| 502004 | 502 | （模拟器原文） | Windows helper 无法执行或返回非预期结果 | 查 helper、窗口和坐标配置 |
| 404004 | 404 | 模拟取链任务不存在 | jobId 无效或已清理 | 使用创建任务响应的 jobId |

### 503001 的 `message` 示例（`data.message` 同义字段在 503 body 为 `message`）

- `wcplus 不可达`
- `Max 未激活或不可用`
- `微信参数未就绪，需采集机侧登录转发或 WeChat Auto`

---

## 3. export 成功体（code 0）

| 字段 | 说明 |
|------|------|
| `data.biz` | 请求的 biz |
| `data.officialAccountName` | 号名称 |
| `data.total` | 列表总数（wcplus） |
| `data.articles[]` | 最多 `limit` 条（默认 10） |
| `articles[].name` | 标题 |
| `articles[].link` | 链接 |
| `articles[].time` | Unix 秒 |
| `articles[].id` | 文章 id |
| `articles[].imageUrl` | 封面 |
| `articles[].content` | 正文 HTML（默认拉取；`withContent=false` 可关） |

---

## 4. import 成功（ADP 最少存什么）

| 必存 | 来源 |
|------|------|
| **biz** | `data.candidate.biz` |

`linkTask`、`queueStart` 等仅供运维排查，ADP 可不持久化。

---

## 5. status（code 0）

| 字段 | 含义 |
|------|------|
| `data.ok` | 可服务 import/export |
| `data.wcplusReachable` | wcplus HTTP 可达 |
| `data.maxActive` | Max 激活 |
| `data.wechatReady` | Max 且微信参数就绪 |
| `data.message` | 人类可读说明 |

FUT 映射示例（由 ADP 定稿）：

| 条件 | 展示 |
|------|------|
| `ok=true` | 正常 |
| `ok=false` 且 `!wechatReady` | 需登录/参数（人工） |
| `ok=false` 且 `!maxActive` | Max/授权异常 |

---

## 6. callback 与 code 关系

- **404 / 409**：仅 HTTP，**无** callback。
- **503**：**import** 时 HTTP `503001` + 可选 callback `collector.not_ready`（**export** 不因未就绪返 503001）。
- **502**：HTTP `502001/502002`；`notify_mode=all` 时另有 `alert.triggered`（`import_failed` 等）；`manual_intervention` 时不发 RPC 失败 callback。
- **模拟取链**：`manual_intervention` 会发 `simulator.manual_intervention`；批次完成但存在失败账号会发一次 `simulator.job_failed`。见 [simulated-latest-link.md](./simulated-latest-link.md)。

详见 [adp-scope.md](./adp-scope.md) §4 与 `callback.notify_mode`。

---

## 7. 待扩展（契约预留）

| 场景 | 建议 code | bridge 现状 |
|------|-----------|-------------|
| 列表不一致 | 待定（如 409002） | 多为 502001 + 英文 error |
| 重复导入（业务层） | 409001 已覆盖 wcplus 入库重复 | — |

定稿后同步改 `internal/handler/errors.go` 与 OpenAPI（若有）。
