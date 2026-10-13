package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"github.com/BlueSkyXN/XRAG/core"
	db "github.com/BlueSkyXN/XRAG/internal/sqlite"
)

type Store struct {
	DB     *db.DB
	Dir    string
	Origin string
}

func Open(dir string) (*Store, error) {
	abs, e := filepath.Abs(dir)
	if e != nil {
		return nil, e
	}
	if e = os.MkdirAll(abs, 0700); e != nil {
		return nil, e
	}
	d, e := db.Open(filepath.Join(abs, "xrag.sqlite"))
	if e != nil {
		return nil, e
	}
	s := &Store{DB: d, Dir: abs}
	e = s.DB.Tx(context.Background(), true, func(c *db.Conn) error {
		existing, err := c.Query("SELECT name FROM sqlite_master WHERE type='table' AND name='meta'")
		if err != nil {
			return err
		}
		if len(existing) > 0 {
			version, err := c.Query("SELECT v FROM meta WHERE k='store_schema'")
			if err != nil {
				return err
			}
			if len(version) != 1 || db.String(version[0], "v") != "demo9" {
				return core.Err("STORAGE_VERSION_UNSUPPORTED", "use a fresh Demo.9 data directory or a Demo.9 snapshot; previous layouts are not migrated", 409)
			}
		}
		for _, q := range append(append(append(append(append(append(append(append([]string{}, schema...), extensionSchema...), productSchema...), featureSchema...), iteration5Schema...), iteration6Schema...), housekeepingSchema...), append(append(feedbackEvalSchema, iteration7Schema...), generationRegistrySchema...)...) {
			if e := c.Exec(q); e != nil {
				return e
			}
		}
		for _, q := range checkpointSchema {
			if e := c.Exec(q); e != nil {
				return e
			}
		}
		if e := c.Exec("INSERT OR IGNORE INTO meta(k,v) VALUES('origin',?)", core.ID("origin_")); e != nil {
			return e
		}
		if e := c.Exec("INSERT OR IGNORE INTO meta(k,v) VALUES('store_schema','demo9')"); e != nil {
			return e
		}
		if e := c.Exec("INSERT OR IGNORE INTO meta(k,v) VALUES('epoch','0')"); e != nil {
			return e
		}
		r, e := c.Query("SELECT v FROM meta WHERE k='origin'")
		if e == nil {
			s.Origin = db.String(r[0], "v")
		}
		return e
	})
	if e != nil {
		d.Close()
		return nil, e
	}
	return s, nil
}
func (s *Store) Close() error { return s.DB.Close() }

