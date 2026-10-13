package server

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/BlueSkyXN/XRAG/adapters"
	"github.com/BlueSkyXN/XRAG/core"
	"github.com/BlueSkyXN/XRAG/internal/store"
)

func admin(p store.Principal) error {
	if p.Role != "admin" {
		return core.Err("PERMISSION_DENIED", "workspace administrator required", 403)
	}
	return nil
}
func readConfirm(r *http.Request) error {
	var q struct {
		Confirm bool `json:"confirm"`
	}
	b, e := body(r, 4096)
	if e != nil {
		return e
	}
	if e = decode(b, &q); e != nil {
		return e
	}
	if !q.Confirm {
		return core.Err("CONFIRM_REQUIRED", "explicit confirm=true is required", 400)
	}
	return nil
}
func (a *App) productRoutes(w http.ResponseWriter, r *http.Request, p store.Principal) bool {
	ctx := r.Context()
	path := r.URL.Path
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if path == "/v1/overview" && r.Method == "GET" {
		v, e := a.Store.Overview(ctx, p)
		if e != nil {
			a.fail(w, e)
		} else {
			v["workers"] = a.Workers
			v["config_source"] = a.ConfigSource
			write(w, 200, v)
		}
		return true
	}
	if path == "/v1/keys" || (len(parts) == 3 && parts[1] == "keys") {
		if e := admin(p); e != nil {
			a.fail(w, e)
			return true
		}
		switch {
		case path == "/v1/keys" && r.Method == "GET":
			v, e := a.Store.Keys(ctx)
			if e != nil {
				a.fail(w, e)
			} else {
				write(w, 200, map[string]any{"items": v, "current_key_id": p.ID, "identity_model": "one key is one application identity; no separate user-password directory"})
			}
		case path == "/v1/keys" && r.Method == "POST":
			var q struct {
				Name    string   `json:"name"`
				Role    string   `json:"role"`
				KBs     []string `json:"kbs"`
				Expires int64    `json:"expires_at"`
			}
			b, e := body(r, 8192)
			if e == nil {
				e = decode(b, &q)
			}
			if e != nil {
				a.fail(w, e)
				return true
			}
			k, token, e := a.Store.CreateManagedKey(ctx, q.Name, q.Role, q.KBs, q.Expires)
			if e != nil {
				a.fail(w, e)
			} else {
				write(w, 201, map[string]any{"key": token, "info": k, "role": k.Role, "kbs": k.KBs, "shown_once": true})
			}
		case len(parts) == 3 && r.Method == "DELETE":
			e := readConfirm(r)
			if e == nil {
				e = a.Store.DisableKey(ctx, parts[2])
			}
			if e != nil {
				a.fail(w, e)
			} else {
				write(w, 200, map[string]any{"id": parts[2], "disabled": true})
			}
		case len(parts) == 3 && r.Method == "PATCH":
			var q struct {
				Name string `json:"name"`
			}
			b, e := body(r, 4096)
			if e == nil {
				e = decode(b, &q)
			}
			if e == nil {
				e = a.Store.RenameKey(ctx, parts[2], q.Name)
			}
			if e != nil {
				a.fail(w, e)
			} else {
				write(w, 200, map[string]any{"id": parts[2], "name": q.Name})
			}
		default:
			a.fail(w, core.Err("METHOD_NOT_ALLOWED", "unsupported key operation", 405))
		}
		return true
	}
	if path == "/v1/settings/providers" {
		if e := admin(p); e != nil {
			a.fail(w, e)
			return true
		}
		if r.Method == "GET" {
			st, e := a.Store.ProviderSetting(ctx)
			if e != nil {
				a.fail(w, e)
				return true
			}
			desired := adapters.Config{}
			if e = json.Unmarshal(st.Data, &desired); e != nil {
				a.fail(w, e)
				return true
			}
			mode := a.ConfigSource
			if mode == "" {
				mode = "managed"
			}
			write(w, 200, map[string]any{"revision": st.Revision, "updated": st.Updated, "config": safeConfig(desired), "active": safeConfig(a.Builder.Config), "source": mode, "editable": mode != "external_file", "restart_required": mode != "external_file" && core.Hash(desired) != core.Hash(a.Builder.Config), "model_calls": 0, "note": "Saving does not alter running processes. Restart Server and ALL workers without --config to load managed settings. Explicit --config overrides this store."})
			return true
		}
		if r.Method == "PUT" {
			if a.ConfigSource == "external_file" {
				a.fail(w, core.Err("CONFIG_EXTERNAL", "this process uses --config; edit that file or restart without --config before using managed settings", 409))
				return true
			}
			var q struct {
				Expected int64           `json:"expected_revision"`
				Config   json.RawMessage `json:"config"`
			}
			b, e := body(r, 64<<10)
			if e == nil {
				e = decode(b, &q)
			}
			var cfg adapters.Config
			if e == nil {
				if len(q.Config) == 0 || string(q.Config) == "null" {
					e = core.Err("INVALID_ARGUMENT", "config object is required", 400)
				} else {
					cfg, e = adapters.ParseConfig(q.Config)
				}
			}
			if e == nil {
				e = validateManagedConfig(cfg)
			}
			if e != nil {
				a.fail(w, e)
				return true
			}
			st, e := a.Store.SaveProviderSetting(ctx, q.Expected, json.RawMessage(core.JSON(cfg)))
			if e != nil {
				a.fail(w, e)
			} else {
				write(w, 200, map[string]any{"revision": st.Revision, "config": safeConfig(cfg), "restart_required": core.Hash(cfg) != core.Hash(a.Builder.Config), "network_calls": 0, "model_calls": 0, "rebuild_required_if_embedding_or_extractor_changed": true})
			}
			return true
		}
		a.fail(w, core.Err("METHOD_NOT_ALLOWED", "GET or PUT required", 405))
		return true
	}
	if path == "/v1/feedback" && r.Method == "GET" {
		limit, offset := 50, 0
		var e error
		if raw := r.URL.Query().Get("limit"); raw != "" {
			limit, e = strconv.Atoi(raw)
		}
		if raw := r.URL.Query().Get("offset"); raw != "" && e == nil {
			offset, e = strconv.Atoi(raw)
		}
		if e != nil {
			a.fail(w, core.Err("INVALID_ARGUMENT", "invalid page", 400))
			return true
		}
		var kbs []string
		if k := r.URL.Query().Get("kb"); k != "" {
			kbs = []string{k}
		}
		v, e := a.Store.Feedbacks(ctx, p, kbs, r.URL.Query().Get("status"), limit, offset)
		if e != nil {
			a.fail(w, e)
		} else {
			write(w, 200, v)
		}
		return true
	}
	if len(parts) == 3 && parts[1] == "feedback" && r.Method == "PATCH" {
		var q struct {
			Status   string `json:"status"`
			Note     string `json:"resolution"`
			Expected int64  `json:"expected_revision"`
		}
		b, e := body(r, 12000)
		if e == nil {
			e = decode(b, &q)
		}
		if e == nil {
			e = a.Store.ResolveFeedback(ctx, p, parts[2], q.Status, q.Note, q.Expected)
		}
		if e != nil {
			a.fail(w, e)
		} else {
			write(w, 200, map[string]any{"id": parts[2], "status": q.Status, "revision": q.Expected + 1})
		}
		return true
	}
	if len(parts) == 4 && parts[1] == "kbs" && parts[3] == "export" && r.Method == "GET" {
		if !store.RoleAtLeast(p, "maintainer") {
			a.fail(w, core.Err("PERMISSION_DENIED", "export requires maintainer", 403))
			return true
		}
		kbs, e := a.Store.Scope(ctx, p, []string{parts[2]})
		if e != nil {
			a.fail(w, e)
			return true
		}
		v, e := a.Store.Snapshot(ctx, kbs)
		if e != nil {
			a.fail(w, e)
			return true
		}
		// This is a published-content export, not a DB backup or Top-K query.
		total := 0
		for _, d := range v.Documents {
			total += len(d.Body)
		}
		if total > 64<<20 || len(v.Documents) > 10000 {
			a.fail(w, core.Err("EXPORT_LIMIT", "Demo export is limited to 64 MiB and 10000 published files", 422))
			return true
		}
		var buf bytes.Buffer
		z := zip.NewWriter(&buf)
		items := []map[string]any{}
		for _, d := range v.Documents {
			if _, e = core.CleanPath(d.Path); e != nil {
				break
			}
			var f io.Writer
			f, e = z.Create("documents/" + d.Path)
			if e != nil {
				break
			}
			_, e = io.WriteString(f, d.Body)
			if e != nil {
				break
			}
			items = append(items, map[string]any{"path": d.Path, "edition_id": d.Edition, "file_hash": d.FileHash, "source": d.Source, "ref": core.EncodeRef(core.Ref{Origin: v.Origin, KB: d.KB, Path: d.Path, Edition: d.Edition, FileHash: d.FileHash})})
		}
		if e == nil {
			var f io.Writer
			f, e = z.Create("manifest.json")
			if e == nil {
				e = json.NewEncoder(f).Encode(map[string]any{"format": "xrag-published-export/v1", "kb": parts[2], "origin_id": v.Origin, "created_at": v.At, "items": items, "scope": "published content only; no drafts, API keys, vectors or graph database backup"})
			}
		}
		if ce := z.Close(); e == nil {
			e = ce
		}
		if e != nil {
			a.fail(w, e)
			return true
		}
		epoch, e := a.Store.Epoch(ctx)
		if e != nil || epoch != v.Epoch {
			a.fail(w, core.Err("VIEW_INVALIDATED", "source withdrawn during export; repeat export", 409))
			return true
		}
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", `attachment; filename="xrag-`+parts[2]+`.zip"`)
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(buf.Bytes())
		return true
	}
	if path == "/v1/backups" || (len(parts) >= 3 && parts[1] == "backups") {
		if e := admin(p); e != nil {
			a.fail(w, e)
			return true
		}
		if path == "/v1/backups" && r.Method == "GET" {
			v, e := a.Store.Backups(ctx)
			if e != nil {
				a.fail(w, e)
			} else {
				write(w, 200, map[string]any{"items": v, "scope": "SQLite snapshot only; excludes source directories, plaintext keys and external backends", "max_snapshots": 20})
			}
			return true
		}
		if path == "/v1/backups" && r.Method == "POST" {
			if e := readConfirm(r); e != nil {
				a.fail(w, e)
				return true
			}
			a.maintenanceMu.Lock()
			defer a.maintenanceMu.Unlock()
			v, e := a.Store.CreateBackup(ctx)
			if e != nil {
				a.fail(w, e)
			} else {
				write(w, 201, v)
			}
			return true
		}
		if len(parts) == 4 && parts[3] == "download" && r.Method == "GET" {
			b, pth, e := a.Store.BackupPath(ctx, parts[2])
			if e != nil {
				a.fail(w, e)
				return true
			}
			f, e := os.Open(pth)
			if e != nil {
				a.fail(w, core.Err("NOT_FOUND", "backup file is unavailable", 404))
				return true
			}
			defer f.Close()
			st, e := f.Stat()
			if e != nil {
				a.fail(w, e)
				return true
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("Content-Disposition", `attachment; filename="`+b.Filename+`"`)
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("X-Content-SHA256", b.SHA256)
			http.ServeContent(w, r, b.Filename, st.ModTime(), f)
			return true
		}
		if len(parts) == 3 && r.Method == "DELETE" {
			e := readConfirm(r)
			if e == nil {
				a.maintenanceMu.Lock()
				e = a.Store.RemoveBackup(ctx, parts[2])
				a.maintenanceMu.Unlock()
			}
			if e != nil {
				a.fail(w, e)
			} else {
				write(w, 200, map[string]any{"removed": parts[2]})
			}
			return true
		}
		a.fail(w, core.Err("METHOD_NOT_ALLOWED", "unsupported backup operation", 405))
		return true
	}
	if path == "/v1/maintenance/cache" {
		if e := admin(p); e != nil {
			a.fail(w, e)
			return true
		}
		apply := r.Method == "POST"
		if !apply && r.Method != "GET" {
			a.fail(w, core.Err("METHOD_NOT_ALLOWED", "GET preview or POST apply", 405))
			return true
		}
		if apply {
			if e := readConfirm(r); e != nil {
				a.fail(w, e)
				return true
			}
		}
		v, e := a.Store.CleanupTransient(ctx, apply)
		if e != nil {
			a.fail(w, e)
		} else {
			write(w, 200, map[string]any{"applied": apply, "expired_rows": v, "scope": "expired result/handle caches and query summaries only; no knowledge, editions or model cache deleted"})
		}
		return true
	}
	return false
}

func validateManagedConfig(c adapters.Config) error {
	refs := []string{}
	for _, ep := range []*adapters.Endpoint{c.Embedding, c.Rerank, c.Chat, c.Qdrant} {
		if ep != nil {
			refs = append(refs, ep.Key)
		}
	}
	if c.Neo4j != nil {
		refs = append(refs, c.Neo4j.Password)
	}
	for _, ref := range refs {
		if ref != "" && ((!strings.HasPrefix(ref, "env:") && !strings.HasPrefix(ref, "file:")) || len(strings.TrimSpace(strings.SplitN(ref, ":", 2)[1])) == 0) {
			return core.Err("INVALID_ARGUMENT", "managed secrets must be non-empty env:NAME or file:/path references; plaintext values are not stored", 400)
		}
	}
	for _, ep := range []*adapters.Endpoint{c.Chat, c.Rerank} {
		if ep != nil && strings.TrimSpace(ep.Model) == "" {
			return core.Err("INVALID_ARGUMENT", "Chat and Rerank configurations require a model", 400)
		}
	}
	if e := store.ValidateSecretReferences(c); e != nil {
		return e
	}
	if len(c.Extractor) > 0 || len(c.ParserCommand) > 0 || len(c.Plugins) > 0 {
		return core.Err("MODE_UNSUPPORTED", "external extractor commands are configured with an explicit --config file, not through the web settings editor", 422)
	}
	return nil
}
func safeConfig(c adapters.Config) adapters.Config {
	var out adapters.Config
	_ = json.Unmarshal([]byte(core.JSON(c)), &out)
	redact := func(s string) string {
		if s != "" && !strings.HasPrefix(s, "env:") && !strings.HasPrefix(s, "file:") {
			return "[inline secret omitted]"
		}
		return s
	}
	for _, ep := range []*adapters.Endpoint{out.Embedding, out.Rerank, out.Chat, out.Qdrant} {
		if ep != nil {
			ep.Key = redact(ep.Key)
		}
	}
	if out.Neo4j != nil {
		out.Neo4j.Password = redact(out.Neo4j.Password)
	}
	if out.Graph.Model != nil {
		out.Graph.Model.Key = redact(out.Graph.Model.Key)
	}
	for i := range out.External {
		out.External[i].Key = redact(out.External[i].Key)
	}
	return out
}
