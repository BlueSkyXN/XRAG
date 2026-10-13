package store

import (
	"context"
	"encoding/json"
	"strings"
	"time"
	"github.com/BlueSkyXN/XRAG/adapters"
	"github.com/BlueSkyXN/XRAG/core"
	db "github.com/BlueSkyXN/XRAG/internal/sqlite"
)

var iteration5Schema = []string{
	`CREATE TABLE IF NOT EXISTS auto_sync_outbox(id TEXT PRIMARY KEY NOT NULL,kb TEXT NOT NULL,target TEXT NOT NULL,item TEXT NOT NULL,revision TEXT NOT NULL,owner TEXT NOT NULL,created INTEGER NOT NULL)`,
	`CREATE TABLE IF NOT EXISTS source_retired(kb TEXT NOT NULL,source TEXT NOT NULL,created INTEGER NOT NULL,PRIMARY KEY(kb,source))`,
	`CREATE TABLE IF NOT EXISTS budget_call(id TEXT PRIMARY KEY NOT NULL,kb TEXT NOT NULL,run TEXT NOT NULL,owner TEXT NOT NULL,day TEXT NOT NULL,operation TEXT NOT NULL,state TEXT NOT NULL,reserved_input INTEGER NOT NULL,reserved_output INTEGER NOT NULL,charged_tokens INTEGER NOT NULL,charged_cost INTEGER NOT NULL,rate TEXT NOT NULL,data TEXT NOT NULL,created INTEGER NOT NULL)`,
	`CREATE INDEX IF NOT EXISTS budget_day ON budget_call(kb,day)`,
	`CREATE INDEX IF NOT EXISTS budget_run ON budget_call(kb,run)`,
	`CREATE TABLE IF NOT EXISTS kb_options(kb TEXT PRIMARY KEY NOT NULL,revision INTEGER NOT NULL,data TEXT NOT NULL,deleted INTEGER NOT NULL DEFAULT 0,updated INTEGER NOT NULL)`,
	`CREATE TABLE IF NOT EXISTS source_options(kb TEXT NOT NULL,source TEXT NOT NULL,revision INTEGER NOT NULL,data TEXT NOT NULL,PRIMARY KEY(kb,source))`,
	`CREATE TABLE IF NOT EXISTS generation_config(kb TEXT NOT NULL,generation TEXT NOT NULL,data TEXT NOT NULL,created INTEGER NOT NULL,PRIMARY KEY(kb,generation))`,
	`CREATE TABLE IF NOT EXISTS evidence_archive(ref TEXT PRIMARY KEY NOT NULL,kb TEXT NOT NULL,owner TEXT NOT NULL,epoch INTEGER NOT NULL,data TEXT NOT NULL,paths TEXT NOT NULL,created INTEGER NOT NULL)`,
	`CREATE TABLE IF NOT EXISTS evidence_owner(ref TEXT NOT NULL,item TEXT NOT NULL,revision TEXT NOT NULL,PRIMARY KEY(ref,item,revision))`,
	`CREATE TABLE IF NOT EXISTS projection_gc(edition TEXT PRIMARY KEY NOT NULL,kb TEXT NOT NULL,data TEXT NOT NULL,state TEXT NOT NULL,created INTEGER NOT NULL)`,
}

// A KB provider override is complete and explicit; omitted override inherits global providers.
// Settings change does not mutate published content or contact any model.
type KBSettings struct {
	ParserPlugin string                `json:"parser_plugin,omitempty"`
	Parser       string                `json:"parser,omitempty"`
	Revision     int                   `json:"revision"`
	Providers    *adapters.Config      `json:"providers,omitempty"`
	Graph        adapters.GraphOptions `json:"graph"`
	ChunkMax     int                   `json:"chunk_max_chars,omitempty"`
	Fields       map[string]string     `json:"fields,omitempty"`
	Dictionary   []string              `json:"dictionary,omitempty"`
	Budget       BudgetPolicy          `json:"budget"`
}
type SourceOptions struct {
	Revision int      `json:"revision"`
	Disabled bool     `json:"disabled"`
	Include  []string `json:"include,omitempty"`
	Exclude  []string `json:"exclude,omitempty"`
}

