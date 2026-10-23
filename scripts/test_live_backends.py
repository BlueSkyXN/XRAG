"""用本机受控 Qdrant HTTP 端点检查联调脚本的集合绑定。"""
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import os
from pathlib import Path
import tempfile
import threading
import unittest
from unittest.mock import patch

import live_backends


class LiveBindingTests(unittest.TestCase):
    def setUp(self):
        self.calls = []
        self.reference = {"o": "fixture-origin", "k": "fixture-kb", "g": "fixture-generation"}
        self.descriptor = {"origin_id": "fixture-origin", "kb": "fixture-kb", "model_identity": "fixture-generation",
                           "model": "fixture-embedding", "dimension": 32, "physical_collection": "fixture-collection"}
        self.result = {"points": [{"payload": self.descriptor}], "next_page_offset": None}
        fixture = self

        class Handler(BaseHTTPRequestHandler):
            def do_POST(self):
                body = json.loads(self.rfile.read(int(self.headers['Content-Length'])))
                fixture.calls.append((self.path, self.headers.get('api-key'), body))
                data = json.dumps({"result": fixture.result}).encode()
                self.send_response(200)
                self.send_header('Content-Type', 'application/json')
                self.send_header('Content-Length', str(len(data)))
                self.end_headers()
                self.wfile.write(data)

            def log_message(self, *_):
                pass

        self.server = ThreadingHTTPServer(('127.0.0.1', 0), Handler)
        self.thread = threading.Thread(target=self.server.serve_forever, kwargs={'poll_interval': .01})
        self.thread.start()
        self.addCleanup(self.close_server)
        self.config = {"qdrant": {"url": f"http://127.0.0.1:{self.server.server_port}"},
                       "embedding": {"model": "fixture-embedding", "dimension": 32}}

    def close_server(self):
        self.server.shutdown()
        self.server.server_close()
        self.thread.join()

    def test_uses_descriptor_name_without_reconstructing_it(self):
        for name in ['legacy-name', 'sparse-xrag-terms-fnv1a32-bm25-v1', 'future-layout-name']:
            with self.subTest(name=name):
                self.descriptor['physical_collection'] = name
                self.assertEqual(live_backends.resolve_collection(self.config, self.reference), name)
        path, key, body = self.calls[-1]
        self.assertEqual(path, '/collections/xrag_demo_meta/points/scroll')
        self.assertIsNone(key)
        self.assertEqual({x['key']: x['match']['value'] for x in body['filter']['must']},
                         {"origin_id": "fixture-origin", "kb": "fixture-kb", "model_identity": "fixture-generation"})
        self.assertEqual(body['limit'], 2)
        self.assertTrue(body['with_payload'])
        self.assertFalse(body['with_vector'])

    def test_rejects_missing_ambiguous_or_incomplete_result(self):
        for points, offset in [([], None), ([{}, {}], None), ([{'payload': self.descriptor}], 'next-page')]:
            with self.subTest(points=len(points), offset=offset):
                self.result = {'points': points, 'next_page_offset': offset}
                with self.assertRaisesRegex(ValueError, 'exactly one'):
                    live_backends.resolve_collection(self.config, self.reference)

    def test_rejects_wrong_identity_or_embedding(self):
        for key in ['origin_id', 'kb', 'model_identity', 'model', 'dimension']:
            with self.subTest(key=key):
                wrong = {**self.descriptor, key: 'wrong'}
                self.result = {'points': [{'payload': wrong}]}
                with self.assertRaisesRegex(ValueError, 'does not match'):
                    live_backends.resolve_collection(self.config, self.reference)

    def test_requires_physical_collection(self):
        for value in [None, '', '  ', 17]:
            with self.subTest(value=value):
                self.descriptor['physical_collection'] = value
                with self.assertRaisesRegex(ValueError, 'physical_collection'):
                    live_backends.resolve_collection(self.config, self.reference)

    def test_resolves_environment_and_file_credentials(self):
        with patch.dict(os.environ, {'XRAG_BINDING_TEST_KEY': 'synthetic-key'}):
            self.config['qdrant']['key'] = 'env:XRAG_BINDING_TEST_KEY'
            live_backends.resolve_collection(self.config, self.reference)
            self.assertEqual(self.calls[-1][1], 'synthetic-key')
        with tempfile.TemporaryDirectory() as tmp:
            key = Path(tmp) / 'key'
            key.write_text('synthetic-file-key\n')
            self.config['qdrant']['key'] = 'file:' + str(key)
            live_backends.resolve_collection(self.config, self.reference)
            self.assertEqual(self.calls[-1][1], 'synthetic-file-key')

    def test_rejects_unavailable_credentials_before_network(self):
        for key in ['plain-value', 'env:XRAG_BINDING_MISSING_KEY']:
            with self.subTest(key=key), patch.dict(os.environ, {}, clear=True):
                self.config['qdrant']['key'] = key
                with self.assertRaises(ValueError):
                    live_backends.resolve_collection(self.config, self.reference)
        self.assertEqual(self.calls, [])

    def test_without_confirmation_does_not_read_config_or_contact_services(self):
        with tempfile.TemporaryDirectory() as tmp:
            report = Path(tmp) / 'report.json'
            with patch('live_backends.urllib.request.urlopen') as network:
                self.assertEqual(live_backends.run(Path(tmp) / 'absent.json', report, False), 2)
                network.assert_not_called()
            self.assertEqual(json.loads(report.read_text())['status'], 'not_run')


if __name__ == '__main__':
    unittest.main()
