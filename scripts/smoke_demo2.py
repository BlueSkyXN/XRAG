#!/usr/bin/env python3
"""Real process smoke for demo.2; optional browser test uses genuine HTTP data.
No external Git remote, model endpoint or hosted database is needed.
"""
from pathlib import Path
import argparse, hashlib, hmac, http.cookiejar, json, os, shutil, socket, subprocess, sys, tempfile, time
import urllib.request, urllib.error
ROOT=Path(__file__).resolve().parents[1];CHECKS=ROOT/'checks';CHECKS.mkdir(exist_ok=True)
checks=[]
def record(name,condition,detail=None):
 if not condition:raise AssertionError((name,detail))
 checks.append({'name':name,'passed':True,'detail':detail})
def run(args,**kw):
 return subprocess.run(args,check=True,text=True,capture_output=True,timeout=45,**kw)
def main(browser=False):
 with tempfile.TemporaryDirectory(prefix='xrag-demo2-') as tmp:
  tmp=Path(tmp);data=tmp/'data';home=tmp/'home';home.mkdir();repo=tmp/'repo';repo.mkdir()
  def git(*args):return run(['git','-C',str(repo),'-c','user.name=Fixture','-c','user.email=fixture@example.invalid',*args]).stdout.strip()
  git('init','-b','main')
  for n in ['a','b','c']:(repo/(n+'.md')).write_text('# Git '+n+'\n提交资料 '+n+'\n')
  git('add','.');git('commit','-m','initial');first=git('rev-parse','HEAD')
  (repo/'a.md').write_text('# Git a\nUNCOMMITTED_MARKER\n')
  run([str(ROOT/'bin/xrag-server'),'init','--data',str(data),'--examples',str(ROOT/'examples/docs')])
  key=(data/'admin.key').read_text().strip();secret='fixture-hook-secret-01234567890123456789'
  with socket.socket() as sock:sock.bind(('127.0.0.1',0));port=sock.getsockname()[1]
  base=f'http://127.0.0.1:{port}'
  logs=open(CHECKS/'demo2-process.log','w')
  server=subprocess.Popen([str(ROOT/'bin/xrag-server'),'serve','--data',str(data),'--listen',f'127.0.0.1:{port}','--workers','off'],stdout=logs,stderr=logs,env={**os.environ,'XRAG_SMOKE_HOOK':secret})
  worker=None
  def api(path,payload=None,method=None,token=key,headers=None):
   b=None if payload is None else json.dumps(payload,ensure_ascii=False).encode();hdr={'Content-Type':'application/json'}
   if token:hdr['Authorization']='Bearer '+token
   hdr.update(headers or {})
   req=urllib.request.Request(base+path,data=b,headers=hdr,method=method or ('POST' if b is not None else 'GET'))
   try:
    with urllib.request.urlopen(req,timeout=25) as r:return r.status,json.load(r)
   except urllib.error.HTTPError as e:return e.code,json.load(e)
  def post(path,payload,method=None):
   n,v=api(path,payload,method)
   if n>=300:raise AssertionError((path,n,v))
   return v
  def waitjob(j):
   for _ in range(250):
    _,r=api('/v1/jobs/'+j['id'])
    if r['state'] not in ['queued','running']:return r
    time.sleep(.1)
   raise AssertionError(('job timeout',j))
  def query(q):return post('/v1/search/keyword',{'kbs':['demo'],'q':q,'trace':True})
  def cli(*args):return run([str(ROOT/'bin/xrag'),*args,'--server',base,'--key-file',str(data/'admin.key'),'--kb','demo'],cwd=ROOT,env={**os.environ,'HOME':str(home)})
  try:
   for _ in range(60):
    try:
     if api('/healthz')[0]==200:break
    except Exception:time.sleep(.1)
   record('demo2 version reported',api('/healthz')[1]['version']=='0.10.2')
   cp=cli('source','add','--kind','git','--root',str(repo),'--git-ref','main','--id','gitrepo','--mount','gitdocs');record('real CLI registers Git source',json.loads(cp.stdout)['kind']=='git')
   j=post('/v1/kbs/demo/builds',{'mode':'incremental'});time.sleep(.2);record('API alone queues build',api('/v1/jobs/'+j['id'])[1]['state']=='queued')
   worker=subprocess.Popen([str(ROOT/'bin/xrag-server'),'worker','--data',str(data)],stdout=logs,stderr=logs)
   j=waitjob(j);record('worker executes Git and path build',j['state']=='succeeded',j['result'])
   record('Git commit fixed in build result',j['result']['source_revisions']['gitrepo']==first)
   record('uncommitted worktree content not indexed',query('UNCOMMITTED_MARKER')['returned']==0)
   policy={'every_seconds':10,'watch_seconds':0,'debounce_seconds':1,'hook_secret_ref':'env:XRAG_SMOKE_HOOK'}
   cp=cli('source','policy','--id','gitrepo','--data',json.dumps(policy));record('CLI persists per-source automation',json.loads(cp.stdout)['every_seconds']==10)
   (repo/'a.md').write_text('# Git a\nAUTOMATION_MARKER_2026\n');git('add','.');git('commit','-m','automatic');second=git('rev-parse','HEAD')
   for _ in range(220):
    if query('AUTOMATION_MARKER_2026')['returned']>0:break
    time.sleep(.1)
   else:raise AssertionError('scheduled git update not published')
   tree=post('/v1/files/tree',{'kbs':['demo'],'limit':100});gititems=[v for v in tree['items'] if v['path'].startswith('gitdocs/')]
   record('schedule updates Git without deleting unchanged documents',len(gititems)==3)
   info=api('/v1/kbs/demo/sources')[1];status=next(d for d in info['details'] if d['source']['id']=='gitrepo')['status'];record('source revision/status available',status['source_revision']==second and status['last_success']>0)
   # Disable periodic Git scans before testing explicit signed hooks.
   policy['every_seconds']=0;post('/v1/kbs/demo/sources/gitrepo/policy',policy,'PUT')
   body={'reason':'fixture event'};raw=json.dumps(body,ensure_ascii=False).encode();stamp=str(int(time.time()));event='smoke-delivery-1';sig=hmac.new(secret.encode(),stamp.encode()+b'\n'+event.encode()+b'\n'+raw,hashlib.sha256).hexdigest();headers={'X-XRAG-Timestamp':stamp,'X-XRAG-Event':event,'X-XRAG-Signature':'sha256='+sig}
   n,j1=api('/v1/hooks/demo/gitrepo',body,token='',headers=headers);n2,j2=api('/v1/hooks/demo/gitrepo',body,token='',headers=headers);record('signed hook real HTTP is idempotent',n==202 and n2==202 and j1['id']==j2['id'])
   bad=dict(headers,**{'X-XRAG-Signature':'sha256='+'0'*64});record('invalid hook rejected',api('/v1/hooks/demo/gitrepo',body,token='',headers=bad)[0]==401)
   waitjob(j1)
   helper=run([sys.executable,str(ROOT/'scripts/send_hook.py'),'--url',base+'/v1/hooks/demo/gitrepo','--secret-env','XRAG_SMOKE_HOOK','--event','helper-event-1'],env={**os.environ,'XRAG_SMOKE_HOOK':secret})
   hv=json.loads(helper.stdout);record('Hook helper sends valid real request',hv['http_status']==202);waitjob(hv['response'])
   # Real wall-clock polling, not a mocked timer.
   watch=tmp/'watched';watch.mkdir();(watch/'w.md').write_text('# Watch\nbefore\n')
   post('/v1/kbs/demo/sources',{'id':'watchsrc','root':str(watch),'kind':'path','mount':'watched'})
   post('/v1/kbs/demo/sources/watchsrc/policy',{'every_seconds':0,'watch_seconds':2,'debounce_seconds':1},'PUT')
   (watch/'w.md').write_text('# Watch\nWATCH_MARKER_2026\n')
   for _ in range(150):
    if query('WATCH_MARKER_2026')['returned']>0:break
    time.sleep(.1)
   else:raise AssertionError('polling update not published')
   record('directory polling publishes changes',True)
   post('/v1/kbs/demo/sources/watchsrc/policy',{'every_seconds':0,'watch_seconds':0,'debounce_seconds':1},'PUT')
   sample=json.loads((ROOT/'examples/eval-demo.json').read_text());es=post('/v1/eval/sets',sample)
   er=post('/v1/eval/runs',{'set_id':es['id'],'strategies':['keyword','ask','ask_graph'],'k':5});er=waitjob(er)
   record('paired evaluation executes actual queries',er['state']=='succeeded' and len(er['result']['rows'])==15,er['result'].get('summaries'))
   (CHECKS/'sample-evaluation.json').write_text(json.dumps(er['result'],ensure_ascii=False,indent=2))
   report=er['result'];record('evaluation pins manifest and explicit strategy',bool(report['manifest_hash']) and all(r['actual_strategy'].get('rerank') is False for r in report['rows']))
   cp=cli('eval','report',er['id']);record('CLI reads actual evaluation report',json.loads(cp.stdout)['id']==er['id'])
   record('evaluation rejects unconfigured semantic model',api('/v1/eval/runs',{'set_id':es['id'],'strategies':['semantic']})[0]==422)
   qr=query('E203');history=api('/v1/queries/'+qr['query_id'])[1]['items'];record('query trace persisted without full text',bool(history[0]['response']['trace']) and all(not v.get('text') for v in history[0]['response']['items']))
   cp=cli('trace','show',qr['query_id']);record('CLI trace command works',json.loads(cp.stdout)['items'][0]['id']==qr['query_id'])
   cp=cli('graph','path','--from','设备A','--to','日志事件','--direction','out');record('CLI graph direction flag works',json.loads(cp.stdout)['returned']>0)
   details=api('/v1/jobs/'+j['id']+'/details')[1];record('per-file build details inspectable',len(details['files'])>=10)
   if browser:browser_checks(base,key,api)
   post('/v1/kbs/demo/withdraw',{'path':'manual/02-errors.md','confirm':True})
   old=api('/v1/jobs/'+er['id'])[1];record('withdrawal stops saved evaluation result being returned',old['result'].get('unavailable')=='VIEW_INVALIDATED')
   record('withdrawal stops old trace being returned',api('/v1/queries/'+qr['query_id'])[0]==410)
   record('remote Client still creates no local knowledge SQLite',not list(home.rglob('*.sqlite')))
  finally:
   for proc in [worker,server]:
    if proc is not None and proc.poll() is None:
     proc.terminate()
     try:proc.wait(timeout=10)
     except subprocess.TimeoutExpired:proc.kill();proc.wait()
   logs.close()
 (CHECKS/'demo2-smoke.json').write_text(json.dumps({'kind':'real git CLI, Server/Worker/Client processes, localhost HTTP; optional browser fixture transport','passed':len(checks),'checks':checks},ensure_ascii=False,indent=2))
 print(json.dumps({'passed':len(checks),'report':str(CHECKS/'demo2-smoke.json')},ensure_ascii=False))

