#!/usr/bin/env python3
"""本机 Server/local 大语料基线；记录数据和耗时，不设置相关性阈值。"""
from __future__ import annotations

import argparse
import json
import math
import os
from pathlib import Path
import platform
import socket
import sqlite3
import subprocess
import tempfile
import time
import urllib.error
import urllib.request

from gen_corpus import generate, ROOT


def percentile(values, fraction):
    ordered = sorted(values)
    position = (len(ordered) - 1) * fraction
    lo, hi = math.floor(position), math.ceil(position)
    return ordered[lo] + (ordered[hi] - ordered[lo]) * (position - lo)


def command(*args, timeout=180):
    return subprocess.run([str(a) for a in args], capture_output=True, text=True, check=True, timeout=timeout).stdout


def call(url, body=None, token=None):
    headers = {'Content-Type': 'application/json'}
    if token:
        headers['Authorization'] = 'Bearer ' + token
    req = urllib.request.Request(url, data=None if body is None else json.dumps(body).encode(), headers=headers)
    with urllib.request.urlopen(req, timeout=60) as response:
        return json.load(response)


def measurement(response, case, elapsed):
    if response.get('failure') or response.get('coverage', {}).get('execution_partial'):
        raise RuntimeError('retrieval failed or execution_partial: ' + case['id'])
    paths = [item['path'] for item in response['items']]
    expected = {item['path'] for item in case['expected']}
    hit = not paths if case.get('expected_empty') else any(path in expected for path in paths)
    stages = {step['stage']: step['count'] for step in response.get('trace', [])}
    if 'keyword' not in stages:
        raise RuntimeError('missing lexical candidate count in trace')
    return {'id': case['id'], 'question': case['question'], 'hit_at_5': hit,
            'latency_ms': round(elapsed * 1000, 3), 'paths': paths,
            'lexical_candidates': stages['keyword'], 'trace_counts': stages,
            'total': response['total'], 'coverage': response['coverage']}


def summary(rows):
    latencies = [r['latency_ms'] for r in rows]
    return {'hit_at_5': sum(r['hit_at_5'] for r in rows), 'cases': len(rows),
            'p50_ms': round(percentile(latencies, .5), 3), 'p95_ms': round(percentile(latencies, .95), 3),
            'max_lexical_candidates': max(r['lexical_candidates'] for r in rows),
            'groups': {group: {'passed': sum(r['hit_at_5'] for r in rows if r['id'].startswith(prefix)),
                               'total': sum(r['id'].startswith(prefix) for r in rows)}
                       for group, prefix in [('questions', 'q'), ('short', 'short'), ('empty', 'empty')]}}


