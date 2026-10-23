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
parser.add_argument('--output-dir', type=Path, required=True)
args = parser.parse_args()
if not shutil.which('say') or not shutil.which('ffmpeg'):
    parser.error('This fixture generator needs macOS say and ffmpeg; other systems can supply an equivalent 16 kHz mono WAV.')
output = args.output_dir.expanduser().resolve()
output.mkdir(parents=True, exist_ok=True)
rate = 16000
pcm = bytearray(rate * 2 * 8)
segments = []
phrases = [('让小熊号起飞', 8), ('小灵同学，使用起飞卡', 10), ('请帮我掷骰子', 10), ('让小熊号起飞', 10), ('现在轮到谁', 10), ('不要掷骰子', 8)]
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
