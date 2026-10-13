package server

import (
	"net/http"
	"strings"
	"github.com/BlueSkyXN/XRAG/core"
	"github.com/BlueSkyXN/XRAG/evaluation"
	"github.com/BlueSkyXN/XRAG/internal/store"
)

func (a *App) iteration6Routes(w http.ResponseWriter, r *http.Request, p store.Principal) bool {
	ctx := r.Context()
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	done := func(v any, e error, status int) {
		if e != nil {
			a.fail(w, e)
		} else {
			write(w, status, v)
		}
	}
	if r.URL.Path == "/v1/maintenance/policy" || r.URL.Path == "/v1/maintenance/runs" || r.URL.Path == "/v1/maintenance/records" || r.URL.Path == "/v1/maintenance/content" {
		if !store.RoleAtLeast(p, "admin") {
			done(nil, core.Err("PERMISSION_DENIED", "maintenance requires admin", 403), 0)
			return true
		}
		switch r.URL.Path {
		case "/v1/maintenance/policy":
			if r.Method == "GET" {
				v, e := a.Store.MaintenanceSetting(ctx)
				done(v, e, 200)
				return true
			}
			if r.Method == "PUT" {
				var q struct {
					Expected int64                   `json:"expected_revision"`
					Policy   store.MaintenancePolicy `json:"policy"`
				}
				b, e := body(r, 12000)
				if e == nil {
					e = decode(b, &q)
				}
				if e != nil {
					done(nil, e, 0)
					return true
				}
				v, e := a.Store.SetMaintenance(ctx, q.Expected, q.Policy)
				done(v, e, 200)
				return true
			}
		case "/v1/maintenance/runs":
			if r.Method == "GET" {
				v, e := a.Store.MaintenanceRuns(ctx)
				done(map[string]any{"items": v}, e, 200)
				return true
			}
			if r.Method == "POST" {
				var q struct {
					Confirm bool `json:"confirm"`
					Backup  bool `json:"backup"`
					Records bool `json:"records"`
					Content bool `json:"content"`
				}
				b, e := body(r, 4096)
				if e == nil {
					e = decode(b, &q)
				}
				if e == nil && !q.Confirm {
					e = core.Err("CONFIRM_REQUIRED", "explicit maintenance confirmation required", 400)
				}
				if e != nil {
					done(nil, e, 0)
					return true
				}
				v, e := a.Store.QueueMaintenance(ctx, p.ID, q.Backup, q.Records, q.Content)
				done(v, e, 202)
				return true
			}
		case "/v1/maintenance/content":
			if r.Method == "GET" {
				v, err := a.Store.MaintenanceSetting(ctx)
				if err != nil {
					done(nil, err, 0)
					return true
				}
				report, err := a.Store.PreviewContent(ctx, v.Policy.ContentDays, min(v.Policy.BatchSize, 100))
				done(report, err, 200)
				return true
			}
		case "/v1/maintenance/records":
			if r.Method == "GET" {
				v, e := a.Store.MaintenanceSetting(ctx)
				if e != nil {
					done(nil, e, 0)
					return true
				}
				report, e := a.Store.RotateRecords(ctx, v.Policy.RecordsDays, v.Policy.BatchSize, false)
				done(report, e, 200)
				return true
			}
		}
		done(nil, core.Err("METHOD_NOT_ALLOWED", "method not supported", 405), 0)
		return true
	}
	if len(parts) == 4 && parts[1] == "feedback" && parts[3] == "test-case" && r.Method == "POST" {
		var q struct {
			Question string `json:"question"`
		}
		b, e := body(r, 4096)
		if e == nil {
			e = decode(b, &q)
		}
		if e != nil {
			done(nil, e, 0)
			return true
		}
		v, e := a.Store.CreateEvalDraft(ctx, p, parts[2], q.Question)
		done(v, e, 201)
		return true
	}
	if strings.HasPrefix(r.URL.Path, "/v1/eval/drafts") || r.URL.Path == "/v1/eval/sets/from-drafts" || (len(parts) == 4 && parts[1] == "eval" && parts[2] == "sets" && r.Method == "GET") {
		if !store.RoleAtLeast(p, "maintainer") {
			done(nil, core.Err("PERMISSION_DENIED", "evaluation management requires maintainer", 403), 0)
			return true
		}
		if r.URL.Path == "/v1/eval/drafts" && r.Method == "GET" {
			kbs := []string{}
			if k := r.URL.Query().Get("kb"); k != "" {
				kbs = []string{k}
			}
			kbs, e := a.Store.Scope(ctx, p, kbs)
			if e != nil {
				done(nil, e, 0)
				return true
			}
			v, e := a.Store.EvalDrafts(ctx, kbs)
			done(map[string]any{"items": v}, e, 200)
			return true
		}
		if len(parts) == 5 && parts[2] == "drafts" && parts[4] == "approve" && r.Method == "POST" {
			var q struct {
				Expected int64           `json:"expected_revision"`
				Case     evaluation.Case `json:"case"`
				Note     string          `json:"note"`
			}
			b, e := body(r, 64<<10)
			if e == nil {
				e = decode(b, &q)
			}
			if e != nil {
				done(nil, e, 0)
				return true
			}
			v, e := a.Store.ApproveEvalDraft(ctx, p, parts[3], q.Expected, q.Case, q.Note)
			done(v, e, 200)
			return true
		}
		if r.URL.Path == "/v1/eval/sets/from-drafts" && r.Method == "POST" {
			var q struct {
				KB      string   `json:"kb"`
				Name    string   `json:"name"`
				Family  string   `json:"family"`
				Purpose string   `json:"purpose"`
				IDs     []string `json:"draft_ids"`
			}
			b, e := body(r, 64<<10)
			if e == nil {
				e = decode(b, &q)
			}
			if e != nil {
				done(nil, e, 0)
				return true
			}
			v, e := a.Store.SaveEvalSetFromDrafts(ctx, p, q.KB, q.Name, q.Family, q.Purpose, q.IDs)
			done(v, e, 201)
			return true
		}
		if len(parts) == 4 && parts[2] == "sets" && r.Method == "GET" {
			v, e := a.Store.EvalSet(ctx, parts[3])
			if e == nil {
				_, e = a.Store.Scope(ctx, p, []string{v.KB})
			}
			done(v, e, 200)
			return true
		}
		done(nil, core.Err("NOT_FOUND", "evaluation route not found", 404), 0)
		return true
	}
	if len(parts) == 4 && parts[1] == "queries" && parts[3] == "diagnose" && r.Method == "POST" {
		var q struct {
			KB     string `json:"kb"`
			Path   string `json:"path"`
			Anchor string `json:"anchor"`
		}
		b, e := body(r, 8192)
		if e == nil {
			e = decode(b, &q)
		}
		if e == nil {
			_, e = a.Store.Scope(ctx, p, []string{q.KB})
		}
		if e != nil {
			done(nil, e, 0)
			return true
		}
		v, e := a.Store.Diagnose(ctx, p, parts[2], q.KB, q.Path, q.Anchor)
		done(v, e, 200)
		return true
	}
	return false
}
