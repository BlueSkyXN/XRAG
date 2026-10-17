package contracts

import (
	"embed"
	"encoding/json"
	"fmt"
	"github.com/BlueSkyXN/XRAG/core"
	"math"
	"strings"
	"unicode/utf8"
)

//go:embed registry.json response.schema.json image-response.schema.json actions/*.json
var FS embed.FS

type Action struct {
	Action      string `json:"action"`
	Path        string `json:"path"`
	CLI         string `json:"cli"`
	Tool        string `json:"tool"`
	Group       string `json:"group"`
	Description string `json:"description"`
	Input       string `json:"input"`
}

var All []Action

func init() {
	b, e := FS.ReadFile("registry.json")
	if e != nil {
		panic(e)
	}
	if e = json.Unmarshal(b, &All); e != nil {
		panic(e)
	}
}
func Find(name string) (Action, bool) {
	for _, a := range All {
		if a.Action == name || a.Tool == name || a.CLI == name || a.Path == name {
			return a, true
		}
	}
	return Action{}, false
}
func Input(a Action) map[string]any {
	b, _ := FS.ReadFile("actions/" + a.Input)
	m := map[string]any{}
	_ = json.Unmarshal(b, &m)
	return m
}
func Output() map[string]any {
	b, _ := FS.ReadFile("response.schema.json")
	m := map[string]any{}
	_ = json.Unmarshal(b, &m)
	return m
}

func ImageOutput() map[string]any {
	b, _ := FS.ReadFile("image-response.schema.json")
	m := map[string]any{}
	_ = json.Unmarshal(b, &m)
	return m
}

// Validate implements exactly the vocabulary used by bundled request contracts. It is not a general JSON Schema engine.
func Validate(s map[string]any, v any) error {
	if t, ok := s["type"].(string); ok {
		good := false
		switch t {
		case "object":
			_, good = v.(map[string]any)
		case "array":
			_, good = v.([]any)
		case "string":
			_, good = v.(string)
		case "boolean":
			_, good = v.(bool)
		case "integer":
			n, ok := v.(float64)
			good = ok && n == math.Trunc(n)
		case "number":
			_, good = v.(float64)
		}
		if !good {
			return fmt.Errorf("expected %s", t)
		}
	}
	if es, ok := s["enum"].([]any); ok {
		found := false
		for _, x := range es {
			if core.JSON(x) == core.JSON(v) {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("value is not allowed")
		}
	}
	if o, ok := v.(map[string]any); ok {
		if req, ok := s["required"].([]any); ok {
			for _, k := range req {
				if _, ok = o[k.(string)]; !ok {
					return fmt.Errorf("missing %s", k)
				}
			}
		}
		props, _ := s["properties"].(map[string]any)
		for k, x := range o {
			p, exists := props[k]
			if !exists {
				if a, ok := s["additionalProperties"].(bool); ok && !a {
					return fmt.Errorf("unknown parameter %s", k)
				}
			} else if err := Validate(p.(map[string]any), x); err != nil {
				return fmt.Errorf("%s: %w", k, err)
			}
		}
	}
	if str, ok := v.(string); ok {
		if n, ok := s["minLength"].(float64); ok && float64(utf8.RuneCountInString(str)) < n {
			return fmt.Errorf("string is empty")
		}
		if n, ok := s["maxLength"].(float64); ok && float64(utf8.RuneCountInString(str)) > n {
			return fmt.Errorf("string too long")
		}
	}
	if n, ok := v.(float64); ok {
		if min, ok := s["minimum"].(float64); ok && n < min {
			return fmt.Errorf("below minimum")
		}
		if max, ok := s["maximum"].(float64); ok && n > max {
			return fmt.Errorf("above maximum")
		}
	}
	if a, ok := v.([]any); ok {
		if n, ok := s["minItems"].(float64); ok && len(a) < int(n) {
			return fmt.Errorf("array is empty")
		}
		if n, ok := s["maxItems"].(float64); ok && len(a) > int(n) {
			return fmt.Errorf("array too long")
		}
		if sub, ok := s["items"].(map[string]any); ok {
			for _, x := range a {
				if e := Validate(sub, x); e != nil {
					return e
				}
			}
		}
	}
	if x, ok := s["not"].(map[string]any); ok {
		if Validate(x, v) == nil {
			return fmt.Errorf("mutually exclusive parameters")
		}
	}
	if xs, ok := s["oneOf"].([]any); ok {
		count := 0
		for _, x := range xs {
			if Validate(x.(map[string]any), v) == nil {
				count++
			}
		}
		if count != 1 {
			return fmt.Errorf("provide exactly one supported argument combination")
		}
	}
	return nil
}
func Decode(a Action, b []byte) (core.Request, error) {
	var args map[string]any
	if e := json.Unmarshal(b, &args); e != nil || args == nil {
		return core.Request{}, core.Err("INVALID_ARGUMENT", "request must be a JSON object", 400)
	}
	if e := Validate(Input(a), args); e != nil {
		return core.Request{}, core.Err("INVALID_ARGUMENT", e.Error(), 400)
	}
	if x, ok := args["entity_id"]; ok {
		args["id"] = x
		delete(args, "entity_id")
	}
	if x, ok := args["hops"]; ok {
		args["depth"] = x
		delete(args, "hops")
	}
	var r core.Request
	e := json.NewDecoder(strings.NewReader(core.JSON(args))).Decode(&r)
	return r, e
}
