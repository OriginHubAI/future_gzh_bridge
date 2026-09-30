def anchor_identify():
    if not hasattr(anchor_identify, 'n'):
        anchor_identify.n = 0
    anchor_identify.n += 1
    return f'img_{anchor_identify.n}.png'

