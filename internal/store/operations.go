package store

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"time"
	"github.com/BlueSkyXN/XRAG/core"
	db "github.com/BlueSkyXN/XRAG/internal/sqlite"
)

// Fresh-install operational tables. Content version history is managed separately.
var extensionSchema = []string{
	`CREATE TABLE IF NOT EXISTS source_config(kb TEXT NOT NULL,source TEXT NOT NULL,git_ref TEXT NOT NULL DEFAULT '',PRIMARY KEY(kb,source),FOREIGN KEY(kb,source) REFERENCES source(kb,id))`,
	`CREATE TABLE IF NOT EXISTS source_policy(kb TEXT NOT NULL,source TEXT NOT NULL,data TEXT NOT NULL,PRIMARY KEY(kb,source),FOREIGN KEY(kb,source) REFERENCES source(kb,id))`,
	`CREATE TABLE IF NOT EXISTS source_state(kb TEXT NOT NULL,source TEXT NOT NULL,last_checked INTEGER NOT NULL DEFAULT 0,last_success INTEGER NOT NULL DEFAULT 0,revision TEXT NOT NULL DEFAULT '',indexed_fingerprint TEXT NOT NULL DEFAULT '',observed_fingerprint TEXT NOT NULL DEFAULT '',error TEXT NOT NULL DEFAULT '',PRIMARY KEY(kb,source))`,
	`CREATE TABLE IF NOT EXISTS job_trigger(id INTEGER PRIMARY KEY,kb TEXT NOT NULL,source TEXT NOT NULL,job_id TEXT NOT NULL,reason TEXT NOT NULL,created INTEGER NOT NULL)`,
	`CREATE TABLE IF NOT EXISTS build_detail(id INTEGER PRIMARY KEY,job_id TEXT NOT NULL,kb TEXT NOT NULL,source TEXT NOT NULL,path TEXT NOT NULL,state TEXT NOT NULL,before_edition TEXT NOT NULL,after_edition TEXT NOT NULL,revision TEXT NOT NULL,error TEXT NOT NULL,created INTEGER NOT NULL)`,
	`CREATE INDEX IF NOT EXISTS build_detail_job ON build_detail(job_id,id)`,
	`CREATE TABLE IF NOT EXISTS eval_set(id TEXT PRIMARY KEY NOT NULL,kb TEXT NOT NULL,name TEXT NOT NULL,revision TEXT NOT NULL,data TEXT NOT NULL,created INTEGER NOT NULL)`,
	`CREATE TABLE IF NOT EXISTS query_record(id TEXT PRIMARY KEY NOT NULL,owner TEXT NOT NULL,kbs TEXT NOT NULL,epoch INTEGER NOT NULL,expires INTEGER NOT NULL,data TEXT NOT NULL)`,
	`CREATE INDEX IF NOT EXISTS query_record_expiry ON query_record(expires)`,
}

type SourcePolicy struct {
	HookProvider    string `json:"hook_provider,omitempty"`
	HookRepository  string `json:"hook_repository,omitempty"`
	HookRef         string `json:"hook_ref,omitempty"`
	Schedule        string `json:"schedule,omitempty"`
	Timezone        string `json:"timezone,omitempty"`
	NativeWatch     bool   `json:"native_watch,omitempty"`
	EverySeconds    int    `json:"every_seconds"`
	WatchSeconds    int    `json:"watch_seconds"`
	DebounceSeconds int    `json:"debounce_seconds"`
	HookSecretRef   string `json:"hook_secret_ref,omitempty"`
}
type SourceState struct {
	LastChecked int64  `json:"last_checked"`
	LastSuccess int64  `json:"last_success"`
	Revision    string `json:"source_revision"`
	Indexed     string `json:"indexed_fingerprint"`
	Observed    string `json:"observed_fingerprint"`
	Error       string `json:"error,omitempty"`
}

