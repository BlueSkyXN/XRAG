package store

// Product operations build on the existing data model. They do not add a new
// retrieval runtime or start workers implicitly.
import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BlueSkyXN/XRAG/core"
	db "github.com/BlueSkyXN/XRAG/internal/sqlite"
)

var productSchema = []string{
	`CREATE TABLE IF NOT EXISTS key_options(key_id TEXT PRIMARY KEY NOT NULL,expires_at INTEGER NOT NULL DEFAULT 0,FOREIGN KEY(key_id) REFERENCES api_key(id))`,
	`CREATE TABLE IF NOT EXISTS managed_setting(name TEXT PRIMARY KEY NOT NULL,revision INTEGER NOT NULL,data TEXT NOT NULL,updated INTEGER NOT NULL)`,
	`CREATE TABLE IF NOT EXISTS feedback_state(feedback_id TEXT PRIMARY KEY NOT NULL,status TEXT NOT NULL,note TEXT NOT NULL,actor TEXT NOT NULL,revision INTEGER NOT NULL,updated INTEGER NOT NULL,FOREIGN KEY(feedback_id) REFERENCES feedback(id))`,
	`CREATE TABLE IF NOT EXISTS backup_record(id TEXT PRIMARY KEY NOT NULL,filename TEXT NOT NULL,size INTEGER NOT NULL,sha256 TEXT NOT NULL,created INTEGER NOT NULL)`,
}

type KeyInfo struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Role      string   `json:"role"`
	KBs       []string `json:"kbs"`
	Created   int64    `json:"created"`
	ExpiresAt int64    `json:"expires_at"`
	Disabled  bool     `json:"disabled"`
	State     string   `json:"state"`
}

func keyInfo(r db.Row) KeyInfo {
	k := KeyInfo{ID: db.String(r, "id"), Name: db.String(r, "name"), Role: db.String(r, "role"), Created: db.Int(r, "created"), ExpiresAt: db.Int(r, "expires_at"), Disabled: db.Int(r, "disabled") != 0, KBs: []string{}, State: "active"}
	_ = json.Unmarshal([]byte(db.String(r, "kbs")), &k.KBs)
	if k.Disabled {
		k.State = "disabled"
	} else if k.ExpiresAt > 0 && k.ExpiresAt <= time.Now().Unix() {
		k.State = "expired"
	}
	return k
}

const keyColumns = `SELECT k.*,COALESCE(o.expires_at,0) expires_at FROM api_key k LEFT JOIN key_options o ON k.id=o.key_id`

