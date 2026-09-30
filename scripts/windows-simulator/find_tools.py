from pathlib import Path

import cv2
import numpy as np

def find_template_bottom_up(
    large_img_path: Path,
    template_path: Path,
    threshold: float = 0.85
):
    """
    从下往上在大图中寻找模板（头像），找到第一个就停止。

    :param large_img_path: 大图路径（32x400 竖长条）
    :param template_path: 头像模板路径（约 32x32）
    :param threshold: 匹配置信度阈值（0~1），越高越严格
    :return: (x, y) 匹配位置左上角坐标，未找到返回 None
    """

    # 读取灰度图
    large_img = cv2.imread(large_img_path, 0)
    template = cv2.imread(template_path, 0)

    if large_img is None or template is None:
        raise FileNotFoundError("图片路径错误，请检查文件是否存在")

    t_h, t_w = template.shape[:2]
    l_h, l_w = large_img.shape[:2]

    # 模板不能比大图大
    if t_h > l_h or t_w > l_w:
        raise ValueError("模板尺寸不能大于大图")

    # ========== 核心：模板匹配 ==========
    result = cv2.matchTemplate(large_img, template, cv2.TM_CCOEFF_NORMED)

    # ========== 从下往上扫描 ==========
    # 按行从底部向上遍历
    for y in range(l_h - t_h, -1, -1):
        for x in range(l_w - t_w + 1):
            if result[y, x] >= threshold:
                return (x, y), result[y, x]

    return None, 0.0
