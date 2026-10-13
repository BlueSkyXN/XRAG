package store

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
	"github.com/BlueSkyXN/XRAG/core"
	db "github.com/BlueSkyXN/XRAG/internal/sqlite"
)

// Knowledge lifecycle tables; fresh-install format for the current implementation.
var featureSchema = []string{
	`CREATE TABLE IF NOT EXISTS source_remote(kb TEXT NOT NULL,source TEXT NOT NULL,data TEXT NOT NULL,PRIMARY KEY(kb,source))`,
	`CREATE TABLE IF NOT EXISTS sync_target(id TEXT PRIMARY KEY NOT NULL,kb TEXT NOT NULL,data TEXT NOT NULL,created INTEGER NOT NULL)`,
	`CREATE TABLE IF NOT EXISTS sync_receipt(target TEXT NOT NULL,item TEXT NOT NULL,revision TEXT NOT NULL,path TEXT NOT NULL,content_hash TEXT NOT NULL,external_version TEXT NOT NULL,created INTEGER NOT NULL,PRIMARY KEY(target,item))`,
	`CREATE TABLE IF NOT EXISTS edition_retirement(edition TEXT PRIMARY KEY NOT NULL,retired_at INTEGER NOT NULL)`,
	`CREATE TRIGGER IF NOT EXISTS edition_retire AFTER UPDATE OF state ON edition WHEN NEW.state IN ('retired','aborted') AND OLD.state<>NEW.state BEGIN INSERT OR IGNORE INTO edition_retirement VALUES(NEW.id,CAST(strftime('%s','now') AS INTEGER)); END`,
	`CREATE TRIGGER IF NOT EXISTS edition_withdraw AFTER UPDATE OF withdrawn ON file WHEN NEW.withdrawn=1 AND OLD.withdrawn=0 BEGIN INSERT OR REPLACE INTO edition_retirement SELECT id,CAST(strftime('%s','now') AS INTEGER) FROM edition WHERE kb=NEW.kb AND path=NEW.path; END`,
	`INSERT OR IGNORE INTO edition_retirement SELECT id,CAST(strftime('%s','now') AS INTEGER) FROM edition WHERE state IN ('retired','aborted')`,
}

type SyncTarget struct {
	Remote    *RemoteSource `json:"remote,omitempty"`
	Push      bool          `json:"push,omitempty"`
	RemoteURL string        `json:"remote_url,omitempty"`
	AutoSync  bool          `json:"auto_sync,omitempty"`
	ID        string        `json:"id"`
	KB        string        `json:"kb"`
	Name      string        `json:"name"`
	Kind      string        `json:"kind"`
	Root      string        `json:"root"`
	Prefix    string        `json:"prefix"`
	Branch    string        `json:"branch,omitempty"`
}
type SyncReceipt struct {
	Target          string `json:"target_id"`
	Item            string `json:"item_id"`
	Revision        string `json:"revision"`
	Path            string `json:"path"`
	ContentHash     string `json:"content_hash"`
	ExternalVersion string `json:"external_version"`
	Created         int64  `json:"created"`
}

