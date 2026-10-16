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
        self.write('.gitignore', 'local/\nwebapp/node_modules/\nchecks/\nbin/\n')
        subprocess.run(['git', '-C', str(self.root), 'add', '.'], check=True)
        self.commit()

    def commit(self):
        subprocess.run(['git', '-C', str(self.root), '-c', 'user.name=ci',
                        '-c', 'user.email=ci@example.invalid', 'commit', '-qm', 'fixture'], check=True)

    def write(self, name, text):
        path = self.root / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(text)
        return path

    def test_source_archive_omits_ignored_local_files(self):
        self.write('local/private-note.md', 'local-only note')
        self.write('webapp/node_modules/dependency.js', 'dependency cache')
        self.write('checks/output.log', 'local log')
        output = self.root / 'local/releases'
        with redirect_stdout(io.StringIO()):
            package.main(output, source_only=True)
        with zipfile.ZipFile(output / 'xrag-1.2.3-source.zip') as archive:
            names = set(archive.namelist())
            self.assertTrue(archive.getinfo('xrag-1.2.3/run-linux.sh').external_attr >> 16 & 0o111)
            self.assertEqual(names, {'xrag-1.2.3/' + p for p in ('.gitignore', 'core/types.go', 'README.md', 'run-linux.sh')})
        self.assertEqual(len(list(output.iterdir())), 1)

    def test_tracked_filtered_files_are_reported(self):
        names = ['.env', 'tracked.key', 'bin/tool', 'linked.go']
        for name in names[:-1]:
            self.write(name, 'fixture')
        (self.root / 'linked.go').symlink_to(self.root / 'README.md')
        subprocess.run(['git', '-C', str(self.root), 'add', '-f', *names], check=True)
        self.commit()
        with self.assertRaises(SystemExit) as caught:
            package.main(self.root / 'local/out', source_only=True)
        for name in names:
            self.assertIn(name, str(caught.exception))

    def test_dirty_worktree_is_rejected(self):
        for kind in ('modified', 'deleted', 'untracked'):
            with self.subTest(kind=kind):
                readme = self.root / 'README.md'
                if kind == 'modified':
                    readme.write_text('changed')
                elif kind == 'deleted':
                    readme.unlink()
                else:
                    self.write('untracked.txt', 'new')
                with self.assertRaisesRegex(SystemExit, 'README.md|untracked.txt'):
                    package.main(self.root / 'local/out', source_only=True)
                readme.write_text('# Project\n')
                (self.root / 'untracked.txt').unlink(missing_ok=True)

    def test_tracked_directory_replaced_with_symlink_is_reported(self):
        self.write('docs/readme.md', 'tracked documentation')
        subprocess.run(['git', '-C', str(self.root), 'add', 'docs/readme.md'], check=True)
        (self.root / 'docs/readme.md').unlink()
        (self.root / 'docs').rmdir()
        self.write('local/private-docs/readme.md', 'private note')
        (self.root / 'docs').symlink_to(self.root / 'local/private-docs', target_is_directory=True)
        with self.assertRaisesRegex(SystemExit, 'docs/readme.md'):
            package.source_files()

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

    def check_build_info(self, wrong_path=False, wrong_commit=False):
        header = b'\x7fELF\x02\x01' + b'\0' * 12 + (62).to_bytes(2, 'little') + b'\0' * 44
        (self.root / 'bin').mkdir(exist_ok=True)
        for name in ('xrag', 'xrag-server'):
            (self.root / 'bin' / name).write_bytes(header)
        real_run = subprocess.run
        commit = real_run(['git', '-C', str(self.root), 'rev-parse', 'HEAD'], check=True, capture_output=True, text=True).stdout.strip()
        def fake_run(args, **kwargs):
            if args[:3] != ['go', 'version', '-m']:
                return real_run(args, **kwargs)
            name = 'other' if wrong_path else Path(args[-1]).name
            sha = '0' * 40 if wrong_commit else commit
            output = (f'\tpath\tgithub.com/BlueSkyXN/XRAG/cmd/{name}\n'
                      f'\tbuild\t-ldflags="-X github.com/BlueSkyXN/XRAG/core.Commit={sha}"\n'
                      '\tbuild\tGOOS=linux\n\tbuild\tGOARCH=amd64\n')
            return subprocess.CompletedProcess(args, 0, stdout=output)
        with patch.object(package.subprocess, 'run', side_effect=fake_run):
            return package.linux_binaries()

    def test_build_path_must_match_program(self):
        with self.assertRaisesRegex(SystemExit, 'Go path'):
            self.check_build_info(wrong_path=True)

    def test_build_commit_must_match_head(self):
        with self.assertRaisesRegex(SystemExit, 'Commit='):
            self.check_build_info(wrong_commit=True)

    def test_matching_build_info_is_accepted(self):
        self.assertEqual(len(self.check_build_info()), 2)


if __name__ == '__main__':
    unittest.main()