func (s *Store) Keys(ctx context.Context) ([]KeyInfo, error) {
	out := []KeyInfo{}
	e := s.DB.Do(ctx, func(c *db.Conn) error {
		rows, e := c.Query(keyColumns + ` ORDER BY k.created DESC,k.id`)
		for _, r := range rows {
			out = append(out, keyInfo(r))
		}
		return e
	})
	return out, e
}
func (s *Store) CreateManagedKey(ctx context.Context, name, role string, kbs []string, expires int64) (KeyInfo, string, error) {
	k := KeyInfo{ID: core.ID("key_"), Name: strings.TrimSpace(name), Role: role, KBs: core.Unique(kbs), Created: time.Now().Unix(), ExpiresAt: expires, State: "active"}
	if k.KBs == nil {
		k.KBs = []string{}
	}
	if k.Name == "" || len(k.Name) > 120 || roles[role] == 0 || (role != "admin" && len(k.KBs) == 0) || (expires != 0 && expires <= k.Created) {
		return k, "", core.Err("INVALID_ARGUMENT", "name, valid role, KB scope and a future expiry (or 0) are required", 400)
	}
	if role == "admin" {
		k.KBs = []string{}
	} // Existing admin means workspace administrator.
	token := core.ID("xrag_")
	e := s.DB.Tx(ctx, true, func(c *db.Conn) error {
		for _, kb := range k.KBs {
			rs, e := c.Query("SELECT 1 FROM kb WHERE id=?", kb)
			if e != nil {
				return e
			}
			if len(rs) != 1 {
				return core.Err("INVALID_ARGUMENT", "unknown knowledge base: "+kb, 400)
			}
		}
		if e := c.Exec("INSERT INTO api_key(id,name,hash,role,kbs,created) VALUES(?,?,?,?,?,?)", k.ID, k.Name, digest(token), role, core.JSON(k.KBs), k.Created); e != nil {
			return e
		}
		return c.Exec("INSERT INTO key_options(key_id,expires_at) VALUES(?,?)", k.ID, expires)
	})
	if e != nil {
		return k, "", e
	}
	return k, token, nil
}
func (s *Store) DisableKey(ctx context.Context, id string) error {
	return s.DB.Tx(ctx, true, func(c *db.Conn) error {
		rows, e := c.Query(keyColumns+" WHERE k.id=?", id)
		if e != nil {
			return e
		}
		if len(rows) == 0 {
			return core.Err("NOT_FOUND", "key not found", 404)
		}
		k := keyInfo(rows[0])
		if k.Disabled {
			return nil
		}
		if k.Role == "admin" && k.ExpiresAt == 0 {
			rs, e := c.Query(`SELECT COUNT(*) n FROM api_key k LEFT JOIN key_options o ON k.id=o.key_id WHERE k.role='admin' AND k.disabled=0 AND COALESCE(o.expires_at,0)=0 AND k.id<>?`, id)
			if e != nil {
				return e
			}
			if db.Int(rs[0], "n") == 0 {
				return core.Err("LAST_ADMIN", "create another permanent admin key before disabling this one", 409)
			}
		}
		return c.Exec("UPDATE api_key SET disabled=1 WHERE id=?", id)
	})
}
func (s *Store) RenameKey(ctx context.Context, id, name string) error {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 120 {
		return core.Err("INVALID_ARGUMENT", "name must contain 1..120 bytes", 400)
	}
	return s.DB.Do(ctx, func(c *db.Conn) error {
		rs, e := c.Query("SELECT 1 FROM api_key WHERE id=?", id)
		if e != nil {
			return e
		}
		if len(rs) == 0 {
			return core.Err("NOT_FOUND", "key not found", 404)
		}
		return c.Exec("UPDATE api_key SET name=? WHERE id=?", name, id)
	})
}

type Setting struct {
	Revision int64           `json:"revision"`
	Updated  int64           `json:"updated"`
	Data     json.RawMessage `json:"config"`
}

func (s *Store) ProviderSetting(ctx context.Context) (Setting, error) {
	v := Setting{Data: json.RawMessage(`{}`)}
	e := s.DB.Do(ctx, func(c *db.Conn) error {
		rs, e := c.Query("SELECT revision,data,updated FROM managed_setting WHERE name='providers'")
		if e != nil {
			return e
		}
		if len(rs) > 0 {
			v = Setting{db.Int(rs[0], "revision"), db.Int(rs[0], "updated"), json.RawMessage(db.String(rs[0], "data"))}
		}
		return nil
	})
	return v, e
}
func (s *Store) SaveProviderSetting(ctx context.Context, expected int64, data json.RawMessage) (Setting, error) {
	v := Setting{expected + 1, time.Now().Unix(), data}
	e := s.DB.Tx(ctx, true, func(c *db.Conn) error {
		rs, e := c.Query("SELECT revision FROM managed_setting WHERE name='providers'")
		if e != nil {
			return e
		}
		current := int64(0)
		if len(rs) > 0 {
			current = db.Int(rs[0], "revision")
		}
		if expected != current {
			return core.Err("VERSION_CONFLICT", "configuration changed; reload before saving", 409)
		}
		return c.Exec(`INSERT INTO managed_setting(name,revision,data,updated) VALUES('providers',?,?,?) ON CONFLICT(name) DO UPDATE SET revision=excluded.revision,data=excluded.data,updated=excluded.updated`, v.Revision, string(data), v.Updated)
	})
	return v, e
}

