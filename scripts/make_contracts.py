import json,pathlib
root=pathlib.Path(__file__).resolve().parents[1]/'contracts'
props={
 'kbs':{'type':'array','items':{'type':'string','minLength':1},'minItems':1,'maxItems':50},
 'q':{'type':'string','minLength':1,'maxLength':1000},'name':{'type':'string','minLength':1},'id':{'type':'string','minLength':1},
 'entity_id':{'type':'string','minLength':1},'from_id':{'type':'string','minLength':1},'to_id':{'type':'string','minLength':1},
 'within':{'type':'string','minLength':1},'limit':{'type':'integer','minimum':1,'maximum':100},'max_chars':{'type':'integer','minimum':1,'maximum':200000},
 'per_file':{'type':'integer','minimum':1,'maximum':100},'trace':{'type':'boolean'},'graph':{'type':'boolean'},'rerank':{'type':'boolean'},'external':{'type':'boolean'},'require_complete':{'type':'boolean'},
 'save_scope':{'type':'string','enum':['auto','required','none']},'direction':{'type':'string','enum':['in','out','both']},
 'max_hops':{'type':'integer','minimum':1,'maximum':5},'hops':{'type':'integer','minimum':1,'maximum':5},'max_expansions':{'type':'integer','minimum':1,'maximum':100000},
 'filters':{'type':'array','maxItems':16,'items':{'type':'object','properties':{'field':{'type':'string'},'op':{'type':'string','enum':['eq','in','prefix']},'value':{}},'required':['field','op','value'],'additionalProperties':False}},
 'path':{'type':'string','minLength':1},'ref':{'type':'string','minLength':1},'offset':{'type':'integer','minimum':0},'length':{'type':'integer','minimum':1,'maximum':100000},
 'pattern':{'type':'string','minLength':1,'maxLength':512},'result_id':{'type':'string','minLength':1},'cursor':{'type':'string','minLength':1}}
common='kbs within filters limit max_chars save_scope trace require_complete'.split()
actions=[
 ('kb.discover','/v1/discover','kb discover','kb_discover','core','查看实际配置和资料能力，不调用模型',['kbs'],[]),
 ('search.keyword','/v1/search/keyword','search keyword','search_keyword','core','关键词与编号检索；total 是文本块数，不是业务实体数',common+['q'],['q']),
 ('search.semantic','/v1/search/semantic','search semantic','search_semantic','core','使用已配置模型；无模型时不可用，Top K 没有总数',common+['q'],['q']),
 ('files.tree','/v1/files/tree','files tree','files_tree','files','列出已发布文件，支持保存范围',common,[]),
 ('files.grep','/v1/files/grep','files grep','files_grep','files','RE2 文本匹配，返回命中文本块',common+['pattern'],['pattern']),
 ('files.read','/v1/files/read','files read','files_read','core','按 ref 读取固定版本；path 读取当前发布文件',['kbs','path','ref','offset','length'],[]),
 ('graph.entity','/v1/graph/entity','graph entity','graph_entity','graph','按名称查实体；同名不等于已消歧',common+['name'],[]),
 ('graph.neighbors','/v1/graph/neighbors','graph neighbors','graph_neighbors','core','沿关系查邻居；within 同时限制关系出处',common+['entity_id','name','direction','hops','max_expansions'],[]),
 ('graph.path','/v1/graph/path','graph path','graph_path','graph','有界路径；保留实际方向，连通不是因果',common+['from_id','to_id','direction','max_hops','max_expansions'],['from_id','to_id']),
 ('graph.chunks','/v1/graph/chunks','graph chunks','graph_chunks','graph','读取实体的原始提及片段',common+['entity_id','name'],[]),
 ('+ask','/v1/ask','+ask','ask','core','固定检索组合，返回资料而非生成答案',common+['q','graph','rerank','external','per_file'],['q']),
 ('+context','/v1/context','+context','context','graph','实体、关系、出处组合，不生成答案',common+['name','entity_id'],[]),
 ('handles.describe','/v1/handles/describe','handle show','handle_describe','utility','查看范围，不重新查询',['kbs','id'],['id']),
 ('results.page','/v1/results/page','results page','results_page','core','读取缓存结果下一页，不重复搜索',['kbs','result_id','cursor','limit','max_chars'],['result_id']),
 ('scope.expand','/v1/scopes/expand','scope expand','scope_expand','utility','显式扩大到同 KB 中相关实体提及块；不是 within 的默认行为',common,['within'])]
output={'$schema':'https://json-schema.org/draft/2020-12/schema','title':'XRAG Demo response','type':'object','properties':{
 'schema_version':{'const':'0.7-demo'},'query_id':{'type':'string'},'action':{'type':'string'},'mode':{'enum':['server','local','direct']},'kbs':{'type':'array','items':{'type':'string'}},'items':{'type':'array','items':{'type':'object','properties':{'kind':{'type':'string'},'ref':{'type':'string'}},'required':['kind']}},'returned':{'type':'integer','minimum':0},'total':{'type':'object','properties':{'value':{'type':['integer','null']},'accuracy':{'enum':['exact','unknown','lower_bound','estimate']},'unit':{'type':'string'},'calculation_complete':{'type':'boolean'},'scope':{'type':'string'}},'required':['value','accuracy','unit','calculation_complete','scope']},'coverage':{'type':'object','required':['execution_partial','display_truncated','approximate','consistency','scope_basis']},'strategy':{'type':'object'},'notes':{'type':'array'},'cache_complete':{'type':'boolean'}},'required':['schema_version','query_id','action','mode','kbs','items','returned','total','coverage','strategy','notes','cache_complete']}
(root/'response.schema.json').write_text(json.dumps(output,ensure_ascii=False,indent=2)+'\n')
registry=[]
for a,path,cli,mcp,group,desc,keys,req in actions:
 schema={'$schema':'https://json-schema.org/draft/2020-12/schema','title':a,'type':'object','properties':{k:props[k] for k in keys},'required':req,'additionalProperties':False}
 if a=='files.read':schema['oneOf']=[{'required':['ref'],'not':{'required':['path']}},{'required':['path','kbs'],'not':{'required':['ref']}}]
 if a in ['graph.neighbors','graph.chunks','+context']:schema['oneOf']=[{'required':['entity_id'],'not':{'required':['name']}},{'required':['name'],'not':{'required':['entity_id']}}]
 filename=a.replace('+','plus_').replace('.','_')+'.json'
 (root/'actions'/filename).write_text(json.dumps(schema,ensure_ascii=False,indent=2)+'\n')
 registry.append({'action':a,'path':path,'cli':cli,'tool':mcp,'group':group,'description':desc,'input':filename})
(root/'registry.json').write_text(json.dumps(registry,ensure_ascii=False,indent=2)+'\n')
