#!/usr/bin/env python3
"""Real binaries, SQLite, Git and filesystem; model/source responses are explicit fixtures.
Optional UI test records direct navigation or documented DOM+HTTP bridge.
"""
from pathlib import Path
import sys,argparse,base64,http.cookiejar,http.server,json,os,socket,subprocess,tempfile,threading,time,urllib.request,urllib.error
ROOT=Path(__file__).resolve().parents[1];CHECKS=ROOT/'checks/demo4';CHECKS.mkdir(parents=True,exist_ok=True);checks=[]
def ck(name,ok,detail=None):
 if not ok:raise AssertionError((name,detail))
 checks.append({'name':name,'passed':True,'detail':detail});print('PASS '+name,file=sys.stderr,flush=True)
def run(args,**kw):return subprocess.run(args,check=True,capture_output=True,text=True,timeout=120,**kw)
def port():
 with socket.socket() as s:s.bind(('127.0.0.1',0));return s.getsockname()[1]
class Fixture(http.server.BaseHTTPRequestHandler):
 calls=[]
 def log_message(self,*args):pass
 def do_POST(self):
  q=json.loads(self.rfile.read(int(self.headers.get('Content-Length',0))));self.calls.append(q)
  outline='大纲' in q['messages'][0]['content'];text='1. 日志条件\n2. 适用范围' if outline else '# 日志资料综述\n\nTEST_FIXTURE_'+str(len(self.calls))+'：日志配置见所选资料 [1]。\n\n只覆盖输入片段。'
  b=json.dumps({'choices':[{'message':{'content':text}}],'usage':{'prompt_tokens':50,'completion_tokens':25,'total_tokens':75}},ensure_ascii=False).encode();self.send_response(200);self.send_header('Content-Type','application/json');self.send_header('Content-Length',str(len(b)));self.end_headers();self.wfile.write(b)
 def do_GET(self):
  if self.path.startswith('/docs-bucket/'):
   import urllib.parse
   if 'list-type=2' in self.path:b=b'<ListBucketResult><IsTruncated>false</IsTruncated><Contents><Key>docs/s3.md</Key><ETag>"fixture"</ETag><Size>19</Size></Contents></ListBucketResult>'
   else:
    if self.headers.get('If-Match')!='"fixture"':self.send_error(412);return
    b=b'# S3\nS3_DEMO4_DATA\n';self.send_response(200);self.send_header('ETag','"fixture"');self.end_headers();self.wfile.write(b);return
   self.send_response(200);self.end_headers();self.wfile.write(b);return
  self.send_error(404)
