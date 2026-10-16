#!/usr/bin/env python3
"""真实 Qdrant 的 direct 召回基线；固定哈希向量仅验证融合流程，无召回阈值。"""
from __future__ import annotations

import argparse
import base64
import hashlib
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import math
from pathlib import Path
import socket
import subprocess
import tempfile
import threading
import time
import urllib.error
import urllib.request

ROOT = Path(__file__).resolve().parents[1]
DIMENSION = 32


def http_json(url, body=None, token=None):
    headers = {'Content-Type': 'application/json'}
    if token:
        headers['Authorization'] = 'Bearer ' + token
    request = urllib.request.Request(url, data=None if body is None else json.dumps(body).encode(), headers=headers)
    with urllib.request.urlopen(request, timeout=60) as response:
        return json.load(response)


def command(*args):
    return subprocess.run([str(a) for a in args], check=True, capture_output=True, text=True, timeout=180).stdout


class Embedding(BaseHTTPRequestHandler):
    def log_message(self, *_):
        pass

    def do_POST(self):
        if self.path != '/embeddings':
            self.send_error(404)
            return
        request = json.loads(self.rfile.read(int(self.headers['Content-Length'])))
        data = []
        for i, text in enumerate(request['input']):
            vector = [x - 127.5 for x in hashlib.sha256(text.encode()).digest()]
            norm = math.sqrt(sum(x * x for x in vector))
            data.append({'index': i, 'embedding': [x / norm for x in vector]})
        body = json.dumps({'data': data, 'usage': {'total_tokens': 0}}).encode()
        self.send_response(200)
        self.send_header('Content-Type', 'application/json')
        self.send_header('Content-Length', str(len(body)))
        self.end_headers()
        self.wfile.write(body)


def measure(response, case):
    if response.get('failure') or response.get('coverage', {}).get('execution_partial'):
        raise RuntimeError('retrieval failed or execution_partial')
    paths = [item['path'].removeprefix('manual/') for item in response['items']]
    expected = [item['path'].removeprefix('manual/') for item in case['expected']]
    hit = not paths if case.get('expected_empty') else any(path in expected for path in paths)
    return {'hit_at_5': hit, 'paths': paths, 'returned': response['returned'],
            'total': response['total'], 'strategy': response['strategy'],
            'items': [{'path': i.get('path'), 'chunk_id': i.get('chunk_id'), 'score': i.get('score'),
                       'routes': i.get('routes', [])} for i in response['items']]}


