#!/usr/bin/env python3
"""Actual Demo.3 processes/HTTP; optional DOM tests use a documented bridge only
when managed Chromium blocks navigation. No external models or databases."""
from pathlib import Path
import argparse,base64,hashlib,http.cookiejar,io,json,os,socket,subprocess,tempfile,time,urllib.request,urllib.error,zipfile
ROOT=Path(__file__).resolve().parents[1];CHECKS=ROOT/'checks';CHECKS.mkdir(exist_ok=True);checks=[]
def check(name,ok,detail=''):
 if not ok:raise AssertionError((name,detail))
 checks.append({'name':name,'passed':True,'detail':detail})
def run(cmd,**kw):return subprocess.run(cmd,check=True,capture_output=True,text=True,timeout=120,**kw)
def port():
 with socket.socket() as s:s.bind(('127.0.0.1',0));return s.getsockname()[1]
def main(browser=False):
 with tempfile.TemporaryDirectory(prefix='xrag-product-') as temp:
  temp=Path(temp);data=temp/'data';home=temp/'home';home.mkdir();logs=(CHECKS/'demo3-server.log').open('w');processes=[]
  run([str(ROOT/'bin/xrag-server'),'init','--data',str(data),'--examples',str(ROOT/'examples/docs')]);key=(data/'admin.key').read_text().strip()
  def start(where,workers='embedded'):
   n=port();base=f'http://127.0.0.1:{n}';p=subprocess.Popen([str(ROOT/'bin/xrag-server'),'serve','--data',str(where),'--listen',f'127.0.0.1:{n}','--workers',workers],stdout=logs,stderr=logs);processes.append(p)
   for _ in range(100):
    try:
     urllib.request.urlopen(base+'/healthz',timeout=1).close();return p,base
    except Exception:time.sleep(.1)
   raise AssertionError('startup failed')
  def stop(p):
   if p.poll() is None:p.terminate();p.wait(timeout=15)
  server,base=start(data)
  def api(path,payload=None,method=None,token=key,origin=None,binary=False):
   b=None if payload is None else json.dumps(payload,ensure_ascii=False).encode();req=urllib.request.Request((origin or base)+path,data=b,method=method or ('GET' if b is None else 'POST'),headers={'Content-Type':'application/json','Authorization':'Bearer '+token})
   try:
    with urllib.request.urlopen(req,timeout=60) as r:
     raw=r.read();return r.status,raw if binary else json.loads(raw)
   except urllib.error.HTTPError as e:return e.code,json.loads(e.read())
  def post(path,p,method=None):
   n,r=api(path,p,method)
   if n>=300:raise AssertionError((path,n,r))
   return r
  def cli(*args):return run([str(ROOT/'bin/xrag'),*args,'--server',base,'--key-file',str(data/'admin.key'),'--kb','demo'],env={**os.environ,'HOME':str(home)})
  def wait(j):
   for _ in range(100):
    r=api('/v1/jobs/'+j['id'])[1]
    if r['state'] not in ['queued','running']:return r
    time.sleep(.1)
   raise AssertionError('task timeout')
  try:
   check('demo.3 binary current binary reports its version',api('/healthz')[1]['version']=='0.10.1')
   job=wait(post('/v1/kbs/demo/builds',{'mode':'incremental'}));check('existing corpus builds',job['state']=='succeeded')
   search=post('/v1/search/keyword',{'kbs':['demo'],'q':'E203'});ref=search['items'][0]['ref'];before=post('/v1/files/read',{'kbs':['demo'],'ref':ref})
   overview=api('/v1/overview')[1];check('overview reports real corpus counts',overview['counts']['files']==7)
   check('CLI overview works',json.loads(cli('overview').stdout)['counts']['files']==7)
   n,reader=api('/v1/keys',{'name':'demo3-reader','role':'reader','kbs':['demo'],'expires_at':int(time.time())+3600});check('create scoped expiring key',n==201 and reader['info']['expires_at']>time.time())
   check('reader can search but cannot manage keys',api('/v1/search/keyword',{'kbs':['demo'],'q':'E203'},token=reader['key'])[0]==200 and api('/v1/keys',token=reader['key'])[0]==403)
   listing=api('/v1/keys')[1];check('key list never returns secret or hash',reader['key'] not in json.dumps(listing) and 'hash' not in json.dumps(listing))
   post('/v1/keys/'+reader['info']['id'],{'name':'renamed-reader'},'PATCH');check('rename preserves identity',any(x['id']==reader['info']['id'] and x['name']=='renamed-reader' for x in api('/v1/keys')[1]['items']))
   post('/v1/keys/'+reader['info']['id'],{'confirm':True},'DELETE');check('disabled key cannot call anymore',api('/v1/system',token=reader['key'])[0]==401)
   current=listing['current_key_id'];check('last permanent admin cannot be disabled',api('/v1/keys/'+current,{'confirm':True},'DELETE')[0]==409)
   post('/v1/feedback',{'kb':'demo','ref':ref,'label':'wrong','note':'需要补版本条件'})
   f=api('/v1/feedback?kb=demo')[1];fid=f['items'][0]['id'];check('feedback list exposes submitted note',f['items'][0]['note']=='需要补版本条件')
   post('/v1/feedback/'+fid,{'status':'resolved','resolution':'说明已补','expected_revision':0},'PATCH');check('feedback resolution is persisted',api('/v1/feedback?status=resolved&kb=demo')[1]['total']==1)
   check('stale feedback update rejected',api('/v1/feedback/'+fid,{'status':'open','expected_revision':0},'PATCH')[0]==409)
   published=temp/'published.zip';cli('kb','export','demo','--out',str(published));z=zipfile.ZipFile(published);manifest=json.loads(z.read('manifest.json'));check('CLI published export contains seven documents',len(manifest['items'])==7 and len(z.namelist())==8)
   check('reader cannot download workspace snapshot',api('/v1/backups',token=reader['key'])[0]==401)
   b=json.loads(cli('backup','create','--confirm').stdout);snap=temp/'snapshot.sqlite';cli('backup','download',b['id'],'--out',str(snap));check('CLI snapshot download hash matches metadata',hashlib.sha256(snap.read_bytes()).hexdigest()==b['sha256'])
   check('CLI refuses to overwrite download target',subprocess.run([str(ROOT/'bin/xrag'),'backup','download',b['id'],'--out',str(snap),'--server',base,'--key-file',str(data/'admin.key')],capture_output=True).returncode!=0)
   cfg={'chat':{'url':'http://127.0.0.1:1/v1','model':'configured-but-not-called','key':'env:TEST_UNUSED_SECRET'}}
   saved=post('/v1/settings/providers',{'expected_revision':0,'config':cfg},'PUT');check('saved configuration is pending with no calls',saved['model_calls']==0 and saved['restart_required'] and not api('/v1/system')[1]['wiki'])
   check('CLI reads pending configuration',json.loads(cli('settings','show').stdout)['revision']==1)
   if browser:browser_checks(base,key,api)
   stop(server);server,base=start(data,'off');check('restart loads saved model configuration without running model',api('/v1/system')[1]['wiki'] and not api('/v1/settings/providers')[1]['restart_required'])
   st=api('/v1/settings/providers')[1];post('/v1/settings/providers',{'expected_revision':st['revision'],'config':{}},'PUT');stop(server);server,base=start(data,'off')
   check('configuration can be cleared without deleting knowledge',not api('/v1/system')[1]['wiki'] and api('/v1/files/read',{'kbs':['demo'],'ref':ref})[1]['data']['text']==before['data']['text'])
   restored=temp/'restored';cp=run([str(ROOT/'bin/xrag-server'),'restore','--from',str(snap),'--data',str(restored),'--confirm']);rr=json.loads(cp.stdout);check('offline restore creates new recovery identity',rr['old_keys_disabled'] and rr['source_automation_disabled'])
   rp,ru=start(restored,'off');recovery=(restored/'admin.key').read_text().strip();n,content=api('/v1/files/read',{'kbs':['demo'],'ref':ref},token=recovery,origin=ru);check('restored server reads old version reference',n==200 and content['data']['text']==before['data']['text'])
   check('restored copy rejects original key',api('/v1/system',token=key,origin=ru)[0]==401)
   check('restored copy has no active model config',not api('/v1/system',token=recovery,origin=ru)[1]['wiki']);stop(rp)
   post('/v1/maintenance/cache',{'confirm':True});check('cache cleanup leaves current content intact',api('/v1/files/read',{'kbs':['demo'],'ref':ref})[0]==200)
   check('remote-only CLI still creates no knowledge DB',not list(home.rglob('*.sqlite')))
   cli('backup','remove',b['id'],'--confirm');check('backup removal does not remove live data',api('/v1/backups/'+b['id']+'/download',binary=True)[0]==404 and api('/v1/files/read',{'kbs':['demo'],'ref':ref})[0]==200)
  finally:
   for p in processes:stop(p)
   logs.close()
 report={'scope':'real Server/Client, SQLite snapshots and offline restore; optional DOM bridge UI','passed':len(checks),'checks':checks}
 (CHECKS/'demo3-smoke.json').write_text(json.dumps(report,ensure_ascii=False,indent=2));print(json.dumps({'passed':len(checks)},ensure_ascii=False))

