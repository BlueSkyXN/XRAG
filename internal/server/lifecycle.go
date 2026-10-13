package server

import (
	"net/http"
	"strings"
	"github.com/BlueSkyXN/XRAG/core"
	"github.com/BlueSkyXN/XRAG/internal/build"
	"github.com/BlueSkyXN/XRAG/internal/store"
)

func (a *App) lifecycleRoutes(w http.ResponseWriter, r *http.Request, p store.Principal) bool {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	ctx := r.Context()
	if len(parts) == 4 && parts[0] == "v1" && parts[1] == "kbs" && parts[3] == "sync-targets" {
		kb := parts[2]
		if _, e := a.Store.Scope(ctx, p, []string{kb}); e != nil {
			a.fail(w, e)
			return true
		}
		if !store.RoleAtLeast(p, "maintainer") {
			a.fail(w, core.Err("PERMISSION_DENIED", "maintainer required", 403))
			return true
		}
		switch r.Method {
		case "GET":
			v, e := a.Store.SyncTargets(ctx, []string{kb})
			if e != nil {
				a.fail(w, e)
			} else {
				write(w, 200, map[string]any{"items": v, "automatic_sync": false})
			}
		case "POST":
			var q struct {
				Target  store.SyncTarget `json:"target"`
				Confirm bool             `json:"confirm"`
			}
			b, e := body(r, 8192)
			if e == nil {
				e = decode(b, &q)
			}
			if e == nil && !q.Confirm {
				e = core.Err("CONFIRM_REQUIRED", "confirm a Server-local destination before writing", 400)
			}
			if e != nil {
				a.fail(w, e)
				return true
			}
			q.Target.KB = kb
			t, e := a.Store.AddSyncTarget(ctx, q.Target)
			if e != nil {
				a.fail(w, e)
			} else {
				write(w, 201, t)
			}
		default:
			a.fail(w, core.Err("METHOD_NOT_ALLOWED", "unsupported method", 405))
		}
		return true
	}
	if len(parts) != 4 || parts[0] != "v1" || parts[1] != "knowledge" {
		return false
	}
	op := parts[3]
	if op != "regenerate" && op != "revisions" && op != "sync" && op != "archive" {
		return false
	}
	k, e := a.Store.Knowledge(ctx, parts[2], "")
	if e == nil {
		_, e = a.Store.Scope(ctx, p, []string{k.KB})
	}
	if e != nil {
		a.fail(w, e)
		return true
	}
	if !store.RoleAtLeast(p, "contributor") {
		a.fail(w, core.Err("PERMISSION_DENIED", "draft management requires contributor", 403))
		return true
	}
	if op == "revisions" && r.Method == "GET" {
		v, e := a.Store.KnowledgeRevisions(ctx, k.ID)
		if e != nil {
			a.fail(w, e)
		} else {
			write(w, 200, map[string]any{"items": v, "published_revision": k.Published, "draft_revision": k.Draft})
		}
		return true
	}
	if !store.RoleAtLeast(p, "maintainer") {
		a.fail(w, core.Err("PERMISSION_DENIED", "maintainer required", 403))
		return true
	}
	if op == "sync" && r.Method == "GET" {
		v, e := a.Store.SyncReceipts(ctx, k.ID)
		if e != nil {
			a.fail(w, e)
		} else {
			write(w, 200, map[string]any{"items": v})
		}
		return true
	}
	if r.Method != "POST" {
		a.fail(w, core.Err("METHOD_NOT_ALLOWED", "POST required", 405))
		return true
	}
	raw, e := body(r, 8192)
	if e != nil {
		a.fail(w, e)
		return true
	}
	switch op {
	case "regenerate":
		var q build.WikiRequest
		if e = decode(raw, &q); e != nil {
			a.fail(w, e)
			break
		}
		scoped, scErr := a.Builder.ForKB(ctx, k.KB)
		if scErr != nil {
			a.fail(w, scErr)
			return true
		}
		if scoped.Config.Chat == nil {
			a.fail(w, core.Err("CAPABILITY_UNAVAILABLE", "configure Chat before Wiki regeneration", 422))
			break
		}
		if k.Kind != "wiki" {
			a.fail(w, core.Err("INVALID_ARGUMENT", "regenerate only applies to Wiki items", 400))
			break
		}
		if q.Expected == "" || q.Expected != k.Draft {
			a.fail(w, core.Err("VERSION_CONFLICT", "current draft revision required", 409))
			break
		}
		q.ID = k.ID
		if q.Topic == "" {
			q.Topic = k.Title
		}
		j, e := a.Store.Enqueue(ctx, k.KB, "wiki", p.ID, r.Header.Get("Idempotency-Key"), q)
		if e != nil {
			a.fail(w, e)
		} else {
			write(w, 202, j)
		}
	case "sync":
		var q struct {
			Target   string `json:"target_id"`
			Revision string `json:"revision"`
			Confirm  bool   `json:"confirm"`
		}
		if e = decode(raw, &q); e != nil {
			a.fail(w, e)
			break
		}
		if !q.Confirm {
			a.fail(w, core.Err("CONFIRM_REQUIRED", "explicit confirm=true required for source writeback", 400))
			break
		}
		t, e := a.Store.SyncTarget(ctx, q.Target)
		if e != nil {
			a.fail(w, e)
			break
		}
		if t.KB != k.KB {
			a.fail(w, core.Err("PERMISSION_DENIED", "destination is in a different KB", 403))
			break
		}
		if q.Revision == "" || q.Revision != k.Published {
			a.fail(w, core.Err("VERSION_CONFLICT", "current published revision required", 409))
			break
		}
		j, e := a.Store.Enqueue(ctx, k.KB, "sync", p.ID, r.Header.Get("Idempotency-Key"), build.SyncRequest{ID: k.ID, Revision: q.Revision, Target: q.Target})
		if e != nil {
			a.fail(w, e)
		} else {
			write(w, 202, j)
		}
	case "archive":
		var q struct {
			Revision string `json:"revision"`
			Confirm  bool   `json:"confirm"`
		}
		e = decode(raw, &q)
		if e == nil && !q.Confirm {
			e = core.Err("CONFIRM_REQUIRED", "confirm=true required", 400)
		}
		if e == nil {
			e = a.Store.ArchiveKnowledge(ctx, k.ID, q.Revision)
		}
		if e != nil {
			a.fail(w, e)
		} else {
			write(w, 200, map[string]any{"id": k.ID, "state": "archived", "history_retained": true})
		}
	}
	return true
}