def evaluate_size(docs, seed, build_timeout, result, server_only=False):
    cases = json.loads((ROOT / 'examples/eval-questions.json').read_text())['cases']
    process = None
    with tempfile.TemporaryDirectory(prefix='xrag-corpus-') as tmp:
        root = Path(tmp)
        corpus, data = root / 'corpus', root / 'data'
        result['corpus'] = generate(docs, seed, corpus)
        server, client = ROOT / 'bin/xrag-server', ROOT / 'bin/xrag'
        command(server, 'init', '--data', data)
        token = (data / 'admin.key').read_text().strip()
        with socket.socket() as sock:
            sock.bind(('127.0.0.1', 0))
            port = sock.getsockname()[1]
        base = f'http://127.0.0.1:{port}'
        config = root / 'providers.json'
        config.write_text('{}')
        client_config = root / 'client.json'
        client_config.write_text('{}')
        try:
            process = subprocess.Popen([str(server), 'serve', '--data', str(data), '--listen', f'127.0.0.1:{port}',
                                        '--config', str(config)], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            for _ in range(100):
                if process.poll() is not None:
                    raise RuntimeError('Server exited before readiness')
                try:
                    result['health'] = call(base + '/healthz')
                    break
                except (OSError, urllib.error.URLError):
                    time.sleep(.1)
            else:
                raise TimeoutError('Server readiness timeout')
            for source in result['corpus']['sources']:
                call(base + '/v1/kbs/demo/sources', {'id': source['id'], 'root': str(corpus / source['path']),
                     'mount': source['path'], 'kind': 'path'}, token)
            started = time.perf_counter()
            job = call(base + '/v1/kbs/demo/builds', {'mode': 'full'}, token)
            while time.perf_counter() - started < build_timeout:
                state = call(base + '/v1/jobs/' + job['id'], token=token)
                if state['state'] not in ('queued', 'running'):
                    break
                time.sleep(.2)
            else:
                raise TimeoutError(f'build exceeded {build_timeout}s')
            result['build_seconds'] = round(time.perf_counter() - started, 3)
            result['build_state'] = state['state']
            result['build_result'] = {key: state[key] for key in ('result', 'error') if key in state}
            if state['state'] != 'succeeded':
                raise RuntimeError('corpus build failed: ' + state['state'])
            with sqlite3.connect(data / 'xrag.sqlite') as conn:
                pages = conn.execute('PRAGMA page_count').fetchone()[0]
                page_size = conn.execute('PRAGMA page_size').fetchone()[0]
            result['sqlite_bytes_after_build'] = pages * page_size
            result['sqlite_files_after_build'] = {p.name: p.stat().st_size for p in data.glob('xrag.sqlite*')}
            print(json.dumps({'docs': docs, 'stage': 'built', 'seconds': result['build_seconds']}, ensure_ascii=False), flush=True)
            result['server'] = []
            for case in cases:
                started = time.perf_counter()
                response = call(base + '/v1/ask', {'kbs': ['demo'], 'q': case['question'], 'limit': 5, 'trace': True}, token)
                result['server'].append(measurement(response, case, time.perf_counter() - started))
            result['keyword'] = []
            for marker, expected in result['corpus']['keyword_expected_chunks'].items():
                response = call(base + '/v1/search/keyword', {'kbs': ['demo'], 'q': marker, 'limit': 5, 'save_scope': 'none'}, token)
                total = response['total']
                exact = total.get('accuracy') == 'exact' and total.get('value') == expected
                result['keyword'].append({'term': marker, 'expected': expected, 'actual': total, 'exact': exact})
                if not exact:
                    raise RuntimeError('keyword count mismatch: ' + marker)
            result['local'] = []
            for case in ([] if server_only else cases):
                started = time.perf_counter()
                response = json.loads(command(client, '+ask', case['question'], '--local', corpus, '--kb', 'demo',
                                              '--limit', '5', '--trace', '--config', client_config, timeout=60))
                result['local'].append(measurement(response, case, time.perf_counter() - started))
            result['local_status'] = 'not_requested' if server_only else 'completed'
            result['summary'] = {mode: summary(result[mode]) for mode in ('server', 'local') if result[mode]}
            result['status'] = 'completed'
        finally:
            if process:
                process.terminate()
                try:
                    process.wait(timeout=15)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait()


def markdown(report):
    lines = ['# XRAG 大语料基线', '', f"状态：{report['status']}。提交：`{report['commit']}`。", '',
             '每档包含原样 demo 手册和指定数量的干扰文档；干扰文档按每个来源最多 4000 篇分组，Server 与 local 检索同一完整目录。固定 seed，无 embedding。每题测量一次，含首次查询。',
             'hit@5 表示返回的前 5 条中是否包含目标文件，空结果题单独按正确返回空计分。Server 延迟含本机 HTTP；local 延迟含 CLI 启动、目录读取和查询。',
             'p50/p95 使用线性插值。SQLite 大小取构建完成后的 page_count × page_size；原始 JSON 另记主文件及 WAL 字节数。', '',
             '| 干扰文档 | 构建秒 | SQLite MiB | 模式 | hit@5 | p50 ms | p95 ms | 最大词法候选 |',
             '|---:|---:|---:|---|---:|---:|---:|---:|']
    for run in report['runs']:
        for mode, stats in run.get('summary', {}).items():
            lines.append(f"| {run['docs']} | {run['build_seconds']} | {run['sqlite_bytes_after_build']/1048576:.2f} | {mode} | {stats['hit_at_5']}/{stats['cases']} | {stats['p50_ms']} | {stats['p95_ms']} | {stats['max_lexical_candidates']} |")
    lines += ['', '## 逐题结果', '', '| 干扰文档 | 题号 | 问句 | Server 命中 / ms / 候选 | local 命中 / ms / 候选 |', '|---:|---|---|---|---|']
    for run in report['runs']:
        local = {r['id']: r for r in run.get('local', [])}
        for row in run.get('server', []):
            def cell(r):
                return f"{'✓' if r['hit_at_5'] else '✗'} / {r['latency_ms']} / {r['lexical_candidates']}" if r else '未完成'
            lines.append(f"| {run['docs']} | {row['id']} | {row['question']} | {cell(row)} | {cell(local.get(row['id']))} |")
    lines += ['', '## 精确计数', '']
    for run in report['runs']:
        for count in run.get('keyword', []):
            lines.append(f"- N={run['docs']}，`{count['term']}`：预期 {count['expected']}，实际 {count['actual'].get('value')}，精确校验 {'通过' if count['exact'] else '失败'}。")
    lines += ['', '合成干扰语料用于测量现有实现，不设置相关性通过阈值，也不据此修改检索策略。时延受当前机器、文件系统和缓存影响。', '']
    if report.get('error'):
        lines += ['失败：' + json.dumps(report['error'], ensure_ascii=False), '']
    return '\n'.join(lines)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--docs', type=int, nargs='+', default=[1000, 10000])
    parser.add_argument('--seed', type=int, default=20261016)
    parser.add_argument('--server-only', action='store_true', help='只测 Server；报告中明确 local 未执行')
    parser.add_argument('--build-timeout', type=int, default=1800)
    parser.add_argument('--report', type=Path, default=ROOT / 'checks/corpus-eval.json')
    args = parser.parse_args()
    report = {'status': 'running', 'commit': command('git', '-C', ROOT, 'rev-parse', 'HEAD').strip(),
              'environment': {'os': platform.system(), 'machine': platform.machine(), 'cpu_count': os.cpu_count(),
                              'python': platform.python_version()}, 'seed': args.seed, 'runs': []}
    try:
        for docs in args.docs:
            run = {'docs': docs, 'status': 'running'}
            report['runs'].append(run)
            evaluate_size(docs, args.seed, args.build_timeout, run, args.server_only)
        report['status'] = 'completed'
    except Exception as exc:
        report['status'] = 'failed'
        if report['runs']:
            report['runs'][-1]['status'] = 'failed'
        report['error'] = {'type': type(exc).__name__, 'message': str(exc)[:500]}
    finally:
        args.report.parent.mkdir(parents=True, exist_ok=True)
        args.report.write_text(json.dumps(report, ensure_ascii=False, indent=2) + '\n')
        args.report.with_suffix('.md').write_text(markdown(report))
        print(json.dumps({'status': report['status'], 'report': str(args.report)}, ensure_ascii=False))
    return 0 if report['status'] == 'completed' else 1


if __name__ == '__main__':
    raise SystemExit(main())
