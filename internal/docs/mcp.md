# wcplus MCP Server（可选）

**默认交付不用 MCP**：业务/远程直接调 **bridge** 的 `http://127.0.0.1:19090/v1/rpc/*` 即可。

仅当上游明确要求 **Model Context Protocol（Tools + stdio）** 时，再编译 `wcplus-mcp.exe`；与是否使用 Cursor **无关**。

## 架构

```text
MCP 客户端（任选） --stdio--> wcplus-mcp.exe --HTTP--> bridge.exe --> wcplus :5001
```

## 编译

```bash
GOOS=windows GOARCH=amd64 go build -o bridge.exe ./cmd/bridge

# 可选
GOOS=windows GOARCH=amd64 go build -o wcplus-mcp.exe ./cmd/mcp-server
```

`scripts/build-windows.sh` 会同时打出两个 exe；若不用 MCP，部署时 **只拷 bridge.exe** 即可。

## 环境变量（仅 wcplus-mcp 进程）

| 变量 | 默认 | 说明 |
|------|------|------|
| `WCPLUS_BRIDGE_URL` | `http://127.0.0.1:19090` | bridge 地址 |
| `WCPLUS_BRIDGE_AUTH_TOKEN` | 空 | 与 bridge `auth_token` 一致 |

## Tools

- `wcplus_initialize`
- `wcplus_login_status`
- `wcplus_login_prepare` / `wcplus_login_finish`
- `wcplus_import_official_account`
- `wcplus_sync_official_account`
- `wcplus_export_latest_articles`

## 客户端怎么连（通用）

1. 先启动 **wcplus** 与 **bridge.exe**。  
2. 在任意支持 MCP 的客户端里配置：**启动命令** = `wcplus-mcp.exe` 绝对路径，**环境变量**见上表。  
3. JSON 字段名因客户端而异；[configs/mcp-client.example.json](../configs/mcp-client.example.json) 仅为一种常见写法参考，**不是**运行 bridge 的必需配置。

## 与 HTTP RPC 的关系

- **推荐（不用 MCP）**：`POST/GET` → `/v1/rpc/...`  
- **MCP**：同一能力的 Tool 封装，内部仍调 bridge
