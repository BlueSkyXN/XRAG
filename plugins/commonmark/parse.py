#!/usr/bin/env python3
"""Read-only CommonMark + table/YAML parser. stdout contains one protocol response.
Install the pinned requirements; no network or file access is performed by parsing.
"""
import sys,json,unicodedata,datetime
try:
 import yaml
 from markdown_it import MarkdownIt
except ImportError:
 print(json.dumps({"error":"Install plugins/commonmark/requirements.txt"}));sys.exit(1)

class BoundedLoader(yaml.SafeLoader):
 def __init__(self,*a,**kw):
  super().__init__(*a,**kw);self.node_count=0;self.level=0
 def compose_node(self,parent,index):
  if self.check_event(yaml.AliasEvent):
   raise ValueError("YAML aliases are not enabled for document metadata")
  self.node_count+=1;self.level+=1
  if self.node_count>20000 or self.level>32:raise ValueError("YAML metadata exceeds limits")
  try:return super().compose_node(parent,index)
  finally:self.level-=1
 def construct_mapping(self,node,deep=False):
  keys=[self.construct_object(k,deep=deep) for k,v in node.value]
  if any(not isinstance(k,str) for k in keys) or len(set(keys))!=len(keys):raise ValueError("metadata keys must be unique strings")
  return super().construct_mapping(node,deep=deep)

def compatible(v):
 if isinstance(v,(datetime.datetime,datetime.date)):return v.isoformat()
 if isinstance(v,dict):return {k:compatible(x) for k,x in v.items()}
 if isinstance(v,list):return [compatible(x) for x in v]
 return v

def parse(req):
 body=unicodedata.normalize("NFC",req["text"].replace("\r\n","\n").replace("\r","\n").lstrip("\ufeff"))
 lines=body.splitlines(keepends=True);start=0;fields={}
 if lines and lines[0].strip()=="---":
  end=next((i for i in range(1,len(lines)) if lines[i].strip() in ("---","...")),None)
  if end is None:raise ValueError("unterminated YAML frontmatter")
  fields=yaml.load("".join(lines[1:end]),Loader=BoundedLoader) or {}
  if not isinstance(fields,dict):raise ValueError("frontmatter must be a mapping")
  fields=compatible(fields);start=end+1
 offsets=[0]
 for line in lines:offsets.append(offsets[-1]+len(line))
 text="".join(lines[start:]);tokens=MarkdownIt("commonmark").enable("table").parse(text)
 heading=[];blocks=[];title=fields.get("title","")
 for i,t in enumerate(tokens):
  if t.level!=0 or t.map is None or t.nesting==-1:continue
  if t.type=="heading_open":
   level=int(t.tag[1]);label=tokens[i+1].content
   heading=heading[:max(0,level-1)]+[label]
   if level==1 and not title:title=label
  a,b=t.map;a+=start;b+=start
  blocks.append({"start":offsets[a],"end":offsets[min(b,len(lines))],"heading":heading[:],"atomic":t.type in ("fence","code_block","table_open"),"type":t.type})
 # Include blank gaps with the preceding block. All offsets refer to normalized original text.
 for i in range(len(blocks)-1):blocks[i]["end"]=blocks[i+1]["start"]
 if blocks:blocks[-1]["end"]=len(body)
 elif text.strip():blocks=[{"start":offsets[start],"end":len(body),"heading":[],"atomic":False,"type":"text"}]
 return {"body":body,"fields":fields,"title":str(title),"blocks":blocks,"parser_revision":"markdown-it-4.2.0+yaml-6.0.3+nfc-v1"}

for line in sys.stdin:
 try:
  req=json.loads(line)
  if req.get("protocol")!="xrag.parser/v1" or len(req.get("text",""))>5242880:raise ValueError("invalid parser input")
  out={"id":req["id"],"result":parse(req)}
 except Exception as e:out={"id":locals().get("req",{}).get("id"),"error":str(e)}
 print(json.dumps(out,ensure_ascii=False,allow_nan=False),flush=True)
