#!/usr/bin/env bash
# Run in Terminal.app (辅助功能已授权). Does not create a bridge job.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"
LINK="${1:-https://mp.weixin.qq.com/s/F2CQkI5mFc05PBIxnGwt_w}"

export WECHAT_SIM_LAUNCH_COMMAND='open -a WeChat {initial_link}'
export WECHAT_SIM_MAC_APP='WeChat'
export WECHAT_SIM_TARGET_APP="${WECHAT_SIM_TARGET_APP:-}"
export WECHAT_SIM_WINDOW_TITLE_REGEX="${WECHAT_SIM_WINDOW_TITLE_REGEX:-微信.*|WeChat.*}"
export WECHAT_SIM_LINK_MODE=menu
export WECHAT_SIM_SKIP_ARTICLE_NAV="${WECHAT_SIM_SKIP_ARTICLE_NAV:-1}"
# Prefer calibrated points from configs/config.yaml; auto heuristics often miss the ⋯ button.
export WECHAT_SIM_MORE_MENU_AUTO="${WECHAT_SIM_MORE_MENU_AUTO:-0}"
export WECHAT_SIM_MORE_MENU_POINT="${WECHAT_SIM_MORE_MENU_POINT:-894,20}"
export WECHAT_SIM_COPY_LINK_POINT="${WECHAT_SIM_COPY_LINK_POINT:-810,108}"
export WECHAT_SIM_COORD_REF_SIZE="${WECHAT_SIM_COORD_REF_SIZE:-920,789}"
export WECHAT_SIM_CLICK_DRIVER="${WECHAT_SIM_CLICK_DRIVER:-quartz}"
export WECHAT_SIM_MENU_USE_KEYBOARD="${WECHAT_SIM_MENU_USE_KEYBOARD:-1}"
export WECHAT_SIM_MENU_DOWN_STEPS="${WECHAT_SIM_MENU_DOWN_STEPS:-1}"
export WECHAT_SIM_WAIT_SEC="${WECHAT_SIM_WAIT_SEC:-6}"
export WECHAT_SIM_WINDOW_TIMEOUT_SEC=35
export WECHAT_SIM_CLIPBOARD_TIMEOUT_SEC=12
export WECHAT_SIM_MAC_WINDOW_PICK="${WECHAT_SIM_MAC_WINDOW_PICK:-largest}"
export WECHAT_SIM_TRY_ALL_MAC_WINDOWS="${WECHAT_SIM_TRY_ALL_MAC_WINDOWS:-1}"

echo "Testing initialLink=$LINK"
printf '{"id":"verify-once","initialLink":"%s"}\n' "$LINK" \
  | python3 scripts/windows-simulator/latest_link_helper.py
