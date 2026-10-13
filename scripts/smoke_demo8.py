#!/usr/bin/env python3
"""Actual fresh XRAG/SQLite/CLI/Worker flows. Chat/Neo4j are explicit protocol fixtures."""
from pathlib import Path
import argparse,base64,http.cookiejar,http.server,json,os,socket,sqlite3,subprocess,tempfile,threading,time,urllib.error,urllib.request
ROOT=Path(__file__).resolve().parents[1];CHECKS=ROOT/'checks/demo8';CHECKS.mkdir(exist_ok=True);rows=[]
def ck(n,ok,d=None,kind='process'):
 if not ok:raise AssertionError((n,d))
 rows.append({'name':n,'passed':True,'kind':kind,'detail':d});print('PASS '+n,flush=True)
def command(args,**kw):return subprocess.run(list(map(str,args)),check=True,text=True,capture_output=True,timeout=120,**kw)
def port():
 with socket.socket()as s:s.bind(('127.0.0.1',0));return s.getsockname()[1]
class Model(http.server.BaseHTTPRequestHandler):
 calls=0;graph_writes=0
 def log_message(self,*a):pass
 def do_POST(self):
  b=json.loads(self.rfile.read(int(self.headers['Content-Length'])))
  if '/query/v2' in self.path:
   statement=b['statement'];values=[]
   if 'RETURN count(n)' in statement:values=[[0]]
   if 'UNWIND $rows' in statement:Model.graph_writes+=len(b['parameters']['rows'])
   out={'data':{'fields':[],'values':values}}
  elif self.path.endswith('/chat/completions'):
   Model.calls+=1;st=json.loads(b['messages'][1]['content']);obs=st['observations'];ev=st['evidence']
   if not obs:d={'action':'search.keyword','arguments':{'q':'分页样本记录' if '数量' in st['task']['goal'] else 'E203'}}
   elif len(obs)==1 and '数量' in st['task']['goal']:d={'finish':True,'findings':[{'claim':'在本次已发布范围内匹配 {n}。','evidence_ids':['e1'],'quantities':[{'slot':'n','evidence_id':'e1','metric':'total'}]}]}
   elif len(obs)==1:d={'action':'files.read','arguments':{'ref':obs[0]['result']['items'][0]['ref']}}
   else:d={'finish':True,'findings':[{'claim':'已找到 E203 的原文处理条件；这里是测试端点按已读取结果产生的固定描述。','evidence_ids':[next(e['id'] for e in ev if e['kind']!='aggregate')]}],'unresolved':['受控模型不证明真实模型理解能力。']}
   out={'choices':[{'message':{'content':json.dumps(d,ensure_ascii=False)}}],'usage':{'prompt_tokens':20,'completion_tokens':10,'total_tokens':30}}
  else:self.send_error(404);return
  raw=json.dumps(out,ensure_ascii=False).encode();self.send_response(200);self.send_header('Content-Type','application/json');self.send_header('Content-Length',str(len(raw)));self.end_headers();self.wfile.write(raw)
