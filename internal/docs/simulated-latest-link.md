# Windows 模拟获取公众号最新文章链接

该能力是独立于 wcplus 采集接口的批处理任务。主流程固定为四步：打开一篇真实微信文章、点击文章中的公众号入口、点击公众号页面的**第一篇**文章、复制新打开文章的 URL。四步对账号列表串行重复，再与已有链接去重。为兼容已有调用，也支持直接从公众号主页进入第一篇文章。

它不解析 MMT/TRS/MMTLS 等私有协议，不抓历史文章、不拉正文、不做 OCR，也不尝试绕过微信登录、验证码或风控。

## 运行结构

```text
业务系统
  └─ POST latest-article-link-jobs
       └─ bridge 任务管理器（串行、重试、去重、持久化、状态）
            └─ Windows 本地 helper（窗口定位、点击、复制、剪贴板）
```

bridge 每次只把一个账号 JSON 写入 helper 标准输入。helper 标准输出必须是唯一的 JSON 响应：

```json
{"ok":true,"articleLink":"https://mp.weixin.qq.com/s/..."}
```

无法继续时，helper 返回：

```json
{
  "ok": false,
  "code": "login_required",
  "error": "微信需要重新登录",
  "manualIntervention": true
}
```

bridge 会暂停当前批次，保留当前公众号，人工处理后调用 `resume` 会从该公众号重试。

## 启用

`configs/config.yaml`：

```yaml
simulator:
  enabled: true
  helper_command: "py"
  helper_args: ["scripts/windows-simulator/latest_link_helper.py"]
  state_file: "./data/latest-link-state.json"
  max_accounts: 2000
  default_retries: 1
  account_timeout_sec: 90
  retry_delay_ms: 500
  # points are relative to the target window's outer top-left corner
  target_app: "Microsoft Edge"
  window_title_regex: "Microsoft Edge.*"
  flow: "four_step"
  link_mode: "menu"
  article_list_point: "260,380"
  first_article_point: "320,470"
  # 只有按内部文章区坐标校准时才设置；外层窗口坐标通常留空
  article_pane_origin: ""
  wait_sec: 3
  window_timeout_sec: 20
  clipboard_timeout_sec: 3
```

`state_file` 保存已见链接、任务结果和断点。bridge 进程意外重启后，未完成任务以 `paused` 恢复，必须由人工确认窗口状态后再调用 `resume`。

以上 UI 参数也可以继续使用旧的 `WECHAT_SIM_*` 环境变量；当两者同时存在时，环境变量优先。这样可以先在实机校准，再把确认过的坐标写回 YAML，重启 bridge 后仍然生效。

示例 helper 与坐标校准说明见 [../../scripts/windows-simulator/README.md](../../scripts/windows-simulator/README.md)。建议第一版在固定 Windows 缩放比例、固定窗口尺寸和单显示器环境运行。

## API

### 创建批量任务

```http
POST /v1/rpc/latest-article-link-jobs
Content-Type: application/json
```

```json
{
  "accounts": [
    {
      "id": "account-001",
      "biz": "Mzxxxx==",
      "nickname": "示例公众号",
      "initialLink": "https://mp.weixin.qq.com/s/real-seed-article-link"
    }
  ],
  "existingLinks": ["https://mp.weixin.qq.com/s/already-stored"],
  "maxRetries": 1,
  "accountTimeoutSec": 90
}
```

| 字段 | 必填 | 说明 |
|---|:---:|---|
| `accounts` | 是 | 1 至 `max_accounts` 个账号 |
| `accounts[].initialLink` | 是 | `four_step`/`article_to_account` 使用真实 `mp.weixin.qq.com/s...` 文章链接；兼容流程也接受 `mp/profile_ext` 主页链接 |
| `id` / `biz` / `nickname` | 否 | 业务侧关联与结果展示字段 |
| `existingLinks` | 否 | 业务文章库已有链接；命中后跳过 |
| `maxRetries` | 否 | 单号非人工失败的额外尝试次数，默认读取配置 |
| `accountTimeoutSec` | 否 | 单号 UI 操作超时，默认读取配置 |

成功响应为 HTTP `202`：

```json
{
  "code": 0,
  "data": {"jobId":"sim-20260920-000001","state":"queued","total":1}
}
```

### 查询进度

```http
GET /v1/rpc/latest-article-link-jobs/{jobId}
```

任务状态：`queued`、`running`、`paused`、`completed`、`stopped`。

单号结果状态：`success`、`duplicate_skipped`、`failed`、`manual_intervention`。

