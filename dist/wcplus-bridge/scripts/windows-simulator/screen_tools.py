import ctypes
import logging
import win32api
import win32con
import win32gui
import win32ui
from PIL import Image

WINDOW_SCALE = 1
def set_window_size_ex(hwnd, width, height, x=0, y=0):
    win32gui.SetWindowPos(
        hwnd,
        win32con.HWND_TOP,  # 置顶
        x, y,
        width, height,
        win32con.SWP_SHOWWINDOW
    )


def find_wechat_hwnd():
    # 方法1：按标题
    hwnd = win32gui.FindWindow(None, "微信")
    if hwnd:
        return hwnd

    # 方法2：按类名（老版本微信）
    hwnd = win32gui.FindWindow("WeChatMainWndForPC", None)
    if hwnd:
        return hwnd

    return None


def set_foreground_window(hwnd):
    # 前置微信
    win32gui.SetForegroundWindow(hwnd)


def get_pos_by_hwnd(hwnd):
    left, top, right, bottom = win32gui.GetWindowRect(hwnd)
    width = right - left
    height = bottom - top

    logging.debug(f"窗口屏幕坐标: left={left}, top={top}, width={width}, height={height}")
    return left, top, width, height


def screenshot_pillow():
    # 获取桌面窗口
    hdesktop = win32gui.GetDesktopWindow()

    # 获取屏幕尺寸
    width = win32api.GetSystemMetrics(win32con.SM_CXVIRTUALSCREEN)
    height = win32api.GetSystemMetrics(win32con.SM_CYVIRTUALSCREEN)
    left = win32api.GetSystemMetrics(win32con.SM_XVIRTUALSCREEN)
    top = win32api.GetSystemMetrics(win32con.SM_YVIRTUALSCREEN)

    # 创建设备上下文
    desktop_dc = win32gui.GetWindowDC(hdesktop)
    img_dc = win32ui.CreateDCFromHandle(desktop_dc)

    # 创建内存设备上下文
    mem_dc = img_dc.CreateCompatibleDC()

    # 创建位图对象
    bitmap = win32ui.CreateBitmap()
    bitmap.CreateCompatibleBitmap(img_dc, width, height)
    mem_dc.SelectObject(bitmap)

    # 拷贝屏幕到位图
    mem_dc.BitBlt(
        (0, 0),
        (width, height),
        img_dc,
        (left, top),
        win32con.SRCCOPY
    )

    # 转成 PIL Image
    bmpinfo = bitmap.GetInfo()
    bmpstr = bitmap.GetBitmapBits(True)

    img = Image.frombuffer(
        'RGB',
        (bmpinfo['bmWidth'], bmpinfo['bmHeight']),
        bmpstr,
        'raw',
        'BGRX',
        0,
        1
    )

    # 清理资源
    win32gui.DeleteObject(bitmap.GetHandle())
    mem_dc.DeleteDC()
    img_dc.DeleteDC()
    win32gui.ReleaseDC(hdesktop, desktop_dc)

    return img


def get_window_scale(hwnd):
    # 设置 DPI 感知
    # ctypes.windll.shcore.SetProcessDpiAwareness(2)
    dpi = ctypes.windll.user32.GetDpiForWindow(hwnd)
    scale = dpi / 96.0
    logging.debug(f"缩放: {scale:.2f}x ({scale * 100:.0f}%)")
    global WINDOW_SCALE
    WINDOW_SCALE = scale
    return scale


def get_scale():
    global WINDOW_SCALE
    return WINDOW_SCALE


def screenshot_by_hwnd(hwnd):
    scale = get_window_scale(hwnd)
    # 10个像素的相对位置是为了去掉windows自带的毛玻璃效果的边框
    wechat_x, wechat_y, wechat_width, wechat_height = get_pos_by_hwnd(hwnd)
    img = screenshot_pillow()
    crop_x, crop_y = wechat_x + 10, wechat_y
    crop_width, crop_height = wechat_width * scale - 20, wechat_height * scale
    crop_box = (crop_x, crop_y, crop_x + crop_width, crop_y + crop_height)
    cropped = img.crop(crop_box)
    # cropped.show()
    return cropped
