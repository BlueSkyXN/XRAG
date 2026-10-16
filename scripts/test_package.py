"""验证发布包不带本地材料，并拒绝错误平台的二进制。"""
from contextlib import redirect_stdout
from pathlib import Path
import io
import subprocess
import tempfile
import unittest
from unittest.mock import patch
import zipfile

import package


class PackageTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name).resolve()
        self.addCleanup(patch.stopall)
        patch.object(package, 'ROOT', self.root).start()
        subprocess.run(['git', 'init', '-q', str(self.root)], check=True)
        self.write('core/types.go', 'package core\nconst Version = "1.2.3"\n')
        self.write('README.md', '# Project\n')
        self.write('run-linux.sh', '#!/bin/sh\nexit 0\n')
        (self.root / 'run-linux.sh').chmod(0o755)
        self.write('.gitignore', 'local/\nwebapp/node_modules/\nchecks/\n')
        subprocess.run(['git', '-C', str(self.root), 'add', '.'], check=True)

    def write(self, name, text):
        path = self.root / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(text)
        return path

    def test_source_archive_omits_local_and_sensitive_files(self):
        self.write('local/private-note.md', 'local-only note')
        self.write('webapp/node_modules/dependency.js', 'dependency cache')
        self.write('checks/output.log', 'local log')
        self.write('untracked.txt', 'untracked working material')
        self.write('.env', 'EXAMPLE_SECRET=fixture')
        self.write('tracked.key', 'fixture key')
        self.write('bin/tool', 'build output')
        self.write('local/private-source.go', 'private source')
        (self.root / 'linked.go').symlink_to(self.root / 'local/private-source.go')
        subprocess.run(['git', '-C', str(self.root), 'add', '-f', '.env', 'tracked.key', 'bin/tool', 'linked.go'], check=True)
        output = self.root / 'local/releases'
        with redirect_stdout(io.StringIO()):
            package.main(output, source_only=True)
        with zipfile.ZipFile(output / 'xrag-1.2.3-source.zip') as archive:
            names = set(archive.namelist())
            self.assertIn('xrag-1.2.3/core/types.go', names)
            self.assertIn('xrag-1.2.3/README.md', names)
            self.assertTrue(archive.getinfo('xrag-1.2.3/run-linux.sh').external_attr >> 16 & 0o111)
            self.assertEqual(names, {'xrag-1.2.3/' + p for p in ('.gitignore', 'core/types.go', 'README.md', 'run-linux.sh')})
        self.assertEqual(len(list(output.iterdir())), 1)

    def test_tracked_directory_replaced_with_symlink_is_excluded(self):
        self.write('docs/readme.md', 'tracked documentation')
        subprocess.run(['git', '-C', str(self.root), 'add', 'docs/readme.md'], check=True)
        (self.root / 'docs/readme.md').unlink()
        (self.root / 'docs').rmdir()
        self.write('local/private-docs/readme.md', 'private note')
        (self.root / 'docs').symlink_to(self.root / 'local/private-docs', target_is_directory=True)
        self.assertNotIn(self.root / 'docs/readme.md', package.source_files())

    def test_non_linux_or_non_amd64_binary_is_rejected_before_writing(self):
        for header in (b'\xcf\xfa\xed\xfe' + b'\0' * 60,
                       b'\x7fELF\x02\x01' + b'\0' * 12 + (183).to_bytes(2, 'little') + b'\0' * 44):
            with self.subTest(header=header[:6]):
                (self.root / 'bin').mkdir(exist_ok=True)
                for name in ('xrag', 'xrag-server'):
                    (self.root / 'bin' / name).write_bytes(header)
                output = self.root / 'out'
                with self.assertRaisesRegex(SystemExit, 'Linux amd64'):
                    package.main(output)
                self.assertFalse(output.exists())


if __name__ == '__main__':
    unittest.main()