func (s *Store) AddSyncTarget(ctx context.Context, t SyncTarget) (SyncTarget, error) {
	if !core.ValidKB(t.ID) || !core.ValidKB(t.KB) {
		return t, core.Err("INVALID_ARGUMENT", "target id and KB must be slugs", 400)
	}
	if t.Kind != "path" && t.Kind != "git" && t.Kind != "s3" {
		return t, core.Err("MODE_UNSUPPORTED", "writeback supports path, git and S3", 422)
	}
	var e error
	if t.Kind == "s3" {
		src := Source{KB: t.KB, ID: t.ID, Kind: "s3", Root: t.Root, Remote: t.Remote}
		if e = validateRemoteSource(&src); e != nil {
			return t, e
		}
		t.Root = src.Root
		t.Remote = src.Remote
		if t.Remote.Anonymous {
			return t, core.Err("INVALID_ARGUMENT", "S3 writeback requires explicit credential references", 400)
		}
		if t.Remote.Prefix != "" {
			return t, core.Err("INVALID_ARGUMENT", "use target.prefix, not remote.prefix for a writeback target", 400)
		}
	} else {
		if t.Root == "" || !filepath.IsAbs(t.Root) {
			return t, core.Err("INVALID_ARGUMENT", "target root must be an absolute Server path", 400)
		}
		root, err := filepath.EvalSymlinks(t.Root)
		if err != nil {
			return t, core.Err("INVALID_ARGUMENT", "target directory must already exist", 400)
		}
		if st, err := os.Stat(root); err != nil || !st.IsDir() {
			return t, core.Err("INVALID_ARGUMENT", "target root must be a directory", 400)
		}
		t.Root = root
	}
	if t.Push {
		if t.Kind != "git" || t.RemoteURL == "" {
			return t, core.Err("INVALID_ARGUMENT", "push requires a git remote_url", 400)
		}
		u, err := url.Parse(t.RemoteURL)
		if !filepath.IsAbs(t.RemoteURL) && (err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "") {
			return t, core.Err("INVALID_ARGUMENT", "push supports explicit HTTPS remote or absolute local bare repository", 400)
		}
	} else if t.RemoteURL != "" {
		return t, core.Err("INVALID_ARGUMENT", "remote_url requires push=true", 400)
	}

	if t.Prefix == "" {
		t.Prefix = "xrag-knowledge"
	}
	t.Prefix, e = core.CleanPath(t.Prefix)
	if e != nil {
		return t, e
	}
	if t.Kind == "git" {
		if t.Branch == "" {
			t.Branch = "xrag/knowledge"
		}
		if !strings.HasPrefix(t.Branch, "xrag/") || strings.Contains(t.Branch, "..") || strings.ContainsAny(t.Branch, " ~^:?*[\\\n\r") || strings.HasSuffix(t.Branch, "/") {
			return t, core.Err("INVALID_ARGUMENT", "writeback branch must be under xrag/", 400)
		}
	} else if t.Branch != "" {
		return t, core.Err("INVALID_ARGUMENT", "branch only applies to git targets", 400)
	}
	if t.Name == "" {
		t.Name = t.ID
	}
	e = s.DB.Tx(ctx, true, func(c *db.Conn) error {
		r, e := c.Query("SELECT id FROM kb WHERE id=?", t.KB)
		if e != nil {
			return e
		}
		if len(r) != 1 {
			return core.Err("NOT_FOUND", "knowledge base not found", 404)
		}
		r, e = c.Query("SELECT id FROM sync_target WHERE id=?", t.ID)
		if e != nil {
			return e
		}
		if len(r) > 0 {
			return core.Err("VERSION_CONFLICT", "target id already exists; create a new binding instead of changing its root", 409)
		}
		return c.Exec("INSERT INTO sync_target(id,kb,data,created) VALUES(?,?,?,?)", t.ID, t.KB, core.JSON(t), time.Now().Unix())
	})
	return t, e
}
func (s *Store) SyncTargets(ctx context.Context, kbs []string) ([]SyncTarget, error) {
	out := []SyncTarget{}
	e := s.DB.Do(ctx, func(c *db.Conn) error {
		rows, e := c.Query("SELECT data FROM sync_target ORDER BY id")
		if e != nil {
			return e
		}
		for _, r := range rows {
			var t SyncTarget
			if e = json.Unmarshal([]byte(db.String(r, "data")), &t); e != nil {
				return e
			}
			if core.Subset([]string{t.KB}, kbs) {
				out = append(out, t)
			}
		}
		return nil
	})
	return out, e
}
func (s *Store) SyncTarget(ctx context.Context, id string) (SyncTarget, error) {
	var t SyncTarget
	e := s.DB.Do(ctx, func(c *db.Conn) error {
		r, e := c.Query("SELECT data FROM sync_target WHERE id=?", id)
		if e != nil {
			return e
		}
		if len(r) != 1 {
			return core.Err("NOT_FOUND", "sync target not found", 404)
		}
		return json.Unmarshal([]byte(db.String(r[0], "data")), &t)
	})
	return t, e
}
func (s *Store) SyncReceipt(ctx context.Context, target, item string) (SyncReceipt, bool, error) {
	var t SyncReceipt
	ok := false
	e := s.DB.Do(ctx, func(c *db.Conn) error {
		r, e := c.Query("SELECT * FROM sync_receipt WHERE target=? AND item=?", target, item)
		if e != nil || len(r) == 0 {
			return e
		}
		ok = true
		t = SyncReceipt{Target: db.String(r[0], "target"), Item: db.String(r[0], "item"), Revision: db.String(r[0], "revision"), Path: db.String(r[0], "path"), ContentHash: db.String(r[0], "content_hash"), ExternalVersion: db.String(r[0], "external_version"), Created: db.Int(r[0], "created")}
		return nil
	})
	return t, ok, e
}
func (s *Store) SaveSyncReceipt(ctx context.Context, j Job, r SyncReceipt) error {
	return s.DB.Tx(ctx, true, func(c *db.Conn) error {
		if e := fence(c, j); e != nil {
			return e
		}
		return c.Exec(`INSERT INTO sync_receipt(target,item,revision,path,content_hash,external_version,created) VALUES(?,?,?,?,?,?,?) ON CONFLICT(target,item) DO UPDATE SET revision=excluded.revision,path=excluded.path,content_hash=excluded.content_hash,external_version=excluded.external_version,created=excluded.created`, r.Target, r.Item, r.Revision, r.Path, r.ContentHash, r.ExternalVersion, time.Now().Unix())
	})
}
func (s *Store) SyncReceipts(ctx context.Context, item string) ([]SyncReceipt, error) {
	out := []SyncReceipt{}
	e := s.DB.Do(ctx, func(c *db.Conn) error {
		rows, e := c.Query("SELECT * FROM sync_receipt WHERE item=? ORDER BY created DESC", item)
		if e != nil {
			return e
		}
		for _, r := range rows {
			out = append(out, SyncReceipt{Target: db.String(r, "target"), Item: item, Revision: db.String(r, "revision"), Path: db.String(r, "path"), ContentHash: db.String(r, "content_hash"), ExternalVersion: db.String(r, "external_version"), Created: db.Int(r, "created")})
		}
		return nil
	})
	return out, e
}
func (s *Store) KnowledgeRevisions(ctx context.Context, id string) ([]Knowledge, error) {
	out := []Knowledge{}
	e := s.DB.Do(ctx, func(c *db.Conn) error {
		rows, e := c.Query("SELECT data FROM knowledge_revision WHERE item_id=? ORDER BY created DESC,id DESC LIMIT 201", id)
		if e != nil {
			return e
		}
		if len(rows) > 200 {
			return core.Err("RESULT_LIMIT", "revision history exceeds 200; use a database export", 422)
		}
		for _, r := range rows {
			var k Knowledge
			if e = json.Unmarshal([]byte(db.String(r, "data")), &k); e != nil {
				return e
			}
			out = append(out, k)
		}
		return nil
	})
	return out, e
}

