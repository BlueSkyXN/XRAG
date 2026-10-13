#!/usr/bin/env python3
"""Generate documented management request schemas. Business validations stay in Go."""
from pathlib import Path
import json
root=Path(__file__).resolve().parents[1]/'contracts/management';root.mkdir(exist_ok=True)
def write(name,schema):
 schema={'$schema':'https://json-schema.org/draft/2020-12/schema',**schema}
 (root/(name+'.schema.json')).write_text(json.dumps(schema,ensure_ascii=False,indent=2)+'\n')
obj=lambda props,req=[]:{'type':'object','properties':props,'required':req,'additionalProperties':False}
write('source-policy',obj({'every_seconds':{'type':'integer','minimum':0,'maximum':604800},'watch_seconds':{'type':'integer','minimum':0,'maximum':3600},'debounce_seconds':{'type':'integer','minimum':0,'maximum':300},'hook_secret_ref':{'type':'string'},'hook_provider':{'type':'string','enum':['custom','github','gitlab','gitea']},'hook_repository':{'type':'string','maxLength':250},'hook_ref':{'type':'string','maxLength':256} }))
expected=obj({'path':{'type':'string','minLength':1},'anchor':{'type':'string','maxLength':2000}},['path'])
case=obj({'id':{'type':'string','minLength':1},'question':{'type':'string','minLength':1,'maxLength':2000},'expected':{'type':'array','maxItems':20,'items':expected},'expected_empty':{'type':'boolean'}},['id','question'])
case['oneOf']=[{'required':['expected'],'properties':{'expected':{'minItems':1},'expected_empty':{'const':False}}},{'required':['expected_empty'],'properties':{'expected_empty':{'const':True},'expected':{'maxItems':0}}}]
write('eval-set',obj({'id':{'type':'string'},'kb':{'type':'string','minLength':1},'name':{'type':'string','minLength':1},'revision':{'type':'string'},'family':{'type':'string'},'purpose':{'enum':['development','holdout']},'cases':{'type':'array','minItems':1,'maxItems':100,'items':case}},['kb','name','cases']))
write('eval-run',obj({'set_id':{'type':'string','minLength':1},'strategies':{'type':'array','minItems':1,'maxItems':5,'uniqueItems':True,'items':{'enum':['keyword','semantic','ask','ask_graph','ask_rerank']}},'k':{'type':'integer','minimum':1,'maximum':50}},['set_id']))
write('hook',obj({'reason':{'type':'string'}}))
management=[
 {'path':'/v1/kbs/{kb}/sources','methods':['GET','POST'],'note':'GET items保持兼容，新增details含policy/status；POST可kind=git并带git_ref'},
 {'path':'/v1/kbs/{kb}/sources/{source}/policy','methods':['GET','PUT'],'input':'source-policy.schema.json'},
 {'path':'/v1/hooks/{kb}/{source}','methods':['POST'],'input':'hook.schema.json','auth':'custom HMAC v1; not XRAG Bearer key'},
 {'path':'/v1/jobs/{id}/details','methods':['GET'],'note':'files/triggers/usage，加limits与truncated'},
 {'path':'/v1/queries','methods':['GET'],'note':'有界历史摘要'},
 {'path':'/v1/queries/{id}','methods':['GET'],'note':'历史过程，不重新执行'},
 {'path':'/v1/eval/sets','methods':['GET','POST'],'input':'eval-set.schema.json'},
 {'path':'/v1/eval/runs','methods':['POST'],'input':'eval-run.schema.json','note':'202 Job；用原有 /v1/jobs/{id} 查询和取消'}]
(root/'registry.json').write_text(json.dumps(management,ensure_ascii=False,indent=2)+'\n')
