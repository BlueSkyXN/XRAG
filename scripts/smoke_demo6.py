#!/usr/bin/env python3
"""Fresh Demo.6 processes, no upstream model responses. Optional real-page/bridge UI checks."""
from pathlib import Path
import argparse,base64,http.cookiejar,json,os,socket,subprocess,tempfile,time,urllib.request,urllib.error
ROOT=Path(__file__).resolve().parents[1];CHECKS=ROOT/'checks/demo6';CHECKS.mkdir(exist_ok=True);checks=[]
def ck(name,ok,detail=None,kind='process'):
 if not ok:raise AssertionError((name,detail))
 checks.append({'name':name,'passed':True,'kind':kind,'detail':detail});print('PASS '+name,flush=True)
def cmd(args,**kw):return subprocess.run([str(x) for x in args],check=True,text=True,capture_output=True,timeout=120,**kw)
def port():
 with socket.socket() as s:s.bind(('127.0.0.1',0));return s.getsockname()[1]
def main(browser=False):
 with tempfile.TemporaryDirectory(prefix='xrag-demo6-') as td:
  t=Path(td);data=t/'data';home=t/'home';home.mkdir();n=port();base=f'http://127.0.0.1:{n}'
  cmd([ROOT/'bin/xrag-server','init','--data',data,'--examples',ROOT/'examples/docs']);key=(data/'admin.key').read_text().strip();log=(CHECKS/'process.log').open('w')
  server=subprocess.Popen([str(ROOT/'bin/xrag-server'),'serve','--data',str(data),'--listen',f'127.0.0.1:{n}','--workers','off'],stdout=log,stderr=log);worker=None
  def api(path,body=None,method=None,token=key):
   request=urllib.request.Request(base+path,data=None if body is None else json.dumps(body,ensure_ascii=False).encode(),method=method or('GET' if body is None else 'POST'),headers={'Authorization':'Bearer '+token,'Content-Type':'application/json'})
   try:
    with urllib.request.urlopen(request,timeout=40)as r:return r.status,json.load(r)
   except urllib.error.HTTPError as e:return e.code,json.load(e)
  def op(path,body=None,method=None):
   status,r=api(path,body,method)
   if status>=300:raise AssertionError((path,status,r))
   return r
  def cli(*args):return cmd([ROOT/'bin/xrag',*args,'--server',base,'--key-file',data/'admin.key','--kb','demo'],env={**os.environ,'HOME':str(home)})
  def wait(path,terminal=lambda j:j['state']not in('queued','running')):
   for _ in range(400):
    j=op(path)
    if terminal(j):return j
    time.sleep(.05)
   raise AssertionError(('timeout',path,j))
  try:
   for _ in range(80):
    try:
     if api('/healthz')[0]==200:break
    except OSError:pass
    time.sleep(.1)
   ck('fresh installed version is demo.6',op('/healthz')['version']=='0.7.0-demo.9')
   pol=op('/v1/maintenance/policy');ck('maintenance defaults disabled',not pol['policy']['enabled'])
   ck('maintenance requires admin',api('/v1/maintenance/policy',token='invalid')[0]==401)
   req=op('/v1/maintenance/runs',{'confirm':True,'backup':True});time.sleep(.25);runs=op('/v1/maintenance/runs')['items'];ck('API-only queues maintenance without execution',runs[0]['state']=='queued'and op('/v1/backups')['items']==[])
   worker=subprocess.Popen([str(ROOT/'bin/xrag-server'),'worker','--data',str(data)],stdout=log,stderr=log)
   for _ in range(200):
    run=op('/v1/maintenance/runs')['items'][0]
    if run['state']not in('queued','running'):break
    time.sleep(.05)
   ck('independent worker executes actual SQLite backup',run['state']=='succeeded'and len(op('/v1/backups')['items'])==1,run.get('error'))
   j=op('/v1/kbs/demo/builds',{});ck('existing corpus builds',wait('/v1/jobs/'+j['id'])['state']=='succeeded')
   r=op('/v1/search/keyword',{'kbs':['demo'],'q':'分页样本记录','limit':10,'trace':True});ck('indexed query reports exact count and page',r['total']['value']==37 and r['returned']==10 and r['strategy']['execution']=='indexed_metadata_then_page_hydration')
   ref=r['items'][0]['ref'];diagnose=op('/v1/queries/'+r['query_id']+'/diagnose',{'kb':'demo','path':r['items'][0]['path'],'anchor':'分页样本记录'});ck('diagnosis reads saved execution without model or rerun',diagnose['model_calls']==0 and not diagnose['reran_query'] and diagnose['returned_on_recorded_page'])
   sizes=[10];seen=[x['chunk_id']for x in r['items']];p=r
   while p.get('next_cursor'):
    p=op('/v1/results/page',{'kbs':['demo'],'result_id':p['result_id'],'cursor':p['next_cursor'],'limit':10});sizes.append(p['returned']);seen.extend(x['chunk_id']for x in p['items'])
   ck('lazy pages are stable and complete',sizes==[10,10,10,7]and len(set(seen))==37,sizes)
   op('/v1/feedback',{'kb':'demo','ref':ref,'label':'irrelevant','note':'用来整理问题的人工反馈'})
   fid=op('/v1/feedback?kb=demo&limit=20')['items'][0]['id'];d=json.loads(cli('feedback','test-case',fid,'--data',json.dumps({'question':'分页样本记录'},ensure_ascii=False)).stdout)
   ck('CLI creates feedback draft without declaring empty truth',d['status']=='draft'and not d['case']['expected_empty']and not d['case']['expected'])
   case={'id':d['case']['id'],'question':'分页样本记录','expected':[{'path':r['items'][0]['path'],'anchor':'分页样本记录'}],'expected_empty':False}
   status,_=api('/v1/eval/sets/from-drafts',{'kb':'demo','name':'not ready','family':'r6','purpose':'development','draft_ids':[d['id']]});ck('unapproved feedback cannot become runnable dataset',status==409)
   d=op('/v1/eval/drafts/'+d['id']+'/approve',{'expected_revision':0,'case':case,'note':'人工确认目标'})
   ck('explicit approval stores expected target',d['status']=='approved'and d['revision']==1)
   s=json.loads(cli('eval','from-drafts','--data',json.dumps({'name':'Demo6 regression','family':'r6','purpose':'development','draft_ids':[d['id']]})).stdout)
   ck('CLI publishes immutable dataset version',s['purpose']=='development'and len(s['cases'])==1)
   exported=t/'testset.json';cli('eval','export',s['id'],'--out',str(exported));ck('dataset export contains actual approved cases',json.loads(exported.read_text())['cases']==s['cases'])
   overlap={'kb':'demo','name':'held','family':'r6','purpose':'holdout','cases':s['cases']};status,err=api('/v1/eval/sets',overlap);ck('development/holdout duplicated question rejected',status==409 and err['error']['code']=='EVAL_SPLIT_OVERLAP')
   ev=op('/v1/eval/runs',{'set_id':s['id'],'strategies':['keyword'],'k':5});report=wait('/v1/jobs/'+ev['id']);ck('published feedback dataset executes actual retrieval',report['state']=='succeeded'and report['result']['purpose']=='development'and report['result']['summaries'][0]['hit_at_k']==1)
   preview=json.loads(cli('maintenance','records').stdout);ck('CLI record preview does not remove content',preview['dry_run']and not preview['removed_content'])
   manual=op('/v1/backups',{'confirm':True});cfg=op('/v1/maintenance/policy');cfg['policy']['backup_keep']=1;op('/v1/maintenance/policy',{'expected_revision':cfg['revision'],'policy':cfg['policy']},'PUT')
   cli('maintenance','run','--data','{"backup":true}','--confirm')
   for _ in range(200):
    xs=op('/v1/maintenance/runs')['items']
    if not any(x['state']in('queued','running')for x in xs):break
    time.sleep(.05)
   backs=op('/v1/backups')['items'];ck('managed backup rotation preserves manually created snapshot',len(backs)==2 and manual['id']in[x['id']for x in backs])
   rec=op('/v1/maintenance/runs',{'confirm':True,'records':True})
   for _ in range(200):
    found=next(x for x in op('/v1/maintenance/runs')['items']if x['id']==rec['id'])
    if found['state']not in('queued','running'):break
    time.sleep(.05)
   ck('online operational-record pass actually executes',found['state']=='succeeded'and not found['result']['records']['removed_content'])
   reader=op('/v1/keys',{'name':'testreader','role':'reader','kbs':['demo']});rk=reader.get('key')or reader.get('token')
   ck('reader cannot schedule housekeeping',api('/v1/maintenance/runs',{'confirm':True,'backup':True},token=rk)[0]==403)
   ck('reader cannot approve evaluation drafts',api('/v1/eval/drafts/'+d['id']+'/approve',{'expected_revision':1,'case':case},token=rk)[0]==403)
   if browser:browser_checks(base,key,api,op)
   ck('pure remote CLI creates no local knowledge DB',not list(home.rglob('*.sqlite')))
  finally:
   for p in(worker,server):
    if p and p.poll()is None:
     p.terminate()
     try:p.wait(timeout=12)
     except subprocess.TimeoutExpired:p.kill();p.wait()
   log.close()
 (CHECKS/'smoke.json').write_text(json.dumps({'passed':len(checks),'checks':checks,'scope':'fresh real SQLite/Server/Worker/CLI; no upstream model or database; UI transport labeled per run'},ensure_ascii=False,indent=2))