var schema = []string{
	`CREATE TABLE IF NOT EXISTS meta(k TEXT PRIMARY KEY NOT NULL,v TEXT NOT NULL)`,
	`CREATE TABLE IF NOT EXISTS kb(id TEXT PRIMARY KEY NOT NULL,name TEXT NOT NULL,created INTEGER NOT NULL)`,
	`CREATE TABLE IF NOT EXISTS source(kb TEXT NOT NULL,id TEXT NOT NULL,root TEXT NOT NULL,mount TEXT NOT NULL,kind TEXT NOT NULL,PRIMARY KEY(kb,id),FOREIGN KEY(kb) REFERENCES kb(id))`,
	`CREATE TABLE IF NOT EXISTS file(kb TEXT NOT NULL,path TEXT NOT NULL,source TEXT NOT NULL,active TEXT NOT NULL DEFAULT '',withdrawn INTEGER NOT NULL DEFAULT 0,PRIMARY KEY(kb,path))`,
	`CREATE TABLE IF NOT EXISTS edition(id TEXT PRIMARY KEY NOT NULL,kb TEXT NOT NULL,path TEXT NOT NULL,hash TEXT NOT NULL,state TEXT NOT NULL,data TEXT NOT NULL,created INTEGER NOT NULL)`,
	`CREATE TABLE IF NOT EXISTS chunk(id TEXT PRIMARY KEY NOT NULL,edition TEXT NOT NULL,kb TEXT NOT NULL,path TEXT NOT NULL,data TEXT NOT NULL)`,
	`CREATE VIRTUAL TABLE IF NOT EXISTS fts USING fts5(chunk_id UNINDEXED,tokens, tokenize='unicode61')`,
	`CREATE TABLE IF NOT EXISTS cache(id TEXT PRIMARY KEY NOT NULL,kind TEXT NOT NULL,owner TEXT NOT NULL,expires INTEGER NOT NULL,data TEXT NOT NULL)`,
	`CREATE TABLE IF NOT EXISTS model_cache(id TEXT PRIMARY KEY NOT NULL,data TEXT NOT NULL)`,
	`CREATE TABLE IF NOT EXISTS job(id TEXT PRIMARY KEY NOT NULL,kb TEXT NOT NULL,kind TEXT NOT NULL,state TEXT NOT NULL,payload TEXT NOT NULL,result TEXT NOT NULL DEFAULT '{}',error TEXT NOT NULL DEFAULT '',owner TEXT NOT NULL,created INTEGER NOT NULL,started INTEGER NOT NULL DEFAULT 0,deadline INTEGER NOT NULL DEFAULT 0,queue_expires INTEGER NOT NULL,lease INTEGER NOT NULL DEFAULT 0,token TEXT NOT NULL DEFAULT '',attempt INTEGER NOT NULL DEFAULT 0,cancel INTEGER NOT NULL DEFAULT 0)`,
	`CREATE UNIQUE INDEX IF NOT EXISTS job_running_kb ON job(kb) WHERE state='running'`,
	`CREATE TABLE IF NOT EXISTS idempotency(owner TEXT NOT NULL,operation TEXT NOT NULL,key TEXT NOT NULL,fingerprint TEXT NOT NULL,job_id TEXT NOT NULL,PRIMARY KEY(owner,operation,key))`,
	`CREATE TABLE IF NOT EXISTS call_usage(id TEXT PRIMARY KEY NOT NULL,job_id TEXT NOT NULL,kind TEXT NOT NULL,state TEXT NOT NULL,usage TEXT NOT NULL,created INTEGER NOT NULL)`,
	`CREATE TABLE IF NOT EXISTS api_key(id TEXT PRIMARY KEY NOT NULL,name TEXT NOT NULL,hash TEXT NOT NULL UNIQUE,role TEXT NOT NULL,kbs TEXT NOT NULL,created INTEGER NOT NULL,disabled INTEGER NOT NULL DEFAULT 0)`,
	`CREATE TABLE IF NOT EXISTS knowledge(id TEXT PRIMARY KEY NOT NULL,kb TEXT NOT NULL,title TEXT NOT NULL,kind TEXT NOT NULL,draft TEXT NOT NULL,published TEXT NOT NULL DEFAULT '',state TEXT NOT NULL,created INTEGER NOT NULL)`,
	`CREATE TABLE IF NOT EXISTS knowledge_revision(id TEXT PRIMARY KEY NOT NULL,item_id TEXT NOT NULL,data TEXT NOT NULL,created INTEGER NOT NULL)`,
	`CREATE TABLE IF NOT EXISTS dependency(item_id TEXT NOT NULL,kb TEXT NOT NULL,path TEXT NOT NULL,PRIMARY KEY(item_id,kb,path))`,
	`CREATE TABLE IF NOT EXISTS feedback(id TEXT PRIMARY KEY NOT NULL,kb TEXT NOT NULL,ref TEXT NOT NULL,label TEXT NOT NULL,note TEXT NOT NULL,owner TEXT NOT NULL,created INTEGER NOT NULL)`,
}

func (s *Store) Epoch(ctx context.Context) (int64, error) {
	var n int64
	e := s.DB.Do(ctx, func(c *db.Conn) error {
		r, e := c.Query("SELECT CAST(v AS INTEGER) AS n FROM meta WHERE k='epoch'")
		if e == nil {
			n = db.Int(r[0], "n")
		}
		return e
	})
	return n, e
}
func epoch(c *db.Conn) (int64, error) {
	r, e := c.Query("SELECT CAST(v AS INTEGER) AS n FROM meta WHERE k='epoch'")
	if e != nil {
		return 0, e
	}
	return db.Int(r[0], "n"), nil
}
func (s *Store) CreateKB(ctx context.Context, id, name string) error {
	if !core.ValidKB(id) {
		return core.Err("INVALID_ARGUMENT", "kb must be a short slug", 400)
	}
	if name == "" {
		name = id
	}
	return s.DB.Do(ctx, func(c *db.Conn) error {
		return c.Exec("INSERT INTO kb(id,name,created) VALUES(?,?,?)", id, name, time.Now().Unix())
	})
}
func (s *Store) KBs(ctx context.Context) ([]map[string]any, error) {
	out := []map[string]any{}
	e := s.DB.Do(ctx, func(c *db.Conn) error {
		r, e := c.Query("SELECT k.id,k.name,(SELECT count(*) FROM file f WHERE f.kb=k.id AND f.active<>'' AND f.withdrawn=0) AS files FROM kb k WHERE NOT EXISTS(SELECT 1 FROM kb_options o WHERE o.kb=k.id AND o.deleted=1) ORDER BY k.id")
		for _, v := range r {
			out = append(out, map[string]any(v))
		}
		return e
	})
	return out, e
}