type FeedbackInfo struct {
	ID           string `json:"id"`
	KB           string `json:"kb"`
	Ref          string `json:"ref,omitempty"`
	Path         string `json:"path,omitempty"`
	Label        string `json:"label"`
	Note         string `json:"note,omitempty"`
	Owner        string `json:"owner"`
	Created      int64  `json:"created"`
	Status       string `json:"status"`
	Resolution   string `json:"resolution,omitempty"`
	Revision     int64  `json:"revision"`
	Updated      int64  `json:"updated"`
	Availability string `json:"availability"`
}
type FeedbackPage struct {
	Items      []FeedbackInfo `json:"items"`
	Total      int64          `json:"total"`
	NextOffset *int           `json:"next_offset"`
}

func (s *Store) Feedbacks(ctx context.Context, p Principal, kbs []string, status string, limit, offset int) (FeedbackPage, error) {
	out := FeedbackPage{Items: []FeedbackInfo{}}
	if limit < 1 || limit > 100 || offset < 0 || offset > 100000 {
		return out, core.Err("INVALID_ARGUMENT", "invalid feedback page", 400)
	}
	if status != "" && !core.Subset([]string{status}, []string{"open", "resolved", "dismissed"}) {
		return out, core.Err("INVALID_ARGUMENT", "unknown feedback status", 400)
	}
	allowed, e := s.Scope(ctx, p, kbs)
	if e != nil {
		return out, e
	}
	where := ` WHERE f.kb IN (` + marks(len(allowed)) + `)`
	args := []any{}
	for _, k := range allowed {
		args = append(args, k)
	}
	if !RoleAtLeast(p, "maintainer") {
		where += " AND f.owner=?"
		args = append(args, p.ID)
	}
	if status != "" {
		where += " AND COALESCE(st.status,'open')=?"
		args = append(args, status)
	}
	e = s.DB.Tx(ctx, false, func(c *db.Conn) error {
		join := ` FROM feedback f LEFT JOIN feedback_state st ON st.feedback_id=f.id`
		rs, e := c.Query(`SELECT COUNT(*) n`+join+where, args...)
		if e != nil {
			return e
		}
		out.Total = db.Int(rs[0], "n")
		qargs := append(append([]any{}, args...), limit, offset)
		rs, e = c.Query(`SELECT f.*,COALESCE(st.status,'open') resolution_status,COALESCE(st.note,'') resolution,COALESCE(st.revision,0) revision,COALESCE(st.updated,0) updated`+join+where+` ORDER BY f.created DESC,f.id DESC LIMIT ? OFFSET ?`, qargs...)
		if e != nil {
			return e
		}
		for _, r := range rs {
			f := FeedbackInfo{ID: db.String(r, "id"), KB: db.String(r, "kb"), Ref: db.String(r, "ref"), Label: db.String(r, "label"), Note: db.String(r, "note"), Owner: db.String(r, "owner"), Created: db.Int(r, "created"), Status: db.String(r, "resolution_status"), Resolution: db.String(r, "resolution"), Revision: db.Int(r, "revision"), Updated: db.Int(r, "updated"), Availability: "available"}
			ref, er := core.DecodeRef(f.Ref)
			if er == nil {
				row, err := c.Query(`SELECT withdrawn,active FROM file WHERE kb=? AND path=?`, ref.KB, ref.Path)
				if err != nil {
					return err
				}
				f.Path = ref.Path
				if ref.Origin != s.Origin || len(row) == 0 || db.Int(row[0], "withdrawn") != 0 {
					f.Availability = "withdrawn"
				}
			} else {
				f.Availability = "unavailable"
			}
			if f.Availability != "available" {
				f.Ref = ""
				f.Path = ""
				f.Note = ""
				f.Resolution = ""
			}
			out.Items = append(out.Items, f)
		}
		if offset+len(out.Items) < int(out.Total) {
			n := offset + len(out.Items)
			out.NextOffset = &n
		}
		return nil
	})
	return out, e
}
func marks(n int) string { return strings.TrimRight(strings.Repeat("?,", n), ",") }
func (s *Store) ResolveFeedback(ctx context.Context, p Principal, id, status, note string, expected int64) error {
	if !RoleAtLeast(p, "maintainer") {
		return core.Err("PERMISSION_DENIED", "maintainer required", 403)
	}
	if !core.Subset([]string{status}, []string{"open", "resolved", "dismissed"}) || len(note) > 8000 {
		return core.Err("INVALID_ARGUMENT", "invalid resolution", 400)
	}
	return s.DB.Tx(ctx, true, func(c *db.Conn) error {
		rs, e := c.Query(`SELECT f.kb,COALESCE(st.revision,0) revision FROM feedback f LEFT JOIN feedback_state st ON st.feedback_id=f.id WHERE f.id=?`, id)
		if e != nil {
			return e
		}
		if len(rs) == 0 {
			return core.Err("NOT_FOUND", "feedback not found", 404)
		}
		if p.Role != "admin" && !core.Subset([]string{db.String(rs[0], "kb")}, p.KBs) {
			return core.Err("PERMISSION_DENIED", "feedback outside current scope", 403)
		}
		if db.Int(rs[0], "revision") != expected {
			return core.Err("VERSION_CONFLICT", "feedback was updated; reload it", 409)
		}
		return c.Exec(`INSERT INTO feedback_state(feedback_id,status,note,actor,revision,updated) VALUES(?,?,?,?,?,?) ON CONFLICT(feedback_id) DO UPDATE SET status=excluded.status,note=excluded.note,actor=excluded.actor,revision=excluded.revision,updated=excluded.updated`, id, status, note, p.ID, expected+1, time.Now().Unix())
	})
}