func (s *Store) KBSettings(ctx context.Context, kb string) (KBSettings, error) {
	k := KBSettings{}
	e := s.DB.Do(ctx, func(c *db.Conn) error {
		rows, e := c.Query("SELECT k.id,COALESCE(o.data,'{}') data,COALESCE(o.revision,0) revision,COALESCE(o.deleted,0) deleted FROM kb k LEFT JOIN kb_options o ON o.kb=k.id WHERE k.id=?", kb)
		if e != nil {
			return e
		}
		if len(rows) != 1 || db.Int(rows[0], "deleted") != 0 {
			return core.Err("NOT_FOUND", "knowledge base not found", 404)
		}
		if e = json.Unmarshal([]byte(db.String(rows[0], "data")), &k); e != nil {
			return e
		}
		k.Revision = int(db.Int(rows[0], "revision"))
		return nil
	})
	return k, e
}
func (s *Store) SetKBSettings(ctx context.Context, kb string, k KBSettings, expected int) (KBSettings, error) {
	if k.ParserPlugin != "" && !core.ValidKB(k.ParserPlugin) {
		return k, core.Err("INVALID_ARGUMENT", "invalid parser plugin ID", 400)
	}
	if k.Parser != "" && k.Parser != "builtin" && k.Parser != "commonmark" {
		return k, core.Err("INVALID_ARGUMENT", "parser must be builtin or commonmark", 400)
	}
	if k.Graph.Model != nil {
		if e := ValidateSecretReferences(adapters.Config{Chat: k.Graph.Model}); e != nil {
			return k, e
		}
	}
	if k.ChunkMax != 0 && (k.ChunkMax < 100 || k.ChunkMax > 20000) {
		return k, core.Err("INVALID_ARGUMENT", "chunk_max_chars is 100..20000", 400)
	}
	if len(k.Dictionary) > 10000 {
		return k, core.Err("INVALID_ARGUMENT", "dictionary exceeds 10000 terms", 400)
	}
	for _, w := range k.Dictionary {
		if strings.TrimSpace(w) == "" || len(w) > 256 {
			return k, core.Err("INVALID_ARGUMENT", "dictionary terms must contain 1..256 bytes", 400)
		}
	}
	for name, t := range k.Fields {
		if !core.ValidKB(name) || !core.Subset([]string{t}, []string{"string", "number", "boolean", "date"}) {
			return k, core.Err("INVALID_ARGUMENT", "invalid registered field name/type", 400)
		}
	}
	if e := adapters.ValidateGraphOptions(k.Graph); e != nil {
		return k, e
	}
	if e := ValidateBudget(k.Budget); e != nil {
		return k, e
	}
	if k.Providers != nil {
		config, e := adapters.ParseConfig([]byte(core.JSON(k.Providers)))
		if e != nil {
			return k, e
		}
		if len(config.Extractor) > 0 || len(config.ParserCommand) > 0 || len(config.Plugins) > 0 {
			return k, core.Err("INVALID_ARGUMENT", "executables must be registered in server startup configuration", 400)
		}
		if e = ValidateSecretReferences(config); e != nil {
			return k, e
		}
		k.Providers = &config
	}
	e := s.DB.Tx(ctx, true, func(c *db.Conn) error {
		rs, e := c.Query("SELECT id FROM kb WHERE id=?", kb)
		if e != nil {
			return e
		}
		if len(rs) != 1 {
			return core.Err("NOT_FOUND", "knowledge base not found", 404)
		}
		rows, e := c.Query("SELECT revision,deleted FROM kb_options WHERE kb=?", kb)
		if e != nil {
			return e
		}
		rev := 0
		if len(rows) > 0 {
			if db.Int(rows[0], "deleted") != 0 {
				return core.Err("NOT_FOUND", "knowledge base removed", 404)
			}
			rev = int(db.Int(rows[0], "revision"))
		}
		if rev != expected {
			return core.Err("VERSION_CONFLICT", "knowledge base configuration changed", 409)
		}
		busy, e := c.Query("SELECT id FROM job WHERE kb=? AND state='running' LIMIT 1", kb)
		if e != nil {
			return e
		}
		if len(busy) > 0 {
			return core.Err("VERSION_CONFLICT", "wait for current KB task before changing its execution settings", 409)
		}
		k.Revision = rev + 1
		return c.Exec("INSERT INTO kb_options(kb,revision,data,updated) VALUES(?,?,?,?) ON CONFLICT(kb) DO UPDATE SET revision=excluded.revision,data=excluded.data,updated=excluded.updated", kb, k.Revision, core.JSON(k), time.Now().Unix())
	})
	return k, e
}
func ValidateSecretReferences(c adapters.Config) error {
	for _, ep := range []*adapters.Endpoint{c.Embedding, c.Chat, c.Rerank, c.Qdrant, c.Graph.Model} {
		if ep != nil && ep.Key != "" && !strings.HasPrefix(ep.Key, "env:") && !strings.HasPrefix(ep.Key, "file:") {
			return core.Err("INVALID_ARGUMENT", "credentials require env/file references", 400)
		}
	}
	if c.Neo4j != nil && c.Neo4j.Password != "" && !strings.HasPrefix(c.Neo4j.Password, "env:") && !strings.HasPrefix(c.Neo4j.Password, "file:") {
		return core.Err("INVALID_ARGUMENT", "Neo4j credentials require env/file references", 400)
	}
	for _, p := range c.External {
		if p.Key != "" && !strings.HasPrefix(p.Key, "env:") && !strings.HasPrefix(p.Key, "file:") {
			return core.Err("INVALID_ARGUMENT", "external credentials require env/file references", 400)
		}
	}
	return nil
}
func noKBJob(c *db.Conn, kb string) error {
	r, e := c.Query("SELECT id FROM job WHERE kb=? AND state IN ('queued','running') LIMIT 1", kb)
	if e != nil {
		return e
	}
	if len(r) > 0 {
		return core.Err("VERSION_CONFLICT", "finish or cancel pending KB tasks first", 409)
	}
	return nil
}
func (s *Store) RenameKB(ctx context.Context, kb, name string) error {
	if strings.TrimSpace(name) == "" || len(name) > 200 {
		return core.Err("INVALID_ARGUMENT", "name must contain 1..200 bytes", 400)
	}
	if _, e := s.KBSettings(ctx, kb); e != nil {
		return e
	}
	return s.DB.Do(ctx, func(c *db.Conn) error { return c.Exec("UPDATE kb SET name=? WHERE id=?", name, kb) })
}

