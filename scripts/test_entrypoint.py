"""验证容器首次初始化、显式构建和恢复副本的启动参数。"""
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]


class EntrypointTests(unittest.TestCase):
    def test_build_selection(self):
        cases = [
            (False, None, False, 'demo'),
            (True, None, False, None),
            (False, 'custom-kb_2', False, 'custom-kb_2'),
            (True, 'custom-kb_2', False, 'custom-kb_2'),
            (False, 'demo', True, None),
            (True, 'demo', True, None),
        ]
        for initialized, build, restore, expected in cases:
            with self.subTest(initialized=initialized, build=build, restore=restore), tempfile.TemporaryDirectory() as tmp:
                root = Path(tmp)
                server = root / 'server'
                server.write_text('''#!/usr/bin/env python3
import json, os, pathlib, sys
args = sys.argv[1:]
if args[0] == 'init':
    pathlib.Path(os.environ['XRAG_DATA_DIR'], 'xrag.sqlite').touch()
else:
    pathlib.Path(os.environ['ARGS_FILE']).write_text(json.dumps(args))
''')
                server.chmod(0o755)
                if initialized:
                    (root / 'xrag.sqlite').touch()
                if restore:
                    (root / 'RESTORE_INFO.json').write_text('{}')
                env = {k: v for k, v in os.environ.items() if k not in ('XRAG_BUILD_ON_START', 'ADMIN_USERNAME', 'ADMIN_PASSWORD')}
                env.update(XRAG_SERVER_BIN=str(server), XRAG_DATA_DIR=str(root), ARGS_FILE=str(root / 'args.json'))
                if build is not None:
                    env['XRAG_BUILD_ON_START'] = build
                result = subprocess.run(['bash', str(ROOT / 'docker/entrypoint.sh')], env=env, capture_output=True, text=True, check=True)
                args = json.loads((root / 'args.json').read_text())
                self.assertEqual(args.count('--build'), int(expected is not None))
                if expected:
                    self.assertEqual(args[args.index('--build') + 1], expected)
                if restore:
                    self.assertIn('skipping automatic build', result.stdout)
                for invalid in ('-bad', 'a/b', '中文', 'a' * 65, 'a b'):
                    env['XRAG_BUILD_ON_START'] = invalid
                    result = subprocess.run(['bash', str(ROOT / 'docker/entrypoint.sh')], env=env, capture_output=True, text=True)
                    self.assertNotEqual(result.returncode, 0)
                    self.assertIn('XRAG_BUILD_ON_START', result.stderr)


if __name__ == '__main__':
    unittest.main()
