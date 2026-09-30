# wcplus bridge 项目说明

本文描述 **当前仓库已实现的内容**、**实现方式**与**使用边界**，便于联调与交付。

**接口与用法完整手册**：[bridge-api-guide.md](./bridge-api-guide.md)

**ADP 当前交付范围（架构图红框）**：[adp-scope.md](./adp-scope.md) — 导入、取最新 10 篇、`/status`、回调通知；同步定时在 wcplus，不在 bridge。错误码与异常状态：[error-codes.md](./error-codes.md)。

**独立 Windows 模拟取链**：[simulated-latest-link.md](./simulated-latest-link.md) — 批量只取每号最新一篇文章 URL，不依赖私有协议或正文采集。

接口参数细节见 README 与联调时的 Apifox/契约说明。

## 1. 背景与目标

**wcplusPro Max** 在本机提供 HTTP API（默认 `http://127.0.0.1:5001`），能力多、粒度细，适合在任务页操作。远程或其它系统需要的是少量稳定动作：

- 导入 / 同步微信公众号数据  
- 导出库内最新文章  
- 检查 Max 授权与微信抓包参数是否就绪  
- 登录转发（Set/Clear Proxy，配合 PC 微信或 WeChat Auto）  
- 初始化时的流程与文档指引  
- 可选：失败告警、成功事件回调  

**wcplus bridge** 是与 wcplus **同机部署**的 Go 边车：对外只暴露约定的 **HTTP RPC**，内部调用 wcplus 已有 API。**不修改 wcplus / Django 源码**。

默认监听：`127.0.0.1:19090`（仅本机；远程访问需 SSH 隧道或改 `listen` 并配置强 `auth_token`）。

## 2. 架构

```
远程系统 / Apifox / 脚本
        │  HTTP（可选 Bearer auth_token）
        ▼
┌──────────────────────────┐
│  bridge.exe (cmd/bridge) │  Gin → handler → service
└──────────────────────────┘
        │  HTTP
        ▼
┌──────────────────────────┐
│  wcplusPro Max (:5001)   │  任务、队列、导出、Proxy、授权
└──────────────────────────┘
        ▲
        │  PC 微信；可选 WeChat Auto（bridge 不直接驱动 Auto）
```

可选组件 **`wcplus-mcp.exe`**（`cmd/mcp-server`）：通过 Model Context Protocol 暴露与 bridge 等价的工具，内部 HTTP 调 bridge。**业务对接默认只需 bridge HTTP**。

## 3. 仓库结构

| 路径 | 说明 |
|------|------|
| `cmd/bridge` | 主程序入口 |
| `cmd/mcp-server` | 可选 MCP 服务 |
| `internal/handler` | HTTP 路由、鉴权、JSON、错误码与告警触发 |
| `internal/service` | 导入、同步、等队列、导出、登录状态、登录会话 |
| `internal/simulator` | 独立模拟取链任务、批量状态、重试、持久化和链接去重 |
| `internal/wcplus` | wcplus API 客户端与类型适配 |
| `internal/config` | YAML + 环境变量 |
| `internal/callback` / `internal/alert` | 事件与 webhook |
| `internal/bridgeclient` | MCP 调 bridge 用 |
| `configs/` | `config.example.yaml` |
| `scripts/build-windows.sh` | 交叉编译 Windows 产物到 `dist/wcplus-bridge/` |

技术栈：Go 1.23、Gin、yaml.v3；MCP 使用 `github.com/modelcontextprotocol/go-sdk` v1.3.1。

## 4. 对外能力（契约面）

| 能力 | 方法 | 路径 | 简要说明 |
|------|------|------|----------|
| 健康检查 | GET | `/health` | bridge 与 wcplus 连通性 |
| 登录状态 | GET | `/v1/rpc/login-status` | Max 是否可用、`req_data`、是否需登录转发 |
| 微信窗口登录态 | GET | `/v1/rpc/wechat-login-status` | 读本机微信窗口标题，不经过 wcplus |
| 初始化 | POST | `/v1/rpc/initialize` | 授权 + 状态 + 流程说明 + 文档链接 |
| 登录转发 | POST | `/v1/rpc/login/prepare`、`/login/finish` | Set/Clear Proxy |
| 导入公众号 | POST | `/v1/rpc/import-official-account` | 搜号/解析 biz，建 link 任务 |
| 同步公众号 | POST | `/v1/rpc/sync-official-account` | 多 step 建任务、跑队列、可选等队列与导出 |
| 导出最新 | GET | `/v1/rpc/export-latest-articles` | 读库最新 N 篇，可选正文 |
| 模拟取最新链接 | POST/GET | `/v1/rpc/latest-article-link-jobs`、`/latest-article-links` | Windows UI 操作，只输出新的文章 URL |

统一成功体：`{"code":0,"data":...}`（`/health` 例外）。失败多为 HTTP 502；参数错误 400；配置了 `auth_token` 时缺 Bearer 为 401。

**已刻意不提供**：低层 `/v1/accounts`、`/v1/tasks` 透传、`update-official-account` 独立接口等；能力已合并进上表 RPC。详见 [remaining-features.md](./remaining-features.md)。

## 5. 实现说明（怎么做）

### 5.1 请求链路

1. `AuthMiddleware`：`auth_token` 非空时校验 `Authorization: Bearer <token>`。  
2. Handler 绑定 JSON/Query，调用对应 Service。  
3. Service 编排 wcplus `Client` 多次调用（必要时间隔，如 sync 多 step 间隔 3 秒）。  
4. 失败：`Notifier.Trigger` → callback `alert.triggered` + 可选 `alert.webhook_url`；部分成功路径返回 502 且带部分 `data`。  
5. 成功：可选 callback 成功事件（如 `import.succeeded`、`sync_export.succeeded`）。