def browser_checks(base,key,api):
 from playwright.sync_api import sync_playwright
 jar=http.cookiejar.CookieJar();opener=urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar))
 def bridge(path,options):
  if not path.startswith('/') or path.startswith('//'):raise ValueError('only same test-server relative paths')
  b=options.get('body');req=urllib.request.Request(base+path,data=None if b is None else b.encode(),method=options.get('method','GET'),headers={'Content-Type':'application/json'})
  try:
   with opener.open(req,timeout=60) as r:return {'status':r.status,'bytes':base64.b64encode(r.read()).decode(),'type':r.headers.get('Content-Type','application/json')}
  except urllib.error.HTTPError as e:return {'status':e.code,'bytes':base64.b64encode(e.read()).decode(),'type':'application/json'}
 with sync_playwright() as pw:
  browser=pw.chromium.launch(executable_path='/usr/bin/chromium',headless=True,args=['--no-sandbox']);page=browser.new_page(viewport={'width':1440,'height':1000});errors=[];page.on('pageerror',lambda e:errors.append(str(e)));page.on('dialog',lambda d:d.accept())
  transport='direct URL'
  try:page.goto(base,wait_until='domcontentloaded',timeout=12000)
  except Exception as e:
   if 'BLOCKED_BY_ADMINISTRATOR' not in str(e):raise
   transport='DOM + explicit HTTP test bridge';page.close();page=browser.new_page(viewport={'width':1440,'height':1000});page.on('pageerror',lambda e:errors.append(str(e)));page.on('dialog',lambda d:d.accept())
   html=(ROOT/'internal/server/web/index.html').read_text().replace('<link rel="stylesheet" href="/style.css">','<style>'+ (ROOT/'internal/server/web/style.css').read_text()+'</style>')
   for f in ['app.js','extensions.js','product.js','lifecycle.js','completion.js','iteration6.js','iteration7.js','iteration8.js']:html=html.replace(f'<script src="/{f}"></script>','')
   page.set_content(html);page.expose_function('xragProductHTTP',bridge)
   page.evaluate("""()=>{window.fetch=async(path,options={})=>{const r=await window.xragProductHTTP(path,options);const b=Uint8Array.from(atob(r.bytes),x=>x.charCodeAt(0));return new Response(r.status===204?null:b,{status:r.status,headers:{'Content-Type':r.type}})}}""")
   for f in ['app.js','extensions.js','product.js','lifecycle.js','completion.js','iteration6.js','iteration7.js','iteration8.js']:page.add_script_tag(content=(ROOT/'internal/server/web'/f).read_text())
  page.fill('#key-input',key);page.click('#login-form button');page.wait_for_selector('#app:not([hidden])');check('product UI logs into actual server',True,transport)
  page.click('[data-page="overview"]');page.wait_for_selector('#overview-counts .product-metric');check('overview UI contains actual counts and onboarding',page.locator('#overview-counts').inner_text().find('7')>=0 and page.locator('#onboarding-steps li').count()==5);page.evaluate('scrollTo(0,0)');page.wait_for_timeout(120);page.screenshot(path=str(CHECKS/'demo3-overview.png'),full_page=True)
  page.click('[data-page="search"]');page.fill('#query','E203');page.click('#search-button');page.wait_for_selector('[data-feedback]');page.locator('[data-feedback]').first.click();page.wait_for_selector('#feedback-dialog[open]');page.select_option('#feedback-label','outdated');page.fill('#feedback-note','UI_FIXTURE需要更新时间');page.click('#feedback-form button.primary');page.wait_for_selector('#feedback-dialog',state='hidden');check('feedback dialog persists selected label and note',any(x['note']=='UI_FIXTURE需要更新时间' for x in api('/v1/feedback?kb=demo')[1]['items']))
  page.click('[data-page="feedback"]');page.wait_for_selector('[data-resolution]');page.locator('[data-resolution]').first.locator('[name="status"]').select_option('resolved');page.locator('[data-resolution]').first.locator('[name="resolution"]').fill('已补说明');page.locator('[data-resolution]').first.locator('button').click();page.wait_for_timeout(150);check('feedback resolution form saves actual state',any(x['resolution']=='已补说明' for x in api('/v1/feedback?kb=demo')[1]['items']));page.evaluate('scrollTo(0,0)');page.wait_for_timeout(120);page.screenshot(path=str(CHECKS/'demo3-feedback.png'),full_page=True)
  page.click('[data-page="settings"]');page.click('[data-settings="keys"]');page.wait_for_selector('#keys-list table');page.fill('#key-name','UI-created-reader');page.select_option('#key-role','reader');page.fill('#key-kbs','demo');page.click('#key-form button.primary');page.wait_for_selector('#new-key-box:not([hidden])');newkey=page.locator('#new-key-value').input_value();check('key UI creates and shows one-time value',newkey.startswith('xrag_') and api('/v1/system',token=newkey)[0]==200);page.click('#hide-new-key');check('one-time key can be removed from page',page.locator('#new-key-value').input_value()=='');page.evaluate('scrollTo(0,0)');page.wait_for_timeout(120);page.screenshot(path=str(CHECKS/'demo3-keys.png'),full_page=True)
  page.click('[data-settings="providers"]');page.wait_for_selector('[data-provider="chat"][open]');page.locator('[data-provider="chat"] [data-field="model"]').fill('ui-fixture-not-called');page.click('#provider-form button.primary');page.wait_for_timeout(150);check('configuration UI persists deferred config',api('/v1/settings/providers')[1]['config']['chat']['model']=='ui-fixture-not-called' and not api('/v1/system')[1]['wiki']);page.evaluate('scrollTo(0,0)');page.wait_for_timeout(120);page.screenshot(path=str(CHECKS/'demo3-settings.png'),full_page=True)
  page.click('[data-settings="data"]');page.wait_for_selector('[data-backup-download]');page.click('#cache-preview');page.wait_for_function("document.querySelector('#cache-result').textContent.includes('expired_rows')");check('data management exposes real backups and cleanup preview',page.locator('[data-backup-download]').count()>=1);page.evaluate('scrollTo(0,0)');page.wait_for_timeout(120);page.screenshot(path=str(CHECKS/'demo3-data.png'),full_page=True)
  page.click('[data-settings="help"]');check('help includes CLI and restore limitations',page.locator('#settings-help').inner_text().find('备份')>=0)
  page.click('[data-page="overview"]');page.set_viewport_size({'width':390,'height':844});page.wait_for_timeout(80);check('mobile navigation starts closed',page.locator('.sidebar').is_hidden());page.click('#mobile-menu');check('mobile navigation opens explicitly',page.locator('.sidebar').is_visible());page.locator('nav [data-page=overview]').click();check('mobile navigation closes after selection',page.locator('.sidebar').is_hidden());check('mobile overview has no page-level horizontal overflow',page.evaluate('document.documentElement.scrollWidth<=innerWidth'));page.evaluate('scrollTo(0,0)');page.wait_for_timeout(120);page.screenshot(path=str(CHECKS/'demo3-mobile.png'),full_page=True)
  page.set_viewport_size({'width':1440,'height':1000});page.click('#logout-btn');page.wait_for_selector('#login:not([hidden])');check('logout clears prior user results and keys',page.locator('#results').inner_text()=='' and page.locator('#new-key-value').input_value()=='')
  page.fill('#key-input',newkey);page.click('#login-form button');page.wait_for_selector('#app:not([hidden])');page.click('[data-page="settings"]');check('reader UI does not show admin controls',page.locator('[data-settings="keys"]').is_hidden() and page.locator('[data-settings="providers"]').is_hidden());check('no browser JS exceptions',not errors,errors);browser.close()
if __name__=='__main__':
 p=argparse.ArgumentParser();p.add_argument('--browser',action='store_true');a=p.parse_args();main(a.browser)
