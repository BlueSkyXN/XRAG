#!/usr/bin/env python3
"""按 Git 跟踪清单打包源码，并校验 Linux amd64 二进制的平台、程序和提交。"""
from pathlib import Path
import argparse
import re
import subprocess
import tarfile
import zipfile

ROOT = Path(__file__).resolve().parents[1]


def release_version():
    text = (ROOT / 'core/types.go').read_text(encoding='utf-8')
    match = re.search(r'^const Version = "([^"]+)"$', text, re.MULTILINE)
    if match is None:
        raise SystemExit('core/types.go 缺少 Version 常量')
    return match.group(1)


def eligible(path):
    rel = path.relative_to(ROOT)
    return (
        path.is_file() and path.resolve() == path
        and rel.parts[0] not in {'bin', 'checks', 'local', 'data'}
        and not any(part in {'.git', '.xrag', 'node_modules', '__pycache__'}
                    or part.startswith('.xrag-') for part in rel.parts)
        and path.name != '.env' and not path.name.startswith('.env.')
        and path.suffix not in {'.pyc', '.key', '.sqlite'}
        and not path.name.endswith(('.sqlite-wal', '.sqlite-shm'))
        and rel.as_posix() not in {'providers.json', 'client.direct.json'}
    )


def require_clean():
    result = subprocess.run(
        ['git', '-C', str(ROOT), 'status', '--porcelain=v1', '--untracked-files=normal'],
        check=True, capture_output=True, text=True,
    )
    if result.stdout:
        raise SystemExit('打包要求工作区干净，请先提交或移走以下改动：\n' + result.stdout)


def source_files():
    result = subprocess.run(
        ['git', '-C', str(ROOT), 'ls-files', '-z'],
        check=True, capture_output=True,
    )
    paths = [ROOT / name.decode('utf-8') for name in result.stdout.split(b'\0') if name]
    excluded = [path.relative_to(ROOT).as_posix() for path in paths if not eligible(path)]
    if excluded:
        raise SystemExit('以下已跟踪文件被打包规则排除，请先核对 Git 清单：\n' + '\n'.join(excluded))
    return sorted(paths)


def linux_binaries():
    commit = subprocess.run(['git', '-C', str(ROOT), 'rev-parse', 'HEAD'],
                            check=True, capture_output=True, text=True).stdout.strip()
    binaries = [ROOT / 'bin' / name for name in ('xrag', 'xrag-server')]
    for path in binaries:
        if not path.is_file() or path.is_symlink():
            raise SystemExit(f'缺少 {path.name}；请在 Linux amd64 上运行 make build，或使用 --source-only')
        with path.open('rb') as handle:
            header = handle.read(64)
        if len(header) < 64 or header[:6] != b'\x7fELF\x02\x01' or int.from_bytes(header[18:20], 'little') != 62:
            raise SystemExit(f'{path.name} 不是 Linux amd64 ELF；请在对应平台构建，或使用 --source-only')
        result = subprocess.run(['go', 'version', '-m', str(path)], check=True, capture_output=True, text=True)
        for setting in ('GOOS=linux', 'GOARCH=amd64'):
            if re.search(r'^\s*build\s+' + setting + r'\s*$', result.stdout, re.MULTILINE) is None:
                raise SystemExit(f'{path.name} 的 Go 构建信息缺少 {setting}，请重新构建')
        expected_path = f'github.com/BlueSkyXN/XRAG/cmd/{path.name}'
        if re.search(r'^\s*path\s+' + re.escape(expected_path) + r'\s*$', result.stdout, re.MULTILINE) is None:
            raise SystemExit(f'{path.name} 的 Go path 不是 {expected_path}，请重新构建')
        for setting in (f'vcs.revision={commit}', 'vcs.modified=false'):
            if re.search(r'^\s*build\s+' + re.escape(setting) + r'\s*$', result.stdout, re.MULTILINE) is None:
                raise SystemExit(f'{path.name} 的 Go 构建信息缺少 {setting}，请在干净工作区运行 make build')
    return binaries


def main(out, source_only=False):
    require_clean()
    version = release_version()
    stem = f'xrag-{version}'
    files = source_files()
    binaries = [] if source_only else linux_binaries()
    out.mkdir(parents=True, exist_ok=True)
    source = out / f'{stem}-source.zip'
    with zipfile.ZipFile(source, 'w', zipfile.ZIP_DEFLATED, compresslevel=8) as archive:
        for path in files:
            archive.write(path, stem + '/' + path.relative_to(ROOT).as_posix())
    print(source)
    if not source_only:
        linux = out / f'{stem}-linux-amd64.tar.gz'
        with tarfile.open(linux, 'w:gz', compresslevel=7) as archive:
            for path in files + binaries:
                archive.add(path, arcname=stem + '/' + path.relative_to(ROOT).as_posix(), recursive=False)
        print(linux)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--out', type=Path, required=True)
    parser.add_argument('--source-only', action='store_true', help='只生成源码 ZIP，无需 Linux 二进制')
    args = parser.parse_args()
    main(args.out, args.source_only)
