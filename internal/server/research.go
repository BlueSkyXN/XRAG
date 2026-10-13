package server

import (
	"context"
	"encoding/json"
	"net/http"
	"github.com/BlueSkyXN/XRAG/core"
	"github.com/BlueSkyXN/XRAG/internal/build"
	"github.com/BlueSkyXN/XRAG/internal/store"
)

func (a *App) ExecuteTask(ctx context.Context, p store.Principal, action string, raw []byte, key string) (any, bool, error) {
	if action != "research.submit" && action != "recipe.run" && action != "task.status" && action != "task.cancel" && action != "task.resume" {
		return nil, false, nil
	}
	if action == "task.status" || action == "task.cancel" || action == "task.resume" {
		var q struct {
			ID string `json:"id"`
		}
		if e := decode(raw, &q); e != nil {
			return nil, true, e
		}
		j, e := a.Store.Job(ctx, q.ID)
		if e != nil {
			return nil, true, e
		}
		if _, e = a.Store.Scope(ctx, p, []string{j.KB}); e != nil || j.Owner != p.ID && !store.RoleAtLeast(p, "maintainer") {
			return nil, true, core.Err("PERMISSION_DENIED", "task outside caller scope", 403)
		}
		if action == "task.resume" {
			fp, er := a.Builder.InvestigationFingerprint(ctx, j.KB)
			if er != nil {
				return nil, true, er
			}
			resumed, er := a.Store.ResumeInvestigation(ctx, j.ID, fp)
			return a.jobVisible(resumed), true, er
		}
		if action == "task.cancel" {
			e = a.Store.Cancel(ctx, j.ID)
			return map[string]any{"id": j.ID, "cancel_requested": e == nil}, true, e
		}
		return a.jobVisible(j), true, nil
	}
	q, e := build.DecodeInvestigation(raw)
	if e != nil {
		return nil, true, e
	}
	if _, e = a.Store.Scope(ctx, p, []string{q.KB}); e != nil {
		return nil, true, e
	}
	kind := "research"
	if action == "recipe.run" {
		kind = "recipe"
		if q.Recipe == nil {
			return nil, true, core.Err("INVALID_RECIPE", "recipe required", 400)
		}
		if e = q.Recipe.Validate(); e != nil {
			return nil, true, e
		}
	} else {
		if q.Recipe != nil {
			return nil, true, core.Err("INVALID_ARGUMENT", "recipe belongs to recipe.run", 400)
		}
		b, e := a.Builder.ForKB(ctx, q.KB)
		if e != nil {
			return nil, true, e
		}
		if b.Config.Chat == nil {
			return nil, true, core.Err("CAPABILITY_UNAVAILABLE", "configure Chat before submitting Research", 422)
		}
	}
	j, e := a.Store.Enqueue(ctx, q.KB, kind, p.ID, key, q)
	return j, true, e
}
func (a *App) taskRoutes(w http.ResponseWriter, r *http.Request, p store.Principal) bool {
	action := ""
	switch r.URL.Path {
	case "/v1/research":
		action = "research.submit"
	case "/v1/recipes/run":
		action = "recipe.run"
	case "/v1/tasks/status":
		action = "task.status"
	case "/v1/tasks/cancel":
		action = "task.cancel"
	case "/v1/tasks/resume":
		action = "task.resume"
	default:
		return false
	}
	if r.Method != "POST" {
		a.fail(w, core.Err("METHOD_NOT_ALLOWED", "use POST", 405))
		return true
	}
	raw, e := body(r, 64000)
	if e != nil {
		a.fail(w, e)
		return true
	}
	v, _, e := a.ExecuteTask(r.Context(), p, action, raw, r.Header.Get("Idempotency-Key"))
	if e != nil {
		a.fail(w, e)
	} else {
		status := 202
		if action == "task.status" || action == "task.cancel" {
			status = 200
		}
		write(w, status, v)
	}
	return true
}

// taskResultVisible applies withdrawal to both completed results and saved
// progress. It intentionally does not replace an invalidated snapshot by latest.
func (a *App) taskResultVisible(j store.Job) store.Job {
	if j.Kind != "research" && j.Kind != "recipe" {
		return j
	}
	var v map[string]any
	if json.Unmarshal(j.Result, &v) != nil {
		return j
	}
	if ep, ok := v["withdrawal_epoch"].(float64); ok {
		now, e := a.Store.Epoch(context.Background())
		if e != nil || int64(ep) != now {
			j.Result = json.RawMessage(`{"unavailable":"VIEW_INVALIDATED","message":"source withdrawal invalidates saved task content"}`)
			return j
		}
	}
	if v != nil && v["unavailable"] == nil {
		if cp, _, e := a.Store.TaskCheckpoint(context.Background(), j.ID); e == nil && cp != nil {
			info := map[string]any{"revision": cp.Revision, "phase": cp.Phase, "next_step": cp.NextStep, "resume_mode": "explicit"}
			if er := cp.CanResume(); er != nil {
				info["blocking_reason"] = core.AsError(er).Code
			}
			v["checkpoint"] = info
			j.Result = json.RawMessage(core.JSON(v))
		}
	}
	return j
}
