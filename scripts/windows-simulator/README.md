# Windows / macOS 模拟取最新文章链接助手

该脚本是桥接服务的本地 UI 驱动，主流程只有四步：打开一个真实微信文章链接，点击文章里的公众号入口，点击公众号页面的第一篇文章，复制新打开文章的链接。四步按账号串行重复执行，输出一条经过校验的文章 URL。它也兼容已经打开公众号主页的旧调用方式，不调用私有协议、不抓正文、不处理验证码。

Windows 使用 `pywinauto` 查找窗口；macOS 使用 Quartz 获取窗口位置，并通过 `open -a` 激活目标应用。两种系统都用 `pyautogui` 完成点击和快捷键。

安装依赖：

```powershell
py -m pip install -r scripts/windows-simulator/requirements.txt
```

macOS：

```bash
python3 -m pip install -r scripts/windows-simulator/requirements.txt
```

macOS 需要在“系统设置 → 隐私与安全性 → 辅助功能”中允许 Terminal 或运行 bridge 的程序控制鼠标键盘；读取窗口列表时按系统提示允许屏幕录制权限。

先将浏览器或微信客户端固定为同一窗口尺寸和 Windows 缩放比例，然后设置环境变量。坐标相对于目标窗口左上角，而不是桌面左上角。需求规定的文章入口流程需要配置：

```powershell
$env:WECHAT_SIM_FLOW = "article_to_account"
$env:WECHAT_SIM_ARTICLE_ACCOUNT_POINT = "260,220"
$env:WECHAT_SIM_ACCOUNT_FIRST_ARTICLE_POINT = "320,470"
```

当前远程 Windows 微信窗口（外框 708×707）已实测为：
`ARTICLE_ACCOUNT_POINT=39,156`、`ACCOUNT_FIRST_ARTICLE_POINT=353,578`、
`MORE_MENU_POINT=403,28`、`COPY_LINK_POINT=256,134`。这些值已经写入
`configs/config.windows.yaml`；窗口尺寸或缩放比例变化后必须重新校准。

如果文章已经由操作者在微信中打开，Windows 配置使用
`WECHAT_SIM_REUSE_CURRENT_ARTICLE=1`，helper 会先排除“微信”聊天壳和“文件传输助手”，
再继续公众号、首篇文章、复制链接三步。发现多个文章窗口时会暂停要求收窄窗口标题，避免误控其他会话。

`ARTICLE_ACCOUNT_POINT` 是初始文章页中公众号名称/头像的位置，`ACCOUNT_FIRST_ARTICLE_POINT` 是进入公众号页后第一篇文章的位置。若 `WECHAT_SIM_FLOW` 留空或设为 `auto`，`/s/` 文章链接会自动选择该流程。

也可以将 `WECHAT_SIM_FLOW` 设为 `four_step`。该模式只接受真实 `/s/...` 文章链接，并固定执行上述四步；`article_to_account` 是同一流程的兼容名称。

如果输入的是公众号主页链接，使用兼容流程：

也可以把同名配置持久化到 `configs/config.yaml` 的 `simulator` 节点（例如
`target_app`、`window_title_regex`、`article_list_point`、
`first_article_point`、`link_mode`）。Bridge 启动时会将这些字段传给
helper；若同时设置了 `WECHAT_SIM_*` 环境变量，环境变量优先。

```powershell
$env:WECHAT_SIM_WINDOW_TITLE_REGEX = "Microsoft Edge.*"
$env:WECHAT_SIM_FLOW = "account_to_article"
$env:WECHAT_SIM_ARTICLE_LIST_POINT = "260,380"
$env:WECHAT_SIM_FIRST_ARTICLE_POINT = "320,470"
$env:WECHAT_SIM_WAIT_SEC = "2"
$env:WECHAT_SIM_CLOSE_HOTKEY = "ctrl,w"
```

macOS 使用应用名称匹配窗口，快捷键默认自动切换为 `command`：

```bash
export WECHAT_SIM_WINDOW_TITLE_REGEX='Google Chrome.*'
export WECHAT_SIM_TARGET_APP='Google Chrome'
export WECHAT_SIM_FLOW='article_to_account'
export WECHAT_SIM_ARTICLE_ACCOUNT_POINT='260,220'
export WECHAT_SIM_ACCOUNT_FIRST_ARTICLE_POINT='320,470'
export WECHAT_SIM_ARTICLE_LIST_POINT='260,380'
export WECHAT_SIM_FIRST_ARTICLE_POINT='320,470'
export WECHAT_SIM_WAIT_SEC='2'
export WECHAT_SIM_CLOSE_HOTKEY='command,w'
```

