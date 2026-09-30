import logging

import win32gui
import win32api
import win32con
import ctypes
from ctypes import wintypes


class MOUSEINPUT(ctypes.Structure):
    _fields_ = [
        ("dx", wintypes.LONG),
        ("dy", wintypes.LONG),
        ("mouseData", wintypes.DWORD),
        ("dwFlags", wintypes.DWORD),
        ("time", wintypes.DWORD),
        ("dwExtraInfo", ctypes.POINTER(ctypes.c_ulong)),
    ]

class KEYBDINPUT(ctypes.Structure):
    _fields_ = [
        ("wVk", wintypes.WORD),
        ("wScan", wintypes.WORD),
        ("dwFlags", wintypes.DWORD),
        ("time", wintypes.DWORD),
        ("dwExtraInfo", ctypes.POINTER(ctypes.c_ulong)),
    ]

class HARDWAREINPUT(ctypes.Structure):
    _fields_ = [
        ("uMsg", wintypes.DWORD),
        ("wParamL", wintypes.WORD),
        ("wParamH", wintypes.WORD),
    ]

class INPUT_union(ctypes.Union):
    _fields_ = [
        ("mi", MOUSEINPUT),
        ("ki", KEYBDINPUT),
        ("hi", HARDWAREINPUT),
    ]

class INPUT(ctypes.Structure):
    _fields_ = [
        ("type", wintypes.DWORD),
        ("union", INPUT_union),
    ]

move_cursor = lambda x, y : ctypes.windll.user32.SetCursorPos(x, y)

def mouse_click():
    extra = ctypes.c_ulong(0)
    inp = INPUT()
    inp.type = win32con.INPUT_MOUSE
    inp.union.mi = MOUSEINPUT(
        dx=0, dy=0, mouseData=0,
        dwFlags=win32con.MOUSEEVENTF_LEFTDOWN,
        time=0,
        dwExtraInfo=ctypes.pointer(extra)
    )
    ctypes.windll.user32.SendInput(1, ctypes.byref(inp), ctypes.sizeof(inp))

    # 鼠标左键抬起
    inp2 = INPUT()
    inp2.type = win32con.INPUT_MOUSE
    inp2.union.mi = MOUSEINPUT(
        dx=0, dy=0, mouseData=0,
        dwFlags=win32con.MOUSEEVENTF_LEFTUP,
        time=0,
        dwExtraInfo=ctypes.pointer(extra)
    )
    ctypes.windll.user32.SendInput(1, ctypes.byref(inp2), ctypes.sizeof(inp2))


def click_screen(hwnd, x, y):
    from screen_tools import get_pos_by_hwnd
    left, top, width, height = get_pos_by_hwnd(hwnd)
    logging.debug(f'width:{width}, height:{height}')
    if 0<= x <= width and 0<=y <= height:
        logging.debug(f'hwnd: {hwnd}, left: {left}, top: {top}')
        logging.debug(f'move cursor to: {left + x}, {top + y}')
        move_cursor(left + x, top + y)
        mouse_click()
    else:
        raise RuntimeError(f'位置下标越界。检查脚本逻辑. x:{x}, y:{y}')

def move_screen(hwnd, x, y):
    from screen_tools import get_pos_by_hwnd
    left, top, width, height = get_pos_by_hwnd(hwnd)
    logging.debug(f'width:{width}, height:{height}')
    if 0<= x <= width and 0<=y <= height:
        logging.debug(f'hwnd: {hwnd}, left: {left}, top: {top}')
        logging.debug(f'move cursor to: {left + x}, {top + y}')
        move_cursor(left + x, top + y)
    else:
        raise RuntimeError(f'位置下标越界。检查脚本逻辑. x:{x}, y:{y}')


import ctypes
import time

# 声明 mouse_event 参数类型
user32 = ctypes.windll.user32

# mouse_event 参数
MOUSEEVENTF_WHEEL = 0x0800

def scroll_up(clicks=1):
    """
    鼠标滚轮向上滚
    :param clicks: 滚动格数，1格约120单位
    """
    # 正数 = 向上滚，负数 = 向下滚
    # 一格 = WHEEL_DELTA = 120
    user32.mouse_event(MOUSEEVENTF_WHEEL, 0, 0, clicks * 120, 0)

def scroll_down(clicks=1):
    """鼠标滚轮向下滚"""
    user32.mouse_event(MOUSEEVENTF_WHEEL, 0, 0, -clicks * 120, 0)