// Archive removes the current publication, but it is not a withdrawal of history.
func (s *Store) ArchiveKnowledge(ctx context.Context, id, expected string) error {
	return s.DB.Tx(ctx, true, func(c *db.Conn) error {
		r, e := c.Query("SELECT kb,published FROM knowledge WHERE id=?", id)
		if e != nil {
			return e
		}
		if len(r) != 1 {
			return core.Err("NOT_FOUND", "knowledge item not found", 404)
		}
		if expected == "" || db.String(r[0], "published") != expected {
			return core.Err("VERSION_CONFLICT", "published revision changed", 409)
		}
		busy, e := c.Query("SELECT id FROM job WHERE kb=? AND state='running'", db.String(r[0], "kb"))
		if e != nil {
			return e
		}
		if len(busy) > 0 {
			return core.Err("WORKSPACE_BUSY", "a knowledge task is running; archive after it finishes", 409)
		}
		if e = c.Exec("UPDATE edition SET state='retired' WHERE id IN (SELECT active FROM file WHERE kb=? AND path=?)", db.String(r[0], "kb"), "_knowledge/"+id+".md"); e != nil {
			return e
		}
		if e = c.Exec("UPDATE file SET active='' WHERE kb=? AND path=?", db.String(r[0], "kb"), "_knowledge/"+id+".md"); e != nil {
			return e
		}
		return c.Exec("UPDATE knowledge SET state='archived' WHERE id=?", id)
	})
}

// A matching marker alone never causes an import to be skipped: destination identity,
// path and normalized content digest must all match a receipt produced by XRAG.
func (s *Store) IsSyncCopy(ctx context.Context, src Source, rel, body string) (bool, error) {
	targets, e := s.SyncTargets(ctx, []string{src.KB})
	if e != nil {
		return false, e
	}
	root := src.Root
	if src.Kind != "s3" {
		root, e = filepath.EvalSymlinks(src.Root)
		if e != nil {
			return false, nil
		}
	}
	for _, t := range targets {
		if t.Kind == "s3" {
			if src.Kind != "s3" || src.Remote == nil || t.Remote == nil || src.Root != t.Root || src.Remote.Bucket != t.Remote.Bucket {
				continue
			}
			match := false
			err := s.DB.Do(ctx, func(c *db.Conn) error {
				rs, er := c.Query("SELECT path,content_hash FROM sync_receipt WHERE target=?", t.ID)
				if er != nil {
					return er
				}
				for _, r := range rs {
					if path.Join(src.Remote.Prefix, rel) == db.String(r, "path") && core.TextHash(body) == db.String(r, "content_hash") {
						match = true
					}
				}
				return nil
			})
			if err != nil {
				return false, err
			}
			if match {
				return true, nil
			}
			continue
		}
		if t.Kind == "git" {
			if src.Kind != "git" || src.Ref != t.Branch || root != t.Root {
				continue
			}
		} else if src.Kind != "path" && src.Kind != "builtin" {
			continue
		}
		candidate := filepath.Clean(filepath.Join(root, filepath.FromSlash(rel)))
		var matches bool
		e = s.DB.Do(ctx, func(c *db.Conn) error {
			rows, e := c.Query("SELECT path,content_hash FROM sync_receipt WHERE target=?", t.ID)
			if e != nil {
				return e
			}
			for _, r := range rows {
				if candidate == filepath.Join(t.Root, filepath.FromSlash(db.String(r, "path"))) && core.TextHash(body) == db.String(r, "content_hash") {
					matches = true
					break
				}
			}
			return nil
		})
		if e != nil {
			return false, e
		}
		if matches {
			return true, nil
		}
	}
	return false, nil
}
