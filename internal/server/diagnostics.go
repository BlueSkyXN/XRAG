package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"github.com/BlueSkyXN/XRAG/core"
	"github.com/BlueSkyXN/XRAG/evaluation"
	"github.com/BlueSkyXN/XRAG/internal/store"
)

func (a *App) diagnosticRoutes(w http.ResponseWriter, r *http.Request, p store.Principal) bool {
	ctx := r.Context()
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if r.URL.Path == "/v1/queries" && r.Method == "GET" || len(parts) == 3 && parts[1] == "queries" && r.Method == "GET" {
		id := ""
		if len(parts) == 3 {
			id = parts[2]
		}
		v, e := a.Store.Queries(ctx, p, id)
		if e != nil {
			a.fail(w, e)
		} else {
			write(w, 200, map[string]any{"items": v})
		}
		return true
	}
	if !strings.HasPrefix(r.URL.Path, "/v1/eval/") {
		return false
	}
	if !store.RoleAtLeast(p, "maintainer") {
		a.fail(w, core.Err("PERMISSION_DENIED", "evaluation management requires maintainer", 403))
		return true
	}
	if r.URL.Path == "/v1/eval/sets" {
		if r.Method == "GET" {
			kbs, e := a.Store.Scope(ctx, p, nil)
			if e != nil {
				a.fail(w, e)
				return true
			}
			v, e := a.Store.EvalSets(ctx, kbs)
			if e != nil {
				a.fail(w, e)
			} else {
				write(w, 200, map[string]any{"items": v})
			}
			return true
		}
		if r.Method == "POST" {
			raw, e := body(r, 256<<10)
			var v evaluation.Set
			if e == nil {
				e = decode(raw, &v)
			}
			if e == nil {
				_, e = a.Store.Scope(ctx, p, []string{v.KB})
			}
			if e == nil {
				v, e = a.Store.SaveEvalSet(ctx, v)
			}
			if e != nil {
				a.fail(w, e)
			} else {
				write(w, 201, v)
			}
			return true
		}
	}
	if r.URL.Path == "/v1/eval/runs" && r.Method == "POST" {
		var q struct {
			SetID      string   `json:"set_id"`
			Strategies []string `json:"strategies"`
			K          int      `json:"k"`
		}
		raw, e := body(r, 8192)
		if e == nil {
			e = decode(raw, &q)
		}
		if e != nil {
			a.fail(w, e)
			return true
		}
		set, e := a.Store.EvalSet(ctx, q.SetID)
		if e != nil {
			a.fail(w, e)
			return true
		}
		if _, e = a.Store.Scope(ctx, p, []string{set.KB}); e != nil {
			a.fail(w, e)
			return true
		}
		if q.K == 0 {
			q.K = 5
		}
		if len(q.Strategies) == 0 {
			q.Strategies = []string{"keyword", "ask", "ask_graph"}
		}
		q.Strategies = core.Unique(q.Strategies)
		if q.K < 1 || q.K > 50 || len(q.Strategies) > 5 || !core.Subset(q.Strategies, evaluation.Strategies) {
			a.fail(w, core.Err("INVALID_ARGUMENT", "unknown strategy or k outside 1..50", 400))
			return true
		}
		scoped, scErr := a.Builder.ForKB(ctx, set.KB)
		if scErr != nil {
			a.fail(w, scErr)
			return true
		}
		for _, s := range q.Strategies {
			if s == "semantic" && scoped.Config.Embedding == nil || s == "ask_rerank" && scoped.Config.Rerank == nil {
				a.fail(w, core.Err("CAPABILITY_UNAVAILABLE", "selected evaluation strategy needs an unconfigured model: "+s, 422))
				return true
			}
		}
		j, e := a.Store.Enqueue(ctx, set.KB, "eval", p.ID, r.Header.Get("Idempotency-Key"), evaluation.Request{Set: set, Strategies: q.Strategies, K: q.K})
		if e != nil {
			a.fail(w, e)
		} else {
			write(w, 202, j)
		}
		return true
	}
	a.fail(w, core.Err("NOT_FOUND", "evaluation route not found", 404))
	return true
}
func (a *App) jobVisible(j store.Job) store.Job {
	j = a.taskResultVisible(j)
	if j.Kind == "eval" && (j.State == "succeeded" || j.State == "partial") {
		var r evaluation.Report
		if json.Unmarshal(j.Result, &r) == nil {
			epoch, e := a.Store.Epoch(context.Background())
			if e != nil || epoch != r.ViewEpoch {
				j.Result = json.RawMessage(`{"unavailable":"VIEW_INVALIDATED","message":"source withdrawal invalidated this saved evaluation report"}`)
			}
		}
	}
	return j
}
