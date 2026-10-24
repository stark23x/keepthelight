"""Builds the Windows icon from the source artwork.

Usage (from the project folder):
    python res/mkicon.py res/keepthelight-original.png res/keepthelight.ico
then run "go generate" to put the new icon into the exe resources.
Needs Pillow (pip install pillow).
"""
import sys, math
from PIL import Image, ImageFilter
src = Image.open(sys.argv[1]).convert('RGBA')
# square crop around the visible artwork (alpha > 40), with a small margin
l, t, r, b = 192, 137, 838, 844
cx, cy = (l + r) / 2, (t + b) / 2
side = int(max(r - l, b - t) * 1.07)
box = (int(cx - side / 2), int(cy - side / 2), int(cx - side / 2) + side, int(cy - side / 2) + side)
img = src.crop(box)
# fade the leftover glow toward the edges so the crop never shows a hard edge
w = img.size[0]
fade = Image.new('L', (w, w))
px = fade.load()
for y in range(w):
    for x in range(w):
        d = min(x, y, w - 1 - x, w - 1 - y) / (w * 0.06)
        px[x, y] = int(255 * min(1.0, d))
a = Image.composite(img.getchannel('A'), Image.new('L', (w, w)), fade)
img.putalpha(Image.eval(Image.merge('LA', (a, fade)).getchannel(0), lambda v: v))
img = img.resize((1024, 1024), Image.LANCZOS)
import os
img.save(os.path.join(os.path.dirname(sys.argv[2]) or '.', 'keepthelight-1024.png'))
sizes = [256, 128, 64, 48, 40, 32, 24, 20, 16]
frames = []
for s in sizes:
    f = img.resize((s, s), Image.LANCZOS)
    if s <= 32:  # small sizes: a touch of sharpening so the bulb and film edge stay readable
        f = f.filter(ImageFilter.UnsharpMask(radius=0.6, percent=70, threshold=1))
    frames.append(f)
frames[0].save(sys.argv[2], sizes=[(s, s) for s in sizes], append_images=frames[1:])
print('crop box', box)
