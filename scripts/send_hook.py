#!/usr/bin/env python3
"""Send one signed XRAG Hook v1. Python stdlib only. Does not send API keys.
Example: python3 scripts/send_hook.py --url http://127.0.0.1:8765/v1/hooks/demo/project \
 --secret-env XRAG_HOOK_SECRET --event commit-123 --reason update
"""
import argparse, hashlib, hmac, json, os, sys, time, uuid
from pathlib import Path
from urllib.parse import urlsplit
from urllib.request import Request,urlopen
from urllib.error import HTTPError,URLError

def main():
 p=argparse.ArgumentParser(description=__doc__)
 p.add_argument('--url',required=True)
 g=p.add_mutually_exclusive_group(required=True);g.add_argument('--secret-env');g.add_argument('--secret-file',type=Path)
 p.add_argument('--event',default=None);p.add_argument('--reason',default='external change notification');p.add_argument('--timeout',type=float,default=20)
 a=p.parse_args();url=urlsplit(a.url)
 if url.scheme not in ('http','https') or not url.netloc or url.username or url.password or not url.path.startswith('/v1/hooks/'):
  p.error('--url must name the intended XRAG /v1/hooks/{kb}/{source} endpoint without URL credentials')
 try:secret=os.environ[a.secret_env] if a.secret_env else a.secret_file.expanduser().read_text().strip()
 except (KeyError,OSError) as e:p.error('Cannot resolve the explicitly selected secret: '+str(e))
 if len(secret)<32:p.error('Secret must have at least 32 characters')
 event=a.event or str(uuid.uuid4())
 if not event or len(event)>128 or '\n' in event or '\r' in event:p.error('Invalid event id')
 body=json.dumps({'reason':a.reason},ensure_ascii=False,separators=(',',':')).encode()
 if len(body)>64*1024:p.error('Payload exceeds 64 KiB')
 stamp=str(int(time.time()));sig=hmac.new(secret.encode(),stamp.encode()+b'\n'+event.encode()+b'\n'+body,hashlib.sha256).hexdigest()
 headers={'Content-Type':'application/json','X-XRAG-Timestamp':stamp,'X-XRAG-Event':event,'X-XRAG-Signature':'sha256='+sig}
 try:
  with urlopen(Request(a.url,data=body,headers=headers),timeout=a.timeout) as r:
   print(json.dumps({'event':event,'http_status':r.status,'response':json.load(r)},ensure_ascii=False,indent=2));return 0
 except HTTPError as e:print(e.read(64*1024).decode(errors='replace'),file=sys.stderr);return 1
 except (URLError,TimeoutError,ValueError) as e:print(str(e),file=sys.stderr);return 1
if __name__=='__main__':sys.exit(main())
