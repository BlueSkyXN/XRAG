package server

import (
	"net/http"
	"strings"
	"github.com/BlueSkyXN/XRAG/core"
	"github.com/BlueSkyXN/XRAG/internal/store"
)

func (a *App) completeRoutes(w http.ResponseWriter, r *http.Request, p store.Principal) bool {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 3 || parts[0] != "v1" || parts[1] != "kbs" {
		return false
	}
	kb := parts[2]
	ctx := r.Context()
	if _, e := a.Store.Scope(ctx, p, []string{kb}); e != nil {
		a.fail(w, e)
		return true
	}
	finish := func(v any, e error) {
		if e != nil {
			a.fail(w, e)
		} else {
			write(w, 200, v)
		}
	}
	needs := func(role string) bool {
		if !store.RoleAtLeast(p, role) {
			a.fail(w, core.Err("PERMISSION_DENIED", role+" role required", 403))
			return true
		}
		return false
	}
	if len(parts) == 3 {
		switch r.Method {
		case "GET":
			settings, e := a.Store.KBSettings(ctx, kb)
			if e != nil {
				finish(nil, e)
				return true
			}
			kbs, _ := a.Store.KBs(ctx)
			for _, k := range kbs {
				if k["id"] == kb {
					if store.RoleAtLeast(p, "maintainer") {
						k["settings"] = settings
					}
					finish(k, nil)
					return true
				}
			}
			finish(nil, core.Err("NOT_FOUND", "KB not found", 404))
		case "PATCH":
			if needs("maintainer") {
				return true
			}
			var q struct {
				Name string `json:"name"`
			}
			b, e := body(r, 8192)
			if e == nil {
				e = decode(b, &q)
			}
			if e == nil {
				e = a.Store.RenameKB(ctx, kb, q.Name)
			}
			finish(map[string]any{"id": kb, "name": q.Name}, e)
		case "DELETE":
			if needs("admin") {
				return true
			}
			e := readConfirm(r)
			if e == nil {
				e = a.Store.DeleteKB(ctx, kb)
			}
			finish(map[string]any{"deleted": true, "source_files_changed": false}, e)
		default:
			write(w, 405, map[string]any{"error": "method not allowed"})
		}
		return true
	}
	if len(parts) == 4 && parts[3] == "settings" {
		if needs("maintainer") {
			return true
		}
		if r.Method == "GET" {
			v, e := a.Store.KBSettings(ctx, kb)
			finish(v, e)
			return true
		}
		if r.Method == "PUT" {
			var q struct {
				Expected int              `json:"expected_revision"`
				Settings store.KBSettings `json:"settings"`
			}
			b, e := body(r, 1<<20)
			if e == nil {
				e = decode(b, &q)
			}
			if e != nil {
				finish(nil, e)
				return true
			}
			oldSettings, readErr := a.Store.KBSettings(ctx, kb)
			if readErr != nil {
				finish(nil, readErr)
				return true
			}
			if core.JSON(q.Settings.Providers) != core.JSON(oldSettings.Providers) && needs("admin") {
				return true
			}
			v, e := a.Store.SetKBSettings(ctx, kb, q.Settings, q.Expected)
			finish(v, e)
			return true
		}
		write(w, 405, map[string]any{"error": "method not allowed"})
		return true
	}
	if len(parts) == 4 && parts[3] == "usage" && r.Method == "GET" {
		if needs("maintainer") {
			return true
		}
		v, e := a.Store.BudgetReport(ctx, kb)
		finish(v, e)
		return true
	}
	if len(parts) == 5 && parts[3] == "sources" {
		if needs("maintainer") {
			return true
		}
		id := parts[4]
		if r.Method == "GET" {
			v, e := a.Store.Source(ctx, kb, id)
			finish(v, e)
			return true
		}
		if r.Method == "PATCH" || r.Method == "DELETE" {
			var q struct {
				Expected int      `json:"expected_revision"`
				Disabled *bool    `json:"disabled"`
				Include  []string `json:"include"`
				Exclude  []string `json:"exclude"`
				Confirm  bool     `json:"confirm"`
				Root     string   `json:"root,omitempty"`
				Mount    string   `json:"mount,omitempty"`
			}
			b, e := body(r, 16384)
			if e == nil {
				e = decode(b, &q)
			}
			if e != nil {
				finish(nil, e)
				return true
			}
			if r.Method == "DELETE" && !q.Confirm {
				finish(nil, core.Err("CONFIRM_REQUIRED", "confirm=true required", 400))
				return true
			}
			old, readErr := a.Store.Source(ctx, kb, id)
			if readErr != nil {
				finish(nil, readErr)
				return true
			}
			opt := old.Options
			if q.Disabled != nil {
				opt.Disabled = *q.Disabled
			}
			if q.Include != nil {
				opt.Include = q.Include
			}
			if q.Exclude != nil {
				opt.Exclude = q.Exclude
			}
			e = a.Store.ChangeSource(ctx, kb, id, &store.Source{Root: q.Root, Mount: q.Mount}, opt, q.Expected, r.Method == "DELETE")
			finish(map[string]any{"source": id, "deleted": r.Method == "DELETE", "revision": q.Expected + 1, "published_content_policy": "disable freezes current content; delete removes it from current queries and retains historical refs"}, e)
			return true
		}
	}
	return false
}
