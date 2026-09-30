import logging
from pathlib import Path

from PIL import Image, ImageFilter
import imagehash

from screen_tools import get_scale


def same_pos_picture(source_img: Image,
                     pos: tuple,
                     anchor_point: Path,
                     catch_mode: bool = False) -> bool:
    scale = get_scale()
    wechat_x, wechat_y, wechat_width, wechat_height = pos
    wechat_width = wechat_width * scale
    wechat_height = wechat_height * scale
    wechat_x = wechat_x * scale
    wechat_y = wechat_y * scale

    crop_x, crop_y = wechat_x, wechat_y
    crop_width, crop_height = wechat_width, wechat_height
    crop_box = (crop_x, crop_y, crop_x + crop_width, crop_y + crop_height)
    cropped = source_img.crop(crop_box)
    if catch_mode:
        cropped.save(anchor_point)
        return True
    if not anchor_point.exists():
        cropped.save(anchor_point)
    img1 = Image.open(anchor_point).convert("RGB")
    img2 = cropped.convert("RGB")

    hash1 = imagehash.phash(img1, hash_size=16)
    hash2 = imagehash.phash(img2, hash_size=16)

    distance = hash1 - hash2
    max_dist = 16 * 16
    similarity = (1 - distance / max_dist) * 100

    logging.debug(f"哈希距离: {distance}")
    logging.debug(f"相似度: {similarity:.1f}%")

    if distance <= 6:
        return True
    else:
        return False


if __name__ == '__main__':
    print(same_pos_picture())
