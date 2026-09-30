import ctypes
import functools
import time

import win32api
import win32clipboard
import win32con
import win32gui

# ==================== 延迟装饰器 ====================
def delay(seconds=1):
    """延迟执行装饰器，默认延迟 1 秒"""
    def decorator(func):
        @functools.wraps(func)
        def wrapper(*args, **kwargs):
            time.sleep(seconds)
            return func(*args, **kwargs)
        return wrapper
    return decorator

# ==================== 模拟按键 ====================
def send_ctrl_w():
    """模拟 Ctrl+C（复制）"""
    win32api.keybd_event(win32con.VK_CONTROL, 0, 0, 0)
    win32api.keybd_event(ord('W'), 0, 0, 0)
    win32api.keybd_event(ord('W'), 0, win32con.KEYEVENTF_KEYUP, 0)
    win32api.keybd_event(win32con.VK_CONTROL, 0, win32con.KEYEVENTF_KEYUP, 0)


def send_ctrl_c():
    """模拟 Ctrl+C（复制）"""
    win32api.keybd_event(win32con.VK_CONTROL, 0, 0, 0)
    win32api.keybd_event(ord('C'), 0, 0, 0)
    win32api.keybd_event(ord('C'), 0, win32con.KEYEVENTF_KEYUP, 0)
    win32api.keybd_event(win32con.VK_CONTROL, 0, win32con.KEYEVENTF_KEYUP, 0)


def send_ctrl_v():
    """模拟 Ctrl+V（粘贴）"""
    win32api.keybd_event(win32con.VK_CONTROL, 0, 0, 0)
    win32api.keybd_event(ord('V'), 0, 0, 0)
    win32api.keybd_event(ord('V'), 0, win32con.KEYEVENTF_KEYUP, 0)
    win32api.keybd_event(win32con.VK_CONTROL, 0, win32con.KEYEVENTF_KEYUP, 0)


def send_ctrl_a():
    """模拟 Ctrl+A（全选）"""
    win32api.keybd_event(win32con.VK_CONTROL, 0, 0, 0)
    win32api.keybd_event(ord('A'), 0, 0, 0)
    win32api.keybd_event(ord('A'), 0, win32con.KEYEVENTF_KEYUP, 0)
    win32api.keybd_event(win32con.VK_CONTROL, 0, win32con.KEYEVENTF_KEYUP, 0)


def send_enter():
    """模拟回车键"""
    win32api.keybd_event(win32con.VK_RETURN, 0, 0, 0)
    win32api.keybd_event(win32con.VK_RETURN, 0, win32con.KEYEVENTF_KEYUP, 0)


# ==================== 剪贴板操作 ====================
def set_clipboard_text(text: str):
    """写入剪贴板（文本）"""
    win32clipboard.OpenClipboard()
    try:
        win32clipboard.EmptyClipboard()
        win32clipboard.SetClipboardText(text, win32clipboard.CF_UNICODETEXT)
    finally:
        win32clipboard.CloseClipboard()


def get_clipboard_text() -> str:
    """读取剪贴板（文本）"""
    win32clipboard.OpenClipboard()
    try:
        if win32clipboard.IsClipboardFormatAvailable(win32clipboard.CF_UNICODETEXT):
            return win32clipboard.GetClipboardData(win32clipboard.CF_UNICODETEXT)
        return ""
    finally:
        win32clipboard.CloseClipboard()


# ==================== 使用示例 ====================
if __name__ == "__main__":
    time.sleep(2)   # 切到目标窗口

    # Ctrl+A 全选
    send_ctrl_a()
    time.sleep(0.1)

    # Ctrl+C 复制
    send_ctrl_c()
    time.sleep(0.2)
    print("复制内容:", get_clipboard_text())

    # 写入新内容到剪贴板
    set_clipboard_text("替换后的文本")

    # Ctrl+V 粘贴
    send_ctrl_v()
    time.sleep(0.1)

    # 回车
    send_enter()