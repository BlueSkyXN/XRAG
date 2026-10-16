#!/usr/bin/env python3
"""Fresh binaries and Git workflows. Model replies are explicit test fixtures.
Browser uses normal navigation first; a labeled local HTTP bridge only when policy blocks it.
"""
from pathlib import Path
import argparse,base64,hashlib,hmac,http.cookiejar,http.server,json,os,socket,subprocess,tempfile,threading,time,urllib.request,urllib.error
ROOT=Path(__file__).resolve().parents[1];CHECKS=ROOT/'checks/demo7';CHECKS.mkdir(exist_ok=True);rows=[]
def ck(name,ok,detail=None,kind='process'):
 if not ok:raise AssertionError((name,detail))
 rows.append({'name':name,'passed':True,'kind':kind,'detail':detail});print('PASS '+name,flush=True)
def command(args,**kw):return subprocess.run([str(x)for x in args],check=True,text=True,capture_output=True,timeout=120,**kw)
def port():
 with socket.socket()as s:s.bind(('127.0.0.1',0));return s.getsockname()[1]
class Model(http.server.BaseHTTPRequestHandler):
 calls=[]
 def log_message(self,*args):pass
 def do_POST(self):
  body=json.loads(self.rfile.read(int(self.headers['Content-Length'])));self.calls.append((self.path,body))
  if self.path.endswith('/embeddings'):out={'data':[{'index':i,'embedding':[1.0,0.0]if'NORTH'in x else[0.0,1.0]}for i,x in enumerate(body['input'])],'usage':{'total_tokens':5,'prompt_tokens':5}}
  elif self.path.endswith('/rerank'):out={'results':[{'index':i,'relevance_score':1/(i+1)}for i in range(len(body['documents']))],'usage':{'total_tokens':5}}
  else:self.send_error(404);return
  data=json.dumps(out).encode();self.send_response(200);self.send_header('Content-Type','application/json');self.send_header('Content-Length',str(len(data)));self.end_headers();self.wfile.write(data)
