#!/usr/bin/env python3
"""Cross-build the pure-Go read/client program. Not a claim of native platform testing."""
from pathlib import Path
import argparse,hashlib,json,os,re,subprocess,time,zipfile
ROOT=Path(__file__).resolve().parents[1]
PLATFORMS=[('darwin','arm64'),('darwin','amd64'),('windows','amd64'),('linux','arm64')]
def main(out):
 version=re.search(r'const Version = "([^"]+)"',(ROOT/'core/types.go').read_text()).group(1);out.mkdir(parents=True,exist_ok=True);records=[]
 for system,arch in PLATFORMS:
  name='xrag.exe'if system=='windows'else'xrag';stem=f'xrag-client-{version}-{system}-{arch}';binary=ROOT/'bin/client'/f'{name}-{system}-{arch}';binary.parent.mkdir(parents=True,exist_ok=True)
  t=time.time();p=subprocess.run(['go','build','-trimpath','-o',str(binary),'./cmd/xrag'],cwd=ROOT,env={**os.environ,'GOOS':system,'GOARCH':arch,'CGO_ENABLED':'0'},capture_output=True,text=True,timeout=180)
  rec={'os':system,'arch':arch,'build_exit':p.returncode,'seconds':round(time.time()-t,2),'native_execution':'not_run'}
  if p.returncode:rec['error']=p.stderr;records.append(rec);break
  readme=f'''# XRAG {version}：仅 Client，{system}/{arch}\n\n本包不包含 Server，不需要 C 编译器和 SQLite 开发库。交叉编译通过，未在此操作系统实机运行。\n\n连接已有 XRAG 服务：\n\n```sh\n./{name} profile add team --server http://YOUR_SERVER:8765 --key-file /YOUR/KEY/FILE --kb demo\n./{name} search keyword 日志 --profile team\n./{name} mcp --profile team --toolsets core,graph,files,utility,research\n./{name} jobs watch JOB_ID --profile team\n```\n\n本地只读 MD/TXT：`./{name} search keyword 日志 --local ./docs --kb demo`。\n该模式不启动 Server、不创建持久向量/图索引。Research/Recipe 持久任务在 Server 执行。\nMac 需允许执行该下载程序；未签名/公证。不修改用户机器系统设置。\nWindows 在 PowerShell 用 `./xrag.exe` 及本机路径。Server 的 Windows 原生移植不在本包中。\n本项目按 GPL-3.0 发布，见 LICENSE 和 LICENSE_NOTICE.md。\n'''
  dest=out/f'{stem}.zip'
  with zipfile.ZipFile(dest,'w',zipfile.ZIP_DEFLATED,compresslevel=8)as z:
   z.write(binary,stem+'/'+name);z.writestr(stem+'/README.md',readme);z.write(ROOT/'LICENSE_NOTICE.md',stem+'/LICENSE_NOTICE.md');z.write(ROOT/'LICENSE',stem+'/LICENSE')
  rec.update({'artifact':dest.name,'binary_sha256':hashlib.sha256(binary.read_bytes()).hexdigest(),'artifact_sha256':hashlib.sha256(dest.read_bytes()).hexdigest(),'bytes':dest.stat().st_size});records.append(rec)
 report={'scope':'pure Go Client cross-compilation and zip creation only; no native target runtime','platforms':records}
 (ROOT/'checks/demo9').mkdir(parents=True,exist_ok=True)
 (ROOT/'checks/demo9/client-builds.json').write_text(json.dumps(report,ensure_ascii=False,indent=2));print(json.dumps(report,ensure_ascii=False))
 if any(r['build_exit']for r in records):raise SystemExit(1)
if __name__=='__main__':
 p=argparse.ArgumentParser();p.add_argument('--out',type=Path,required=True);main(p.parse_args().out)
