#!/usr/bin/env python3
"""Fresh real XRAG processes; explicit auth/Chat fixtures and SQLite write-fault injection.
No external identity provider, commercial planner or third-party MCP client is claimed.
"""
from pathlib import Path
import argparse,base64,http.cookiejar,http.server,json,os,socket,sqlite3,subprocess,tempfile,threading,time,urllib.error,urllib.request
ROOT=Path(__file__).resolve().parents[1];CHECKS=ROOT/'checks/demo9';CHECKS.mkdir(exist_ok=True);rows=[]
def ck(name,ok,detail=None,kind='process'):
 if not ok:raise AssertionError((name,detail))
 rows.append({'name':name,'passed':True,'kind':kind,'detail':detail});print('PASS '+name,flush=True)
def port():
 with socket.socket()as s:s.bind(('127.0.0.1',0));return s.getsockname()[1]
def command(args,**kw):return subprocess.run(list(map(str,args)),check=True,text=True,capture_output=True,timeout=120,**kw)
class Identity(http.server.BaseHTTPRequestHandler):
 resource='';issuer='';calls=0
 def log_message(self,*a):pass
 def do_POST(self):
  from urllib.parse import parse_qs
  Identity.calls+=1
  form=parse_qs(self.rfile.read(int(self.headers['Content-Length'])).decode());token=form.get('token',[''])[0]
  if self.headers.get('Authorization')!='Basic '+base64.b64encode(b'fixture-client:fixture-secret').decode():self.send_error(401);return
  claims={'active':token in('fixture-reader','fixture-full'),'sub':'fixture-user','iss':self.issuer,'aud':self.resource,'exp':int(time.time())+600,'scope':'xrag:read'+(' xrag:research xrag:write'if token=='fixture-full'else''),'token_type':'Bearer'}
  raw=json.dumps(claims).encode();self.send_response(200);self.send_header('Content-Type','application/json');self.end_headers();self.wfile.write(raw)
