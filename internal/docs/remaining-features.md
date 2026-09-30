# 契约 API 与边界

对外仅暴露产品聊天中约定的 RPC + `/health`。调试请直接访问 wcplus `http://127.0.0.1:5001` 官方 API。

## RPC

| 能力 | 接口 |
|------|------|
| 导入 | `POST /v1/rpc/import-official-account` |
| 删除 | `POST /v1/rpc/delete-official-account`（`biz` 必填；wcplus 删除路径需在 Windows F12 核对） |
| 同步 | `POST /v1/rpc/sync-official-account`（`waitQueue` 等等队列；`exportAfter` 同步后导出最新） |
| 导出最新 | `GET /v1/rpc/export-latest-articles` |
| 初始化 | `POST /v1/rpc/initialize`（含登录流程说明字段） |
| 登录转发 | `login/prepare`、`login/finish` |
| 状态 | `GET /v1/rpc/login-status` |

## 已移除的扩展面（不影响契约）

- 低层 `/v1/accounts`、`/v1/tasks` 等透传 → 用 RPC 或直连 wcplus
- `search-official-account` → `import` 内部仍会搜号
- `update-official-account` → 使用 `sync` + `exportAfter: true`
- `wait-queue`、`max-status`、`login-guide`、`all-articles` → 合并进 sync / initialize / login-status / export

## MCP（可选，默认不用）

业务对接 **只须 bridge HTTP**。若合同要求 MCP，见 [mcp.md](./mcp.md) 与 `configs/mcp-client.example.json`。

## Bridge 外

- 微信登出：人工
- 全自动开文：WeChat Auto + 同机 wcplus
- UI/win32 模拟初始化：未实现；当前为 API 级 initialize
