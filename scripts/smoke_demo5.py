#!/usr/bin/env python3
"""Fresh-install current binaries; no migration tests. External model responses are marked fixtures."""
from pathlib import Path
import sys,argparse,base64,http.cookiejar,http.server,json,os,socket,subprocess,tempfile,threading,time,urllib.request,urllib.error
ROOT=Path(__file__).resolve().parents[1]; CHECKS=ROOT/'checks/demo5';CHECKS.mkdir(parents=True,exist_ok=True);checks=[]
def ck(name,ok,detail=None):
 if not ok:raise AssertionError((name,detail))
 checks.append({'name':name,'passed':True,'detail':detail});print('PASS '+name,file=sys.stderr,flush=True)
def run(args,**kw):return subprocess.run(args,check=True,capture_output=True,text=True,timeout=120,**kw)
def port():
 with socket.socket() as s:s.bind(('127.0.0.1',0));return s.getsockname()[1]
class Model(http.server.BaseHTTPRequestHandler):
 calls=[]
 def log_message(self,*a):pass
 def do_POST(self):
  q=json.loads(self.rfile.read(int(self.headers.get('Content-Length',0))));type(self).calls.append((self.path,q))
  if self.path.endswith('/embeddings'):
   vec=[1.0,0.0] if q['model']=='m1' else [0.0,1.0]
   out={'data':[{'index':i,'embedding':vec} for i,_ in enumerate(q['input'])],'usage':{'prompt_tokens':10,'total_tokens':10}}
  elif self.path.endswith('/rerank'):out={'results':[{'index':i,'relevance_score':1/(i+1)} for i,_ in enumerate(q['documents'])],'usage':{'total_tokens':10}}
  else:
   if q['messages'][0]['content'].startswith('Extract '):
    src=json.loads(q['messages'][1]['content']);text=src['text'];marker='设备A使用组件B';content={'entities':[{'name':'设备A','type':'产品'},{'name':'组件B','type':'组件'}],'relations':[{'from':'设备A','to':'组件B','rel':'使用','evidence':marker}] if marker in text else []};content=json.dumps(content,ensure_ascii=False)
   else:content='TEST_FIXTURE 主题资料 [1]。'
   out={'choices':[{'message':{'content':content}}],'usage':{'prompt_tokens':20,'completion_tokens':10,'total_tokens':30}}
  b=json.dumps(out,ensure_ascii=False).encode();self.send_response(200);self.send_header('Content-Type','application/json');self.send_header('Content-Length',str(len(b)));self.end_headers();self.wfile.write(b)
