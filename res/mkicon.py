"""Builds the icon files from the logo, res/keepthelight.svg.

Usage (from the project folder):
    python res/mkicon.py
then run "go generate" to put the new icon into the exe resources.
Needs Pillow (pip install pillow) and rsvg-convert (librsvg) on the PATH.

Writes res/keepthelight.ico (16-256 px), res/keepthelight-1024.png and
res/about-icon.png (128 px: the window's header and the About box). Every size
is drawn from the SVG, not shrunk from a big picture, so edges stay sharp. The
smallest sizes are simplified: 16 and 20 px have no sprocket holes (they would
only blur), 24 px has three instead of five.
"""
import os, re, subprocess, tempfile
from PIL import Image

RES = os.path.dirname(os.path.abspath(__file__))
SVG = open(os.path.join(RES, 'keepthelight.svg'), encoding='utf-8').read()
HOLES = re.search(r'<g fill="#0F1A33">.*?</g>', SVG).group(0)


def three(x):
    return ''.join(f'<rect x="{x - 0.5}" y="{y}" width="6" height="8" rx="1.6"></rect>' for y in (26, 46, 66))


VARIANTS = {
    'full': SVG,
    'none': SVG.replace(HOLES, ''),
    'three': SVG.replace(HOLES, '<g fill="#0F1A33">' + three(20.5) + three(74.5) + '</g>'),
}


def render(size, variant='full'):
    with tempfile.TemporaryDirectory() as d:
        src, out = os.path.join(d, 'in.svg'), os.path.join(d, 'out.png')
        open(src, 'w', encoding='utf-8').write(VARIANTS[variant])
        subprocess.run(['rsvg-convert', '-w', str(size), '-h', str(size), src, '-o', out], check=True)
        return Image.open(out).convert('RGBA').copy()


render(1024).save(os.path.join(RES, 'keepthelight-1024.png'))
render(128).save(os.path.join(RES, 'about-icon.png'))
sizes = [256, 128, 64, 48, 40, 32, 24, 20, 16]
frames = [render(s, 'none' if s <= 20 else 'three' if s == 24 else 'full') for s in sizes]
frames[0].save(os.path.join(RES, 'keepthelight.ico'), sizes=[(s, s) for s in sizes], append_images=frames[1:])
print('icon files written')