浏览器文章页默认使用地址栏直接复制，不会打开分享窗口。macOS 微信客户端的文章页没有可用的地址栏；只有明确需要时才配置分享模式：

如果要在 macOS 上完全走“直接复制”，请让初始链接由浏览器打开：

```bash
export WECHAT_SIM_LAUNCH_COMMAND='open -a "Google Chrome" {initial_link}'
export WECHAT_SIM_TARGET_APP='Google Chrome'
export WECHAT_SIM_WINDOW_TITLE_REGEX='Google Chrome.*'
export WECHAT_SIM_LINK_MODE='direct'
```

复制成功后如需自动发到微信“文件传输助手”，再配置微信窗口和聊天输入框坐标：

```bash
export WECHAT_SIM_SEND_TO_FILE_TRANSFER=1
export WECHAT_SIM_SEND_TARGET_APP='微信'
export WECHAT_SIM_SEND_WINDOW_TITLE_REGEX='微信.*'
export WECHAT_SIM_FILE_TRANSFER_CHAT_POINT='120,520'
export WECHAT_SIM_FILE_TRANSFER_INPUT_POINT='520,700'
export WECHAT_SIM_SEND_PASTE_HOTKEY='command,v'
export WECHAT_SIM_SEND_HOTKEY='enter'
```

坐标必须按实际微信窗口校准：`CHAT_POINT` 点击左侧“文件传输助手”，`INPUT_POINT` 点击聊天输入框。helper 只有在剪贴板已校验为 URL 后才会粘贴并回车发送；未配置开关时不会发送。

四步主流程不会搜索聊天联系人。初始文章打开后，helper 直接点击当前文章中的公众号入口，再点击公众号页面的第一篇文章。

校准工具输出的点都相对于整个目标窗口左上角。若原生微信的控件点是按内部文章区坐标记录的，才额外设置 `WECHAT_SIM_ARTICLE_PANE_ORIGIN=x,y`（或 `simulator.article_pane_origin`）；浏览器窗口不要设置该项。

```bash
export WECHAT_SIM_LAUNCH_COMMAND='open -a WeChat {initial_link}'
export WECHAT_SIM_MAC_APP='WeChat'
export WECHAT_SIM_TARGET_APP='微信'
export WECHAT_SIM_WINDOW_TITLE_REGEX='微信.*'
export WECHAT_SIM_LINK_MODE='menu'
export WECHAT_SIM_MORE_MENU_POINT='820,48'
export WECHAT_SIM_COPY_LINK_POINT='820,88'
```

若使用视觉定位菜单，可设置 `WECHAT_SIM_REQUIRE_MENU_VISION=1`。此时识别不到
文章页右上角「⋯」会直接暂停，不会回退到旧坐标，避免误点聊天工具条或刷新按钮。

校准示例：`more_menu_point` 对准文章页右上角「⋯」，`copy_link_point` 对准下拉菜单里的「复制链接」行（见截图菜单第二项）。坐标相对微信窗口左上角；窗口变化后需重校。

如果传入的 `initialLink` 本身已经是文章页，默认会执行“文章页 → 公众号 → 第一篇文章”流程。调试已经手工打开的目标文章时可设置 `WECHAT_SIM_SKIP_ARTICLE_NAV=1`，helper 会等待当前窗口、跳过重新打开和关闭页面；正常批量验收不要设置此项。

如果初始文章已经由操作者在原生微信中打开、但仍希望自动执行后续三步，可显式设置
`WECHAT_SIM_REUSE_CURRENT_ARTICLE=1`。该模式不会搜索聊天联系人，也不会重新打开链接，
但会继续执行“进入公众号 → 点击第一篇 → 复制链接”；必须先确认当前文章就是本次任务的
`initialLink`，并重新校准当前窗口中的两个文章点击点。它不是默认行为，避免误控旧窗口。

原生微信优先试 `current` 模式（聚焦文章页后发送 `Ctrl+C`，不需要分享菜单）；如果当前微信版本不响应，再用 `menu` 模式。浏览器使用 `direct`/`shortcut`。如果操作者已经在微信文章页手动点了“复制链接”，可设置 `WECHAT_SIM_LINK_MODE=clipboard`，任务只读取并校验现有剪贴板，不打开窗口、不清空剪贴板。

`vision: true`（或 `WECHAT_SIM_VISION=1`）会在四步中的公众号入口、第一篇文章和菜单按钮处先做窗口截图的颜色/亮度定位，并在定位失败时回退到校准坐标。截图只限定在目标窗口内；它不是 OCR，也不会替代剪贴板 URL 校验。

