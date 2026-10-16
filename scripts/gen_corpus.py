#!/usr/bin/env python3
"""生成可复现的大语料：原样 demo 手册、干扰文档及精确 keyword 计数。"""
from __future__ import annotations

import argparse
import hashlib
import json
from pathlib import Path
import random
import shutil

ROOT = Path(__file__).resolve().parents[1]
SOURCE_BATCH_SIZE = 4000
MARKERS = {'CORPUSMARKER007': 7, 'CORPUSMARKER113': 113, 'CORPUSMARKERZERO': None}


def generate(docs: int, seed: int, out: Path) -> dict:
    if docs < 0:
        raise ValueError('--docs must be nonnegative')
    if out.exists() and any(out.iterdir()):
        raise FileExistsError('output directory must be empty')
    out.mkdir(parents=True, exist_ok=True)
    suite_bytes = (ROOT / 'examples/eval-questions.json').read_bytes()
    suite = json.loads(suite_bytes)
    anchors = [item['anchor'] for case in suite['cases'] for item in case['expected']]
    shutil.copytree(ROOT / 'examples/docs', out / 'manual')
    noise = out / 'noise'
    noise.mkdir()
    rng = random.Random(seed)
    words = ['日志', '错误', '组件', '版本', '排查', '队列', '任务', '事件', '检查', '记录', '状态', '运行']
    counts = {marker: 0 for marker in MARKERS}
    for i in range(1, docs + 1):
        tokens = rng.choices(words, k=35)
        body = f'# 运维记录 {i:06d}\n\n编号 R{rng.getrandbits(64):016X}。' + '、'.join(tokens) + '。\n'
        for marker, period in MARKERS.items():
            if period and i % period == 0:
                body += marker + ' '
                counts[marker] += 1
        body += '\n'
        if any(anchor in body for anchor in anchors):
            raise ValueError('generated distractor contains an evaluation anchor')
        batch = noise / f'batch-{(i - 1) // SOURCE_BATCH_SIZE + 1:04d}'
        batch.mkdir(exist_ok=True)
        (batch / f'{i:06d}.md').write_text(body, encoding='utf-8')
    files = {}
    for path in sorted(out.rglob('*')):
        if path.is_file():
            files[path.relative_to(out).as_posix()] = hashlib.sha256(path.read_bytes()).hexdigest()
    manifest = {'schema': 'xrag-corpus-v1', 'docs': docs, 'seed': seed,
                'manual_files': len(files) - docs, 'total_files': len(files),
                'sources': [{'id': 'manual', 'path': 'manual'}] + [
                    {'id': 'noise-' + batch.name, 'path': batch.relative_to(out).as_posix()}
                    for batch in sorted(noise.iterdir())],
                'keyword_expected_chunks': counts,
                'question_sha256': hashlib.sha256(suite_bytes).hexdigest(),
                'content_sha256': hashlib.sha256(json.dumps(files, sort_keys=True).encode()).hexdigest()}
    (out / 'manifest.json').write_text(json.dumps(manifest, ensure_ascii=False, indent=2) + '\n', encoding='utf-8')
    return manifest


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--docs', type=int, required=True)
    parser.add_argument('--seed', type=int, default=20261016)
    parser.add_argument('--out', type=Path, required=True)
    args = parser.parse_args()
    print(json.dumps(generate(args.docs, args.seed, args.out), ensure_ascii=False))
