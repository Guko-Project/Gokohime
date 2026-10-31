#!/usr/bin/env python3
"""Regenerate the embedded help card. Requires Pillow and a Chinese font.

python scripts/render-help.py --font /path/to/ChineseFont.ttf
Only the generated PNG is needed at runtime.
"""
import argparse
from pathlib import Path

from PIL import Image, ImageDraw, ImageFont

ROOT = Path(__file__).resolve().parents[1]
parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--kind', choices=['score', 'sing'], default='score')
parser.add_argument('--font', type=Path, required=True)
args = parser.parse_args()

WIDTH, MARGIN = 1200, 64
BG, INK, MUTED, ACCENT = '#faf6f8', '#382e36', '#6d5a66', '#a94d73'
image = Image.new('RGB', (WIDTH, 4000), BG)
draw = ImageDraw.Draw(image)
fonts = {size: ImageFont.truetype(str(args.font), size=size) for size in (29, 31, 34, 48)}
y = 55


def text(value, x, top, size=31, color=INK):
    draw.text((x, top), value, font=fonts[size], fill=color, anchor='lt')


def lines(value, width, size):
    result, current = [], ''
    for char in value:
        if current and draw.textlength(current + char, font=fonts[size]) > width:
            result.append(current)
            current = ''
        current += char
    return result + [current]


def section(title):
    global y
    y += 20
    text(title, MARGIN, y, 34, ACCENT)
    y += 57


def row(symbol, meaning):
    global y
    wrapped = lines(meaning, WIDTH-MARGIN-380, 29)
    height = max(1, len(wrapped))*43+25
    draw.line((MARGIN, y-12, WIDTH-MARGIN, y-12), fill='#e8d8df', width=1)
    text(symbol, MARGIN+10, y, 29)
    for index, line in enumerate(wrapped):
        text(line, 380, y+index*43, 29, MUTED)
    y += height


def draw_voices():
    global y
    section('可选音源 · 常用别名')
    voice_pairs = [
        (('洛天依', '洛 / 天依'), ('初音中文', '初 / 初中')),
        (('初音日文', '初日 / 初音'), ('镜音铃', '铃')),
        (('镜音连', '连'), ('言和', '言')),
        (('心华', '心'), ('绮萱', '绮')),
    ]
    for pair in voice_pairs:
        draw.line((MARGIN, y-12, WIDTH-MARGIN, y-12), fill='#e8d8df', width=1)
        for (name, aliases), x in zip(pair, (MARGIN+10, 635)):
            text(name, x, y, 29)
            text(aliases, x+190, y, 29, MUTED)
        y += 60


def draw_score():
    global y
    text('.score  自填乐谱', MARGIN, y, 48)
    y += 84
    section('发送示例')
    example = ['.score 洛', '1=C4 BPM=120 4/4', '1 (2:0.5 3:0.5) 5 0', '晴 天 好']
    box_height = len(example)*49+40
    draw.rounded_rectangle((MARGIN, y-8, WIDTH-MARGIN, y+box_height), radius=18, fill='#f4e6ed')
    for index, line in enumerate(example):
        text(line, MARGIN+27, y+15+index*49, 31)
    y += box_height+25
    draw_voices()
    section('谱子格式')
    for symbol, meaning in [
        ('1 2 3 4 5 6 7', '音阶；音符之间用空格分隔，默认每音一拍。'),
        ('0 / 0:2', '休止一拍 / 两拍，不占歌词；纯休止行的歌词填 -。'),
        ("1' / 1,", '高 / 低一个八度；可重复标记。使用英文符号。'),
        ('#4 / b7', '升 / 降半音。'),
        ('5:2 / 3:0.5 / 3:1.5', '两拍 / 半拍 / 一拍半；一拍始终为四分音符。'),
        ('(2:0.5 3:0.5)', '一字多音：两音共用一个字。括号内不能放休止符。'),
        ('|', '小节线。使用时，每小节必须填满拍数；歌词可对应分节。'),
        ('行(xing)', '指定中文读音，不带声调。'),
    ]:
        row(symbol, meaning)
    section('可选参数（下列为默认值）')
    for symbol, meaning in [
        ('1=C4', '调号：1 对应中央 C。'),
        ('BPM=120', '速度：每分钟 120 拍。'),
        ('4/4', '拍号：每小节四拍。'),
        ('LANG=zh', '语言：zh 中文 / ja 日文 / en 英文。'),
    ]:
        row(symbol, meaning)
    y += 30


def draw_sing():
    global y
    text('.sing  点歌翻唱', MARGIN, y, 48)
    y += 84
    section('点歌示例')
    examples = [
        '.sing 洛 晴天 周杰伦',
        '.msing 心 雨爱 杨丞琳',
        '.bsong 初 雨爱 杨丞琳 0-120',
        '.bmsong 初日 メルト 初音ミク 12.5-90',
    ]
    height = len(examples)*49+40
    draw.rounded_rectangle((MARGIN, y-8, WIDTH-MARGIN, y+height), radius=18, fill='#f4e6ed')
    for index, line in enumerate(examples):
        text(line, MARGIN+27, y+15+index*49, 31)
    y += height+25
    draw_voices()
    section('命令')
    for symbol, meaning in [
        ('.sing / .ssing', '按提取的乐谱翻唱，输出干声。'),
        ('.msing', '模仿原唱的旋律与节奏，输出干声。'),
        ('.bsong', '乐谱翻唱 + 原曲伴奏。'),
        ('.bmsong', '模仿原唱 + 原曲伴奏。'),
        ('.song', '不带参数显示本图；带参数同 .sing。'),
        ('.score', '自填简谱和歌词；发送命令查看格式。'),
        ('.voices', '完整音源、别名及可用状态。'),
    ]:
        row(symbol, meaning)
    section('输入格式')
    for symbol, meaning in [
        ('[音源] 歌名 [原唱]', '方括号项可省略；默认洛天依。'),
        ('音源未命中', '保留原输入搜歌，使用洛天依。'),
        ('歌词', '可用歌词找歌；三句及以上完整匹配后演唱指定范围。'),
        ('起始秒-结束秒', '放在最后，如 0-120；起始必须小于结束。'),
        ('完整歌词 / 秒数范围', '不受短片段长度限制。'),
    ]:
        row(symbol, meaning)
    section('完整歌词示例')
    examples = ['.sing 洛 第一句歌词', '第二句歌词', '第三句歌词']
    height = len(examples)*49+40
    draw.rounded_rectangle((MARGIN, y-8, WIDTH-MARGIN, y+height), radius=18, fill='#f4e6ed')
    for index, line in enumerate(examples):
        text(line, MARGIN+27, y+15+index*49, 31)
    y += height+50


(draw_score if args.kind == 'score' else draw_sing)()
output = ROOT / f'plugin/sing/assets/{args.kind}-help.png'
image.crop((0, 0, WIDTH, y)).save(output, optimize=True)
print(f'{output} ({WIDTH} × {y})')