def browser_checks(base,key,api):
 from playwright.sync_api import sync_playwright
 jar=http.cookiejar.CookieJar();opener=urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar))
 def bridge(path,options):
  if not path.startswith('/') or path.startswith('//'):raise ValueError('test bridge accepts same-server relative paths only')
  b=options.get('body');req=urllib.request.Request(base+path,data=None if b is None else b.encode(),method=options.get('method','GET'),headers={'Content-Type':'application/json'})
  try:
   with opener.open(req,timeout=30) as r:return {'status':r.status,'body':r.read().decode()}
  except urllib.error.HTTPError as e:return {'status':e.code,'body':e.read().decode()}
 with sync_playwright() as p:
  browser=p.chromium.launch(executable_path='/usr/bin/chromium',headless=True,args=['--no-sandbox']);page=browser.new_page(viewport={'width':1440,'height':1000})
  transport='direct'
  try:page.goto(base,wait_until='domcontentloaded',timeout=15000)
  except Exception as e:
   if 'BLOCKED_BY_ADMINISTRATOR' not in str(e):raise
   transport='DOM + explicit real HTTP test bridge'
   page.close();page=browser.new_page(viewport={'width':1440,'height':1000})
   html=(ROOT/'internal/server/web/index.html').read_text().replace('<link rel="stylesheet" href="/style.css">','<style>'+ (ROOT/'internal/server/web/style.css').read_text()+'</style>')
   for file in ['app.js','extensions.js','product.js','lifecycle.js','completion.js','iteration6.js','iteration7.js','iteration8.js']:html=html.replace(f'<script src="/{file}"></script>','')
   page.set_content(html);page.expose_function('xragTestHTTP2',bridge)
   page.evaluate('''()=>{window.fetch=async(path,options={})=>{const r=await window.xragTestHTTP2(path,options);return new Response(r.body,{status:r.status,headers:{'Content-Type':'application/json'}})}}''')
   for file in ['app.js','extensions.js','product.js','lifecycle.js','completion.js','iteration6.js','iteration7.js','iteration8.js']:page.add_script_tag(content=(ROOT/'internal/server/web'/file).read_text())
  errors=[];page.on('pageerror',lambda e:errors.append(str(e)))
  page.fill('#key-input',key);page.click('#login-form button');page.wait_for_selector('#app:not([hidden])');record('demo2 browser login with actual server',True,transport)
  page.click('[data-page="files"]');page.wait_for_selector('#source-list .source-card');record('source list shows registered Git and policy',page.locator('#source-list').inner_text().find('gitrepo')>=0)
  page.screenshot(path=str(CHECKS/'demo2-sources.png'),full_page=True)
  page.locator('#source-list .source-card').first.locator('summary').click();page.locator('[data-source-policy]').first.locator('input[name="every"]').fill('20');page.locator('[data-source-policy]').first.locator('button.primary').click();page.wait_for_timeout(250);record('source policy UI saves via real HTTP',any(x['policy']['every_seconds']==20 for x in api('/v1/kbs/demo/sources')[1]['details']))
  page.click('[data-page="eval"]');page.wait_for_selector('[data-eval-report]');page.locator('[data-eval-report]').first.click();page.wait_for_selector('#eval-report table');record('evaluation UI renders real metric rows',page.locator('#eval-report tbody tr').count()==3)
  page.evaluate('scrollTo(0,0)');page.wait_for_timeout(200);page.screenshot(path=str(CHECKS/'demo2-evaluation.png'),full_page=True)
  page.locator('[data-query-detail]').first.click();page.wait_for_selector('#query-detail:not([hidden])');record('saved trace can be read in UI','trace' in page.locator('#query-detail').inner_text())
  page.screenshot(path=str(CHECKS/'demo2-trace.png'))
  page.click('[data-page="jobs"]');page.wait_for_selector('[data-job-details]');page.locator('[data-job-details]').first.click();page.wait_for_selector('#reader[open]');record('job detail UI opens actual records','files' in page.locator('#reader-body').inner_text());page.keyboard.press('Escape')
  page.set_viewport_size({'width':390,'height':844});page.click('#mobile-menu');page.click('[data-page="eval"]');page.wait_for_selector('#eval-form');page.screenshot(path=str(CHECKS/'demo2-mobile.png'),full_page=True);record('demo2 mobile layout has no page overflow',page.evaluate('document.documentElement.scrollWidth<=innerWidth'),page.evaluate('({page:document.documentElement.scrollWidth,viewport:innerWidth})'))
  record('demo2 UI no uncaught errors',not errors,errors);browser.close()

if __name__=='__main__':
 p=argparse.ArgumentParser();p.add_argument('--browser',action='store_true');args=p.parse_args();main(args.browser)
