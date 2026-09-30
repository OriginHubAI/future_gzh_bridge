# wcplus bridge

Go + Gin 边车：与 **wcplusPro Max** 同机，按产品约定封装 **导入 / 同步 / 导出最新 / 登录转发 / 初始化 / 告警与回调**。

另提供一个可独立启用的 Windows/macOS **模拟取最新文章链接**任务：逐个打开微信文章初始链接，进入文章所属公众号，点击公众号首篇文章并复制新文章链接，按四步流程重复执行，去重后输出链接列表。它不依赖私有协议，也不抓文章正文。见 [模拟取链说明](internal/docs/simulated-latest-link.md)。

**对接集成（推荐先看：接口做什么、怎么做、参数）**：[internal/docs/对接集成手册.md](internal/docs/对接集成手册.md)

**完整说明（项目做了什么 + 全部接口 + 用法）**：[internal/docs/bridge-api-guide.md](internal/docs/bridge-api-guide.md)

**ADP 对接范围（当前只做这些）**：[internal/docs/adp-scope.md](internal/docs/adp-scope.md)

**项目说明（做了什么、怎么做）**：[internal/docs/project-overview.md](internal/docs/project-overview.md)

- [数据导出 API](https://www.wcplus.cn/doc/data_export_api)
- [Max API](https://www.wcplus.cn/doc/api_document)

## 运行

1. 启动 **wcplusPro**（`http://127.0.0.1:5001`），**Max 已激活**。
2. `cp configs/config.example.yaml configs/config.yaml`
3. `go run ./cmd/bridge -config configs/config.yaml`（Windows：`GOOS=windows GOARCH=amd64 go build -o bridge.exe ./cmd/bridge`）

## 对外 RPC（契约面）

| 需求 | 方法 | 路径 |
|------|------|------|
| 导入公众号 | POST | `/v1/rpc/import-official-account`（nickname / biz / 含 `__biz` 的 link） |
| 删除公众号 | POST | `/v1/rpc/delete-official-account`（`biz` 必填） |
| 采集机状态 | GET | `/v1/rpc/status` |
| 同步公众号 | POST | `/v1/rpc/sync-official-account` |
| 同步并导出最新 | POST | 同上，`exportAfter: true`（见下） |
| 导出最新文章 | GET | `/v1/rpc/export-latest-articles` |
| 软件初始化 | POST | `/v1/rpc/initialize` |
| 登录转发 | POST | `/v1/rpc/login/prepare`、`/v1/rpc/login/finish` |
| 状态 | GET | `/v1/rpc/login-status` |
| 微信窗口登录态 | GET | `/v1/rpc/wechat-login-status` |
| 模拟获取最新链接 | POST | `/v1/rpc/latest-article-link-jobs` |
| 模拟任务状态 | GET | `/v1/rpc/latest-article-link-jobs/{jobId}` |
| 模拟结果链接 | GET | `/v1/rpc/latest-article-links?jobId={jobId}` |
| 健康 | GET | `/health` |

失败：HTTP 502；配置了 `alert.webhook_url` 或 `callback.base_url` 时会 POST 告警/事件。

### 同步 + 等队列 + 导出最新（原 update 合并进 sync）

```bash
curl -X POST http://127.0.0.1:19090/v1/rpc/sync-official-account \
  -H "Content-Type: application/json" \
  -d '{
    "biz": "你的Biz==",
    "nickname": "公众号昵称",
    "steps": ["link", "article"],
    "runQueue": true,
    "waitQueue": true,
    "waitTimeoutSec": 600,
    "exportAfter": true,
    "exportLimit": 20,
    "withContent": true
  }'
```

### 仅导出已有数据

```bash
curl "http://127.0.0.1:19090/v1/rpc/export-latest-articles?biz=你的Biz==&limit=20&withContent=true"
```

## 配置

见 `configs/config.example.yaml`：`listen`、`auth_token`、`wcplus.base_url`、`simulator`、`callback`、`alert`。

环境变量：`WCPLUS_BRIDGE_AUTH_TOKEN`、`WCPLUS_BRIDGE_CALLBACK_URL`、`WCPLUS_ALERT_WEBHOOK_URL` 等。

## 登录转发

```bash
curl -X POST http://127.0.0.1:19090/v1/rpc/login/prepare -H "Content-Type: application/json" -d '{}'
# 电脑微信打开目标号文章
curl -X POST http://127.0.0.1:19090/v1/rpc/login/finish
```

`initialize` 响应含 `forwardFlow`、`wcplusTaskURL`、`docs`（原 login-guide 信息）。

详见 [internal/docs/remaining-features.md](internal/docs/remaining-features.md)。

## Windows 模拟取链（独立任务）

启用 `simulator.enabled` 后，bridge 会将单个公众号初始链接交给本机脚本，脚本以 JSON stdin/stdout 返回一条文章链接。默认示例脚本位于 [scripts/windows-simulator](scripts/windows-simulator/README.md)，需在 Windows 上校准窗口标题和点击坐标。

```bash
curl -X POST http://127.0.0.1:19090/v1/rpc/latest-article-link-jobs \
  -H "Content-Type: application/json" \
  -d '{"accounts":[{"id":"demo","initialLink":"https://mp.weixin.qq.com/..."}]}'
```

返回的 `jobId` 用于查询状态和读取仅包含新链接的结果。重复链接会标记并跳过；需要登录、窗口或坐标人工处理时，任务进入 `paused`。

## MCP（可选，默认不用）

远程/业务系统 **只调 bridge HTTP 即可**，不必装 MCP 客户端。若合同要求 Model Context Protocol，见 [internal/docs/mcp.md](internal/docs/mcp.md)。
