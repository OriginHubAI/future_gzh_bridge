#!/usr/bin/env python3
"""Launch the separately deployed collector; keep JSON stdout intact."""
import json
import os
from pathlib import Path
import sys


def main():
    root = os.environ.get("WECHAT_CONTROL_ROOT", "").strip()
    # The Windows delivery package is self-contained. An explicit root still
    # wins for operators who keep wechat_control-main outside the package, but
    # a fresh PowerShell no longer needs an environment-variable prerequisite.
    helper = (
        Path(root).expanduser().resolve() / "wechat_control_helper.py"
        if root
        else Path(__file__).resolve().parent / "wechat_control_helper.py"
    )
    if not helper.is_file():
        message = (
            "wechat_control_helper.py not found; set WECHAT_CONTROL_ROOT to "
            "the directory containing it"
        )
        print(json.dumps({"ok": False, "code": "manual_intervention", "error": message, "manualIntervention": True}))
        return
    os.chdir(helper.parent)
    os.environ["PYTHONIOENCODING"] = "utf-8"
    os.execv(sys.executable, [sys.executable, str(helper), *sys.argv[1:]])


if __name__ == "__main__":
    main()
