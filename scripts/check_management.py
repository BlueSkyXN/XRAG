#!/usr/bin/env python3
"""Optional development check; jsonschema required only for this script."""
from pathlib import Path
import json
from jsonschema import Draft202012Validator
r=Path(__file__).resolve().parents[1];results=[]
for p in (r/'contracts').rglob('*.json'):
 d=json.loads(p.read_text())
 if isinstance(d,dict) and '$schema' in d:
  Draft202012Validator.check_schema(d);results.append(str(p.relative_to(r)))
pairs=[('examples/eval-demo.json','eval-set'),('examples/source-policy.json','source-policy'),('examples/settings.empty.json','provider-settings'),('examples/settings.chat.json','provider-settings'),('examples/key-reader.json','key-create'),('examples/feedback-resolution.json','feedback-resolution'),('examples/confirm.json','confirm')]
pairs += [('examples/source-s3.json','source-create'),('examples/source-weknora.json','source-create'),('examples/sync-target-path.json','sync-target-create'),('examples/sync-target-git.json','sync-target-create'),('examples/knowledge-sync.json','knowledge-sync'),('examples/knowledge-archive.json','knowledge-archive'),('examples/wiki-regenerate.json','wiki-regenerate'),('examples/wiki-generate.json','wiki-generate')]
pairs += [('examples/kb-settings.json', 'kb-settings'), ('examples/kb-models.json', 'kb-settings'), ('examples/kb-commonmark.json', 'kb-settings'), ('examples/kb-external.json', 'kb-settings'), ('examples/source-cron.json', 'source-policy'), ('examples/source-native.json', 'source-policy'), ('examples/source-edit.json', 'source-edit'), ('examples/build-vectors.json', 'build-request'), ('examples/build-graph.json', 'build-request'), ('examples/sync-target-s3.json', 'sync-target-create'), ('examples/sync-target-git-push.json', 'sync-target-create'), ('examples/kb-rename.json', 'kb-rename')]
pairs += [('examples/maintenance-policy.json', 'maintenance-policy'), ('examples/maintenance-run.json', 'maintenance-run'), ('examples/feedback-test-case.json', 'feedback-test-case'), ('examples/eval-from-drafts.json', 'eval-from-drafts'), ('examples/query-diagnose.json', 'query-diagnose'), ('examples/eval-approval.json', 'eval-approval'), ('examples/eval-holdout.json', 'eval-set')]
pairs += [('examples/source-policy-github.json','source-policy'),('examples/mcp-auth.json','mcp-auth')]
for ex,s in pairs:
 d=json.loads((r/'contracts/management'/(s+'.schema.json')).read_text());Draft202012Validator(d).validate(json.loads((r/ex).read_text()))
action_pairs=[('examples/task-resume.json','task.id.json'),('examples/research-task.json','research.submit.json'),('examples/recipe-run.json','recipe.run.json')]
for ex,schema in action_pairs:
 d=json.loads((r/'contracts/actions'/schema).read_text());Draft202012Validator(d).validate(json.loads((r/ex).read_text()))
pairs += action_pairs
a={'schemas':len(results),'example_requests':len(pairs),'scope':'JSON Schema documents and request examples; runtime behavior separately tested in Go','files':results}
(r/'checks').mkdir(parents=True,exist_ok=True)
(r/'checks/management-contracts.json').write_text(json.dumps(a,ensure_ascii=False,indent=2));print(json.dumps(a,ensure_ascii=False))
