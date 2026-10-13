package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"time"
	"github.com/BlueSkyXN/XRAG/core"
	"github.com/BlueSkyXN/XRAG/research"
)

// YAML is a local optional input adapter. Never install dependencies, execute a
// YAML tag, or accept aliases that can expand into unbounded data.
func LoadRecipe(ctx context.Context, file string) (research.Recipe, error) {
	var r research.Recipe
	raw, err := os.ReadFile(file)
	if err != nil {
		return r, err
	}
	if len(raw) > 64000 {
		return r, core.Err("RECIPE_LIMIT", "recipe exceeds 64KiB", 422)
	}
	if !json.Valid(raw) {
		script := `import json,sys,yaml
class L(yaml.SafeLoader):
 def compose_node(self,parent,index):
  if self.check_event(yaml.AliasEvent): raise ValueError('aliases forbidden')
  return super().compose_node(parent,index)
 def construct_mapping(self,node,deep=False):
  d={}
  for k,v in node.value:
   k=self.construct_object(k,deep=deep)
   if not isinstance(k,str) or k in d: raise ValueError('duplicate/non-string key')
   d[k]=self.construct_object(v,deep=deep)
  return d
x=yaml.load(sys.stdin.read(),Loader=L)
print(json.dumps(x,ensure_ascii=False,allow_nan=False))`
		c, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		cmd := exec.CommandContext(c, "python3", "-I", "-c", script)
		cmd.Stdin = bytes.NewReader(raw)
		raw, err = cmd.Output()
		if err != nil {
			return r, core.Err("RECIPE_FORMAT_INVALID", "use JSON, or install Python3/PyYAML for strict YAML (no aliases/duplicate keys)", 422)
		}
		if len(raw) > 128000 {
			return r, core.Err("RECIPE_LIMIT", "expanded recipe exceeds limit", 422)
		}
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err = dec.Decode(&r); err != nil {
		return r, core.Err("INVALID_RECIPE", "invalid recipe fields", 400)
	}
	return r, r.Validate()
}