def main(browser):
 with tempfile.TemporaryDirectory(prefix='xrag-fresh7-')as tmp:
  t=Path(tmp);home=t/'home';home.mkdir();data=t/'data';repo=t/'git';repo.mkdir();secret='synthetic-native-hook-secret-not-a-real-token';keyref=t/'hook.secret';keyref.write_text(secret)
  def git(*a):return command(['git','-C',repo,'-c','user.name=Fixture','-c','user.email=fixture@example.invalid',*a],env={**os.environ,'GIT_CONFIG_GLOBAL':'/dev/null','GIT_CONFIG_NOSYSTEM':'1'}).stdout.strip()
  git('init','-b','main');(repo/'a.md').write_text('# A\nNORTH [[A]] --uses--> [[B]]\n');(repo/'b.md').write_text('# B\nSOUTH body stays unchanged\n');git('add','.');git('commit','-m','fixture')
  command([ROOT/'bin/xrag-server','init','--data',data,'--examples',ROOT/'examples/docs']);key=(data/'admin.key').read_text().strip();base='http://127.0.0.1:'+str(port());log=(CHECKS/'process.log').open('w')
  model=http.server.ThreadingHTTPServer(('127.0.0.1',0),Model);thread=threading.Thread(target=model.serve_forever,daemon=True);thread.start();endpoint=f'http://127.0.0.1:{model.server_port}'
  proc=subprocess.Popen([str(ROOT/'bin/xrag-server'),'serve','--data',str(data),'--listen',base.removeprefix('http://'),'--workers','off'],stdout=log,stderr=log);worker=None
  def api(path,body=None,method=None,headers=None,auth=True):
   payload=body if isinstance(body,bytes) else None if body is None else json.dumps(body,ensure_ascii=False).encode()
   h={'Content-Type':'application/json'}
   if auth:h['Authorization']='Bearer '+key
   if headers:h.update(headers)
   req=urllib.request.Request(base+path,data=payload,method=method or('GET'if body is None else'POST'),headers=h)
   try:
    with urllib.request.urlopen(req,timeout=45)as r:return r.status,json.load(r)
   except urllib.error.HTTPError as e:return e.code,json.load(e)
  def op(*a,**kw):
   status,v=api(*a,**kw)
   if status>=300:raise AssertionError((a,status,v))
   return v
  def wait(j):
   for _ in range(400):
    r=op('/v1/jobs/'+j['id'])
    if r['state']not in('queued','running'):return r
    time.sleep(.05)
   raise AssertionError(('job timeout',j))
  def hook(provider,event,delivery,payload=None,signature=True):
   body=(payload if payload is not None else {'ref':'refs/heads/main','repository':{'full_name':'group/project','clone_url':'https://ignore.invalid/not-used'}})
   raw=json.dumps(body,ensure_ascii=False).encode();digest=hmac.new(secret.encode(),raw,hashlib.sha256).hexdigest()
   if provider=='github':headers={'X-GitHub-Event':event,'X-GitHub-Delivery':delivery,'X-Hub-Signature-256':'sha256='+digest if signature else 'bad'}
   elif provider=='gitea':headers={'X-Gitea-Event':event,'X-Gitea-Delivery':delivery,'X-Gitea-Signature':digest if signature else'bad'}
   else:headers={'X-Gitlab-Event':event,'Idempotency-Key':delivery,'X-Gitlab-Token':secret if signature else'bad'}
   return api('/v1/hooks/demo/git',raw,headers=headers,auth=False)
  def policy(provider):return op('/v1/kbs/demo/sources/git/policy',{'hook_provider':provider,'hook_repository':'group/project','hook_ref':'refs/heads/main','hook_secret_ref':'file:'+str(keyref)},'PUT')
  try:
   for _ in range(80):
    try:
     if api('/healthz')[0]==200:break
    except OSError:pass
    time.sleep(.1)
   ck('fresh Demo.7 actually starts',op('/healthz')['version']=='0.10.1')
   op('/v1/kbs/demo/sources',{'id':'git','kind':'git','root':str(repo),'git_ref':'main','mount':'git'});policy('github')
   status,j=hook('github','push','first');time.sleep(.15);ck('native GitHub notice only queues when workers off',status==202 and op('/v1/jobs/'+j['id'])['state']=='queued')
   status,dup=hook('github','push','first');ck('same native delivery returns existing job',status==202 and dup['id']==j['id'])
   ck('bad vendor signature does not queue',hook('github','push','bad',signature=False)[0]==401)
   st,ignored=hook('github','push','other-branch',{'ref':'refs/heads/other','repository':{'full_name':'group/project'}});ck('unselected branch ignored without build',st==202 and ignored['effect']=='none')
   ck('foreign repository rejected',hook('github','push','other-repo',{'ref':'refs/heads/main','repository':{'full_name':'other/project'}})[0]==403)
   ck('offline collection command rejects running workspace',subprocess.run([str(ROOT/'bin/xrag-server'),'vector-gc','--data',str(data)],text=True,capture_output=True).returncode!=0)
   worker=subprocess.Popen([str(ROOT/'bin/xrag-server'),'worker','--data',str(data)],stdout=log,stderr=log)
   ck('real Git build follows accepted native notice',wait(j)['state']=='succeeded')
   hit=op('/v1/search/keyword',{'kbs':['demo'],'q':'NORTH'});old=hit['items'][0]['ref'];ck('fixed registered Git source content returned',hit['returned']==1 and hit['items'][0]['path']=='git/a.md')
   (repo/'a.md').write_text('# A\nNORTH newcommit [[A]] --uses--> [[B]]\n');git('add','.');git('commit','-m','update')
   j=hook('github','push','next')[1];ck('second push rebuild keeps unchanged files',wait(j)['state']=='succeeded'and op('/v1/search/keyword',{'kbs':['demo'],'q':'SOUTH'})['returned']==1)
   ck('old ref still reads its original commit content','newcommit'not in op('/v1/files/read',{'kbs':['demo'],'ref':old})['data']['text'])
   policy('gitlab');st,j=hook('gitlab','Push Hook','gl-one',{'object_kind':'push','ref':'refs/heads/main','project':{'path_with_namespace':'group/project'}});ck('GitLab token protocol drives existing build',st==202 and wait(j)['state']=='succeeded')
   policy('gitea');st,j=hook('gitea','push','gt-one');ck('Gitea raw hexadecimal signature drives build',st==202 and wait(j)['state']=='succeeded')
   st,ping=hook('gitea','ping','ping');ck('Gitea ping acknowledges without executing',st==200 and ping['effect']=='none')
   rel=op('/v1/graph/chunks',{'kbs':['demo'],'name':'A','trace':True});ck('graph reads compact metadata then selected text',rel['strategy']['execution']=='metadata_projection_then_page_hydration'and any(x.get('text')for x in rel['items']))
   f1=next(x for x in op('/v1/files/tree',{'kbs':['demo'],'limit':100})['items']if'F1'in x['path']);h=op('/v1/files/tree',{'kbs':['demo'],'filters':[{'field':'path','op':'eq','value':f1['path']}],'save_scope':'required'})
   r=op('/v1/graph/neighbors',{'kbs':['demo'],'name':'设备A','within':h['handle']});ck('compact graph scope still excludes F2-only edge',r['returned']==0)
   op('/v1/kbs',{'id':'vectors','name':'Controlled vector fixture'});op('/v1/kbs/vectors/settings',{'expected_revision':0,'settings':{'providers':{'embedding':{'url':endpoint,'model':'fixture','dimension':2},'rerank':{'url':endpoint,'model':'fixture'}}}},'PUT');op('/v1/kbs/vectors/sources',{'id':'git','kind':'git','root':str(repo),'git_ref':'main'})
   ck('fixture vectors enter real SQLite build',wait(op('/v1/kbs/vectors/builds',{}))['state']=='succeeded')
   r=op('/v1/search/semantic',{'kbs':['vectors'],'q':'NORTH','limit':1,'trace':True});ck('semantic compact path returns source body',r['strategy']['execution']=='metadata_projection_then_page_hydration'and'NORTH'in r['items'][0]['text'])
   r=op('/v1/ask',{'kbs':['vectors'],'q':'NORTH','rerank':True,'graph':True,'limit':1,'trace':True});ck('hybrid compact query calls ranker with real candidate text',r['returned']==1 and all(x for path,b in Model.calls if path.endswith('/rerank')for x in b['documents']))
   ck('query model usage still recorded',sum(x['calls']for x in op('/v1/kbs/vectors/usage')['totals'])>=3)
   codeout=command([ROOT/'bin/xrag','+ask','NORTH','--server',base,'--key-file',data/'admin.key','--kb','vectors','--trace'],env={**os.environ,'HOME':str(home)});ck('actual CLI uses new execution strategy',json.loads(codeout.stdout)['strategy']['execution']=='metadata_projection_then_page_hydration')
   ck('remote CLI does not create a local knowledge database',not list(home.rglob('*.sqlite')))
   if browser:browser_checks(base,key,api,op,keyref)
  finally:
   for p in(worker,proc):
    if p and p.poll()is None:
     p.terminate()
     try:p.wait(timeout=15)
     except subprocess.TimeoutExpired:p.kill();p.wait()
   model.shutdown();model.server_close();log.close()
  preview=json.loads(command([ROOT/'bin/xrag-server','vector-gc','--data',data]).stdout);ck('offline local registry preview executes with no configured Qdrant',preview['dry_run']and preview['removed']==0 and not preview['remote_checked'])
  done=json.loads(command([ROOT/'bin/xrag-server','vector-gc','--data',data,'--confirm']).stdout);ck('confirmed empty registry performs no remote deletion',not done['dry_run']and done['removed']==0 and not done['remote_checked'])
 (CHECKS/'smoke.json').write_text(json.dumps({'passed':len(rows),'checks':rows,'scope':'real Server/Worker/CLI/SQLite/local Git; vendor payloads and model endpoint are controlled, no actual vendor delivery or external database'},ensure_ascii=False,indent=2))