def browser_checks(base,key,api,op):
 from playwright.sync_api import sync_playwright
 errors=[];jar=http.cookiejar.CookieJar();opener=urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar))
 def bridge(path,options):
  if not path.startswith('/')or path.startswith('//'):raise ValueError('local test route only')
  body=options.get('body');req=urllib.request.Request(base+path,data=None if body is None else body.encode(),method=options.get('method','GET'),headers={'Content-Type':'application/json'})
  try:
   with opener.open(req,timeout=60)as r:return{'status':r.status,'bytes':base64.b64encode(r.read()).decode(),'type':r.headers.get('Content-Type','application/json')}
  except urllib.error.HTTPError as e:return{'status':e.code,'bytes':base64.b64encode(e.read()).decode(),'type':'application/json'}
 def ui(name,ok,detail=None):ck(name,ok,detail,'browser')
 with sync_playwright()as pw:
  b=pw.chromium.launch(executable_path='/usr/bin/chromium',headless=True,args=['--no-sandbox']);p=b.new_page(viewport={'width':1440,'height':1000});p.on('pageerror',lambda e:errors.append(str(e)));transport='direct URL'
  try:p.goto(base,wait_until='domcontentloaded',timeout=10000)
  except Exception as e:
   if 'BLOCKED_BY_ADMINISTRATOR'not in str(e):raise
   transport='DOM + explicit HTTP bridge';p.close();p=b.new_page(viewport={'width':1440,'height':1000});p.on('pageerror',lambda e:errors.append(str(e)))
   h=(ROOT/'internal/server/web/index.html').read_text().replace('<link rel="stylesheet" href="/style.css">','<style>'+(ROOT/'internal/server/web/style.css').read_text()+'</style>');scripts=['app.js','extensions.js','product.js','lifecycle.js','completion.js','iteration6.js','iteration7.js','iteration8.js']
   for f in scripts:h=h.replace(f'<script src="/{f}"></script>','')
   p.set_content(h);p.expose_function('demo6HTTP',bridge);p.evaluate("""()=>{window.fetch=async(path,o={})=>{const r=await window.demo6HTTP(path,o);return new Response(Uint8Array.from(atob(r.bytes),x=>x.charCodeAt(0)),{status:r.status,headers:{'Content-Type':r.type}})}}""")
   for f in scripts:p.add_script_tag(content=(ROOT/'internal/server/web'/f).read_text())
  p.on('dialog',lambda d:d.accept());p.fill('#key-input',key);p.click('#login-form button');p.wait_for_selector('#app:not([hidden])');ui('UI signs into actual service',True,transport)
  p.click('[data-page="settings"]');p.click('[data-settings="maintenance"]');p.wait_for_function("document.querySelector('#maintenance-revision').textContent.includes('修订')");ui('maintenance UI shows persisted off state',not p.is_checked('#maintenance-enabled'))
  p.fill('#maintenance-backup-every','86400');p.fill('#maintenance-records-every','86400');p.check('#maintenance-enabled');p.click('#maintenance-form button');p.wait_for_function("document.querySelector('#toast').textContent.includes('计划已保存')");ui('UI saves real enabled schedule',op('/v1/maintenance/policy')['policy']['enabled'])
  p.click('#maintenance-preview');p.wait_for_function("document.querySelector('#maintenance-preview-result').textContent.includes('dry_run')");ui('preview states no content deletion','"removed_content": false'in p.locator('#maintenance-preview-result').inner_text());p.screenshot(path=str(CHECKS/'maintenance.png'),timeout=10000)
  p.uncheck('#maintenance-enabled');p.click('#maintenance-form button');p.wait_for_timeout(150);ui('UI can disable schedule',not op('/v1/maintenance/policy')['policy']['enabled'])
  p.click('[data-page="feedback"]');p.wait_for_selector('[data-feedback-draft]');p.locator('[data-feedback-draft]').first.click();p.fill('#feedback-question','E203 的说明在哪里？');p.click('#feedback-case-form button');p.wait_for_selector('#eval-drafts [data-approve-draft]');ui('feedback UI creates pending test question','E203 的说明在哪里' in p.locator('#eval-drafts').inner_text())
  draft=next(x for x in op('/v1/eval/drafts?kb=demo')['items']if x['case']['question']=='E203 的说明在哪里？');p.locator('[data-approve-draft]').filter().first.wait_for(state='attached')
  # Select exact row, not a timestamp-dependent index.
  card=p.locator('#eval-drafts .result-card').filter(has_text='E203 的说明在哪里');card.locator('button').click();p.fill('#eval-approve-targets',json.dumps([{'path':'manual/guide.md'}]));
  # Find the actual E203 file in the current service rather than assuming a fixture filename.
  hit=op('/v1/search/keyword',{'kbs':['demo'],'q':'E203'})['items'][0];p.fill('#eval-approve-targets',json.dumps([{'path':hit['path'],'anchor':'E203'}]));p.fill('#eval-approve-note','人工确认 E203 原文');p.click('#eval-approve-form button');p.wait_for_function("!document.querySelector('#lifecycle-dialog')?.open")
  p.wait_for_timeout(150);d=next(x for x in op('/v1/eval/drafts?kb=demo')['items']if x['id']==draft['id']);ui('UI approval records exact expected passage',d['status']=='approved'and d['case']['expected'][0]['anchor']=='E203')
  p.locator('#eval-drafts .result-card').filter(has_text='E203 的说明在哪里').locator('input[type=checkbox]').check();p.fill('#eval-draft-name','UI holdout');p.fill('#eval-draft-family','ui-family');p.select_option('#eval-draft-purpose','holdout');p.click('#eval-from-drafts button');p.wait_for_function("Array.from(document.querySelector('#eval-set').options).some(o=>o.textContent.includes('UI holdout')&&o.textContent.includes('留出集'))");ui('UI publishes distinct holdout dataset','留出集'in p.locator('#eval-set').inner_text());p.screenshot(path=str(CHECKS/'feedback-datasets.png'),timeout=10000)
  p.click('[data-page="search"]');p.click('[data-mode="search.keyword"]');p.fill('#query','E203');p.click('#search-button');p.wait_for_selector('#results [data-read]');p.click('[data-page="eval"]');p.wait_for_selector('[data-query-diagnose]');p.locator('[data-query-diagnose]').first.click();p.fill('#diagnose-path',hit['path']);p.fill('#diagnose-anchor','E203');p.click('#diagnose-form button');p.wait_for_function("document.querySelector('#diagnose-result').textContent.includes('reran_query')");ui('UI diagnosis is historical not rerun','"reran_query": false'in p.locator('#diagnose-result').inner_text());p.screenshot(path=str(CHECKS/'diagnosis.png'),timeout=10000)
  p.click('#lifecycle-close');p.set_viewport_size({'width':390,'height':844});p.click('#mobile-menu');p.click('[data-page="settings"]');p.click('[data-settings="maintenance"]');p.wait_for_timeout(100);ui('new maintenance controls fit narrow page',p.evaluate('document.documentElement.scrollWidth<=innerWidth'));p.screenshot(path=str(CHECKS/'mobile.png'),timeout=10000);ui('full application has no JavaScript exceptions',not errors,errors);b.close()
if __name__=='__main__':
 p=argparse.ArgumentParser();p.add_argument('--browser',action='store_true');main(p.parse_args().browser)
