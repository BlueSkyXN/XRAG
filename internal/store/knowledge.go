package store

import (
	"context"
	"encoding/json"
	"strings"
	"time"
	"github.com/BlueSkyXN/XRAG/core"
	db "github.com/BlueSkyXN/XRAG/internal/sqlite"
)

type Knowledge struct {
	ID           string   `json:"id"`
	KB           string   `json:"kb"`
	Title        string   `json:"title"`
	Kind         string   `json:"kind"`
	Draft        string   `json:"draft_revision"`
	Published    string   `json:"published_revision"`
	State        string   `json:"state"`
	Body         string   `json:"body_md,omitempty"`
	Refs         []string `json:"refs,omitempty"`
	Expected     string   `json:"expected_revision,omitempty"`
	Created      int64    `json:"created"`
	AppliesTo    string   `json:"applies_to,omitempty"`
	NotAppliesTo string   `json:"not_applies_to,omitempty"`
}

func (s *Store) SaveKnowledge(ctx context.Context, k Knowledge) (Knowledge, error) {
	if len(k.Refs) == 0 || strings.TrimSpace(k.Body) == "" || strings.TrimSpace(k.Title) == "" {
		return k, core.Err("INVALID_ARGUMENT", "title, body_md and at least one reference are required", 400)
	}
	if len(k.Body) > 200000 || len(k.Refs) > 50 {
		return k, core.Err("INVALID_ARGUMENT", "knowledge item too large", 400)
	}
	if k.Kind == "" {
		k.Kind = "case"
	}
	if !core.Subset([]string{k.Kind}, []string{"case", "fact", "rule", "procedure", "wiki"}) {
		return k, core.Err("INVALID_ARGUMENT", "invalid knowledge kind", 400)
	}
	paths := []string{}
	archives := []archivedEvidence{}
	for _, raw := range k.Refs {
		ref, err := core.DecodeRef(raw)
		if err != nil {
			return k, err
		}
		if ref.ResultID != "" {
			a, e := s.prepareEvidence(ctx, raw, k.KB)
			if e != nil {
				return k, e
			}
			archives = append(archives, a)
			paths = append(paths, a.Paths...)
			continue
		}
		if ref.KB != k.KB {
			return k, core.Err("PERMISSION_DENIED", "knowledge references must be in this KB", 403)
		}
		if _, _, _, e := s.Read(ctx, ref); e != nil {
			return k, e
		}
		paths = append(paths, ref.Path)
	}
	paths = core.Unique(paths)
	rev := core.ID("kr_")
	newItem := k.ID == ""
	if newItem {
		k.ID = core.ID("ki_")
	}
	k.Draft = rev
	k.State = "draft"
	k.Created = time.Now().Unix()
	e := s.DB.Tx(ctx, true, func(c *db.Conn) error {
		for _, a := range archives {
			ep, err := epoch(c)
			if err != nil {
				return err
			}
			if ep != a.Epoch {
				return core.Err("VIEW_INVALIDATED", "evidence changed before archive commit", 409)
			}
			if err = c.Exec("INSERT OR IGNORE INTO evidence_archive(ref,kb,owner,epoch,data,paths,created) VALUES(?,?,?,?,?,?,?)", a.Ref, k.KB, a.Owner, a.Epoch, core.JSON(a.Data), core.JSON(a.Paths), time.Now().Unix()); err != nil {
				return err
			}
			if err = c.Exec("INSERT INTO evidence_owner(ref,item,revision) VALUES(?,?,?)", a.Ref, k.ID, rev); err != nil {
				return err
			}
		}
		for _, path := range paths {
			rows, err := c.Query("SELECT withdrawn FROM file WHERE kb=? AND path=?", k.KB, path)
			if err != nil {
				return err
			}
			if len(rows) != 1 || db.Int(rows[0], "withdrawn") != 0 {
				return core.Err("REF_WITHDRAWN", "knowledge evidence was withdrawn before save", 410)
			}
			if path == "_knowledge/"+k.ID+".md" {
				return core.Err("INVALID_ARGUMENT", "knowledge cannot cite itself", 400)
			}
		}
		if !newItem {
			rows, e := c.Query("SELECT * FROM knowledge WHERE id=?", k.ID)
			if e != nil {
				return e
			}
			if len(rows) != 1 {
				return core.Err("NOT_FOUND", "knowledge item not found", 404)
			}
			if db.String(rows[0], "kb") != k.KB {
				return core.Err("PERMISSION_DENIED", "cannot move knowledge across KBs", 403)
			}
			if k.Expected == "" || k.Expected != db.String(rows[0], "draft") {
				return core.Err("VERSION_CONFLICT", "draft revision changed", 409)
			}
			k.Published = db.String(rows[0], "published")
			if k.Published != "" {
				k.State = db.String(rows[0], "state")
			}
			if e = c.Exec("UPDATE knowledge SET title=?,draft=? WHERE id=?", k.Title, rev, k.ID); e != nil {
				return e
			}
		} else {
			if e := c.Exec("INSERT INTO knowledge(id,kb,title,kind,draft,state,created) VALUES(?,?,?,?,?,?,?)", k.ID, k.KB, k.Title, k.Kind, rev, "draft", k.Created); e != nil {
				return e
			}
		}
		if e := c.Exec("INSERT INTO knowledge_revision(id,item_id,data,created) VALUES(?,?,?,?)", rev, k.ID, core.JSON(k), k.Created); e != nil {
			return e
		}
		for _, p := range paths {
			if e := c.Exec("INSERT OR IGNORE INTO dependency(item_id,kb,path) VALUES(?,?,?)", k.ID, k.KB, p); e != nil {
				return e
			}
		}
		return nil
	})
	return k, e
}
func (s *Store) Knowledge(ctx context.Context, id, revision string) (Knowledge, error) {
	k := Knowledge{}
	e := s.DB.Do(ctx, func(c *db.Conn) error {
		r, e := c.Query("SELECT * FROM knowledge WHERE id=?", id)
		if e != nil {
			return e
		}
		if len(r) != 1 {
			return core.Err("NOT_FOUND", "knowledge item not found", 404)
		}
		if revision == "" {
			revision = db.String(r[0], "draft")
		}
		rr, e := c.Query("SELECT data FROM knowledge_revision WHERE id=? AND item_id=?", revision, id)
		if e != nil {
			return e
		}
		if len(rr) != 1 {
			return core.Err("NOT_FOUND", "revision not found", 404)
		}
		if e = json.Unmarshal([]byte(db.String(rr[0], "data")), &k); e != nil {
			return e
		}
		k.Published = db.String(r[0], "published")
		k.State = db.String(r[0], "state")
		return nil
	})
	return k, e
}
func (s *Store) KnowledgeList(ctx context.Context, kbs []string) ([]Knowledge, error) {
	out := []Knowledge{}
	e := s.DB.Do(ctx, func(c *db.Conn) error {
		rows, e := c.Query("SELECT * FROM knowledge ORDER BY created DESC,id")
		for _, r := range rows {
			kb := db.String(r, "kb")
			if core.Subset([]string{kb}, kbs) {
				out = append(out, Knowledge{ID: db.String(r, "id"), KB: kb, Title: db.String(r, "title"), Kind: db.String(r, "kind"), Draft: db.String(r, "draft"), Published: db.String(r, "published"), State: db.String(r, "state"), Created: db.Int(r, "created")})
			}
		}
		return e
	})
	return out, e
}
func (s *Store) Feedback(ctx context.Context, kb, ref, label, note, owner string) error {
	if len(note) > 8000 {
		return core.Err("INVALID_ARGUMENT", "feedback note exceeds 8000 bytes", 400)
	}
	r, e := core.DecodeRef(ref)
	if e != nil {
		return e
	}
	if r.ResultID != "" {
		ev, e := s.prepareEvidence(WithActor(ctx, owner), ref, kb)
		if e != nil {
			return e
		}
		if e = s.DB.Tx(ctx, true, func(c *db.Conn) error {
			ep, err := epoch(c)
			if err != nil {
				return err
			}
			if ep != ev.Epoch {
				return core.Err("VIEW_INVALIDATED", "source changed before feedback", 409)
			}
			return c.Exec("INSERT OR IGNORE INTO evidence_archive(ref,kb,owner,epoch,data,paths,created) VALUES(?,?,?,?,?,?,?)", ref, kb, owner, ev.Epoch, core.JSON(ev.Data), core.JSON(ev.Paths), time.Now().Unix())
		}); e != nil {
			return e
		}
	} else {
		if r.KB != kb {
			return core.Err("PERMISSION_DENIED", "feedback reference not in KB", 403)
		}
		if _, _, _, e = s.Read(ctx, r); e != nil {
			return e
		}
	}
	if !core.Subset([]string{label}, []string{"useful", "irrelevant", "wrong", "outdated"}) {
		return core.Err("INVALID_ARGUMENT", "invalid feedback label", 400)
	}
	return s.DB.Do(ctx, func(c *db.Conn) error {
		return c.Exec("INSERT INTO feedback(id,kb,ref,label,note,owner,created) VALUES(?,?,?,?,?,?,?)", core.ID("fb_"), kb, ref, label, note, owner, time.Now().Unix())
	})
}
