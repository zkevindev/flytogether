#!/usr/bin/env python3
"""Generate a local synthetic microphone WAV for the optional browser voice test."""
import argparse
import json
from pathlib import Path
import shutil
import subprocess
import tempfile
import wave

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--scenario', choices=['flight', 'lucky', 'coach', 'roll', 'boomerang', 'rescue', 'escort', 'audience-host', 'audience-a', 'audience-b', 'mcp'], default='flight')
parser.add_argument('--output-dir', type=Path, required=True)
args = parser.parse_args()
if not shutil.which('say') or not shutil.which('ffmpeg'):
    parser.error('This fixture generator needs macOS say and ffmpeg; other systems can supply an equivalent 16 kHz mono WAV.')
output = args.output_dir.expanduser().resolve()
output.mkdir(parents=True, exist_ok=True)
rate = 16000
pcm = bytearray(rate * 2 * 8)
segments = []
phrases = [('现在轮到谁', 8), ('使用起飞卡', 10), ('请帮我掷骰子', 10), ('让小熊号起飞', 10), ('现在轮到谁', 10), ('不要掷骰子', 8)]
if args.scenario == 'roll':
    phrases = [('好，掷骰子', 10), ('不要掷骰子', 9), ('好，骰子', 9), ('刚才掷骰子了吗', 10), ('好，掷骰子', 8)]
if args.scenario == 'coach':
    phrases = [('帮我选', 10), ('另一架呢', 10), ('安全一点', 10), ('刚才那个', 10), ('就它', 10), ('不要掷骰子', 8)]
if args.scenario == 'lucky':
    phrases = [('帮我使用幸运骰', 10), ('不要掷骰子', 8)]
if args.scenario in ['boomerang','rescue','escort']:
    phrases = [({'boomerang':'使用回旋镖攻击爸爸的小熊号','rescue':'使用救援卡救援小朋友的小熊号','escort':'使用团队护航'}[args.scenario],12)]
if args.scenario == 'audience-host':
    phrases = [('帮我使用幸运骰', 50), ('不要掷骰子', 20)]
if args.scenario == 'audience-a':
    phrases = [('我的暗号是蓝莓，现在轮到谁', 14), ('详细介绍飞行棋的所有规则', 4), ('停一下，现在轮到谁', 16), ('帮房主使用炸弹', 20)]
if args.scenario == 'audience-b':
    phrases = [('我的暗号是橘子，现在轮到谁', 16), ('谁更接近终点', 14), ('现在轮到谁', 25)]
if args.scenario == 'mcp':
    # Give the real Realtime response time to finish before Chrome loops the WAV.
    phrases = [('请查一下本局星际救援任务卡的目标和当前进展', 60)]
with tempfile.TemporaryDirectory(prefix='fly-voice-fixture-') as temp:
    source, target = Path(temp) / 'phrase.aiff', Path(temp) / 'phrase.pcm'
    for text, silence in phrases:
        subprocess.run(['say', '-v', 'Tingting', '-r', '190', '-o', str(source), text], check=True)
        subprocess.run(['ffmpeg', '-hide_banner', '-loglevel', 'error', '-y', '-i', str(source), '-ar', str(rate), '-ac', '1', '-f', 's16le', str(target)], check=True)
        start = len(pcm) / rate / 2
        pcm.extend(target.read_bytes())
        segments.append({'text': text, 'start_seconds': start, 'end_seconds': len(pcm) / rate / 2})
        pcm.extend(bytes(rate * 2 * silence))
with wave.open(str(output / 'browser-input.wav'), 'wb') as f:
    f.setnchannels(1)
    f.setsampwidth(2)
    f.setframerate(rate)
    f.writeframes(pcm)
(output / 'audio-segments.json').write_text(json.dumps(segments, ensure_ascii=False, indent=2) + '\n')
print('Synthetic browser microphone fixture generated in the requested local directory.')