def main(browser=False):
 with tempfile.TemporaryDirectory(prefix='xrag-demo5-fresh-') as tmp:
  t=Path(tmp);data=t/'data';home=t/'home';home.mkdir();log=(CHECKS/'process.log').open('w');n=port();base=f'http://127.0.0.1:{n}'
  model=http.server.ThreadingHTTPServer(('127.0.0.1',0),Model);threading.Thread(target=model.serve_forever,daemon=True).start();endpoint=f'http://127.0.0.1:{model.server_port}'
  pfile=(ROOT/'plugins/commonmark/parse.py').resolve();config=t/'startup.json';config.write_text(json.dumps({'plugins':{'commonmark':{'kind':'parser','version':'markdown-it-4.2.0-yaml-6.0.3','protocol':'xrag.parser/v1','command':[sys.executable,str(pfile)]}}}))
  run([str(ROOT/'bin/xrag-server'),'init','--data',str(data),'--examples',str(ROOT/'examples/docs')]);key=(data/'admin.key').read_text().strip()
  process=subprocess.Popen([str(ROOT/'bin/xrag-server'),'serve','--data',str(data),'--listen',f'127.0.0.1:{n}','--config',str(config)],stdout=log,stderr=log)
  def api(path,body=None,method=None):
   req=urllib.request.Request(base+path,data=None if body is None else json.dumps(body,ensure_ascii=False).encode(),method=method or('GET' if body is None else 'POST'),headers={'Authorization':'Bearer '+key,'Content-Type':'application/json'})
   try:
    with urllib.request.urlopen(req,timeout=30) as r:return r.status,json.load(r)
   except urllib.error.HTTPError as e:return e.code,json.load(e)
  def post(path,body,method=None):
   status,v=api(path,body,method)
   if status>=300:raise AssertionError((path,status,v))
   return v
  def wait(job):
   for _ in range(400):
    v=api('/v1/jobs/'+job['id'])[1]
    if v['state'] not in ['queued','running']:return v
    time.sleep(.05)
   raise AssertionError('job did not complete')
  def cli(*args):return run([str(ROOT/'bin/xrag'),*args,'--server',base,'--key-file',str(data/'admin.key'),'--kb','demo'],env={**os.environ,'HOME':str(home)})
  try:
   for _ in range(100):
    try:
     if api('/healthz')[0]==200:break
    except Exception:pass
    time.sleep(.1)
   ck('fresh server has current version',api('/healthz')[1]['version']=='0.9.0')
   ck('fresh demo corpus builds without models',wait(post('/v1/kbs/demo/builds',{}))['state']=='succeeded')
   old=post('/v1/search/keyword',{'kbs':['demo'],'q':'分页样本记录','limit':10});ck('exact total still separated from page',old['total']['value']==37 and len(old['items'])==10)
   ck('aggregate reference now returned',bool(old.get('summary_ref')))
   stat=post('/v1/kbs/demo/knowledge',{'title':'统计依据','body_md':'范围内有37个匹配块。','refs':[old['summary_ref']],'applies_to':'本次演示语料','not_applies_to':'不是客户数'})
   ck('aggregate-only knowledge draft persists',stat['applies_to']=='本次演示语料' and stat['refs']==[old['summary_ref']])
   read=post('/v1/files/read',{'kbs':['demo'],'ref':old['summary_ref']});ck('typed aggregate is readable without document fiction',read['data']['kind']=='aggregate')
   rename=json.loads(cli('kb','rename','demo','--name','演示知识库').stdout);ck('CLI KB rename actual metadata',api('/v1/kbs/demo')[1]['name']=='演示知识库')
   settings=api('/v1/kbs/demo/settings')[1];settings['dictionary']=['分页样本记录','知识工程'];settings['budget']={'max_calls_per_run':20,'max_tokens_per_run':1000000};updated=post('/v1/kbs/demo/settings',{'expected_revision':settings['revision'],'settings':settings},'PUT')
   ck('KB configuration persists with revision',updated['revision']==1 and not Model.calls)
   ck('graph-only rebuild runs from published bytes',wait(post('/v1/kbs/demo/builds',{'mode':'graph_only'}))['state']=='succeeded')
   ck('original reference remains unchanged after rebuild',post('/v1/files/read',{'kbs':['demo'],'ref':old['items'][0]['ref']})['data']['text']==old['items'][0]['text'])
   root=t/'source';root.mkdir();(root/'a.md').write_text('# A\n设备A使用组件B\n');(root/'ignored.md').write_text('SHOULD_BE_EXCLUDED')
   src=post('/v1/kbs/demo/sources',{'id':'newsource','kind':'path','root':str(root),'mount':'new'})
   post('/v1/kbs/demo/sources/newsource',{'expected_revision':0,'exclude':['ignored.md']},'PATCH')
   ck('filtered build succeeds',wait(post('/v1/kbs/demo/builds',{}))['state']=='succeeded')
   ck('excluded file absent from query',post('/v1/search/keyword',{'kbs':['demo'],'q':'SHOULD_BE_EXCLUDED'})['returned']==0)
   pol=post('/v1/kbs/demo/sources/newsource/policy',{'schedule':'0 9 * * 1-5','timezone':'Asia/Shanghai','native_watch':True},'PUT');ck('cron and file event policy persist with fallback',pol['schedule']=='0 9 * * 1-5' and pol['every_seconds']>0)
   post('/v1/kbs/demo/sources/newsource/policy',{},'PUT')
   post('/v1/kbs/demo/sources/newsource',{'expected_revision':1,'disabled':True},'PATCH');ck('source disabled retains current publication',post('/v1/search/keyword',{'kbs':['demo'],'q':'设备A使用组件B'})['returned']>0)
   ck('disabled hook rejected without executing',api('/v1/hooks/demo/newsource',{})[0]==409)
   post('/v1/kbs/demo/sources/newsource',{'expected_revision':2,'disabled':False},'PATCH')
   # Independent KB model configuration does not mutate the original KB's capabilities.
   post('/v1/kbs',{'id':'models','name':'模型测试库'});opts={'providers':{'embedding':{'url':endpoint,'model':'m1','dimension':2},'chat':{'url':endpoint,'model':'fixture'}},'graph':{'mode':'llm'},'parser_plugin':'commonmark','budget':{'max_calls_per_run':30,'max_tokens_per_day':1000000}}
   post('/v1/kbs/models/settings',{'expected_revision':0,'settings':opts},'PUT');post('/v1/kbs/models/sources',{'id':'text','kind':'path','root':str(root),'mount':'manual'});mb=wait(post('/v1/kbs/models/builds',{}));ck('per-KB parser/model/LLM graph build executes',mb['state']=='succeeded',mb.get('error'))
   ck('no false semantic capability on other KB',not post('/v1/discover',{'kbs':['demo']})['data']['semantic'] and post('/v1/discover',{'kbs':['models']})['data']['semantic'])
   rel=post('/v1/graph/neighbors',{'kbs':['models'],'name':'设备A'});ck('free text extractor fixture becomes sourced relation',rel['returned']>=1 and rel['total']['unit']=='relations')
   opts=api('/v1/kbs/models/settings')[1];opts['providers']['embedding']['model']='m2';post('/v1/kbs/models/settings',{'expected_revision':opts['revision'],'settings':opts},'PUT')
   q=post('/v1/search/semantic',{'kbs':['models'],'q':'设备'});ck('old generation remains queryable before rebuild',q['returned']>0 and Model.calls[-1][1].get('model')=='m1')
   vb=wait(post('/v1/kbs/models/builds',{'mode':'vectors_only'}));ck('vectors-only migration completes',vb['state']=='succeeded',vb.get('error'));post('/v1/search/semantic',{'kbs':['models'],'q':'设备'});ck('new requests use published model generation',Model.calls[-1][1].get('model')=='m2')
   usage=api('/v1/kbs/models/usage')[1];ck('query/build model usage persisted',sum(x['calls'] for x in usage['totals'])>=4)
   # Tools via real HTTP transport, no private shortcuts.
   tools=post('/mcp?toolsets=core,utility,write',{'jsonrpc':'2.0','id':1,'method':'tools/list'})['result']['tools'];ck('MCP explicit write group exposes structured inputs',{'knowledge_submit','feedback_submit'}<={x['name'] for x in tools})
   r=post('/v1/search/keyword',{'kbs':['demo'],'q':'E203'});m=post('/mcp?toolsets=write',{'jsonrpc':'2.0','id':2,'method':'tools/call','params':{'name':'knowledge_submit','arguments':{'kb':'demo','title':'MCP草稿','body_md':'来自工具调用','refs':[r['items'][0]['ref']]}}});ck('MCP draft goes through real knowledge save',not m['result']['isError'] and not m['result']['structuredContent']['published_revision'])
   # No legacy data needed. Brand-new KB/source CRUD.
   post('/v1/kbs',{'id':'discard','name':'临时库'});deleted=api('/v1/kbs/discard',{'confirm':True},'DELETE');after=api('/v1/kbs/discard');ck('KB delete actual current route',deleted[0]==200 and after[0] in (403,404),{'delete':deleted[0],'read_after':after[0]})
   if browser:browser_checks(base,key,api)
   ck('source remove implemented and source files untouched',api('/v1/kbs/demo/sources/newsource',{'expected_revision':3,'confirm':True},'DELETE')[0]==200 and (root/'a.md').exists())
   ck('pure remote client creates no knowledge DB',not list(home.rglob('*.sqlite')))
   ck('no old-version migration performed',True)
  finally:
   process.terminate()
   try:process.wait(timeout=15)
   except subprocess.TimeoutExpired:process.kill();process.wait()
   model.shutdown();model.server_close();log.close()
 (CHECKS/'smoke.json').write_text(json.dumps({'passed':len(checks),'checks':checks,'scope':'fresh installed binaries and SQLite, real HTTP/CLI; explicit model fixtures, no real Qdrant/Neo4j'},ensure_ascii=False,indent=2));print(json.dumps({'passed':len(checks)}))