func (s *Store) Overview(ctx context.Context, p Principal) (map[string]any, error) {
	kbs, e := s.VisibleKBs(ctx, p)
	if e != nil {
		return nil, e
	}
	out := map[string]any{"version": core.Version, "origin_id": s.Origin, "kbs": kbs, "identity": p, "counts": map[string]any{}, "jobs": map[string]int64{}}
	e = s.DB.Tx(ctx, false, func(c *db.Conn) error {
		args := []any{}
		for _, k := range kbs {
			args = append(args, k)
		}
		in := marks(len(kbs))
		counts := out["counts"].(map[string]any)
		for name, q := range map[string]string{
			"files":         `SELECT COUNT(*) n FROM file WHERE active<>'' AND withdrawn=0 AND kb IN (` + in + `)`,
			"sources":       `SELECT COUNT(*) n FROM source WHERE kb IN (` + in + `)`,
			"knowledge":     `SELECT COUNT(*) n FROM knowledge WHERE kb IN (` + in + `)`,
			"source_errors": `SELECT COUNT(*) n FROM source_state WHERE error<>'' AND kb IN (` + in + `)`,
		} {
			rs, e := c.Query(q, args...)
			if e != nil {
				return e
			}
			counts[name] = db.Int(rs[0], "n")
		}
		// Queue statistics honor the same job visibility as the jobs page.
		q := `SELECT state,COUNT(*) n FROM job WHERE kb IN (` + in + `)`
		jobargs := append([]any{}, args...)
		if !RoleAtLeast(p, "maintainer") {
			q += " AND owner=?"
			jobargs = append(jobargs, p.ID)
		}
		rs, e := c.Query(q+" GROUP BY state", jobargs...)
		if e != nil {
			return e
		}
		for _, r := range rs {
			out["jobs"].(map[string]int64)[db.String(r, "state")] = db.Int(r, "n")
		}
		if p.Role == "admin" {
			sizes := map[string]int64{}
			for _, file := range []string{"xrag.sqlite", "xrag.sqlite-wal", "xrag.sqlite-shm"} {
				if st, e := os.Stat(filepath.Join(s.Dir, file)); e == nil {
					sizes[file] = st.Size()
				}
			}
			out["database_bytes"] = sizes
			rs, e = c.Query("SELECT sqlite_version() version")
			if e != nil {
				return e
			}
			out["sqlite_version"] = db.String(rs[0], "version")
		}
		return nil
	})
	return out, e
}