// DeleteKB retires current publication and withdraws references. Source files are not touched.
// The slug remains reserved to prevent identity reuse; this is logical product deletion, not disk erasure.
func (s *Store) DeleteKB(ctx context.Context, kb string) error {
	if _, e := s.KBSettings(ctx, kb); e != nil {
		return e
	}
	return s.DB.Tx(ctx, true, func(c *db.Conn) error {
		if e := noKBJob(c, kb); e != nil {
			return e
		}
		qs := []string{"UPDATE edition SET state='retired' WHERE kb=? AND state='active'", "UPDATE file SET active='',withdrawn=1 WHERE kb=?", "UPDATE knowledge SET state='blocked' WHERE kb=?", "DELETE FROM source_policy WHERE kb=?"}
		for _, q := range qs {
			if e := c.Exec(q, kb); e != nil {
				return e
			}
		}
		if e := c.Exec("INSERT INTO kb_options(kb,revision,data,deleted,updated) VALUES(?,1,'{}',1,?) ON CONFLICT(kb) DO UPDATE SET deleted=1,revision=revision+1", kb, time.Now().Unix()); e != nil {
			return e
		}
		return c.Exec("UPDATE meta SET v=CAST(v AS INTEGER)+1 WHERE k='epoch'")
	})
}
func (s *Store) ChangeSource(ctx context.Context, kb, id string, src *Source, opt SourceOptions, expected int, remove bool) error {
	old, e := s.Source(ctx, kb, id)
	if e != nil {
		return e
	}
	if src != nil {
		if src.Kind != "" && src.Kind != old.Kind {
			return core.Err("INVALID_ARGUMENT", "source kind is immutable; register another source", 400)
		}
		if src.Mount != "" && src.Mount != old.Mount {
			return core.Err("INVALID_ARGUMENT", "mount is immutable; register another source", 400)
		}
		// Endpoints are a binding, not a label. Changing a binding requires a new source ID.
		if src.Root != "" && src.Root != old.Root {
			return core.Err("INVALID_ARGUMENT", "source root is immutable; add a new source to change its binding", 400)
		}
	}
	for _, p := range append(append([]string{}, opt.Include...), opt.Exclude...) {
		if e := core.ValidateGlob(p); e != nil {
			return e
		}
	}
	return s.DB.Tx(ctx, true, func(c *db.Conn) error {
		if e := noKBJob(c, kb); e != nil {
			return e
		}
		rows, e := c.Query("SELECT revision FROM source_options WHERE kb=? AND source=?", kb, id)
		if e != nil {
			return e
		}
		rev := 0
		if len(rows) > 0 {
			rev = int(db.Int(rows[0], "revision"))
		}
		if expected != rev {
			return core.Err("VERSION_CONFLICT", "source configuration changed", 409)
		}
		if remove {
			if e := c.Exec("INSERT OR IGNORE INTO source_retired(kb,source,created) VALUES(?,?,?)", kb, id, time.Now().Unix()); e != nil {
				return e
			}
			for _, q := range []string{"DELETE FROM source_policy WHERE kb=? AND source=?", "DELETE FROM source_state WHERE kb=? AND source=?", "DELETE FROM source_remote WHERE kb=? AND source=?", "DELETE FROM source_config WHERE kb=? AND source=?", "DELETE FROM source_options WHERE kb=? AND source=?"} {
				if e = c.Exec(q, kb, id); e != nil {
					return e
				}
			}
			if e = c.Exec("UPDATE edition SET state='retired' WHERE id IN (SELECT active FROM file WHERE kb=? AND source=?)", kb, id); e != nil {
				return e
			}
			if e = c.Exec("UPDATE file SET active='' WHERE kb=? AND source=?", kb, id); e != nil {
				return e
			}
			if e = c.Exec("UPDATE knowledge SET state='needs_review' WHERE id IN (SELECT item_id FROM dependency WHERE kb=? AND path IN (SELECT path FROM file WHERE kb=? AND source=?)) AND state='published'", kb, kb, id); e != nil {
				return e
			}
			return c.Exec("DELETE FROM source WHERE kb=? AND id=?", kb, id)
		}
		opt.Revision = rev + 1
		if e = c.Exec("INSERT INTO source_options(kb,source,revision,data) VALUES(?,?,?,?) ON CONFLICT(kb,source) DO UPDATE SET revision=excluded.revision,data=excluded.data", kb, id, opt.Revision, core.JSON(opt)); e != nil {
			return e
		}
		// A disabled source remains queryable at its last published state, but stops all scanning and hooks.
		return nil
	})
}
func (s *Store) SaveGeneration(ctx context.Context, kb string, cfg adapters.Config) error {
	if cfg.Embedding == nil {
		return nil
	}
	g := cfg.Embedding.SemanticID()
	// Only retrieval dependencies; no graph/chat prompt or operating state.
	c := adapters.Config{Embedding: cfg.Embedding, Qdrant: cfg.Qdrant}
	return s.DB.Do(ctx, func(cn *db.Conn) error {
		return cn.Exec("INSERT OR IGNORE INTO generation_config(kb,generation,data,created) VALUES(?,?,?,?)", kb, g, core.JSON(c), time.Now().Unix())
	})
}
func (s *Store) Generation(ctx context.Context, kb, g string) (adapters.Config, error) {
	var cfg adapters.Config
	e := s.DB.Do(ctx, func(c *db.Conn) error {
		r, e := c.Query("SELECT data FROM generation_config WHERE kb=? AND generation=?", kb, g)
		if e != nil {
			return e
		}
		if len(r) != 1 {
			return core.Err("MODEL_MISMATCH", "generation descriptor is missing", 409)
		}
		return json.Unmarshal([]byte(db.String(r[0], "data")), &cfg)
	})
	return cfg, e
}
func (s *Store) Discover(ctx context.Context, kbs []string) (map[string]any, error) {
	out := map[string]any{"origin_id": s.Origin, "documents": 0, "chunks": 0, "fields": []string{}, "metadata_only": true, "observed_at": time.Now().Unix()}
	n, m := 0, 0
	fields := map[string]bool{}
	types := map[string]bool{}
	e := s.DB.Tx(ctx, false, func(c *db.Conn) error {
		for _, kb := range core.Unique(kbs) {
			rs, e := c.Query("SELECT COUNT(*) n,COALESCE(SUM(json_array_length(e.data,'$.chunks')),0) m FROM file f JOIN edition e ON e.id=f.active WHERE f.kb=? AND f.withdrawn=0", kb)
			if e != nil {
				return e
			}
			n += int(db.Int(rs[0], "n"))
			m += int(db.Int(rs[0], "m"))
			rs, e = c.Query("SELECT DISTINCT j.key AS name FROM file f JOIN edition e ON e.id=f.active,json_each(e.data,'$.fields') j WHERE f.kb=? AND f.withdrawn=0", kb)
			if e != nil {
				return e
			}
			for _, r := range rs {
				fields[db.String(r, "name")] = true
			}
			rs, e = c.Query("SELECT DISTINCT json_extract(j.value,'$.relation') AS name FROM file f JOIN edition e ON e.id=f.active,json_each(e.data,'$.supports') j WHERE f.kb=? AND f.withdrawn=0", kb)
			if e != nil {
				return e
			}
			for _, r := range rs {
				types[db.String(r, "name")] = true
			}
		}
		return nil
	})
	fs, ts := []string{}, []string{}
	for f := range fields {
		fs = append(fs, f)
	}
	for f := range types {
		ts = append(ts, f)
	}
	out["documents"] = n
	out["chunks"] = m
	out["fields"] = fs
	out["relation_types"] = ts
	return out, e
}

// VisibleKBs is for navigation and capability discovery only. Empty is a valid
// administrative view, unlike a query scope which must select at least one KB.
func (s *Store) VisibleKBs(ctx context.Context, p Principal) ([]string, error) {
	rows, err := s.KBs(ctx)
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for _, row := range rows {
		id := row["id"].(string)
		if p.Role == "admin" || core.Subset([]string{id}, p.KBs) {
			ids = append(ids, id)
		}
	}
	return ids, nil
}
