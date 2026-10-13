package server

import (
	"context"
	"encoding/json"
	"net/http"
	"github.com/BlueSkyXN/XRAG/contracts"
	"github.com/BlueSkyXN/XRAG/core"
	"github.com/BlueSkyXN/XRAG/internal/store"
)

func (a *App) ExecuteWrite(ctx context.Context, p store.Principal, action string, b []byte) (any, bool, error) {
	spec, ok := contracts.FindWrite(action)
	if !ok {
		return nil, false, nil
	}
	if !store.RoleAtLeast(p, "contributor") {
		return nil, true, core.Err("PERMISSION_DENIED", "contributor role required", 403)
	}
	var obj map[string]any
	if len(b) > 240000 || json.Unmarshal(b, &obj) != nil || obj == nil {
		return nil, true, core.Err("INVALID_ARGUMENT", "invalid write request", 400)
	}
	if e := contracts.Validate(contracts.Input(spec), obj); e != nil {
		return nil, true, core.Err("INVALID_ARGUMENT", e.Error(), 400)
	}
	kb, _ := obj["kb"].(string)
	if _, e := a.Store.Scope(ctx, p, []string{kb}); e != nil {
		return nil, true, e
	}
	ctx = store.WithActor(ctx, p.ID)
	if spec.Action == "knowledge.submit" {
		var k store.Knowledge
		_ = json.Unmarshal(b, &k)
		v, e := a.Store.SaveKnowledge(ctx, k)
		return v, true, e
	}
	ref, _ := obj["ref"].(string)
	label, _ := obj["label"].(string)
	note, _ := obj["note"].(string)
	e := a.Store.Feedback(ctx, kb, ref, label, note, p.ID)
	return map[string]any{"saved": e == nil}, true, e
}
func (a *App) writeToolRoute(w http.ResponseWriter, r *http.Request, p store.Principal) bool {
	spec, ok := contracts.FindWrite(r.URL.Path)
	if !ok {
		return false
	}
	if r.Method != "POST" {
		a.fail(w, core.Err("METHOD_NOT_ALLOWED", "POST required", 405))
		return true
	}
	b, e := body(r, 240000)
	if e != nil {
		a.fail(w, e)
		return true
	}
	v, _, e := a.ExecuteWrite(r.Context(), p, spec.Action, b)
	if e != nil {
		a.fail(w, e)
	} else {
		write(w, 201, v)
	}
	return true
}