def evaluate(qdrant, report_path):
    suite_bytes = (ROOT / 'examples/eval-questions.json').read_bytes()
    suite = json.loads(suite_bytes)
    report = {'status': 'running', 'question_sha256': hashlib.sha256(suite_bytes).hexdigest(),
              'commit': command('git', '-C', ROOT, 'rev-parse', 'HEAD').strip(),
              'embedding': {'algorithm': 'SHA-256 bytes centered at 127.5, L2 normalized',
                            'dimension': DIMENSION, 'semantic_quality': 'not_measured'},
              'cases': [{**case, 'results': {}} for case in suite['cases']]}
    process = None
    embedding = ThreadingHTTPServer(('127.0.0.1', 0), Embedding)
    threading.Thread(target=embedding.serve_forever, daemon=True).start()
    try:
        for _ in range(100):
            try:
                report['qdrant'] = http_json(qdrant)
                break
            except (OSError, urllib.error.URLError):
                time.sleep(.2)
        else:
            raise RuntimeError('Qdrant readiness timeout')
        with tempfile.TemporaryDirectory(prefix='xrag-direct-baseline-') as tmp:
            root = Path(tmp)
            data = root / 'data'
            server, client = ROOT / 'bin/xrag-server', ROOT / 'bin/xrag'
            command(server, 'init', '--data', data, '--examples', ROOT / 'examples/docs')
            token = (data / 'admin.key').read_text().strip()
            cfg = {'qdrant': {'url': qdrant}, 'embedding': {
                'url': f'http://127.0.0.1:{embedding.server_port}', 'model': 'fixture-hash-v1',
                'dimension': DIMENSION, 'revision': '1'}}
            providers = root / 'providers.json'
            with socket.socket() as sock:
                sock.bind(('127.0.0.1', 0))
                port = sock.getsockname()[1]
            base = f'http://127.0.0.1:{port}'
            def api(path, body=None):
                return http_json(base + path, body, token)
            def start(config):
                nonlocal process
                providers.write_text(json.dumps(config))
                process = subprocess.Popen([str(server), 'serve', '--data', str(data), '--listen',
                    f'127.0.0.1:{port}', '--config', str(providers)], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
                for _ in range(100):
                    if process.poll() is not None:
                        raise RuntimeError('Server exited before readiness')
                    try:
                        http_json(base + '/healthz')
                        return
                    except (OSError, urllib.error.URLError):
                        time.sleep(.1)
                raise RuntimeError('Server readiness timeout')
            def stop():
                nonlocal process
                process.terminate()
                process.wait(timeout=20)
                process = None
            start(cfg)
            job = api('/v1/kbs/demo/builds', {'mode': 'full'})
            for _ in range(300):
                result = api('/v1/jobs/' + job['id'])
                if result['state'] not in ('queued', 'running'):
                    break
                time.sleep(.2)
            if result['state'] != 'succeeded':
                raise RuntimeError('Qdrant build did not succeed: ' + result['state'])
            sample = api('/v1/search/keyword', {'kbs': ['demo'], 'q': 'E203', 'limit': 5})
            encoded = sample['items'][0]['ref'].split(':', 1)[1]
            ref = json.loads(base64.urlsafe_b64decode(encoded + '=' * (-len(encoded) % 4)))
            origin = ref['o']
            origin_hash = hashlib.sha256(json.dumps(origin, ensure_ascii=False, separators=(',', ':')).encode()).hexdigest()[:8]
            collection = f'xrag_demo_{origin_hash}_demo_{ref["g"][:12]}'
            info = http_json(qdrant + '/collections/' + collection)
            report['collection'] = collection
            report['payload_schema'] = info['result']['payload_schema']
            for mode in ('fusion', 'lexical'):
                if mode == 'lexical':
                    stop()
                    start({})
                for row in report['cases']:
                    row['results']['server_' + mode] = {}
                    for action, route in (('ask', '/v1/ask'), ('keyword', '/v1/search/keyword')):
                        response = api(route, {'kbs': ['demo'], 'q': row['question'], 'limit': 5})
                        row['results']['server_' + mode][action] = measure(response, row)
            stop()
            report['server_stopped_during_direct'] = True
            for mode in ('lexical', 'fusion'):
                profile = {'profile_id': 'baseline', 'type': 'direct', 'default_kbs': ['demo'],
                           'physical_collection': collection, 'origin_id': origin, 'qdrant': cfg['qdrant']}
                if mode == 'fusion':
                    profile['embedding'] = cfg['embedding']
                config = root / f'direct-{mode}.json'
                config.write_text(json.dumps({'default_profile': 'baseline', 'profiles': {'baseline': profile}}))
                for row in report['cases']:
                    row['results']['direct_' + mode] = {}
                    for action, argv in (('ask', ['+ask']), ('keyword', ['search', 'keyword'])):
                        response = json.loads(command(client, *argv, row['question'], '--limit', '5', '--config', config))
                        row['results']['direct_' + mode][action] = measure(response, row)
            # 用实际 payload 文本索引核对词与完整问句；不修改 direct 检索实现。
            report['word_probes'] = {}
            for text in ('E203', '日志丢失', '日志丢失怎么排查', '如何排查日志丢失', '分页样本记录'):
                result = http_json(qdrant + '/collections/' + collection + '/points/count',
                    {'filter': {'must': [{'key': 'text', 'match': {'text': text}}]}, 'exact': True})
                report['word_probes'][text] = result['result']['count']
        report['totals'] = {}
        for mode in ('server_lexical', 'server_fusion', 'direct_lexical', 'direct_fusion'):
            report['totals'][mode] = {}
            for action in ('ask', 'keyword'):
                groups = {}
                for group, prefix in (('questions', 'q'), ('short', 'short'), ('empty', 'empty')):
                    rows = [r for r in report['cases'] if r['id'].startswith(prefix)]
                    groups[group] = {'passed': sum(r['results'][mode][action]['hit_at_5'] for r in rows), 'total': len(rows)}
                report['totals'][mode][action] = groups
        report['status'] = 'completed'
    except Exception as exc:
        report['status'] = 'infrastructure_failed'
        report['error'] = {'type': type(exc).__name__, 'message': str(exc)[:500]}
    finally:
        if process:
            process.terminate()
            try:
                process.wait(timeout=15)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait()
        embedding.shutdown()
        embedding.server_close()
        report_path.parent.mkdir(parents=True, exist_ok=True)
        report_path.write_text(json.dumps(report, ensure_ascii=False, indent=2) + '\n')
        print(json.dumps({'status': report['status'], 'report': str(report_path), 'totals': report.get('totals')}, ensure_ascii=False))
    return 0 if report['status'] == 'completed' else 1


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--qdrant', default='http://127.0.0.1:6333')
    parser.add_argument('--report', type=Path, required=True)
    args = parser.parse_args()
    raise SystemExit(evaluate(args.qdrant.rstrip('/'), args.report))
