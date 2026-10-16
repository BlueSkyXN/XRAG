#!/usr/bin/env python3
"""Re-extract both release artifacts, compile/test source and run packaged Linux binaries.
All runtime data lives in TemporaryDirectory; no user data or external services.
"""
from pathlib import Path
import argparse,hashlib,io,json,os,socket,subprocess,tarfile,tempfile,time,urllib.request,zipfile
ROOT=Path(__file__).resolve().parents[1]
def main(src,lin):
 rows=[]
 def ck(n,v):
  if not v:raise AssertionError(n)
  rows.append({'name':n,'passed':True})
 def run(c,cwd=None):return subprocess.run(c,cwd=cwd,check=True,text=True,capture_output=True,timeout=120).stdout
 with tempfile.TemporaryDirectory(prefix='xrag-release9-') as t:
  t=Path(t);s=t/'source';s.mkdir()
  with zipfile.ZipFile(src) as z:
   ck('source archive omits binary and runtime database',not any('/bin/' in n or n.endswith(('.sqlite','.key')) for n in z.namelist()))
   z.extractall(s)
  source=next(p for p in s.iterdir() if p.is_dir());run(['make','build'],source);ck('freshly unpacked source builds',True)
  raw=run(['go','test','-count=1','-json','./...'],source)
  passed=[d for l in raw.splitlines() if l.startswith('{') for d in [json.loads(l)] if d.get('Action')=='pass' and d.get('Test') and '/' not in d['Test']]
  ck('unpacked source passes the current top-level tests',len(passed)==json.loads((ROOT/'checks/demo9/test-summary.json').read_text())['passed'])
  b=t/'linux';b.mkdir()
  with tarfile.open(lin) as f:f.extractall(b,filter='data')
  root=next(p for p in b.iterdir() if p.is_dir());ck('packaged binary reports demo.9',run([str(root/'bin/xrag-server'),'version']).strip()=='0.10.0')
  with socket.socket() as sock:sock.bind(('127.0.0.1',0));port=sock.getsockname()[1]
  data=t/'data';base=f'http://127.0.0.1:{port}';env=dict(os.environ,XRAG_DEMO_DATA=str(data),XRAG_DEMO_LISTEN=f'127.0.0.1:{port}');env.pop('XRAG_DEMO_CONFIG',None)
  proc=subprocess.Popen(['bash','run-linux.sh'],cwd=root,env=env,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
  try:
   for _ in range(100):
    try:urllib.request.urlopen(base+'/healthz',timeout=1).close();break
    except Exception:time.sleep(.1)
   else:raise AssertionError('packaged startup failed')
   ck('packaged run-linux starts real service',True);key=(data/'admin.key').read_text().strip()
   def api(path,body=None,binary=False):
    req=urllib.request.Request(base+path,data=None if body is None else json.dumps(body).encode(),headers={'Authorization':'Bearer '+key,'Content-Type':'application/json'})
    with urllib.request.urlopen(req,timeout=30) as r:return r.read() if binary else json.load(r)
   for _ in range(100):
    result=api('/v1/search/keyword',{'kbs':['demo'],'q':'分页样本记录'})
    if result['total']['value']==37:break
    time.sleep(.1)
   ck('packaged script builds and searches corpus',result['total']['value']==37)
   ck('packaged overview returns stored counts',api('/v1/overview')['counts']['files']==7)
   ck('packaged search uses indexed page hydration',result.get('strategy',{}).get('execution')=='indexed_metadata_then_page_hydration')
   ck('packaged maintenance defaults off',api('/v1/maintenance/policy')['policy']['enabled'] is False)
   ck('packaged feedback drafts start empty',api('/v1/eval/drafts?kb=demo')['items']==[])
   ck('packaged settings start without a model',not api('/v1/settings/providers')['restart_required'] and not api('/v1/system')['wiki'])
   ck('packaged per-KB settings API is available',api('/v1/kbs/demo/settings')['revision']==0)
   ck('packaged query registry includes external retrieval',any(x['action']=='search.external' for x in api('/v1/schema')['actions']))
   ck('packaged usage report starts with zero model calls',api('/v1/kbs/demo/usage')['totals']==[])
   ck('packaged key listing is available',len(api('/v1/keys')['items'])==1)
   ck('packaged writeback API is available with no automatic target',api('/v1/kbs/demo/sync-targets')['items']==[])
   ck('packaged content maintenance defaults off',api('/v1/maintenance/policy')['policy']['content_every_seconds']==0)
   recipe=json.loads((root/'examples/recipe-run.json').read_text())
   job=api('/v1/recipes/run',recipe)
   for _ in range(200):
    job=api('/v1/jobs/'+job['id'])
    if job['state'] not in ('queued','running'):break
    time.sleep(.05)
   ck('packaged recipe performs real retrieval without planner model',job['state']=='succeeded' and job['result']['stop_reason']=='recipe_completed')
   ck('packaged recipe produces document and aggregate evidence',len(job['result']['evidence'])>=2)
   ck('packaged research task tool group is opt-in and usable',len(api('/mcp?toolsets=research',{'jsonrpc':'2.0','id':1,'method':'tools/list'})['result']['tools'])==5)
   ck('packaged recipe persists completed checkpoint',job['result']['checkpoint']['phase']=='complete')
   info=api('/mcp',{'jsonrpc':'2.0','id':1,'method':'initialize','params':{'protocolVersion':'2025-11-25'}})['result']
   ck('packaged MCP advertises actual resources and prompts',info['protocolVersion']=='2025-11-25' and 'resources' in info['capabilities'] and 'prompts' in info['capabilities'])
   req=urllib.request.Request(base+'/mcp',data=json.dumps({'jsonrpc':'2.0','id':2,'method':'tools/list'}).encode(),headers={'Authorization':'Bearer '+key,'Content-Type':'application/json','Accept':'application/json, text/event-stream'})
   with urllib.request.urlopen(req,timeout=30) as response:
    stream=response.read();ck('packaged MCP sends SSE over real HTTP',response.headers.get('Content-Type','').startswith('text/event-stream') and b'event: message' in stream)
   watched=run([str(root/'bin/xrag'),'jobs','watch',job['id'],'--server',base,'--key-file',str(data/'admin.key'),'--kb','demo'])
   ck('packaged Client watches task SSE',json.loads(watched.splitlines()[-1])['event']=='done')
   ck('packaged OAuth remains opt-in',api('/v1/system')['mcp']['oauth_resource_server'] is False)
   export=api('/v1/kbs/demo/export',binary=True)
   with zipfile.ZipFile(io.BytesIO(export)) as z:ck('packaged export is actual ZIP',len(json.loads(z.read('manifest.json'))['items'])==7)
   back=api('/v1/backups',{'confirm':True});snap=api('/v1/backups/'+back['id']+'/download',binary=True);ck('packaged snapshot bytes match SHA-256',hashlib.sha256(snap).hexdigest()==back['sha256'])
   restore=t/'restored';file=t/'snapshot.sqlite';file.write_bytes(snap);v=json.loads(run([str(root/'bin/xrag-server'),'restore','--from',str(file),'--data',str(restore),'--confirm']));ck('packaged restore creates new directory and identity',v['old_keys_disabled'] and (restore/'admin.key').exists())
  finally:
   proc.terminate()
   try:proc.wait(timeout=15)
   except subprocess.TimeoutExpired:proc.kill();proc.wait()
 report={'passed':len(rows),'scope':'fresh source Go tests; packaged Linux startup/export/backup/restore; no browser or external models','checks':rows}
 (ROOT/'checks/package-check.json').write_text(json.dumps(report,ensure_ascii=False,indent=2));print(json.dumps(report,ensure_ascii=False))
if __name__=='__main__':
 p=argparse.ArgumentParser();p.add_argument('--source',required=True,type=Path);p.add_argument('--linux',required=True,type=Path);a=p.parse_args();main(a.source,a.linux)