// CleanupTransient removes only expired, non-authoritative response caches.
// It deliberately never deletes editions, model caches, knowledge or task history.
func (s *Store) CleanupTransient(ctx context.Context, apply bool) (map[string]int64, error) {
	counts := map[string]int64{}
	e := s.DB.Tx(ctx, apply, func(c *db.Conn) error {
		now := time.Now().Unix()
		for _, table := range []string{"cache", "query_record"} {
			r, e := c.Query("SELECT COUNT(*) n FROM "+table+" WHERE expires<=?", now)
			if e != nil {
				return e
			}
			counts[table] = db.Int(r[0], "n")
			if apply {
				if e = c.Exec("DELETE FROM "+table+" WHERE expires<=?", now); e != nil {
					return e
				}
			}
		}
		return nil
	})
	return counts, e
}

// PrepareRestoredCopy is only used by the offline restore command in a NEW
// directory. It changes operational identities, never document identities.
func (s *Store) PrepareRestoredCopy(ctx context.Context) (string, error) {
	e := s.DB.Tx(ctx, true, func(c *db.Conn) error {
		for _, q := range []string{
			"UPDATE maintenance_policy SET data=json_set(data,'$.enabled',json('false')),next_backup=0,next_records=0,next_content=0",
			"UPDATE maintenance_run SET state='cancelled',finished=CAST(strftime('%s','now') AS INTEGER),error='restored copy; original schedule stopped' WHERE state IN('queued','running')",
			"DELETE FROM automatic_backup",
			"DELETE FROM meta WHERE k LIKE 'projection_recovery:%'",
			`UPDATE api_key SET disabled=1`,
			`UPDATE job SET state='failed',error='RESTORED_COPY: explicitly resubmit after checking sources',lease=0,token='',cancel=1 WHERE state IN ('queued','running')`,
			`UPDATE call_usage SET state='outcome_unknown' WHERE state='sent'`,
			`DELETE FROM source_policy`,
			`DELETE FROM managed_setting WHERE name='providers'`,
			`DELETE FROM cache`,
			`DELETE FROM auto_sync_outbox`,
			`UPDATE budget_call SET state='outcome_unknown' WHERE state='sent'`,
			`DELETE FROM backup_record`,
		} {
			if e := c.Exec(q); e != nil {
				return e
			}
		}
		// Restoration retains knowledge but does not resume delivery to old external targets.
		rows, err := c.Query("SELECT id,data FROM sync_target")
		if err != nil {
			return err
		}
		for _, r := range rows {
			var t SyncTarget
			if err = json.Unmarshal([]byte(db.String(r, "data")), &t); err != nil {
				return err
			}
			t.AutoSync = false
			t.Push = false
			t.RemoteURL = ""
			if err = c.Exec("UPDATE sync_target SET data=? WHERE id=?", core.JSON(t), t.ID); err != nil {
				return err
			}
		}
		rows, err = c.Query("SELECT kb,data FROM kb_options")
		if err != nil {
			return err
		}
		for _, r := range rows {
			var k KBSettings
			if err = json.Unmarshal([]byte(db.String(r, "data")), &k); err != nil {
				return err
			}
			k.Providers = nil
			k.Graph.Model = nil
			k.Graph.Mode = "explicit"
			k.Graph.Plugin = ""
			k.Parser = "builtin"
			k.ParserPlugin = ""
			if err = c.Exec("UPDATE kb_options SET data=?,revision=revision+1 WHERE kb=?", core.JSON(k), db.String(r, "kb")); err != nil {
				return err
			}
		}

		return nil
	})
	if e != nil {
		return "", e
	}
	return s.CreateKey(ctx, "recovery-admin", "admin", nil)
}