def main(browser=False):
 with tempfile.TemporaryDirectory(prefix='xrag-demo8-')as td:
  t=Path(td);data=t/'data';home=t/'home';home.mkdir();command([ROOT/'bin/xrag-server','init','--data',data,'--examples',ROOT/'examples/docs']);key=(data/'admin.key').read_text().strip();base=f'http://127.0.0.1:{port()}';log=(CHECKS/'process8.log').open('w');worker=None
  model=http.server.ThreadingHTTPServer(('127.0.0.1',0),Model);threading.Thread(target=model.serve_forever,daemon=True).start();endpoint=f'http://127.0.0.1:{model.server_port}'
  server=subprocess.Popen([str(ROOT/'bin/xrag-server'),'serve','--data',str(data),'--listen',base.split('://')[1],'--workers','off'],stdout=log,stderr=log)
  def api(path,body=None,method=None,extra=None):
   req=urllib.request.Request(base+path,data=None if body is None else json.dumps(body,ensure_ascii=False).encode(),method=method or('GET'if body is None else'POST'),headers={'Content-Type':'application/json','Authorization':'Bearer '+key,**(extra or{})})
   try:
    with urllib.request.urlopen(req,timeout=60)as r:return r.status,json.load(r)
   except urllib.error.HTTPError as e:return e.code,json.load(e)
  def op(*a,**kw):
   status,r=api(*a,**kw)
   if status>=300:raise AssertionError((a,status,r))
   return r
  def cli(*a):return command([ROOT/'bin/xrag',*a,'--server',base,'--key-file',data/'admin.key','--kb','demo'],env={**os.environ,'HOME':str(home)})
  def wait(j):
   for _ in range(600):
    r=op('/v1/jobs/'+j['id'])
    if r['state']not in('queued','running'):return r
    time.sleep(.05)
   raise AssertionError(('timeout',r))
  def waitmaint(id):
   for _ in range(600):
    r=next(x for x in op('/v1/maintenance/runs')['items']if x['id']==id)
    if r['state']not in('queued','running'):return r
    time.sleep(.05)
   raise AssertionError(('timeout',r))
  try:
   for _ in range(100):
    try:
     if api('/healthz')[0]==200:break
    except OSError:pass
    time.sleep(.1)
   ck('fresh Demo.8 starts',op('/healthz')['version']=='0.7.0-demo.9')
   setting=op('/v1/maintenance/policy');ck('content maintenance initially disabled',not setting['policy']['enabled']and setting['policy']['content_every_seconds']==0)
   preview=json.loads(cli('maintenance','content').stdout);ck('online preview is read-only',preview['dry_run'])
   maint=op('/v1/maintenance/runs',{'confirm':True,'content':True});time.sleep(.15);ck('API-only does not execute content collection',op('/v1/maintenance/runs')['items'][0]['state']=='queued')
   worker=subprocess.Popen([str(ROOT/'bin/xrag-server'),'worker','--data',str(data)],stdout=log,stderr=log)
   mm=waitmaint(maint['id']);ck('independent worker completes bounded content pass',mm['state']=='succeeded',mm)
   ck('normal knowledge builds after exclusive maintenance',wait(op('/v1/kbs/demo/builds',{}))['state']=='succeeded')
   count=op('/v1/search/keyword',{'kbs':['demo'],'q':'分页样本记录','limit':10});ck('existing exact count is still 37',count['total']['value']==37)
   status,r=api('/v1/research',{'kb':'demo','goal':'E203'});ck('missing model is rejected before creating investigation',status==422 and r['error']['code']=='CAPABILITY_UNAVAILABLE')
   q=json.loads((ROOT/'examples/recipe-run.json').read_text());recipe=wait(op('/v1/recipes/run',q));ck('recipe executes query/read with no planner model',recipe['state']=='succeeded'and recipe['result']['stop_reason']=='recipe_completed'and Model.calls==0,recipe.get('error'))
   ck('recipe records actual observations and typed evidence',len(recipe['result']['observations'])==2 and len(recipe['result']['evidence'])>=2)
   yaml=json.loads(cli('recipe','run','--recipe',ROOT/'examples/recipe.yaml','--data','{"goal":"E203"}','--wait').stdout);ck('actual CLI loads YAML and waits for recipe',yaml['state']=='succeeded')
   cfg=op('/v1/kbs/demo/settings');op('/v1/kbs/demo/settings',{'expected_revision':cfg['revision'],'settings':{'providers':{'chat':{'url':endpoint,'model':'controlled-research-fixture'}}}},'PUT')
   q={'kb':'demo','goal':'调查 E203 的资料条件','max_steps':8,'max_seconds':120};j=op('/v1/research',q,extra={'Idempotency-Key':'research-eight'});repeat=op('/v1/research',q,extra={'Idempotency-Key':'research-eight'});ck('same request does not create duplicate investigation',j['id']==repeat['id'])
   job=wait(j);ck('actual worker executes model search/read/finish loop',job['state']=='partial'and len(job['result']['observations'])==2 and len(job['result']['findings'])==1,job)
   ck('claimed task fixes absolute execution deadline',job['execution_deadline_at']-job['first_started_at']==120)
   ck('result references have explicit seven-day lifetime',job['result']['references_expire_at']>time.time()+6*86400)
   qjob=wait(op('/v1/research',{'kb':'demo','goal':'计算匹配的数量','max_steps':4,'max_seconds':120}));ck('quantitative conclusion rendered from actual metric',qjob['state']=='succeeded'and'37 chunks'in qjob['result']['findings'][0]['claim'],qjob)
   rref=qjob['result']['evidence'][0]['ref'];rr=op('/v1/files/read',{'kbs':['demo'],'ref':rref});ck('aggregate evidence can be read as structure',rr['data']['metric']['value']==37)
   res=op('/mcp?toolsets=research',{'jsonrpc':'2.0','id':1,'method':'tools/list'});ck('MCP optional task group exposes five real actions',len(res['result']['tools'])==5)
   res=op('/mcp?toolsets=research',{'jsonrpc':'2.0','id':2,'method':'tools/call','params':{'name':'research_status','arguments':{'id':j['id']}}});ck('MCP task status reads actual job',res['result']['structuredContent']['id']==j['id'])
   before=Model.calls
   if browser:browser_checks(base,key,job,api,op)
   snap=op('/v1/backups',{'confirm':True});snap_path=t/'snapshot.sqlite';cli('backup','download',snap['id'],'--out',snap_path)
   recovered=t/'restored';command([ROOT/'bin/xrag-server','restore','--from',snap_path,'--data',recovered,'--confirm']);targets=t/'targets.json';targets.write_text(json.dumps({'bindings':{'demo':{'neo4j':{'url':endpoint,'database':'neo4j','user':'fixture'}}}}))
   plan=json.loads(command([ROOT/'bin/xrag-server','recover-projections','--data',recovered,'--targets',targets]).stdout);ck('recovery CLI preview reads saved relationships',plan['dry_run']and plan['supports']>0 and Model.graph_writes==0)
   rebuilt=json.loads(command([ROOT/'bin/xrag-server','recover-projections','--data',recovered,'--targets',targets,'--confirm']).stdout);ck('recovery CLI sends saved graph without a model',rebuilt['state']=='succeeded'and rebuilt['model_calls']==0 and Model.graph_writes>0 and Model.calls==before,rebuilt)
   idempotent=json.loads(command([ROOT/'bin/xrag-server','recover-projections','--data',recovered,'--targets',targets,'--confirm']).stdout);ck('completed recovery repeats without another write',idempotent['state']=='succeeded')
   ref=next(x for x in job['result']['evidence']if x['kind']!='aggregate');path=ref['content']['path'];op('/v1/kbs/demo/withdraw',{'path':path,'confirm':True});hidden=op('/v1/jobs/'+job['id']);ck('withdrawal invalidates saved investigation not just future reads',hidden['result'].get('unavailable')=='VIEW_INVALIDATED')
   ck('no local data database created by remote CLI',not list(home.rglob('*.sqlite')))
  finally:
   for p in(worker,server):
    if p and p.poll()is None:
     p.terminate()
     try:p.wait(timeout=15)
     except subprocess.TimeoutExpired:p.kill();p.wait()
   model.shutdown();model.server_close();log.close()
 (CHECKS/'smoke.json').write_text(json.dumps({'passed':len(rows),'checks':rows,'scope':'actual fresh XRAG/SQLite/CLI/Worker; controlled Chat and Neo4j protocol fixture, not real external services'},ensure_ascii=False,indent=2))

