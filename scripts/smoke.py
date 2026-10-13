#!/usr/bin/env python3
"""Run actual server/client processes. --browser performs DOM tests via a documented
HTTP bridge when managed Chromium blocks all navigation; this is not direct URL E2E.
No real models or external database instances are used by this script.
"""
import argparse,contextlib,http.cookiejar,json,os,pathlib,socket,subprocess,tempfile,time,urllib.request,urllib.error
ROOT=pathlib.Path(__file__).resolve().parents[1]
CHECKS=ROOT/'checks';CHECKS.mkdir(exist_ok=True)
results=[]
def ok(name,condition,detail=''):
 if not condition:raise AssertionError(name+': '+str(detail))
 results.append({'name':name,'passed':True,'detail':detail})
def freeport():
 with socket.socket() as s:s.bind(('127.0.0.1',0));return s.getsockname()[1]
def main(browser=False):
 with tempfile.TemporaryDirectory(prefix='xrag-smoke-') as temp:
  root=pathlib.Path(temp);data=root/'server';home=root/'home';home.mkdir()
  subprocess.run([str(ROOT/'bin/xrag-server'),'init','--data',str(data),'--examples',str(ROOT/'examples/docs')],check=True,stdout=subprocess.DEVNULL)
  key=(data/'admin.key').read_text().strip();port=freeport();base=f'http://127.0.0.1:{port}'
  logs=(CHECKS/'process-server.log').open('w');server=subprocess.Popen([str(ROOT/'bin/xrag-server'),'serve','--data',str(data),'--listen',f'127.0.0.1:{port}','--workers','off'],stdout=logs,stderr=logs);worker=None
  def api(path,payload=None,method=None,token=key):
   b=None if payload is None else json.dumps(payload,ensure_ascii=False).encode()
   req=urllib.request.Request(base+path,data=b,method=method or ('GET' if b is None else 'POST'),headers={'Content-Type':'application/json','Authorization':'Bearer '+token})
   try:
    with urllib.request.urlopen(req,timeout=15) as r:return r.status,json.load(r)
   except urllib.error.HTTPError as e:return e.code,json.load(e)
  def post(path,p):
   status,r=api(path,p)
   if status>=300:raise AssertionError((path,status,r))
   return r
  def query(name,args=None):return post('/v1/'+name,{'kbs':['demo'],**(args or {})})
  def waitjob(j):
   for _ in range(100):
    _,r=api('/v1/jobs/'+j['id'])
    if r['state'] not in ('running','queued'):return r
    time.sleep(.1)
   raise AssertionError('job timed out '+j['id'])
  def cli(*args):return subprocess.run([str(ROOT/'bin/xrag'),*args,'--server',base,'--key-file',str(data/'admin.key')],cwd=ROOT,env={**os.environ,'HOME':str(home)},text=True,capture_output=True,timeout=15)
  try:
   for _ in range(50):
    try:
     if api('/healthz')[0]==200:break
    except Exception:time.sleep(.1)
   ok('server starts without external models',api('/v1/system')[1]['embedding'] is False)
   status,_=api('/v1/kbs',token='');ok('missing API key rejected',status==401)
   status,_=api('/v1/search/keyword',{'kbs':['demo'],'q':'日志','unknown':1});ok('unknown action parameter rejected',status==400)
   j=post('/v1/kbs/demo/builds',{'mode':'incremental'});time.sleep(.3);state=api('/v1/jobs/'+j['id'])[1]['state'];ok('API without Worker leaves build queued',state=='queued',state)
   worker=subprocess.Popen([str(ROOT/'bin/xrag-server'),'worker','--data',str(data)],stdout=logs,stderr=logs)
   j=waitjob(j);ok('separate Worker builds actual files',j['state']=='succeeded',j['result'])
   r=query('search/keyword',{'q':'分页样本记录','limit':10});seen=[];sizes=[];query_id=r['query_id']
   while True:
    sizes.append(r['returned']);seen.extend(x['chunk_id'] for x in r['items']);ok_total=(r['total']['value']==37 and r['query_id']==query_id)
    if not ok_total:raise AssertionError(r)
    if not r.get('next_cursor'):break
    r=post('/v1/results/page',{'kbs':['demo'],'result_id':r['result_id'],'cursor':r['next_cursor'],'limit':10})
   ok('HTTP cursor pages 37 without repeats',[10,10,10,7]==sizes and len(set(seen))==37,sizes)
   r=query('files/tree',{'limit':100});f1=next(i for i in r['items'] if 'F1' in i['path'])
   scoped=query('files/tree',{'filters':[{'field':'path','op':'eq','value':f1['path']}],'save_scope':'required'})
   rr=query('graph/neighbors',{'name':'设备A','direction':'out','within':scoped['handle']});ok('HTTP within excludes F2 graph support',rr['returned']==0)
   rr=query('graph/path',{'from_id':'设备A','to_id':'日志事件','direction':'out','max_hops':3});ok('HTTP actual graph path returns directed support',rr['returned']==1 and len(rr['items'][0]['edges'])==2)
   rr=query('search/keyword',{'q':'E203'});ref=rr['items'][0]['ref'];rr=query('files/read',{'ref':ref});ok('HTTP read opens actual cited original','E203' in rr['data']['text'])
   status,_=api('/v1/search/semantic',{'kbs':['demo'],'q':'日志'});ok('semantic unavailable is explicit, not simulated',status==422)
   cp=cli('search','keyword','E203','--kb','demo');ok('real Client remote query',cp.returncode==0 and json.loads(cp.stdout)['returned']>0,cp.stderr[:200]);ok('remote Client creates no local SQLite',not list(home.rglob('*.sqlite')))
   dry=cli('search','keyword','E203','--kb','demo','--dry-run');ok('Client dry-run reports request',dry.returncode==0 and json.loads(dry.stdout)['will_execute'] is False)
   k=post('/v1/kbs/demo/knowledge',{'title':'独立进程知识草稿','kind':'case','body_md':'烟测独特条目标记','refs':[ref]})
   rr=query('search/keyword',{'q':'烟测独特条目标记'});ok('saved draft absent from search',rr['returned']==0)
   kj=post('/v1/knowledge/'+k['id']+'/publish',{'revision':k['draft_revision']});kj=waitjob(kj);rr=query('search/keyword',{'q':'烟测独特条目标记'});ok('published knowledge becomes searchable',kj['state']=='succeeded' and rr['returned']==1)
   httpmcp=post('/mcp',{'jsonrpc':'2.0','id':1,'method':'tools/list'});tools=httpmcp['result']['tools'];ok('MCP HTTP tools carry output schema',len(tools)>5 and all('outputSchema'in t for t in tools))
   mc=post('/mcp',{'jsonrpc':'2.0','id':2,'method':'tools/call','params':{'name':'search_keyword','arguments':{'kbs':['demo'],'q':'E203'}}});ok('MCP HTTP returns real structured result',mc['result']['structuredContent']['returned']>0)
   inp='\n'.join(json.dumps(x) for x in [{'jsonrpc':'2.0','id':1,'method':'initialize','params':{'protocolVersion':'2025-06-18'}},{'jsonrpc':'2.0','method':'notifications/initialized'},{'jsonrpc':'2.0','id':2,'method':'tools/call','params':{'name':'search_keyword','arguments':{'kbs':['demo'],'q':'E203'}}}])+'\n'
   cp=subprocess.run([str(ROOT/'bin/xrag'),'mcp','--server',base,'--key-file',str(data/'admin.key')],input=inp,text=True,capture_output=True,timeout=15,cwd=ROOT);lines=[json.loads(x) for x in cp.stdout.splitlines()];ok('MCP stdio actual Client binary roundtrip',cp.returncode==0 and len(lines)==2 and lines[-1]['result']['structuredContent']['returned']>0,cp.stderr[:200])
   status,_=api('/v1/kbs/demo/wiki',{'topic':'日志'});ok('Wiki requires real configured model',status==422)
   uj=post('/v1/kbs/demo/upload',{'path':'api-upload.md','body':'# 新文件\n实际上传成功标记\n'});uj=waitjob(uj);rr=query('search/keyword',{'q':'实际上传成功标记'});ok('uploaded bytes build and search',uj['state']=='succeeded' and rr['returned']==1)
   if browser:browser_checks(base,key,api,query,waitjob)
   worker.terminate();worker.wait(timeout=10);worker=None
   r=query('search/keyword',{'q':'E203'});ok('query continues after Worker stops',r['returned']>0)
   j=post('/v1/kbs/demo/builds',{'mode':'incremental'});post('/v1/jobs/'+j['id']+'/cancel',{});ok('queued cancellation recorded',api('/v1/jobs/'+j['id'])[1]['cancel_requested'] is True)
   server.terminate();server.wait(timeout=10)
   cp=subprocess.run([str(ROOT/'bin/xrag'),'search','keyword','E203','--local',str(ROOT/'examples/docs'),'--kb','demo'],cwd=ROOT,text=True,capture_output=True,env={**os.environ,'HOME':str(home)},timeout=15);ok('local Client works without any Server',cp.returncode==0 and json.loads(cp.stdout)['returned']>0,cp.stderr[:200])
   ok('local Client creates no persistent knowledge database',not list(home.rglob('*.sqlite')))
  finally:
   for p in [worker,server]:
    if p is not None and p.poll() is None:
     p.terminate()
     try:p.wait(timeout=10)
     except subprocess.TimeoutExpired:p.kill();p.wait()
   logs.close()
 (CHECKS/'process-smoke.json').write_text(json.dumps({'kind':'real processes; browser uses explicit HTTP bridge when navigation is blocked','passed':len(results),'checks':results},ensure_ascii=False,indent=2))
 print(json.dumps({'passed':len(results),'report':str(CHECKS/'process-smoke.json')},ensure_ascii=False))

