#!/usr/bin/env python3
"""Write documentation schemas for demo.4 lifecycle and remote-source requests.
Go operations implement state, identity and cross-field constraints separately.
"""
from pathlib import Path
import json
ROOT=Path(__file__).resolve().parents[1]
DEST=ROOT/'contracts/management'
def obj(props, required=()):
 return {'type':'object','properties':props,'required':list(required),'additionalProperties':False}
def text(n=1): return {'type':'string','minLength':n}
def save(name, schema):
 (DEST/(name+'.schema.json')).write_text(json.dumps({'$schema':'https://json-schema.org/draft/2020-12/schema',**schema},ensure_ascii=False,indent=2)+'\n')
secret={'type':'string','pattern':'^(env|file):.+$'}
remote=obj({'region':text(),'bucket':text(),'prefix':{'type':'string'},'access_key':secret,'secret_key':secret,'session_token':secret,'anonymous':{'type':'boolean'},'knowledge_base':text(),'api_key':secret,'auth_header':{'enum':['X-API-Key','Authorization']}})
src=obj({'id':text(),'kb':text(),'root':text(),'mount':text(),'kind':{'enum':['path','builtin','git','s3','weknora']},'git_ref':text(),'remote':remote},['id','root','mount','kind'])
src['allOf']=[{'if':{'properties':{'kind':{'enum':['s3','weknora']}}},'then':{'required':['remote']}},{'if':{'properties':{'kind':{'const':'weknora'}}},'then':{'properties':{'remote':{'required':['knowledge_base','api_key']}}}},{'if':{'properties':{'kind':{'const':'s3'}}},'then':{'properties':{'remote':{'required':['bucket']}}}}]
save('source-create',src)
target=obj({'id':text(),'kb':text(),'name':{'type':'string'},'kind':{'enum':['path','git']},'root':text(),'prefix':text(),'branch':{'type':'string','pattern':'^xrag/.+'}},['id','kind','root'])
save('sync-target-create',obj({'target':target,'confirm':{'const':True}},['target','confirm']))
save('knowledge-sync',obj({'target_id':text(),'revision':text(),'confirm':{'const':True}},['target_id','revision','confirm']))
save('knowledge-archive',obj({'revision':text(),'confirm':{'const':True}},['revision','confirm']))
wiki={'topic':{'type':'string','minLength':1},'item_id':text(),'expected_revision':text(),'path':{'type':'string'},'single_pass':{'type':'boolean'}}
save('wiki-generate',obj(wiki,['topic']))
save('wiki-regenerate',obj(wiki,['expected_revision']))
routes=[
 {'path':'/v1/kbs/{kb}/sync-targets','methods':['GET','POST'],'input':'sync-target-create.schema.json','role':'Maintainer','note':'registration only; no automatic writeback'},
 {'path':'/v1/knowledge/{id}/revisions','methods':['GET'],'role':'Contributor','note':'immutable revisions; not exposed as public search'},
 {'path':'/v1/knowledge/{id}/regenerate','methods':['POST'],'input':'wiki-regenerate.schema.json','role':'Maintainer','note':'202 Job; current publication unchanged'},
 {'path':'/v1/knowledge/{id}/sync','methods':['GET','POST'],'input':'knowledge-sync.schema.json','role':'Maintainer','note':'GET latest receipts per target, POST explicit current published revision'},
 {'path':'/v1/knowledge/{id}/archive','methods':['POST'],'input':'knowledge-archive.schema.json','role':'Maintainer','note':'remove current publication, preserve allowed old references'},
 {'path':'/v1/kbs/{kb}/wiki','methods':['POST'],'input':'wiki-generate.schema.json','role':'Maintainer','note':'existing route now supports optional path and single_pass'},
 {'path':'/v1/kbs/{kb}/sources','methods':['POST'],'input':'source-create.schema.json','role':'Maintainer','note':'existing route adds S3-compatible and WeKnora parsed-text export'}]
(DEST/'lifecycle-routes.json').write_text(json.dumps(routes,ensure_ascii=False,indent=2)+'\n')
examples={
 'source-s3':{'id':'object-docs','kind':'s3','root':'http://127.0.0.1:9000','mount':'objects','remote':{'region':'us-east-1','bucket':'docs-bucket','prefix':'manuals/','access_key':'env:S3_ACCESS_KEY','secret_key':'env:S3_SECRET_KEY'}},
 'source-weknora':{'id':'existing-knowledge','kind':'weknora','root':'http://127.0.0.1:8080','mount':'imported','remote':{'knowledge_base':'REPLACE_WITH_KB_ID','api_key':'env:WEKNORA_API_KEY','auth_header':'X-API-Key'}},
 'sync-target-path':{'target':{'id':'docs-out','kind':'path','root':'/REPLACE/WITH/EXISTING/DIRECTORY','prefix':'xrag-knowledge'},'confirm':True},
 'sync-target-git':{'target':{'id':'git-out','kind':'git','root':'/REPLACE/WITH/LOCAL/GIT/REPOSITORY','prefix':'xrag-knowledge','branch':'xrag/knowledge'},'confirm':True},
 'knowledge-sync':{'target_id':'docs-out','revision':'REPLACE_WITH_CURRENT_PUBLISHED_REVISION','confirm':True},
 'knowledge-archive':{'revision':'REPLACE_WITH_CURRENT_PUBLISHED_REVISION','confirm':True},
 'wiki-regenerate':{'expected_revision':'REPLACE_WITH_CURRENT_DRAFT_REVISION','path':'manuals/'},
 'wiki-generate':{'topic':'日志问题说明','path':'manuals/','single_pass':False}}
for name,data in examples.items():
 (ROOT/'examples'/(name+'.json')).write_text(json.dumps(data,ensure_ascii=False,indent=2)+'\n')