func (s *Store) Source(ctx context.Context, kb, id string) (Source, error) {
	xs, e := s.Sources(ctx, kb)
	if e != nil {
		return Source{}, e
	}
	for _, x := range xs {
		if x.ID == id {
			return x, nil
		}
	}
	return Source{}, core.Err("NOT_FOUND", "source not found", 404)
}
func (s *Store) Policy(ctx context.Context, kb, id string) (SourcePolicy, error) {
	p := SourcePolicy{DebounceSeconds: 10}
	e := s.DB.Do(ctx, func(c *db.Conn) error {
		r, e := c.Query("SELECT data FROM source_policy WHERE kb=? AND source=?", kb, id)
		if e != nil {
			return e
		}
		if len(r) > 0 {
			return json.Unmarshal([]byte(db.String(r[0], "data")), &p)
		}
		return nil
	})
	return p, e
}
func (s *Store) SetPolicy(ctx context.Context, kb, id string, p SourcePolicy) error {
	src, e := s.Source(ctx, kb, id)
	if e != nil {
		return e
	}
	if p.HookProvider != "" && p.HookProvider != "custom" && p.HookProvider != "github" && p.HookProvider != "gitlab" && p.HookProvider != "gitea" {
		return core.Err("INVALID_ARGUMENT", "hook_provider must be custom/github/gitlab/gitea", 400)
	}
	if p.HookProvider != "" && p.HookProvider != "custom" {
		if src.Kind != "git" {
			return core.Err("MODE_UNSUPPORTED", "native Git webhooks require a Git source", 422)
		}
		if p.HookSecretRef == "" || !regexp.MustCompile(`^[A-Za-z0-9_.-]+(?:/[A-Za-z0-9_.-]+)+$`).MatchString(p.HookRepository) || len(p.HookRepository) > 250 {
			return core.Err("INVALID_ARGUMENT", "native webhook requires hook_repository namespace/name and a secret reference", 400)
		}
		if !regexp.MustCompile(`^refs/(heads|tags)/[A-Za-z0-9_./-]+$`).MatchString(p.HookRef) || strings.Contains(p.HookRef, "..") || len(p.HookRef) > 256 {
			return core.Err("INVALID_ARGUMENT", "hook_ref must be an explicit refs/heads/... or refs/tags/... value", 400)
		}
	} else if p.HookRepository != "" || p.HookRef != "" {
		return core.Err("INVALID_ARGUMENT", "repository/ref belong to a native webhook provider", 400)
	}
	if p.Schedule != "" {
		if _, e := core.ParseCron(p.Schedule, p.Timezone); e != nil {
			return e
		}
	}
	if p.NativeWatch {
		if src.Kind != "path" && src.Kind != "builtin" {
			return core.Err("MODE_UNSUPPORTED", "native watch applies to local directory sources", 422)
		}
		if p.EverySeconds == 0 {
			p.EverySeconds = 1800
		}
	}
	if p.EverySeconds < 0 || (p.EverySeconds > 0 && p.EverySeconds < 10) || p.EverySeconds > 604800 {
		return core.Err("INVALID_ARGUMENT", "every_seconds is 0 (off) or 10..604800", 400)
	}
	if p.WatchSeconds < 0 || (p.WatchSeconds > 0 && p.WatchSeconds < 2) || p.WatchSeconds > 3600 {
		return core.Err("INVALID_ARGUMENT", "watch_seconds is 0 (off) or 2..3600; polling, not OS events", 400)
	}
	if p.WatchSeconds > 0 && src.Kind != "path" && src.Kind != "builtin" {
		return core.Err("MODE_UNSUPPORTED", "git sources use periodic checks or hooks; watch polling is for path/builtin", 422)
	}
	if p.DebounceSeconds == 0 {
		p.DebounceSeconds = 10
	}
	if p.DebounceSeconds < 1 || p.DebounceSeconds > 300 {
		return core.Err("INVALID_ARGUMENT", "debounce_seconds must be 1..300", 400)
	}
	if p.HookSecretRef != "" && !strings.HasPrefix(p.HookSecretRef, "env:") && !strings.HasPrefix(p.HookSecretRef, "file:") {
		return core.Err("INVALID_ARGUMENT", "hook_secret_ref uses env:NAME or file:/absolute/path", 400)
	}
	return s.DB.Do(ctx, func(c *db.Conn) error {
		return c.Exec("INSERT INTO source_policy(kb,source,data) VALUES(?,?,?) ON CONFLICT(kb,source) DO UPDATE SET data=excluded.data", kb, id, core.JSON(p))
	})
}
func (s *Store) SourceState(ctx context.Context, kb, id string) (SourceState, error) {
	var v SourceState
	e := s.DB.Do(ctx, func(c *db.Conn) error {
		r, e := c.Query("SELECT * FROM source_state WHERE kb=? AND source=?", kb, id)
		if e != nil {
			return e
		}
		if len(r) > 0 {
			x := r[0]
			v = SourceState{db.Int(x, "last_checked"), db.Int(x, "last_success"), db.String(x, "revision"), db.String(x, "indexed_fingerprint"), db.String(x, "observed_fingerprint"), db.String(x, "error")}
		}
		return nil
	})
	return v, e
}
func (s *Store) ObserveSource(ctx context.Context, src Source, rev, fp string, scanErr error) error {
	msg := ""
	if scanErr != nil {
		msg = scanErr.Error()
	}
	return s.DB.Do(ctx, func(c *db.Conn) error {
		return c.Exec(`INSERT INTO source_state(kb,source,last_checked,revision,observed_fingerprint,error) VALUES(?,?,?,?,?,?) ON CONFLICT(kb,source) DO UPDATE SET last_checked=excluded.last_checked,revision=excluded.revision,observed_fingerprint=excluded.observed_fingerprint,error=excluded.error`, src.KB, src.ID, time.Now().Unix(), rev, fp, msg)
	})
}
func (s *Store) SourceIndexed(ctx context.Context, src Source, rev, fp string, j Job) error {
	return s.DB.Tx(ctx, true, func(c *db.Conn) error {
		if e := fence(c, j); e != nil {
			return e
		}
		return c.Exec(`INSERT INTO source_state(kb,source,last_checked,last_success,revision,indexed_fingerprint,observed_fingerprint,error) VALUES(?,?,?,?,?,?,?,'') ON CONFLICT(kb,source) DO UPDATE SET last_success=excluded.last_success,revision=excluded.revision,indexed_fingerprint=excluded.indexed_fingerprint,error=''`, src.KB, src.ID, time.Now().Unix(), time.Now().Unix(), rev, fp, fp)
	})
}
func (s *Store) Trigger(ctx context.Context, kb, source, reason, owner, key string, extra map[string]any) (Job, error) {
	payload := map[string]any{"mode": "incremental", "trigger": reason, "source": source}
	for k, v := range extra {
		payload[k] = v
	}
	j, e := s.Enqueue(ctx, kb, "build", owner, key, payload)
	if e != nil {
		return j, e
	}
	e = s.DB.Do(ctx, func(c *db.Conn) error {
		return c.Exec("INSERT INTO job_trigger(kb,source,job_id,reason,created) VALUES(?,?,?,?,?)", kb, source, j.ID, reason, time.Now().Unix())
	})
	return j, e
}
func (s *Store) BuildDetail(ctx context.Context, j Job, src, path, state, before, after, rev string, err error) error {
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	return s.DB.Do(ctx, func(c *db.Conn) error {
		return c.Exec("INSERT INTO build_detail(job_id,kb,source,path,state,before_edition,after_edition,revision,error,created) VALUES(?,?,?,?,?,?,?,?,?,?)", j.ID, j.KB, src, path, state, before, after, rev, msg, time.Now().Unix())
	})
}
func (s *Store) JobDetails(ctx context.Context, id string) (map[string]any, error) {
	limits := map[string]int{"files": 1000, "triggers": 100, "usage": 200}
	truncated := map[string]bool{}
	out := map[string]any{"limits": limits, "truncated": truncated}
	e := s.DB.Do(ctx, func(c *db.Conn) error {
		for key, q := range map[string]string{
			"files":    "SELECT * FROM build_detail WHERE job_id=? ORDER BY id LIMIT 1001",
			"triggers": "SELECT * FROM job_trigger WHERE job_id=? ORDER BY id LIMIT 101",
			"usage":    "SELECT id,kind,state,usage,created FROM call_usage WHERE job_id=? ORDER BY created,id LIMIT 201",
		} {
			r, e := c.Query(q, id)
			if e != nil {
				return e
			}
			truncated[key] = len(r) > limits[key]
			if truncated[key] {
				r = r[:limits[key]]
			}
			out[key] = r
		}
		return nil
	})
	return out, e
}