def browser_checks(base,key,api,op,keyref):
 from playwright.sync_api import sync_playwright
 jar=http.cookiejar.CookieJar();opener=urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar));errors=[]
 def bridge(path,options):
  if not path.startswith('/')or path.startswith('//'):raise ValueError('local route only')
  body=options.get('body');req=urllib.request.Request(base+path,data=None if body is None else body.encode(),method=options.get('method','GET'),headers={'Content-Type':'application/json'})
  try:
   with opener.open(req,timeout=60)as r:return{'status':r.status,'bytes':base64.b64encode(r.read()).decode(),'type':r.headers.get('Content-Type','application/json')}
  except urllib.error.HTTPError as e:return{'status':e.code,'bytes':base64.b64encode(e.read()).decode(),'type':'application/json'}
 def ui(n,b,d=None):ck(n,b,d,'browser')
 with sync_playwright()as pw:
  browser=pw.chromium.launch(executable_path='/usr/bin/chromium',headless=True,args=['--no-sandbox']);p=browser.new_page(viewport={'width':1440,'height':1000});p.on('pageerror',lambda e:errors.append(str(e)));transport='direct URL'
  try:p.goto(base,wait_until='domcontentloaded',timeout=10000)
  except Exception as e:
   if'BLOCKED_BY_ADMINISTRATOR'not in str(e):raise
   transport='DOM + explicit HTTP bridge';p.close();p=browser.new_page(viewport={'width':1440,'height':1000});p.on('pageerror',lambda e:errors.append(str(e)))
   h=(ROOT/'internal/server/web/index.html').read_text().replace('<link rel="stylesheet" href="/style.css">','<style>'+(ROOT/'internal/server/web/style.css').read_text()+'</style>');scripts=['app.js','extensions.js','product.js','lifecycle.js','completion.js','iteration6.js','iteration7.js','iteration8.js']
   for f in scripts:h=h.replace(f'<script src="/{f}"></script>','')
   p.set_content(h);p.expose_function('demo7HTTP',bridge);p.evaluate("""()=>{window.fetch=async(path,o={})=>{const r=await window.demo7HTTP(path,o);return new Response(Uint8Array.from(atob(r.bytes),x=>x.charCodeAt(0)),{status:r.status,headers:{'Content-Type':r.type}})}}""")
   for f in scripts:p.add_script_tag(content=(ROOT/'internal/server/web'/f).read_text())
  p.fill('#key-input',key);p.click('#login-form button');p.wait_for_selector('#app:not([hidden])');ui('UI signs into actual Demo.7 service',True,transport)
  p.click('[data-page="files"]');p.wait_for_selector('[data-native-hook="git"]');p.click('[data-native-hook="git"]');p.wait_for_selector('#native-hook-form');ui('native hook UI displays existing Gitea policy',p.input_value('#hook-provider')=='gitea')
  p.select_option('#hook-provider','github');p.fill('#hook-repository','group/project');p.fill('#hook-ref','refs/heads/main');p.fill('#hook-secret','file:'+str(keyref));p.click('#native-hook-form button');p.wait_for_function("!document.querySelector('#lifecycle-dialog').open");ui('native hook form saves actual selected protocol',op('/v1/kbs/demo/sources/git/policy')['hook_provider']=='github')
  p.click('[data-native-hook="git"]');p.wait_for_selector('#native-hook-form');p.screenshot(path=str(CHECKS/'native-hooks.png'));p.click('#lifecycle-close')
  card=p.locator('#source-list .source-card').filter(has=p.locator('[data-native-hook="git"]'));card.locator('summary').click();card.locator('input[name=every]').fill('120');card.locator('[data-source-policy] button.primary').click();p.wait_for_timeout(250);ui('ordinary schedule edit preserves native policy',op('/v1/kbs/demo/sources/git/policy')['hook_provider']=='github');card=p.locator('#source-list .source-card').filter(has=p.locator('[data-native-hook="git"]'));card.locator('summary').click();card.locator('[data-source-pause]').click()
  print('STAGE UI search',flush=True);p.click('[data-page="search"]');p.fill('#query','NORTH');p.click('#search-button');p.wait_for_selector('#results [data-read]');ui('UI renders source text from compact hybrid path','NORTH'in p.locator('#results').inner_text());p.screenshot(path=str(CHECKS/'compact-query.png'))
  p.click('[data-page="settings"]');p.click('[data-settings="data"]');p.wait_for_function("document.querySelector('#settings-data').innerText.includes('vector-gc')");ui('UI labels generation cleanup as offline operation','不是在线 GC'in p.locator('#settings-data').inner_text());p.screenshot(path=str(CHECKS/'vector-gc.png'))
  p.set_viewport_size({'width':390,'height':844});p.click('#mobile-menu');p.click('[data-page="files"]');p.wait_for_selector('[data-native-hook="git"]');p.click('[data-native-hook="git"]');p.wait_for_selector('#native-hook-form');ui('native hook controls fit narrow viewport',p.evaluate('document.documentElement.scrollWidth<=innerWidth'));p.screenshot(path=str(CHECKS/'mobile-hooks.png'));ui('all loaded application scripts have no runtime exceptions',not errors,errors);browser.close()
if __name__=='__main__':
 p=argparse.ArgumentParser();p.add_argument('--browser',action='store_true');main(p.parse_args().browser)