### 5.2 各 RPC 与 wcplus API 对应关系

| Bridge 能力 | 主要 wcplus 接口 |
|-------------|------------------|
| Ping / health | `GET /api/gzh/list` |
| import | `search_gzh` / `gzh/search` / `gzh/list` + `POST /api/task/new`（gzh_article_link）+ `task/control run` |
| delete | `POST /api/gzh/delete`（及备选路径，见 `gzh_delete.go`；公开文档未列，需 F12 核对） |
| sync | 多次 `task/new`（link / article / reading_data）+ `task/control run` |
| waitQueue | 轮询 `GET /api/task/all` |
| export | `GET /api/report/gzh_articles` + 可选 `GET /api/article/content` |
| login-status | `task/control`（探 Max）、`GET /api/settings/license`、`GET /api/req_data/get_gzh` |
| login prepare/finish | `POST /api/settings/proxy/set`、`/unset` |

适配细节示例：

- 搜号 JSON 形态不一 → `internal/wcplus/candidate.go` 多路径解析。  
- `task/all` 字段 `Tasks` / `tasks` → 自定义反序列化。  
- 文章 `PDate` 统一为 Unix 秒（`int64`）。

### 5.3 同步 step 语义（bridge 固定传给 wcplus 的参数）

| step | wcplus crawlerType | 用途 |
|------|-------------------|------|
| `link` | `gzh_article_link` | 文章链接列表 |
| `article` | `article` | 文章内容 |
| `reading` | `reading_data` | 阅读数据 |

`steps` 省略时默认 `link` + `article`。`sync` 上还可组合：

- `waitQueue` / `waitTimeoutSec`：等任务队列空闲（默认超时 600s，轮询间隔 10s）。  
- `exportAfter` / `exportLimit` / `withContent`：同步（及 wait）成功后导出最新（原「update + 导出」合并能力）。

### 5.4 登录与 WeChat Auto

- bridge 只调用 wcplus **Proxy Set/Unset**，与任务页按钮一致。  
- **WeChat Auto** 为 wcplus 官方 Windows 插件，可自动开文；与 bridge **不要同时抢 Proxy**（二选一或先 Auto 再 finish）。  
- 原有登录转发路径不驱动微信窗口。独立的模拟取链任务则通过可配置 Windows helper 操作浏览器或微信窗口，二者互不抢 Proxy；见 [simulated-latest-link.md](./simulated-latest-link.md)。

### 5.5 配置

`configs/config.yaml`（由 `config.example.yaml` 复制）：

| 项 | 含义 |
|----|------|
| `listen` | bridge 监听地址 |
| `auth_token` | 非空则启用 Bearer 鉴权 |
| `wcplus.base_url` | wcplus 根 URL |
| `wcplus.timeout_sec` | 调 wcplus 超时 |
| `simulator.*` | 可选 Windows helper、任务持久化、单号超时与重试 |
| `callback.base_url` / `token` | 事件 POST |
| `alert.webhook_url` | 失败额外 webhook |

环境变量：`WCPLUS_BRIDGE_LISTEN`、`WCPLUS_BRIDGE_AUTH_TOKEN`、`WCPLUS_BASE_URL`、`WCPLUS_BRIDGE_CALLBACK_URL` 等（见 `internal/config/config.go`）。

## 6. 构建与部署

```bash
# 开发机（Mac/Linux）
go run ./cmd/bridge -config configs/config.yaml

# Windows 交付包
./scripts/build-windows.sh
# 产出：dist/wcplus-bridge/bridge.exe、wcplus-mcp.exe、configs/config.yaml
```

Windows 运行顺序：

1. 启动 wcplusPro（5001）。  
2. `bridge.exe -config configs\config.yaml`。  
3. 本机验证：`curl http://127.0.0.1:19090/health`。

若启用独立模拟取链，还需安装 Python helper 依赖并校准窗口坐标，见 [simulated-latest-link.md](./simulated-latest-link.md)。该能力的状态在 `GET /v1/rpc/simulator-status`，不混入原有 wcplus `/status`。

Apifox 或其它机器访问时，须与 bridge **同机**或使用端口转发；`127.0.0.1:19090` 仅指运行 bridge 的那台机器。

## 7. 典型业务流程

```
health / initialize / login-status
  ├─ needsManualLogin → login/prepare →（微信或 WeChat Auto 打开目标号文章）→ login/finish
  ├─ 新号 → import-official-account
  └─ 更新 → sync-official-account（可选 waitQueue、exportAfter）
       或只读 → export-latest-articles?biz=...
```

## 8. 常见问题（与 bridge 关系）

| wcplus 日志 | 含义 | 建议 |
|-------------|------|------|
| `ReqDataNotMatchError` / `weixin_article_list` | 任务 biz 与当前微信 `req_data` 不匹配 | 对**目标号**做登录转发；确认微信里打开的是同一公众号；再 sync |

bridge 只建任务与读状态；**参数是否匹配由 wcplus + 微信抓包决定**。

## 9. 文档索引

| 文档 | 内容 |
|------|------|
| [README.md](../../README.md) | 快速运行、cURL 示例 |
| [remaining-features.md](./remaining-features.md) | 契约边界、已移除接口 |
| [mcp.md](./mcp.md) | 可选 MCP 工具列表 |

## 10. 明确不在本项目范围

- 修改 wcplus / Django 或替代 wcplus 采集引擎  
- 远程机器上不跑 wcplus 却期望 bridge 单独完成采集  
- 必须的一键 WeChat Auto 编排（当前无 Auto 调用 API）  
- 低层 wcplus API 全量网关  

---

*文档随代码仓库维护；接口以 `internal/handler/rpc.go` 注册为准。*