坐标相对于目标窗口左上角。macOS 多显示器、显示缩放或窗口尺寸变化后需要重新校准。

若需要通过特定程序打开初始链接，可设置：

```powershell
$env:WECHAT_SIM_LAUNCH_COMMAND = "C:\\Program Files (x86)\\Microsoft\\Edge\\Application\\msedge.exe {initial_link}"
```

手工验证协议：

```powershell
'{"id":"demo","initialLink":"https://mp.weixin.qq.com/..."}' |
  py scripts/windows-simulator/latest_link_helper.py
```

成功输出：

```json
{"ok":true,"articleLink":"https://mp.weixin.qq.com/s/..."}
```

在 macOS 上可以先验证 Windows 分支的协议、坐标偏移、`Ctrl+L/C` 和剪贴板恢复逻辑（不需要安装 Windows 或启动微信）：

```bash
python3 scripts/windows-simulator/test_latest_link_helper.py
```

这只是跨平台逻辑回归测试；Windows 真机仍需用 `py` 安装依赖，并在实际微信/浏览器窗口上校准标题和坐标后再验收 UI。

需要人工处理时会输出 `manualIntervention: true`，桥接服务会将整个批次置为 `paused`。处理完登录、窗口或坐标问题后调用任务的 `resume` 接口，当前公众号会重新执行。

坐标只适合快速版本。Windows 后续稳定化应优先用 `pywinauto` 的控件属性定位目标元素，macOS 可继续使用 Quartz 窗口信息配合 Accessibility 元素定位；不要通过 OCR、私有协议解析或绕过微信登录来替代人工流程。

## 坐标校准工具

项目附带一个不执行点击的交互式校准工具。它会读取目标窗口外框和当前鼠标位置，把屏幕坐标换算成 helper 使用的“相对于窗口左上角”的坐标，并打印可直接粘贴的环境变量。Windows 使用 `pywinauto`，macOS 使用 Quartz；两边的坐标约定与 `latest_link_helper.py` 相同。

浏览器直取模式（公众号列表 → 第一篇文章）：

```powershell
py scripts/windows-simulator/calibrate_coordinates.py `
  --title-regex "Microsoft Edge.*" `
  --target-app "Microsoft Edge" `
  --flow account_to_article `
  --mode direct `
  --format powershell
```

原生微信分享模式（文章页 → 公众号页 → 第一篇文章 → 分享 → 复制链接）：

```powershell
py scripts/windows-simulator/calibrate_coordinates.py `
  --title-regex "微信.*" `
  --target-app "微信" `
  --flow article_to_account `
  --mode menu `
  --format powershell
```

macOS 将 `py` 替换为 `python3`，并把格式改为 `bash`（默认值）。工具会依次提示操作员把鼠标移到目标控件后按 Enter；它不会点击控件，也不会修改 bridge 配置。输出的变量对应关系如下：

| 操作点 | 环境变量 |
| --- | --- |
| 公众号文章列表 | `WECHAT_SIM_ARTICLE_LIST_POINT` |
| 账号页第一篇文章 | `WECHAT_SIM_FIRST_ARTICLE_POINT` |
| 文章页进入公众号 | `WECHAT_SIM_ARTICLE_ACCOUNT_POINT` |
| 账号页第一篇文章（文章页流程） | `WECHAT_SIM_ACCOUNT_FIRST_ARTICLE_POINT` |
| 分享 | `WECHAT_SIM_SHARE_POINT` |
| 复制链接 | `WECHAT_SIM_COPY_LINK_POINT` |

可以使用 `--output calibrated.env` 保存同一份输出；启动 bridge 前请在同一终端执行该文件中的命令，或手工复制变量。窗口尺寸、显示缩放、位置改变后应重新校准。工具实现位于 `calibrate_coordinates.py`，不会自动探测文章按钮，也不会绕过微信登录/验证码。

如果希望把结果固化到 bridge 的 YAML，可使用 `--format yaml --output calibrated.yaml`。输出是可合并到现有 `simulator:` 节点的片段（不是完整配置文件）；Go bridge 会把这些字段转换成同名的 `WECHAT_SIM_*` 变量，进程环境变量仍可覆盖 YAML：

```powershell
py scripts/windows-simulator/calibrate_coordinates.py `
  --title-regex "Microsoft Edge.*" --flow account_to_article `
  --mode direct --format yaml --output calibrated.yaml
```
