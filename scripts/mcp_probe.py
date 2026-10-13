#!/usr/bin/env python3
"""Read-only, explicit MCP transport check using Python stdlib. Not a third-party Agent certification."""
import argparse,json,os,sys,urllib.request,urllib.error
from pathlib import Path

def main(args):
 token=Path(args.key_file).read_text().strip()if args.key_file else os.environ.get(args.token_env,'')
 if not token:raise ValueError('Provide --key-file or --token-env. No token goes into the report.')
 class NoRedirect(urllib.request.HTTPRedirectHandler):
  def redirect_request(self,*a,**kw):return None
 opener=urllib.request.build_opener(NoRedirect());report={'server':args.url,'client':'XRAG stdlib protocol probe (not an external Agent)','checks':[]};counter=0
 def call(method,params=None,sse=False):
  nonlocal counter
  counter+=1
  req=urllib.request.Request(args.url,data=json.dumps({'jsonrpc':'2.0','id':counter,'method':method,'params':params or{}}).encode(),headers={'Content-Type':'application/json','Accept':'application/json, text/event-stream'if sse else'application/json','Authorization':'Bearer '+token,'MCP-Protocol-Version':'2025-11-25'})
  with opener.open(req,timeout=args.timeout)as r:
   raw=r.read(2*1024*1024+1)
   if len(raw)>2*1024*1024:raise ValueError('response exceeds probe limit')
   if r.headers.get('Content-Type','').startswith('text/event-stream'):
    values=[json.loads(line[6:])for line in raw.decode().splitlines()if line.startswith('data: ')]
    data=next(x for x in values if x.get('id')==counter)
   else:data=json.loads(raw)
  if 'error'in data:raise ValueError('MCP '+str(data['error'].get('code'))+': '+data['error'].get('message',''))
  report['checks'].append({'method':method,'requested_sse':sse,'passed':True});return data['result']
 try:
  info=call('initialize',{'protocolVersion':'2025-11-25','capabilities':{},'clientInfo':{'name':'xrag-probe','version':'1'}});report['protocol']=info['protocolVersion']
  report['tools']=[x['name']for x in call('tools/list',sse=True)['tools']]
  if 'resources'in info.get('capabilities',{}):report['resources']=[x['uri']for x in call('resources/list')['resources']];call('resources/read',{'uri':'xrag://guide/retrieval'})
  if 'prompts'in info.get('capabilities',{}):report['prompts']=[x['name']for x in call('prompts/list')['prompts']]
  report['status']='passed'
 except Exception as e:report.update(status='failed',error={'type':type(e).__name__,'message':str(e)[:300]})
 if args.report:Path(args.report).write_text(json.dumps(report,ensure_ascii=False,indent=2))
 print(json.dumps(report,ensure_ascii=False,indent=2));return 0 if report['status']=='passed'else 2
if __name__=='__main__':
 p=argparse.ArgumentParser(description=__doc__);p.add_argument('--url',required=True);g=p.add_mutually_exclusive_group(required=True);g.add_argument('--key-file');g.add_argument('--token-env');p.add_argument('--report');p.add_argument('--timeout',type=int,default=30);sys.exit(main(p.parse_args()))