def browser_checks(base,key,api,query,waitjob):
 from playwright.sync_api import sync_playwright
 # The managed browser forbids direct navigation. Load only our own page assets;
 # bridge fetch to the actual running service with a cookie-aware HTTP session.
 jar=http.cookiejar.CookieJar();opener=urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar))
 def bridge(path,options):
  if not path.startswith('/'):raise ValueError('only local API paths')
  b=options.get('body');data=None if b is None else b.encode()
  req=urllib.request.Request(base+path,data=data,method=options.get('method','GET'),headers={'Content-Type':'application/json'})
  try:
   with opener.open(req,timeout=15) as r:return {'status':r.status,'body':r.read().decode()}
  except urllib.error.HTTPError as e:return {'status':e.code,'body':e.read().decode()}
 html=(ROOT/'internal/server/web/index.html').read_text().replace('<link rel="stylesheet" href="/style.css">','<style>'+ (ROOT/'internal/server/web/style.css').read_text()+'</style>').replace('<script src="/app.js"></script>','').replace('<script src="/extensions.js"></script>','').replace('<script src="/product.js"></script>','').replace('<script src="/lifecycle.js"></script>','').replace('<script src="/completion.js"></script>','').replace('<script src="/iteration6.js"></script>','').replace('<script src="/iteration7.js"></script>','')
 with sync_playwright() as p:
  browser=p.chromium.launch(executable_path='/usr/bin/chromium',headless=True,args=['--no-sandbox'])
  page=browser.new_page(viewport={'width':1440,'height':1060},device_scale_factor=1)
  errors=[];page.on('pageerror',lambda e:errors.append(str(e)));page.expose_function('xragTestHTTP',bridge)
  page.set_content(html, wait_until='domcontentloaded')
  page.evaluate('''() => { window.fetch=async(path,options={})=>{const r=await window.xragTestHTTP(path,options);return new Response(r.body,{status:r.status,headers:{'Content-Type':'application/json'}})}; }''')
  page.add_script_tag(content=(ROOT/'internal/server/web/app.js').read_text())
  page.add_script_tag(content=(ROOT/'internal/server/web/extensions.js').read_text())
  page.add_script_tag(content=(ROOT/'internal/server/web/product.js').read_text())
  page.add_script_tag(content=(ROOT/'internal/server/web/lifecycle.js').read_text())
  page.add_script_tag(content=(ROOT/'internal/server/web/completion.js').read_text())
  page.add_script_tag(content=(ROOT/'internal/server/web/iteration6.js').read_text())
  page.add_script_tag(content=(ROOT/'internal/server/web/iteration7.js').read_text())
  page.fill('#key-input',key);page.click('#login-form button');page.wait_for_selector('#app:not([hidden])')
  page.screenshot(path=str(CHECKS/'desktop-home.png'))
  page.click('[data-mode="search.keyword"]');page.fill('#query','分页样本记录');page.click('#search-button');page.wait_for_function("document.querySelector('#search-summary').textContent.includes('37')")
  ok('DOM real search renders 37 total', '37' in page.locator('#search-summary').inner_text());page.screenshot(path=str(CHECKS/'desktop-search.png'))
  ok('DOM search first page renders 10 cards',page.locator('#results .result-card').count()==10)
  page.locator('#results [data-read]').first.click();page.wait_for_selector('#reader[open]');ok('DOM read displays server original','分页样本记录' in page.locator('#reader-body').inner_text());page.keyboard.press('Escape')
  page.click('[data-page="graph"]');page.click('#path-btn');page.wait_for_selector('#graph-result .edge-row');ok('DOM path displays directed graph edges',page.locator('#graph-result .edge-row').count()==2);page.screenshot(path=str(CHECKS/'desktop-graph.png'))
  page.click('[data-page="files"]');page.wait_for_selector('#file-list tbody tr');ok('DOM published file list is actual data',page.locator('#file-list tbody tr').count()>=8)
  page.click('#estimate-btn');page.wait_for_function("document.querySelector('#estimate-output').textContent.length>10");ok('DOM build estimate returns actual counts',len(page.locator('#estimate-output').inner_text())>10)
  page.click('[data-page="knowledge"]');page.wait_for_selector('#knowledge-list .result-card');ok('DOM published knowledge item visible','published' in page.locator('#knowledge-list').inner_text());page.screenshot(path=str(CHECKS/'desktop-knowledge.png'))
  page.click('[data-page="settings"]');page.wait_for_selector('#capability-cards .card');ok('DOM capability view states missing Chat','未配置' in page.locator('#page-settings').inner_text())
  page.set_viewport_size({'width':390,'height':844});page.click('#mobile-menu');page.click('[data-page="search"]');page.screenshot(path=str(CHECKS/'mobile-search.png'));ok('DOM mobile has no page horizontal overflow',page.evaluate('document.documentElement.scrollWidth<=innerWidth'))
  ok('DOM no JavaScript runtime errors',not errors,errors)
  browser.close()
if __name__=='__main__':
 p=argparse.ArgumentParser();p.add_argument('--browser',action='store_true');args=p.parse_args();main(args.browser)