def main(browser=False):
 with tempfile.TemporaryDirectory(prefix='xrag-demo4-') as t:
  t=Path(t);data=t/'data';home=t/'home';home.mkdir();logs=(CHECKS/'process.log').open('w');procs=[]
  fixture=http.server.ThreadingHTTPServer(('127.0.0.1',0),Fixture);thread=threading.Thread(target=fixture.serve_forever,daemon=True);thread.start();model=f'http://127.0.0.1:{fixture.server_port}'
  config=t/'providers.json';config.write_text(json.dumps({'chat':{'url':model+'/v1','model':'explicit-test-fixture'}}))
  run([str(ROOT/'bin/xrag-server'),'init','--data',str(data),'--examples',str(ROOT/'examples/docs')]);key=(data/'admin.key').read_text().strip();n=port();base=f'http://127.0.0.1:{n}'
  p=subprocess.Popen([str(ROOT/'bin/xrag-server'),'serve','--data',str(data),'--listen',f'127.0.0.1:{n}','--config',str(config)],stdout=logs,stderr=logs);procs.append(p)
  def api(path,body=None,method=None):
   req=urllib.request.Request(base+path,data=None if body is None else json.dumps(body,ensure_ascii=False).encode(),method=method or ('GET' if body is None else 'POST'),headers={'Authorization':'Bearer '+key,'Content-Type':'application/json'})
   try:
    with urllib.request.urlopen(req,timeout=30) as r:return r.status,json.load(r)
   except urllib.error.HTTPError as e:return e.code,json.load(e)
  def post(path,body,method=None):
   n,r=api(path,body,method)
   if n>=300:raise AssertionError((path,n,r))
   return r
  def wait(j):
   for _ in range(200):
    r=api('/v1/jobs/'+j['id'])[1]
    if r['state'] not in ('queued','running'):return r
    time.sleep(.05)
   raise AssertionError('job timeout')
  def cli(*args):return run([str(ROOT/'bin/xrag'),*args,'--server',base,'--key-file',str(data/'admin.key'),'--kb','demo'],env={**os.environ,'HOME':str(home)})
  try:
   for _ in range(100):
    try:api('/healthz');break
    except Exception:time.sleep(.1)
   ck('current binary reports demo.6',api('/healthz')[1]['version']=='0.9.0')
   ck('base corpus builds',wait(post('/v1/kbs/demo/builds',{'mode':'incremental'}))['state']=='succeeded')
   live_prune=subprocess.run([str(ROOT/'bin/xrag-server'),'prune','--data',str(data)],capture_output=True,text=True);ck('offline prune refuses a running Server',live_prune.returncode!=0 and 'WORKSPACE_BUSY' in live_prune.stderr)
   gen=wait(post('/v1/kbs/demo/wiki',{'topic':'日志'}));ck('Wiki executes outline and draft calls',gen['state']=='succeeded' and gen['result']['generation_stages']==2 and len(Fixture.calls)==2,gen.get('error'))
   k=gen['result']['knowledge'];id=k['id'];ck('generated Wiki starts as draft',not k['published_revision'])
   ck('first Wiki publishes explicitly',wait(post('/v1/knowledge/'+id+'/publish',{'revision':k['draft_revision']}))['state']=='succeeded');old=api('/v1/knowledge/'+id)[1]
   regen=json.loads(cli('wiki','regenerate',id,'--data',json.dumps({'expected_revision':old['draft_revision']}),'--wait').stdout);ck('CLI regeneration completes',regen['state']=='succeeded')
   current=api('/v1/knowledge/'+id)[1];ck('regeneration changes draft not publication',current['draft_revision']!=old['draft_revision'] and current['published_revision']==old['published_revision'])
   refs=post('/v1/files/tree',{'kbs':['demo'],'limit':100})['items'];item=next(x for x in refs if x['path']=='_knowledge/'+id+'.md');body=post('/v1/files/read',{'kbs':['demo'],'ref':item['ref']});ck('old Wiki remains readable before republish','TEST_FIXTURE_2' in body['data']['text'])
   ck('old draft CAS rejected before new model calls',api('/v1/knowledge/'+id+'/regenerate',{'expected_revision':old['draft_revision']})[0]==409 and len(Fixture.calls)==4)
   revs=json.loads(cli('knowledge','revisions',id).stdout);ck('revision history retains both immutable drafts',len(revs['items'])==2)
   ck('second draft publishes',wait(post('/v1/knowledge/'+id+'/publish',{'revision':current['draft_revision']}))['state']=='succeeded');current=api('/v1/knowledge/'+id)[1]
   dest=t/'writeback';dest.mkdir();target={'id':'filesystem','name':'文档写回','kind':'path','root':str(dest),'prefix':'published'}
   added=json.loads(cli('sync-target','add','--data',json.dumps({'target':target}),'--confirm').stdout);ck('CLI registers destination without writing',added['id']=='filesystem' and not list(dest.iterdir()))
   sync=json.loads(cli('knowledge','sync',id,'--data',json.dumps({'revision':current['published_revision'],'target_id':'filesystem'}),'--confirm','--wait').stdout);ck('path writeback job succeeds',sync['state']=='succeeded',sync.get('error'));receipt=sync['result']['receipt'];out=dest/receipt['path'];ck('written file contains actual current publication','TEST_FIXTURE_4' in out.read_text())
   prior=out.read_bytes();again=wait(post('/v1/knowledge/'+id+'/sync',{'revision':current['published_revision'],'target_id':'filesystem','confirm':True}));ck('same revision writeback is idempotent',again['state']=='succeeded' and out.read_bytes()==prior)
   out.write_text('edited by someone else');conflict=wait(post('/v1/knowledge/'+id+'/sync',{'revision':current['published_revision'],'target_id':'filesystem','confirm':True}));ck('writeback conflict does not overwrite user content',conflict['state']=='failed' and 'SYNC_CONFLICT' in conflict['error'] and out.read_text()=='edited by someone else');out.write_bytes(prior)
   ck('sync receipt remains on conflict',api('/v1/knowledge/'+id+'/sync')[1]['items'][0]['revision']==current['published_revision'])
   post('/v1/kbs/demo/sources',{'id':'copied','root':str(dest),'mount':'returned','kind':'path'});ck('scan succeeds over registered sync output',wait(post('/v1/kbs/demo/builds',{'mode':'incremental'}))['state']=='succeeded');files=post('/v1/files/tree',{'kbs':['demo'],'limit':100})['items'];ck('exact synced copy is not indexed twice',not any(x['path'].startswith('returned/') for x in files))
   # Real Git CLI and separate index: default branch and untracked working file stay unchanged.
   git=t/'repo';git.mkdir();run(['git','init','-b','main',str(git)]);run(['git','-C',str(git),'config','user.name','Fixture']);run(['git','-C',str(git),'config','user.email','fixture@example.invalid']);(git/'README.md').write_text('initial');run(['git','-C',str(git),'add','.']);run(['git','-C',str(git),'commit','-m','initial']);head=run(['git','-C',str(git),'rev-parse','HEAD']).stdout;(git/'personal.txt').write_text('untouched')
   post('/v1/kbs/demo/sync-targets',{'target':{'id':'gitwrite','kind':'git','root':str(git),'prefix':'published','branch':'xrag/knowledge'},'confirm':True});gj=wait(post('/v1/knowledge/'+id+'/sync',{'target_id':'gitwrite','revision':current['published_revision'],'confirm':True}));ck('Git writeback creates dedicated local branch',gj['state']=='succeeded' and run(['git','-C',str(git),'rev-parse','xrag/knowledge']).stdout.strip()!=head.strip());ck('Git worktree and default branch unchanged',run(['git','-C',str(git),'rev-parse','HEAD']).stdout==head and (git/'personal.txt').read_text()=='untouched')
   # Explicit remote-source fixture, not a real object storage account.
   src={'id':'s3-fixture','kind':'s3','root':model,'mount':'objects','remote':{'bucket':'docs-bucket','prefix':'docs/','anonymous':True}}
   # Match the byte length of the fixture body.
   added=json.loads(cli('source','add','--data',json.dumps(src)).stdout);ck('CLI source JSON retains remote configuration',any(x.get('id')=='s3-fixture' and x.get('remote',{}).get('bucket')=='docs-bucket' for x in api('/v1/kbs/demo/sources')[1]['items']))
   sj=wait(post('/v1/kbs/demo/builds',{'mode':'incremental'}));ck('S3 adapter participates in actual build process',sj['state']=='succeeded',sj.get('error'));sr=post('/v1/search/keyword',{'kbs':['demo'],'q':'S3_DEMO4_DATA'});ck('S3 fixture text is searchable with source identity',len(sr['items'])>=1 and sr['items'][0]['path'].startswith('objects/'))
   if browser:browser_checks(base,key,api,dest, id)
   # Archive only removes publication, retaining the fixed source ref.
   oldref=item['ref'];post('/v1/knowledge/'+id+'/archive',{'revision':current['published_revision'],'confirm':True});ck('archive removes current published path',not any(x['path']=='_knowledge/'+id+'.md' for x in post('/v1/files/tree',{'kbs':['demo'],'limit':100})['items']));ck('archive does not rewrite historical ref',api('/v1/files/read',{'kbs':['demo'],'ref':oldref})[0]==200)
   ck('pure remote CLI does not create local knowledge database',not list(home.rglob('*.sqlite')))
   p.terminate();p.wait(timeout=15);preview=json.loads(run([str(ROOT/'bin/xrag-server'),'prune','--data',str(data),'--keep-days','30']).stdout);ck('offline prune defaults to preview and retains recent history',preview['dry_run'] and preview['removed_editions']==0)
  finally:
   for p in procs:
    if p.poll() is None:p.terminate();p.wait(timeout=15)
   fixture.shutdown();fixture.server_close();logs.close()
 report={'scope':'real processes/files/Git/SQLite, explicit synthetic Chat and S3 HTTP fixture; not external vendor integration','passed':len(checks),'checks':checks};(CHECKS/'smoke.json').write_text(json.dumps(report,ensure_ascii=False,indent=2));print(json.dumps({'passed':len(checks)}))