def main(browser=False):
 with tempfile.TemporaryDirectory(prefix='xrag-demo9-')as temp:
  t=Path(temp);data=t/'data';home=t/'home';home.mkdir();base=f'http://127.0.0.1:{port()}';log=(CHECKS/'process9.log').open('w');worker=None
  identity=http.server.ThreadingHTTPServer(('127.0.0.1',0),Identity);Identity.resource=base+'/mcp';Identity.issuer=f'http://127.0.0.1:{identity.server_port}';threading.Thread(target=identity.serve_forever,daemon=True).start()
  auth=t/'mcp-auth.json';auth.write_text(json.dumps({'resource':Identity.resource,'issuer':Identity.issuer,'introspection_endpoint':Identity.issuer+'/introspect','client_id':'fixture-client','client_secret_ref':'env:XRAG_INTROSPECTION_SECRET','allow_loopback_http':True,'subjects':{'fixture-user':{'name':'Fixture reader','role':'reader','kbs':['demo']}}}))
  command([ROOT/'bin/xrag-server','init','--data',data,'--examples',ROOT/'examples/docs']);key=(data/'admin.key').read_text().strip()
  env={**os.environ,'XRAG_INTROSPECTION_SECRET':'fixture-secret','HOME':str(home)}
  server=subprocess.Popen([str(ROOT/'bin/xrag-server'),'serve','--data',str(data),'--listen',base.split('://')[1],'--workers','off','--mcp-auth',str(auth)],stdout=log,stderr=log,env=env)
  def api(path,body=None,method=None,token=None,accept='application/json',binary=False):
   req=urllib.request.Request(base+path,data=None if body is None else json.dumps(body,ensure_ascii=False).encode(),method=method or('GET'if body is None else'POST'),headers={'Content-Type':'application/json','Authorization':'Bearer '+(key if token is None else token),'Accept':accept})
   try:
    with urllib.request.urlopen(req,timeout=40)as r:
     b=r.read();return r.status,r.headers,b if binary else(json.loads(b)if b else None)
   except urllib.error.HTTPError as e:
    b=e.read();return e.code,e.headers,b if binary else(json.loads(b)if b else None)
  def op(*a,**kw):
   status,_,r=api(*a,**kw)
   if status>=300:raise AssertionError((a,status,r))
   return r
  def cli(*a):return command([ROOT/'bin/xrag',*a,'--server',base,'--key-file',data/'admin.key','--kb','demo'],env=env)
  def wait(j):
   for _ in range(600):
    j=op('/v1/jobs/'+j['id'])
    if j['state']not in('queued','running'):return j
    time.sleep(.05)
   raise AssertionError(('timeout',j))
  def rpc(method,params=None,id=1):return{'jsonrpc':'2.0','id':id,'method':method,'params':params or{}}
  def fail_dispatch(enable):
   # Fault injection only in the disposable test database. No product test switch.
   with sqlite3.connect(data/'xrag.sqlite',timeout=10)as con:
    if enable:con.execute("CREATE TRIGGER demo9_dispatch_fault BEFORE UPDATE ON research_checkpoint WHEN json_extract(NEW.data,'$.phase')='executing' BEGIN SELECT RAISE(ABORT,'fixture transient checkpoint write failure'); END")
    else:con.execute('DROP TRIGGER demo9_dispatch_fault')
  try:
   for _ in range(100):
    try:
     if api('/healthz')[0]==200:break
    except OSError:pass
    time.sleep(.1)
   ck('fresh Demo.9 starts',op('/healthz')['version']=='0.10.2')
   ck('runtime advertises actual MCP features',op('/v1/system')['mcp']['oauth_resource_server']and op('/v1/system')['investigation']['resume']=='explicit_checkpoint')
   worker=subprocess.Popen([str(ROOT/'bin/xrag-server'),'worker','--data',str(data)],stdout=log,stderr=log,env=env)
   ck('real worker builds baseline corpus',wait(op('/v1/kbs/demo/builds',{}))['state']=='succeeded')
   exact=op('/v1/search/keyword',{'kbs':['demo'],'q':'分页样本记录'});ck('existing exact count remains 37',exact['total']['value']==37)
   res=op('/mcp',rpc('initialize',{'protocolVersion':'2025-11-25'}));ck('MCP negotiates documented protocol',res['result']['protocolVersion']=='2025-11-25')
   ck('MCP advertises resources and prompts not Tasks','resources'in res['result']['capabilities']and'prompts'in res['result']['capabilities']and'tasks'not in res['result']['capabilities'])
   probe=json.loads(command(['python3',ROOT/'scripts/mcp_probe.py','--url',base+'/mcp','--key-file',data/'admin.key','--report',CHECKS/'mcp-probe.json']).stdout);ck('shipped protocol probe passes against actual server',probe['status']=='passed'and len(probe['checks'])==5)
   status,headers,body=api('/mcp',rpc('tools/list'),accept='application/json, text/event-stream',binary=True);ck('real HTTP sends SSE final response',status==200 and headers.get('Content-Type','').startswith('text/event-stream')and b'event: message\ndata: {'in body)
   status,_,body=api('/mcp',{'jsonrpc':'2.0','method':'notifications/initialized'},binary=True);ck('notification returns 202 and no body',status==202 and not body)
   ck('resource guide is actual embedded usage text','Search returns candidates'in json.dumps(op('/mcp',rpc('resources/read',{'uri':'xrag://guide/retrieval'}))))
   rawref=exact['items'][0]['ref'];rr=op('/mcp',rpc('resources/read',{'uri':'xrag://refs/'+rawref}));ck('MCP resource reads actual stored reference','分页样本记录'in json.dumps(rr,ensure_ascii=False))
   prompt=op('/mcp',rpc('prompts/get',{'name':'xrag_retrieve','arguments':{'question':'日志怎么处理','kb':'demo'}}));ck('retrieval prompt contains bounded user task','日志怎么处理'in json.dumps(prompt,ensure_ascii=False))
   meta=op('/.well-known/oauth-protected-resource/mcp',token='');ck('resource metadata is discoverable without a token',meta['resource']==base+'/mcp')
   status,head,_=api('/mcp',rpc('tools/list'),token='');ck('unauthenticated response includes resource metadata challenge',status==401 and'resource_metadata='in head.get('WWW-Authenticate',''))
   res=op('/mcp',rpc('tools/call',{'name':'search_keyword','arguments':{'kbs':['demo'],'q':'E203'}}),token='fixture-reader');ck('opaque token introspection reaches actual retrieval',res['result']['structuredContent']['returned']>0 and Identity.calls>0)
   status,head,_=api('/mcp?toolsets=research',rpc('tools/call',{'name':'recipe_run','arguments':{'kb':'demo','goal':'E203'}}),token='fixture-reader');ck('missing task scope returns actionable 403',status==403 and'xrag:research'in head.get('WWW-Authenticate',''))
   res=op('/mcp?toolsets=write',rpc('tools/list'),token='fixture-full');ck('token write scope cannot exceed mapped reader role',res['result']['tools']==[])
   recipe=json.loads((ROOT/'examples/recipe-run.json').read_text());fail_dispatch(True)
   j=wait(op('/v1/recipes/run',recipe));ck('temporary checkpoint write fault leaves a failed real job',j['state']=='failed',j.get('error'))
   ck('durable planned decision remains visible after failure',j['result']['checkpoint']['phase']=='planned')
   fail_dispatch(False);before=(j['id'],j['first_started_at'],j['execution_deadline_at'])
   resumed=json.loads(cli('recipe','resume',j['id'],'--wait').stdout);ck('CLI resumes real job after fault is removed',resumed['state']=='succeeded'and resumed['id']==before[0])
   ck('resume preserves original start and deadline',resumed['first_started_at']==before[1]and resumed['execution_deadline_at']==before[2])
   ck('resume records second claim and completes both observations',resumed['attempt']==2 and len(resumed['result']['observations'])==2)
   events=cli('jobs','watch',j['id']).stdout.splitlines();parsed=[json.loads(x)for x in events];ck('CLI watch reads actual SSE snapshots and done',parsed[0]['event']=='task'and parsed[-1]['event']=='done'and parsed[0]['data']['job']['id']==j['id'])
   res=op('/mcp?toolsets=research',rpc('tools/list'));ck('task group includes explicit resume tool',len(res['result']['tools'])==5 and any(x['name']=='research_resume'for x in res['result']['tools']))
   # Leave a genuinely interrupted task for the UI to resume against the real API.
   fail_dispatch(True);ui_job=wait(op('/v1/recipes/run',recipe));fail_dispatch(False)
   ck('second failed task has a usable checkpoint',ui_job['state']=='failed'and ui_job['result']['checkpoint']['phase']=='planned')
   if browser:browser_checks(base,key,ui_job,api,op)
   # Stdio now exposes the same guide/prompt capability without an HTTP MCP loop.
   lines=[rpc('initialize',{'protocolVersion':'2025-11-25'}),rpc('resources/list',id=2),rpc('prompts/list',id=3)]
   out=command([ROOT/'bin/xrag','mcp','--server',base,'--key-file',data/'admin.key','--kb','demo'],env=env,input=''.join(json.dumps(x)+'\n'for x in lines)).stdout
   stdio=[json.loads(x)for x in out.splitlines()];ck('actual CLI stdio serves resources and prompts',len(stdio)==3 and'resources'in stdio[1]['result']and'prompts'in stdio[2]['result'])
   ck('new remote operations do not initialize client database',not list(home.rglob('*.sqlite')))
  finally:
   for p in(worker,server):
    if p and p.poll()is None:
     p.terminate()
     try:p.wait(timeout=15)
     except subprocess.TimeoutExpired:p.kill();p.wait()
   identity.shutdown();identity.server_close();log.close()
 (CHECKS/'smoke.json').write_text(json.dumps({'passed':len(rows),'checks':rows,'scope':'actual fresh XRAG processes/SQLite/CLI; local OAuth introspection fixture; explicit transient DB-write fault, not real provider or commercial client'},ensure_ascii=False,indent=2))

