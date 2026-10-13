#!/usr/bin/env python3
"""Example source-annotation parser, not an LLM or automatic knowledge extractor."""
import json, re, sys
try:
    request=json.loads(sys.stdin.readline())
    relations=[]
    for line in request['text'].splitlines():
        match=re.fullmatch(r'关系[:：]\s*([^|]+)\|\s*([^|]+)\|\s*([^|]+)',line)
        if match:
            a,rel,b=(s.strip() for s in match.groups())
            relations.append(dict(from_=a,to=b,rel=rel,evidence=line))
    relations=[{('from' if k=='from_' else k):v for k,v in row.items()} for row in relations]
    print(json.dumps({'id':request['id'],'result':{'relations':relations}},ensure_ascii=False))
except Exception as exc:
    print(json.dumps({'id':locals().get('request',{}).get('id'),'error':str(exc)},ensure_ascii=False))
    sys.exit(1)