// Query history stores a bounded diagnostic summary, never the full document corpus.
type QueryRecord struct {
	ID       string        `json:"id"`
	Owner    string        `json:"owner"`
	KBs      []string      `json:"kbs"`
	Epoch    int64         `json:"epoch"`
	Created  int64         `json:"created"`
	Request  core.Request  `json:"request"`
	Response core.Response `json:"response"`
}

func (s *Store) RecordQuery(ctx context.Context, p Principal, q core.Request, r core.Response, viewEpoch int64) error {
	// File contents and arbitrary model output are not retained in diagnostic summaries.
	if r.Action == "files.read" || r.Action == "kb.discover" || r.Action == "results.page" || r.Action == "handles.describe" {
		return nil
	}
	epoch, e := s.Epoch(ctx)
	if e != nil {
		return e
	}
	if epoch != viewEpoch {
		return core.Err("VIEW_INVALIDATED", "source changed before diagnostic record", 409)
	}
	rr := r
	rr.Items = append([]core.Item(nil), r.Items...)
	rr.Data = nil
	for i := range rr.Items {
		rr.Items[i].Text = ""
		rr.Items[i].Fields = nil
		rr.Items[i].Edges = nil
		rr.Items[i].Refs = nil
	}
	if len(rr.Items) > 100 {
		rr.Items = rr.Items[:100]
	}
	if len(rr.Trace) > 30 {
		rr.Trace = rr.Trace[:30]
	}
	// Step ID slices are copied before bounding to avoid modifying the API response.
	rr.Trace = append([]core.Step(nil), rr.Trace...)
	for i := range rr.Trace {
		if len(rr.Trace[i].IDs) > 200 {
			rr.Trace[i].IDs = rr.Trace[i].IDs[:200]
			rr.Trace[i].Note += "; trace IDs limited to 200"
		}
	}
	rec := QueryRecord{r.QueryID, p.ID, q.Kbs, epoch, time.Now().Unix(), q, rr}
	return s.DB.Tx(ctx, true, func(c *db.Conn) error {
		if e := c.Exec("DELETE FROM query_record WHERE expires<?", time.Now().Unix()); e != nil {
			return e
		}
		if e := c.Exec("INSERT OR REPLACE INTO query_record(id,owner,kbs,epoch,expires,data) VALUES(?,?,?,?,?,?)", rec.ID, p.ID, core.JSON(q.Kbs), epoch, time.Now().Unix()+7*86400, core.JSON(rec)); e != nil {
			return e
		}
		return c.Exec("DELETE FROM query_record WHERE id NOT IN(SELECT id FROM query_record ORDER BY expires DESC,id DESC LIMIT 2000)")
	})
}
func (s *Store) Queries(ctx context.Context, p Principal, id string) ([]QueryRecord, error) {
	kbs, e := s.Scope(ctx, p, nil)
	if e != nil {
		return nil, e
	}
	out := []QueryRecord{}
	e = s.DB.Do(ctx, func(c *db.Conn) error {
		ep, e := epoch(c)
		if e != nil {
			return e
		}
		sql := "SELECT * FROM query_record WHERE expires>? AND epoch=?"
		args := []any{time.Now().Unix(), ep}
		if id != "" {
			sql += " AND id=?"
			args = append(args, id)
		}
		sql += " ORDER BY expires DESC,id DESC LIMIT 100"
		r, e := c.Query(sql, args...)
		if e != nil {
			return e
		}
		for _, row := range r {
			var v QueryRecord
			if e = json.Unmarshal([]byte(db.String(row, "data")), &v); e != nil {
				return e
			}
			if core.Subset(v.KBs, kbs) && (v.Owner == p.ID || RoleAtLeast(p, "maintainer")) {
				out = append(out, v)
			}
		}
		return nil
	})
	if e == nil && id != "" && len(out) == 0 {
		return out, core.Err("RESULT_EXPIRED", "trace unavailable, outside scope, expired or withdrawn", 410)
	}
	return out, e
}