type Source struct {
	KB      string        `json:"kb"`
	ID      string        `json:"id"`
	Root    string        `json:"root"`
	Mount   string        `json:"mount"`
	Kind    string        `json:"kind"`
	Ref     string        `json:"git_ref,omitempty"`
	Remote  *RemoteSource `json:"remote,omitempty"`
	Options SourceOptions `json:"options"`
}

func (s *Store) AddSource(ctx context.Context, src Source) error {
	for _, p := range append(append([]string{}, src.Options.Include...), src.Options.Exclude...) {
		if e := core.ValidateGlob(p); e != nil {
			return e
		}
	}
	if _, err := s.KBSettings(ctx, src.KB); err != nil {
		return err
	}
	if strings.TrimSpace(src.Root) == "" {
		return core.Err("INVALID_ARGUMENT", "source root is required", 400)
	}
	if !core.ValidKB(src.ID) || !core.ValidKB(src.KB) {
		return core.Err("INVALID_ARGUMENT", "invalid source id", 400)
	}
	if src.Kind != "path" && src.Kind != "builtin" && src.Kind != "git" && src.Kind != "s3" && src.Kind != "weknora" {
		return core.Err("MODE_UNSUPPORTED", "supported sources: path, builtin, git, s3, weknora", 422)
	}
	var e error
	if src.Kind == "git" {
		if src.Ref == "" {
			src.Ref = "HEAD"
		}
		if len(src.Ref) > 256 || !regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_./-]*$`).MatchString(src.Ref) || strings.Contains(src.Ref, "..") {
			return core.Err("INVALID_ARGUMENT", "git_ref must be a ref name or commit id", 400)
		}
	} else if src.Ref != "" {
		return core.Err("INVALID_ARGUMENT", "git_ref is only valid for git sources", 400)
	}
	if src.Kind == "s3" || src.Kind == "weknora" {
		if e = validateRemoteSource(&src); e != nil {
			return e
		}
	} else if src.Remote != nil {
		return core.Err("INVALID_ARGUMENT", "remote settings only apply to s3/weknora", 400)
	} else if strings.Contains(src.Root, "://") {
		u, err := url.Parse(src.Root)
		if src.Kind != "git" || err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return core.Err("INVALID_ARGUMENT", "remote git source requires an HTTPS URL without credentials or query parameters", 400)
		}
	} else {
		src.Root, e = filepath.Abs(src.Root)
		if e != nil {
			return e
		}
		info, err := os.Stat(src.Root)
		if err != nil || !info.IsDir() {
			return core.Err("INVALID_ARGUMENT", "source root must be an existing directory or a git HTTPS URL", 400)
		}
	}
	if src.Mount != "" {
		if _, e = core.CleanPath(src.Mount); e != nil {
			return e
		}
		if strings.HasPrefix(src.Mount, "_knowledge") {
			return core.Err("INVALID_ARGUMENT", "reserved mount", 400)
		}
	}
	return s.DB.Tx(ctx, true, func(c *db.Conn) error {
		reserved, err := c.Query("SELECT 1 FROM source_retired WHERE kb=? AND source=?", src.KB, src.ID)
		if err != nil {
			return err
		}
		if len(reserved) > 0 {
			return core.Err("VERSION_CONFLICT", "removed source IDs remain reserved; use a new ID", 409)
		}
		rs, e := c.Query("SELECT mount FROM source WHERE kb=?", src.KB)
		if e != nil {
			return e
		}
		for _, r := range rs {
			m := db.String(r, "mount")
			if core.PathMatch(src.Mount, m) || core.PathMatch(m, src.Mount) {
				return core.Err("VERSION_CONFLICT", "source mount overlaps another source", 409)
			}
		}
		if e = c.Exec("INSERT INTO source(kb,id,root,mount,kind) VALUES(?,?,?,?,?)", src.KB, src.ID, src.Root, src.Mount, src.Kind); e != nil {
			return e
		}
		if e = c.Exec("INSERT INTO source_config(kb,source,git_ref) VALUES(?,?,?)", src.KB, src.ID, src.Ref); e != nil {
			return e
		}
		if e = c.Exec("INSERT INTO source_options(kb,source,revision,data) VALUES(?,?,0,?)", src.KB, src.ID, core.JSON(src.Options)); e != nil {
			return e
		}
		if src.Remote != nil {
			return c.Exec("INSERT INTO source_remote(kb,source,data) VALUES(?,?,?)", src.KB, src.ID, core.JSON(src.Remote))
		}
		return nil
	})
}
func (s *Store) Sources(ctx context.Context, kb string) ([]Source, error) {
	out := []Source{}
	e := s.DB.Do(ctx, func(c *db.Conn) error {
		rows, e := c.Query("SELECT s.*, COALESCE(c.git_ref,'') git_ref, COALESCE(r.data,'null') remote_data,COALESCE(o.data,'{}') options_data FROM source s LEFT JOIN source_config c ON s.kb=c.kb AND s.id=c.source LEFT JOIN source_remote r ON s.kb=r.kb AND s.id=r.source LEFT JOIN source_options o ON o.kb=s.kb AND o.source=s.id WHERE s.kb=? ORDER BY s.id", kb)
		for _, r := range rows {
			var remote *RemoteSource
			if e := json.Unmarshal([]byte(db.String(r, "remote_data")), &remote); e != nil {
				return e
			}
			var opt SourceOptions
			if e := json.Unmarshal([]byte(db.String(r, "options_data")), &opt); e != nil {
				return e
			}
			out = append(out, Source{Options: opt, KB: db.String(r, "kb"), ID: db.String(r, "id"), Root: db.String(r, "root"), Mount: db.String(r, "mount"), Kind: db.String(r, "kind"), Ref: db.String(r, "git_ref"), Remote: remote})
		}
		return e
	})
	return out, e
}
func (s *Store) Snapshot(ctx context.Context, kbs []string) (core.View, error) {
	v := core.View{Origin: s.Origin, Mode: "server", At: time.Now(), Documents: []core.Document{}}
	e := s.DB.Tx(ctx, false, func(c *db.Conn) error {
		var e error
		v.Epoch, e = epoch(c)
		if e != nil {
			return e
		}
		for _, kb := range core.Unique(kbs) {
			rs, e := c.Query("SELECT e.data FROM file f JOIN edition e ON e.id=f.active WHERE f.kb=? AND f.withdrawn=0 ORDER BY f.path", kb)
			if e != nil {
				return e
			}
			for _, r := range rs {
				var d core.Document
				if e = json.Unmarshal([]byte(db.String(r, "data")), &d); e != nil {
					return e
				}
				v.Documents = append(v.Documents, d)
			}
		}
		return nil
	})
	return v, e
}
func (s *Store) Read(ctx context.Context, r core.Ref) (core.Document, *core.Chunk, string, error) {
	var d core.Document
	var ch *core.Chunk
	fresh := "historical"
	if r.Origin != s.Origin {
		return d, nil, fresh, core.Err("REF_ORIGIN_MISMATCH", "select the profile that issued this reference", 409)
	}
	e := s.DB.Do(ctx, func(c *db.Conn) error {
		rows, e := c.Query("SELECT active,withdrawn FROM file WHERE kb=? AND path=?", r.KB, r.Path)
		if e != nil {
			return e
		}
		if len(rows) == 0 {
			return core.Err("REF_GONE", "file no longer available", 410)
		}
		if db.Int(rows[0], "withdrawn") != 0 {
			return core.Err("REF_WITHDRAWN", "source was explicitly withdrawn", 410)
		}
		if db.String(rows[0], "active") == r.Edition {
			fresh = "current"
		}
		rs, e := c.Query("SELECT data,state FROM edition WHERE id=? AND kb=? AND path=?", r.Edition, r.KB, r.Path)
		if e != nil {
			return e
		}
		if len(rs) == 0 || db.String(rs[0], "state") == "staging" || db.String(rs[0], "state") == "aborted" {
			return core.Err("REF_GONE", "edition unavailable or unpublished", 410)
		}
		if e = json.Unmarshal([]byte(db.String(rs[0], "data")), &d); e != nil {
			return e
		}
		if d.FileHash != r.FileHash {
			return core.Err("REF_GONE", "reference fingerprint mismatch", 410)
		}
		if r.ChunkID != "" {
			for i := range d.Chunks {
				if d.Chunks[i].ID == r.ChunkID {
					x := d.Chunks[i]
					ch = &x
					break
				}
			}
			if ch == nil {
				return core.Err("REF_GONE", "chunk not in requested edition", 410)
			}
		}
		return nil
	})
	return d, ch, fresh, e
}
func (s *Store) Lexical(ctx context.Context, v core.View, q string) ([]core.Ranked, error) {
	groups := map[string]map[string]bool{}
	words := map[string][]string{}
	for _, d := range v.Documents {
		k := core.Hash(d.Dictionary)
		if groups[k] == nil {
			groups[k] = map[string]bool{}
			words[k] = d.Dictionary
		}
		for _, ch := range d.Chunks {
			groups[k][ch.ID] = true
		}
	}
	if len(groups) == 0 {
		if _, e := core.FTSQuery(q); e != nil {
			return nil, e
		}
		return []core.Ranked{}, nil
	}
	out := []core.Ranked{}
	for k, ids := range groups {
		expression, e := core.DictionaryQuery(q, words[k])
		if e != nil {
			return nil, e
		}
		e = s.DB.Do(ctx, func(c *db.Conn) error {
			rows, e := c.Query("SELECT chunk_id,bm25(fts) AS score FROM fts WHERE fts MATCH ? AND chunk_id IN(SELECT value FROM json_each(?)) ORDER BY score,chunk_id LIMIT 50001", expression, core.JSON(sortedMapIDs(ids)))
			if e != nil {
				return e
			}
			if len(rows) > 50000 {
				return core.Err("CANDIDATE_LIMIT", "keyword candidate limit reached", 422)
			}
			for _, row := range rows {
				id := db.String(row, "chunk_id")
				if ids[id] {
					out = append(out, core.Ranked{ID: id, Score: -db.Float(row, "score")})
				}
			}
			return nil
		})
		if e != nil {
			return nil, e
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score == out[j].Score {
			return out[i].ID < out[j].ID
		}
		return out[i].Score > out[j].Score
	})
	return out, nil
}

func (s *Store) PutCache(ctx context.Context, id string, v core.Cache) error {
	return s.DB.Do(ctx, func(c *db.Conn) error {
		if e := c.Exec("DELETE FROM cache WHERE expires<?", time.Now().Unix()); e != nil {
			return e
		}
		return c.Exec("INSERT INTO cache(id,kind,owner,expires,data) VALUES(?,?,?,?,?)", id, v.Kind, v.Owner, v.Expires, core.JSON(v))
	})
}
func (s *Store) GetCache(ctx context.Context, id string) (core.Cache, error) {
	var v core.Cache
	e := s.DB.Do(ctx, func(c *db.Conn) error {
		rs, e := c.Query("SELECT data,kind,owner,expires FROM cache WHERE id=?", id)
		if e != nil {
			return e
		}
		if len(rs) == 0 {
			return core.Err("RESULT_EXPIRED", "saved result or handle unavailable", 410)
		}
		if e := json.Unmarshal([]byte(db.String(rs[0], "data")), &v); e != nil {
			return e
		}
		v.Expires = db.Int(rs[0], "expires")
		v.Kind = db.String(rs[0], "kind")
		v.Owner = db.String(rs[0], "owner")
		return nil
	})
	return v, e
}
func (s *Store) ModelGet(ctx context.Context, id string) (json.RawMessage, bool, error) {
	var out json.RawMessage
	ok := false
	e := s.DB.Do(ctx, func(c *db.Conn) error {
		r, e := c.Query("SELECT data FROM model_cache WHERE id=?", id)
		if e == nil && len(r) > 0 {
			out = json.RawMessage(db.String(r[0], "data"))
			ok = true
		}
		return e
	})
	return out, ok, e
}
func (s *Store) ModelPut(ctx context.Context, id string, v any) error {
	return s.DB.Do(ctx, func(c *db.Conn) error {
		return c.Exec("INSERT OR REPLACE INTO model_cache(id,data) VALUES(?,?)", id, core.JSON(v))
	})
}
func (s *Store) Current(ctx context.Context, kb, p string) (core.Document, bool, error) {
	var d core.Document
	exists := false
	e := s.DB.Do(ctx, func(c *db.Conn) error {
		rs, e := c.Query("SELECT e.data,f.withdrawn FROM file f LEFT JOIN edition e ON e.id=f.active WHERE f.kb=? AND f.path=?", kb, p)
		if e != nil {
			return e
		}
		if len(rs) > 0 && db.Int(rs[0], "withdrawn") != 0 {
			return core.Err("REF_WITHDRAWN", "source path is withdrawn; restoring requires a new path", 410)
		}
		if len(rs) > 0 && db.String(rs[0], "data") != "" {
			exists = true
			return json.Unmarshal([]byte(db.String(rs[0], "data")), &d)
		}
		return nil
	})
	return d, exists, e
}
func (s *Store) Stage(ctx context.Context, d core.Document) error {
	compact := queryDocument(d)
	return s.DB.Tx(ctx, true, func(c *db.Conn) error {
		if e := c.Exec("INSERT INTO edition(id,kb,path,hash,state,data,created) VALUES(?,?,?,?,?,?,?)", d.Edition, d.KB, d.Path, d.FileHash, "staging", core.JSON(d), time.Now().Unix()); e != nil {
			return e
		}
		if e := c.Exec("INSERT INTO query_document(id,kb,path,data) VALUES(?,?,?,?)", d.Edition, d.KB, d.Path, core.JSON(compact)); e != nil {
			return e
		}
		for _, ch := range d.Chunks {
			if e := c.Exec("INSERT INTO chunk(id,edition,kb,path,data) VALUES(?,?,?,?,?)", ch.ID, d.Edition, d.KB, d.Path, core.JSON(ch)); e != nil {
				return e
			}
			if e := c.Exec("INSERT INTO fts(chunk_id,tokens) VALUES(?,?)", ch.ID, strings.Join(core.DictionaryTerms(ch.Title+" "+ch.Text, d.Dictionary, false), " ")); e != nil {
				return e
			}
		}
		return nil
	})
}
func fence(c *db.Conn, j Job) error {
	if j.ID == "" {
		return nil
	}
	rs, e := c.Query("SELECT state,token,lease,cancel,deadline FROM job WHERE id=?", j.ID)
	if e != nil {
		return e
	}
	if len(rs) != 1 || db.String(rs[0], "state") != "running" || db.String(rs[0], "token") != j.Token || db.Int(rs[0], "lease") <= time.Now().Unix() || db.Int(rs[0], "cancel") != 0 || db.Int(rs[0], "deadline") <= time.Now().Unix() {
		return core.Err("JOB_FENCE_LOST", "job expired, was cancelled or lost its lease", 409)
	}
	return nil
}
func (s *Store) Publish(ctx context.Context, d core.Document, j Job, item, rev string) error {
	return s.DB.Tx(ctx, true, func(c *db.Conn) error { return activateFile(c, d, j, item, rev) })
}
func activateFile(c *db.Conn, d core.Document, j Job, item, rev string) error {
	if e := fence(c, j); e != nil {
		return e
	}
	r, e := c.Query("SELECT active,withdrawn,source FROM file WHERE kb=? AND path=?", d.KB, d.Path)
	if e != nil {
		return e
	}
	if len(r) > 0 {
		if db.Int(r[0], "withdrawn") != 0 {
			return core.Err("REF_WITHDRAWN", "cannot republish withdrawn source", 410)
		}
		if db.String(r[0], "source") != d.Source {
			return core.Err("VERSION_CONFLICT", "another source owns this path", 409)
		}
		if e = c.Exec("UPDATE edition SET state='retired' WHERE id=?", db.String(r[0], "active")); e != nil {
			return e
		}
	}
	if item != "" {
		deps, de := c.Query("SELECT 1 FROM dependency d JOIN file f ON f.kb=d.kb AND f.path=d.path WHERE d.item_id=? AND f.withdrawn=1 LIMIT 1", item)
		if de != nil {
			return de
		}
		if len(deps) > 0 {
			return core.Err("REF_WITHDRAWN", "knowledge dependency was withdrawn before publication", 410)
		}
		rs, e := c.Query("SELECT draft FROM knowledge WHERE id=?", item)
		if e != nil {
			return e
		}
		if len(rs) != 1 || db.String(rs[0], "draft") != rev {
			return core.Err("VERSION_CONFLICT", "draft revision changed before publish", 409)
		}
	}
	if e = c.Exec("INSERT INTO file(kb,path,source,active,withdrawn) VALUES(?,?,?,?,0) ON CONFLICT(kb,path) DO UPDATE SET active=excluded.active", d.KB, d.Path, d.Source, d.Edition); e != nil {
		return e
	}
	if e = c.Exec("UPDATE edition SET state='active' WHERE id=? AND state='staging'", d.Edition); e != nil {
		return e
	}
	if c.Changes() != 1 {
		return core.Err("VERSION_CONFLICT", "edition cannot be activated", 409)
	}
	oldContentChanged := true
	if len(r) > 0 && db.String(r[0], "active") != "" {
		hs, er := c.Query("SELECT hash FROM edition WHERE id=?", db.String(r[0], "active"))
		if er != nil {
			return er
		}
		if len(hs) > 0 && db.String(hs[0], "hash") == d.FileHash {
			oldContentChanged = false
		}
	}
	if oldContentChanged {
		if e = c.Exec("UPDATE knowledge SET state='needs_review' WHERE id IN(SELECT item_id FROM dependency WHERE kb=? AND path=?) AND published<>'' AND state='published'", d.KB, d.Path); e != nil {
			return e
		}
	}
	if item != "" {
		if e = c.Exec("UPDATE knowledge SET published=?,state='published' WHERE id=?", rev, item); e != nil {
			return e
		}
		return recordAutoSync(c, d.KB, item, rev, j.Owner)
	}
	return nil
}
func (s *Store) PublishProjectionBatch(ctx context.Context, j Job, docs []core.Document, expected map[string]string, viewEpoch int64) error {
	return s.DB.Tx(ctx, true, func(c *db.Conn) error {
		current, e := epoch(c)
		if e != nil {
			return e
		}
		if current != viewEpoch {
			return core.Err("VIEW_INVALIDATED", "withdrawal changed during projection build", 409)
		}
		for _, d := range docs {
			r, e := c.Query("SELECT active FROM file WHERE kb=? AND path=?", d.KB, d.Path)
			if e != nil {
				return e
			}
			if len(r) != 1 || db.String(r[0], "active") != expected[d.Path] {
				return core.Err("VERSION_CONFLICT", "file changed while projections were prepared", 409)
			}
			if e = activateFile(c, d, j, "", ""); e != nil {
				return e
			}
		}
		return nil
	})
}
func (s *Store) Abort(ctx context.Context, edition string) error {
	return s.DB.Do(ctx, func(c *db.Conn) error {
		return c.Exec("UPDATE edition SET state='aborted' WHERE id=? AND state='staging'", edition)
	})
}
func (s *Store) RemoveMissing(ctx context.Context, src Source, seen map[string]bool, j Job) error {
	return s.DB.Tx(ctx, true, func(c *db.Conn) error {
		if e := fence(c, j); e != nil {
			return e
		}
		rs, e := c.Query("SELECT path,active FROM file WHERE kb=? AND source=? AND active<>'' AND withdrawn=0", src.KB, src.ID)
		if e != nil {
			return e
		}
		for _, r := range rs {
			p := db.String(r, "path")
			if !seen[p] {
				if e = c.Exec("UPDATE edition SET state='retired' WHERE id=?", db.String(r, "active")); e != nil {
					return e
				}
				if e = c.Exec("UPDATE file SET active='' WHERE kb=? AND path=?", src.KB, p); e != nil {
					return e
				}
				if e = c.Exec("UPDATE knowledge SET state='needs_review' WHERE id IN(SELECT item_id FROM dependency WHERE kb=? AND path=?) AND state='published'", src.KB, p); e != nil {
					return e
				}
			}
		}
		return nil
	})
}
func (s *Store) Withdraw(ctx context.Context, kb, p string) error {
	return s.DB.Tx(ctx, true, func(c *db.Conn) error {
		if e := c.Exec("UPDATE file SET withdrawn=1 WHERE kb=? AND path=?", kb, p); e != nil {
			return e
		}
		if c.Changes() == 0 {
			return core.Err("REF_GONE", "file not found", 410)
		}
		if e := c.Exec("UPDATE meta SET v=CAST(v AS INTEGER)+1 WHERE k='epoch'"); e != nil {
			return e
		}
		queue := []string{p}
		seen := map[string]bool{}
		for len(queue) > 0 {
			current := queue[0]
			queue = queue[1:]
			if seen[current] {
				continue
			}
			seen[current] = true
			rs, e := c.Query("SELECT item_id FROM dependency WHERE kb=? AND path=?", kb, current)
			if e != nil {
				return e
			}
			for _, r := range rs {
				id := db.String(r, "item_id")
				if e = c.Exec("UPDATE knowledge SET state='blocked' WHERE id=?", id); e != nil {
					return e
				}
				derived := "_knowledge/" + id + ".md"
				if e = c.Exec("UPDATE file SET withdrawn=1 WHERE kb=? AND path=?", kb, derived); e != nil {
					return e
				}
				queue = append(queue, derived)
			}
		}
		return nil
	})
}
func digest(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }

type Principal struct {
	ID   string   `json:"id"`
	Name string   `json:"name"`
	Role string   `json:"role"`
	KBs  []string `json:"kbs"`
}

var roles = map[string]int{"reader": 1, "contributor": 2, "maintainer": 3, "admin": 4}

func RoleAtLeast(p Principal, role string) bool { return roles[p.Role] >= roles[role] }
func (s *Store) CreateKey(ctx context.Context, name, role string, kbs []string) (string, error) {
	_, token, e := s.CreateManagedKey(ctx, name, role, kbs, 0)
	return token, e
}
func (s *Store) Authenticate(ctx context.Context, token string) (Principal, error) {
	p := Principal{}
	e := s.DB.Do(ctx, func(c *db.Conn) error {
		rs, e := c.Query("SELECT k.id,k.name,k.role,k.kbs FROM api_key k LEFT JOIN key_options o ON o.key_id=k.id WHERE k.hash=? AND k.disabled=0 AND (COALESCE(o.expires_at,0)=0 OR o.expires_at>?)", digest(token), time.Now().Unix())
		if e != nil {
			return e
		}
		if len(rs) != 1 {
			return core.Err("UNAUTHENTICATED", "provide an XRAG API key", 401)
		}
		p.ID = db.String(rs[0], "id")
		p.Name = db.String(rs[0], "name")
		p.Role = db.String(rs[0], "role")
		return json.Unmarshal([]byte(db.String(rs[0], "kbs")), &p.KBs)
	})
	return p, e
}
func (s *Store) Scope(ctx context.Context, p Principal, kbs []string) ([]string, error) {
	rs, e := s.KBs(ctx)
	if e != nil {
		return nil, e
	}
	all := []string{}
	for _, r := range rs {
		all = append(all, r["id"].(string))
	}
	allowed := p.KBs
	if p.Role == "admin" {
		allowed = all
	}
	if kbs == nil {
		kbs = allowed
	}
	kbs = core.Unique(kbs)
	if len(kbs) == 0 {
		return nil, core.Err("INVALID_ARGUMENT", "no knowledge bases selected", 400)
	}
	if !core.Subset(kbs, allowed) || !core.Subset(kbs, all) {
		return nil, core.Err("PERMISSION_DENIED", "knowledge base outside current scope", 403)
	}
	return kbs, nil
}
func (s *Store) CountKeys(ctx context.Context) (int, error) {
	n := 0
	e := s.DB.Do(ctx, func(c *db.Conn) error {
		r, e := c.Query("SELECT count(*) n FROM api_key")
		if e == nil {
			n = int(db.Int(r[0], "n"))
		}
		return e
	})
	return n, e
}
func (s *Store) Backup(ctx context.Context, dest string) error {
	abs, e := filepath.Abs(dest)
	if e != nil {
		return e
	}
	if _, e = os.Stat(abs); e == nil {
		return core.Err("VERSION_CONFLICT", "backup destination already exists", 409)
	}
	return s.DB.Do(ctx, func(c *db.Conn) error { return c.Exec("VACUUM INTO ?", abs) })
}
func (s *Store) String() string { return fmt.Sprintf("XRAG(%s)", s.Origin) }
