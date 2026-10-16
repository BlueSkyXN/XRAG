#!/usr/bin/env python3
"""Package source and Linux binaries without runtime data or secrets.
Usage: python3 scripts/package.py --out /tmp/releases
"""
from pathlib import Path
import argparse, tarfile, zipfile
ROOT=Path(__file__).resolve().parents[1];NAME='xrag-demo-0.10.0'
def eligible(p):
 rel=p.relative_to(ROOT)
 return p.is_file() and not p.is_symlink() and not any(x in {'.git','.xrag-demo','__pycache__'} for x in rel.parts) and p.suffix not in {'.pyc','.key','.sqlite'} and not p.name.endswith(('.sqlite-wal','.sqlite-shm')) and rel.as_posix() not in {'providers.json','client.direct.json'}
def main(out):
 out.mkdir(parents=True,exist_ok=True)
 files=[p for p in sorted(ROOT.rglob('*')) if eligible(p)]
 with zipfile.ZipFile(out/'XRAG_V0.7_Demo9_源码.zip','w',zipfile.ZIP_DEFLATED,compresslevel=8) as z:
  for p in files:
   if p.relative_to(ROOT).parts[0]!='bin':z.write(p,NAME+'/'+p.relative_to(ROOT).as_posix())
 for binary in ['xrag','xrag-server']:
  if not (ROOT/'bin'/binary).exists():raise SystemExit('Run make build before packaging Linux binaries')
 with tarfile.open(out/'XRAG_V0.7_Demo9_Linux_amd64.tar.gz','w:gz',compresslevel=7) as tar:
  for p in files:
   if p.relative_to(ROOT).parts[:2] != ('bin','client'):tar.add(p,arcname=NAME+'/'+p.relative_to(ROOT).as_posix(),recursive=False)
 print(out/'XRAG_V0.7_Demo9_源码.zip');print(out/'XRAG_V0.7_Demo9_Linux_amd64.tar.gz')
if __name__=='__main__':
 p=argparse.ArgumentParser();p.add_argument('--out',type=Path,required=True);a=p.parse_args();main(a.out)
