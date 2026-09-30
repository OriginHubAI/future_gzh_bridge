#!/usr/bin/env bash
# 完整路径：种子文章 → 公众号 → 第一篇（最新）→ ⋯ → 复制链接
# 在 Terminal.app 运行（系统设置 → 隐私 → 辅助功能 已授权 Terminal）。
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"
LINK="${1:-https://mp.weixin.qq.com/s/F2CQkI5mFc05PBIxnGwt_w}"

export WECHAT_SIM_LAUNCH_COMMAND='open -a WeChat {initial_link}'
export WECHAT_SIM_MAC_APP='WeChat'
export WECHAT_SIM_TARGET_APP='微信'
export WECHAT_SIM_WINDOW_TITLE_REGEX='微信.*'
export WECHAT_SIM_FLOW='article_to_account'
export WECHAT_SIM_LINK_MODE='menu'
export WECHAT_SIM_SKIP_ARTICLE_NAV=0
export WECHAT_SIM_ARTICLE_ACCOUNT_POINT="${WECHAT_SIM_ARTICLE_ACCOUNT_POINT:-165,195}"
export WECHAT_SIM_ACCOUNT_FIRST_ARTICLE_POINT="${WECHAT_SIM_ACCOUNT_FIRST_ARTICLE_POINT:-460,265}"
export WECHAT_SIM_MORE_MENU_AUTO=0
export WECHAT_SIM_MORE_MENU_POINT="${WECHAT_SIM_MORE_MENU_POINT:-872,52}"
export WECHAT_SIM_COPY_LINK_POINT="${WECHAT_SIM_COPY_LINK_POINT:-810,108}"
export WECHAT_SIM_COORD_REF_SIZE="${WECHAT_SIM_COORD_REF_SIZE:-920,789}"
export WECHAT_SIM_CLICK_DRIVER="${WECHAT_SIM_CLICK_DRIVER:-quartz}"
export WECHAT_SIM_MAC_WINDOW_PICK="${WECHAT_SIM_MAC_WINDOW_PICK:-launched}"
export WECHAT_SIM_TRY_ALL_MAC_WINDOWS="${WECHAT_SIM_TRY_ALL_MAC_WINDOWS:-0}"
export WECHAT_SIM_MENU_USE_KEYBOARD="${WECHAT_SIM_MENU_USE_KEYBOARD:-1}"
export WECHAT_SIM_MENU_DOWN_STEPS="${WECHAT_SIM_MENU_DOWN_STEPS:-1}"
export WECHAT_SIM_WAIT_SEC="${WECHAT_SIM_WAIT_SEC:-6}"
export WECHAT_SIM_WINDOW_TIMEOUT_SEC=35
export WECHAT_SIM_CLIPBOARD_TIMEOUT_SEC=12

echo "Testing latest-article flow, seed initialLink=$LINK"
echo "Calibrate if this fails: python3 scripts/windows-simulator/calibrate_coordinates.py --mode menu --flow article_to_account"
printf '{"id":"test-latest","initialLink":"%s"}\n' "$LINK" \
  | python3 scripts/windows-simulator/latest_link_helper.py