`paused` 表示需要人工登录、调整窗口、校准坐标等。普通窗口加载失败会按重试规则重试，耗尽后仅将该账号标记为 `failed`，随后继续下一个账号。

### 暂停、继续、停止

```http
POST /v1/rpc/latest-article-link-jobs/{jobId}/pause
POST /v1/rpc/latest-article-link-jobs/{jobId}/resume
POST /v1/rpc/latest-article-link-jobs/{jobId}/stop
```

停止不会删除已有结果。重新创建新任务时，已写入 `state_file` 或传入 `existingLinks` 的链接仍会去重。

### 只读取新的文章链接

```http
GET /v1/rpc/latest-article-links?jobId={jobId}
```

响应只包含 `success` 的链接，不返回文章正文、封面或历史文章：

```json
{
  "code": 0,
  "data": {
    "jobId": "sim-...",
    "success": 1,
    "duplicate": 2,
    "failed": 0,
    "links": [
      {
        "id": "account-001",
        "biz": "Mzxxxx==",
        "nickname": "示例公众号",
        "articleLink": "https://mp.weixin.qq.com/s/..."
      }
    ]
  }
}
```

### 模拟器状态

```http
GET /v1/rpc/simulator-status
```

可用于前端展示是否启用、helper 是否已配置、各任务状态数量。它与原有 `GET /v1/rpc/status` 分离，后者仍表示原有采集机状态。

### 微信窗口登录态

```http
GET /v1/rpc/wechat-login-status
```

需要 `simulator.enabled` 且 helper 已配置。helper 加 `--wechat-status`，只读本机微信进程和窗口标题，不经过 wcplus，也不读聊天内容。

| `status` | 含义 |
|----------|------|
| `logged_in` | 已登录。可见主窗口标题为 `微信` / `WeChat`（宽高至少 640×480），或 `微信 (窗口)` / `WeChat (窗口)` |
| `logged_out` | 未登录。微信未启动、停在扫码页，或看不到已登录主窗口 |

直接调用 helper：`python3 scripts/windows-simulator/latest_link_helper.py --wechat-status`。

## Windows / macOS helper 的固定操作流程

主流程不使用 OCR，按需求的四步执行：

1. 使用 `initialLink` 打开微信文章窗口，并通过 UIA/Quartz 找到外层窗口。
2. 点击文章中的公众号入口。
3. 等待公众号页加载，点击第一篇文章。
4. 通过 `current` 或 `menu` 复制新文章 URL，并校验为 `mp.weixin.qq.com/s...`。

关闭页面、恢复剪贴板、重试、去重和批量状态属于外围管理动作，不改变四步业务流程。若设置 `WECHAT_SIM_SEND_TO_FILE_TRANSFER=1`，发送到文件传输助手仍是可选的后置动作。

四步主流程不搜索聊天联系人：初始文章打开后，直接点击当前文章里的公众号入口进入公众号页。

`WECHAT_SIM_ARTICLE_ACCOUNT_POINT` 对应初始文章页公众号入口，`WECHAT_SIM_ACCOUNT_FIRST_ARTICLE_POINT` 对应公众号页面首篇文章。开启 `vision` 后这两个点会先尝试窗口内截图定位，失败才使用校准点。已经提供公众号主页初始链接的旧调用可设 `WECHAT_SIM_FLOW=account_to_article`，继续使用 `WECHAT_SIM_ARTICLE_LIST_POINT` 和 `WECHAT_SIM_FIRST_ARTICLE_POINT`。页面结构、显示缩放或窗口尺寸变化后仍可重新校准。

macOS 微信客户端不能可靠响应 `command+l`。要走直接复制，应让初始链接由 Chrome 等浏览器打开并使用 `WECHAT_SIM_LINK_MODE=direct`；原生微信请使用 `WECHAT_SIM_LINK_MODE=menu` 并校准菜单坐标。Windows 浏览器保持地址栏快捷键流程。复制前会清空剪贴板并等待新的 URL，避免旧链接被误判为本次结果。

## 错误处理与验收

- 某个账号无法打开、复制为空或 URL 无效：按 `maxRetries` 重试，最终记录为 `failed`，不终止全批次；批次结束时发出一次 `simulator.job_failed` 通知。
- 登录失效、验证码、窗口找不到、坐标未配置：helper 返回 `manualIntervention`，任务暂停并发出 `simulator.manual_intervention` 通知。
- 重复 URL：记录 `duplicate_skipped`，不写入结果链接列表。
- 任务重启：完成记录和去重记录保留；中断任务需要人工确认后恢复。

验收时用 10 个账号先校准，再扩至 100/1000 个账号；每个账号至多输出一条新链接，且输出不含正文或历史数据。