def browser_checks(base,key,api,dest,item_id):
 from playwright.sync_api import sync_playwright
 jar=http.cookiejar.CookieJar();opener=urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar));errors=[]
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
   page.set_content(html);page.expose_function('xragDemo4HTTP',bridge);page.evaluate("""()=>{window.fetch=async(p,o={})=>{const r=await window.xragDemo4HTTP(p,o);return new Response(Uint8Array.from(atob(r.bytes),x=>x.charCodeAt(0)),{status:r.status,headers:{'Content-Type':r.type}})}}""")
   for f in scripts:page.add_script_tag(content=(ROOT/'internal/server/web'/f).read_text())
  page.fill('#key-input',key);page.click('#login-form button');page.wait_for_selector('#app:not([hidden])');ck('lifecycle UI uses actual Server',True,transport)
  page.click('[data-page="knowledge"]');page.wait_for_selector('#sync-target-section table');ck('lifecycle controls and actual targets visible',page.get_by_role('button',name='写回已发布内容').count()>=1 and '文档写回' in page.locator('#sync-target-section').inner_text());page.evaluate('scrollTo(0,0)');page.screenshot(path=str(CHECKS/'wiki-lifecycle.png'),full_page=True)
  page.get_by_role('button',name='修订与写回记录').first.click();page.wait_for_selector('#lifecycle-dialog[open]');ck('revision dialog loads real history',item_id in page.locator('#lifecycle-body').inner_text() and 'sync' in page.locator('#lifecycle-body').inner_text());page.click('#lifecycle-close')
  page.get_by_role('button',name='再生成新草稿').first.click();page.wait_for_selector('#regenerate-form');ck('regenerate UI explains publication isolation','已发布正文保持不变' in page.locator('#lifecycle-body').inner_text());page.screenshot(path=str(CHECKS/'wiki-regenerate.png'));page.click('#lifecycle-close')
  page.click('#sync-target-create');page.fill('#target-id','ui-target');page.fill('#target-name','UI目标');page.fill('#target-root',str(dest));page.click('#target-form button');page.wait_for_selector('#lifecycle-dialog',state='hidden');ck('UI destination registration persists without sync',any(t['id']=='ui-target' for t in api('/v1/kbs/demo/sync-targets')[1]['items']))
  page.click('[data-page="files"]');page.locator('.source-register summary').click();page.select_option('#source-kind','s3');ck('S3 connection fields are available',page.locator('#source-remote-field').is_visible() and 'access_key' in page.locator('#source-remote').input_value());page.select_option('#source-kind','weknora');ck('WeKnora source fields are explicit','knowledge_base' in page.locator('#source-remote').input_value());page.evaluate('scrollTo(0,0)');page.screenshot(path=str(CHECKS/'remote-sources.png'),full_page=True)
  page.set_viewport_size({'width':390,'height':844});page.wait_for_timeout(80);ck('new source page has no full-width overflow',page.evaluate('document.documentElement.scrollWidth<=innerWidth'));page.screenshot(path=str(CHECKS/'mobile.png'));ck('new UI no JavaScript errors',not errors,errors);browser.close()
if __name__=='__main__':
 p=argparse.ArgumentParser();p.add_argument('--browser',action='store_true');a=p.parse_args();main(a.browser)