def browser_checks(base,key,job,api,op):
 """Workbench checks against the real server; see scripts/workbench_browser.py for the transport."""
 import sys;sys.dont_write_bytecode=True;sys.path.insert(0,str(ROOT/'scripts'))
 from workbench_browser import open_workbench,login_with_key,go,logout
 from playwright.sync_api import sync_playwright
 with sync_playwright()as pw:
  browser,p,origin,transport,errors=open_workbench(pw,base)
  login_with_key(p,key);go(p,origin,'/research')
  p.locator(f'[data-job-id="{job["id"]}"]').click()
  detail=p.get_by_role('region',name='过程与依据')
  detail.get_by_role('button',name='继续任务',exact=True).wait_for(state='visible',timeout=15000)
  ck('UI shows saved planned checkpoint of actual failed task','planned'in detail.inner_text(),transport,'browser')
  p.screenshot(path=str(CHECKS/'checkpoint.png'),full_page=True)
  detail.get_by_role('button',name='继续任务',exact=True).click();detail.get_by_text('recipe_completed').first.wait_for(timeout=20000)
  ck('UI resumes the actual saved job and displays completed evidence','类型化依据'in detail.inner_text(),None,'browser')
  ck('API confirms UI did not submit a replacement task',op('/v1/jobs/'+job['id'])['attempt']==2,None,'browser')
  detail.get_by_role('button',name='实时查看').click();detail.get_by_text('任务已结束').first.wait_for(timeout=10000)
  ck('UI receives actual terminal SSE body through selected transport','任务已结束'in detail.inner_text(),transport,'browser')
  p.screenshot(path=str(CHECKS/'task-events.png'),full_page=True)
  ck('viewing the stream does not request task cancellation',not op('/v1/jobs/'+job['id'])['cancel_requested'],None,'browser')
  go(p,origin,'/access');mcp=p.get_by_role('region',name='MCP');mcp.get_by_text('外部 OAuth').wait_for(timeout=10000)
  ck('MCP help matches new transport and external auth role','SSE'in mcp.inner_text()and'OAuth'in mcp.inner_text(),None,'browser')
  p.screenshot(path=str(CHECKS/'mcp.png'),full_page=True)
  p.set_viewport_size({'width':390,'height':844});go(p,origin,'/research');p.wait_for_timeout(400)
  ck('current pages avoid full-page mobile overflow',p.evaluate('document.documentElement.scrollWidth<=innerWidth'),None,'browser')
  p.screenshot(path=str(CHECKS/'mobile.png'),full_page=True)
  p.set_viewport_size({'width':1440,'height':1050});logout(p)
  ck('logout clears saved investigation from DOM',job['id']not in p.content(),None,'browser')
  ck('new UI has no JavaScript exceptions',not errors,errors,'browser');browser.close()
if __name__=='__main__':
 p=argparse.ArgumentParser();p.add_argument('--browser',action='store_true');main(p.parse_args().browser)