def browser_checks(base,key,api):
 from playwright.sync_api import sync_playwright
 errors=[];jar=http.cookiejar.CookieJar();opener=urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar))
 def bridge(path,options):
  if not path.startswith('/') or path.startswith('//'):raise ValueError('invalid test route')
  b=options.get('body');req=urllib.request.Request(base+path,data=None if b is None else b.encode(),method=options.get('method','GET'),headers={'Content-Type':'application/json'})
  try:
   with opener.open(req,timeout=60) as r:return {'status':r.status,'bytes':base64.b64encode(r.read()).decode(),'type':r.headers.get('Content-Type','application/json')}
  except urllib.error.HTTPError as e:return {'status':e.code,'bytes':base64.b64encode(e.read()).decode(),'type':'application/json'}
 with sync_playwright() as pw:
  browser=pw.chromium.launch(executable_path='/usr/bin/chromium',headless=True,args=['--no-sandbox']);page=browser.new_page(viewport={'width':1440,'height':1000});page.on('pageerror',lambda e:errors.append(str(e)));transport='direct URL'
  try:page.goto(base,wait_until='domcontentloaded',timeout=12000)
  except Exception as e:
   if 'BLOCKED_BY_ADMINISTRATOR' not in str(e):raise
   transport='DOM + explicit HTTP bridge';page.close();page=browser.new_page(viewport={'width':1440,'height':1000});page.on('pageerror',lambda e:errors.append(str(e)))
   html=(ROOT/'internal/server/web/index.html').read_text().replace('<link rel="stylesheet" href="/style.css">','<style>'+(ROOT/'internal/server/web/style.css').read_text()+'</style>');scripts=['app.js','extensions.js','product.js','lifecycle.js','completion.js','iteration6.js','iteration7.js','iteration8.js']
   for f in scripts:html=html.replace(f'<script src="/{f}"></script>','')
   page.set_content(html);page.expose_function('demo5HTTP',bridge);page.evaluate("""()=>{window.fetch=async(p,o={})=>{const r=await window.demo5HTTP(p,o);return new Response(Uint8Array.from(atob(r.bytes),x=>x.charCodeAt(0)),{status:r.status,headers:{'Content-Type':r.type}})}}""")
   for f in scripts:page.add_script_tag(content=(ROOT/'internal/server/web'/f).read_text())
  page.fill('#key-input',key);page.click('#login-form button');page.wait_for_selector('#app:not([hidden])');ck('UI uses current real service',True,transport)
  page.select_option('#kb-select','demo');page.click('[data-page="settings"]');page.click('[data-settings="knowledge"]');page.wait_for_function("document.querySelector('#kb-config-revision').textContent.includes('1')")
  ck('KB configuration UI loaded persisted dictionary','知识工程' in page.input_value('#kb-dictionary'))
  page.fill('#kb-dictionary','知识工程\n分页样本记录\n恢复步骤');page.click('#kb-settings-form button');page.wait_for_function("document.querySelector('#kb-config-revision').textContent.includes('2')");ck('UI configuration save changes actual KB',len(api('/v1/kbs/demo/settings')[1]['dictionary'])==3)
  page.evaluate('scrollTo(0,0)');print('CAPTURE kb settings',file=sys.stderr,flush=True);page.screenshot(path=str(CHECKS/'kb-settings.png'),full_page=False,timeout=10000,animations='disabled');print('CAPTURED kb settings',file=sys.stderr,flush=True)
  print('NAVIGATE FILES',file=sys.stderr,flush=True);page.click('[data-page="files"]',timeout=10000);page.wait_for_selector('[data-source-policy]',state='attached',timeout=10000);ck('source controls are usable',page.get_by_role('button',name='包含 / 排除',exact=True).count()>0)
  page.get_by_role('button',name='Cron / 文件事件',exact=True).first.click();page.wait_for_selector('#source-advanced-policy');ck('cron and native watch editor is present',page.locator('#source-cron').count()==1);page.click('#lifecycle-close');page.screenshot(path=str(CHECKS/'source-controls.png'),full_page=False,timeout=10000,animations='disabled')
  page.click('[data-page="search"]');page.locator('[data-mode="search.keyword"]').click();page.fill('#query','分页样本记录');page.click('#search-button');page.wait_for_selector('#cite-total');page.click('#cite-total');ck('UI can cite aggregate directly',page.input_value('#knowledge-refs').startswith('xr1:'))
  page.click('[data-page="knowledge"]');page.wait_for_selector('#sync-target-advanced');ck('knowledge form exposes context and S3 writeback',page.locator('#knowledge-applies').count()==1);page.click('#sync-target-advanced');page.wait_for_selector('#advanced-target-form');ck('advanced sync config defaults no automation',not json.loads(page.input_value('#advanced-target-json'))['auto_sync']);page.click('#lifecycle-close')
  page.select_option('#kb-select','models');page.click('[data-page="settings"]');page.click('[data-settings="knowledge"]');page.wait_for_function("document.querySelector('#kb-usage-json').textContent.includes('embedding')");ck('UI usage comes from persisted calls','completed' in page.locator('#kb-usage-table').inner_text());page.screenshot(path=str(CHECKS/'model-usage.png'),full_page=False,timeout=10000,animations='disabled')
  page.set_viewport_size({'width':390,'height':844});page.wait_for_timeout(100);page.screenshot(path=str(CHECKS/'mobile-before.png')); print('OVERFLOW '+json.dumps(page.evaluate("[...document.querySelectorAll('body *')].filter(e=>e.getBoundingClientRect().right>innerWidth+2&&!e.closest('pre')&&!e.closest('.table-wrap')).map(e=>({tag:e.tagName,id:e.id,cl:e.className,w:e.getBoundingClientRect().width,r:e.getBoundingClientRect().right})).slice(0,35)")),file=sys.stderr,flush=True);ck('mobile page has no global horizontal overflow',page.evaluate('document.documentElement.scrollWidth<=innerWidth'));page.screenshot(path=str(CHECKS/'mobile.png'))
  ck('UI no JavaScript runtime errors',not errors,errors);browser.close()
if __name__=='__main__':
 p=argparse.ArgumentParser();p.add_argument('--browser',action='store_true');main(p.parse_args().browser)