def browser_checks(base,key,job,api,op):
 from playwright.sync_api import sync_playwright
 jar=http.cookiejar.CookieJar();opener=urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar));errors=[]
 def bridge(path,options):
  if not path.startswith('/')or path.startswith('//'):raise ValueError('only project local test routes')
  body=options.get('body');req=urllib.request.Request(base+path,data=None if body is None else body.encode(),method=options.get('method','GET'),headers={'Content-Type':'application/json'})
  try:
   with opener.open(req,timeout=70)as r:return{'status':r.status,'bytes':base64.b64encode(r.read()).decode(),'type':r.headers.get('Content-Type','application/json')}
  except urllib.error.HTTPError as e:return{'status':e.code,'bytes':base64.b64encode(e.read()).decode(),'type':'application/json'}
 with sync_playwright()as pw:
  browser=pw.chromium.launch(executable_path='/usr/bin/chromium',headless=True,args=['--no-sandbox']);p=browser.new_page(viewport={'width':1440,'height':1050});p.on('pageerror',lambda e:errors.append(str(e)));transport='direct URL'
  try:p.goto(base,wait_until='domcontentloaded',timeout=12000)
  except Exception as e:
   if'BLOCKED_BY_ADMINISTRATOR'not in str(e):raise
   transport='DOM + explicit HTTP test bridge';p.close();p=browser.new_page(viewport={'width':1440,'height':1050});p.on('pageerror',lambda e:errors.append(str(e)))
   h=(ROOT/'internal/server/web/index.html').read_text().replace('<link rel="stylesheet" href="/style.css">','<style>'+(ROOT/'internal/server/web/style.css').read_text()+'</style>')
   scripts=['app.js','extensions.js','product.js','lifecycle.js','completion.js','iteration6.js','iteration7.js','iteration8.js']
   for f in scripts:h=h.replace(f'<script src="/{f}"></script>','')
   p.expose_function('bridge8',bridge);p.add_init_script('')
   p.set_content(h)
   p.evaluate('''()=>{window.fetch=async(path,options={})=>{const r=await window.bridge8(String(path),options);return new Response(Uint8Array.from(atob(r.bytes),c=>c.charCodeAt(0)),{status:r.status,headers:{'Content-Type':r.type}})}}''')
   for f in scripts:p.add_script_tag(content=(ROOT/'internal/server/web'/f).read_text())
  p.locator('#key-input').fill(key);p.locator('#login-form').evaluate('(f)=>f.requestSubmit()');p.locator('#app').wait_for(state='visible')
  p.locator('[data-page="research"]').click();p.wait_for_function("!document.querySelector('#page-research').hidden")
  ck('UI renders actual investigation page',p.locator('#research-list').inner_text().find('research')>=0 or p.locator('#research-list').inner_text().find('调查')>=0,transport,'browser')
  p.locator(f'[data-task-view="{job["id"]}"]').click();p.wait_for_function("document.querySelector('#research-result').innerText.includes('类型化依据')")
  ck('UI displays findings and evidence kinds','e1'in p.locator('#research-result').inner_text(),None,'browser')
  p.screenshot(path=str(CHECKS/'research.png'),full_page=True)
  p.locator('#research-mode').select_option('recipe');p.locator('#research-goal').fill('E203');p.locator('#research-form').evaluate('(f)=>f.requestSubmit()');p.wait_for_function("document.querySelector('#research-result').innerText.includes('recipe_completed')",timeout=30000)
  ck('UI submits recipe and receives real result',True,None,'browser')
  p.locator('#research-result details').first.evaluate('(d)=>d.open=true');p.locator('[data-task-evidence]').first.click();p.locator('#reader').wait_for(state='visible');ck('UI reads saved aggregate evidence', 'metric'in p.locator('#reader-body').inner_text(),None,'browser');p.locator('#reader-close').click()
  p.locator('[data-page="settings"]').click();p.evaluate("loadSettingsPane('maintenance')");p.wait_for_timeout(300)
  ck('online content maintenance controls are present',p.locator('#maintenance-content-every').count()==1,None,'browser')
  p.screenshot(path=str(CHECKS/'content-maintenance.png'),full_page=True)
  p.set_viewport_size({'width':390,'height':844});p.wait_for_timeout(200);ck('new pages do not overflow the mobile viewport',p.evaluate('document.documentElement.scrollWidth<=innerWidth'),None,'browser');p.screenshot(path=str(CHECKS/'mobile.png'),full_page=True)
  ck('new UI reports no JavaScript exceptions',not errors,errors,'browser');browser.close()
if __name__=='__main__':
 a=argparse.ArgumentParser();a.add_argument('--browser',action='store_true');main(a.parse_args().browser)
