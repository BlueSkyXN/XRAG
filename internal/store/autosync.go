package store

import (
	"context"
	"encoding/json"
	"time"
	"github.com/BlueSkyXN/XRAG/core"
	db "github.com/BlueSkyXN/XRAG/internal/sqlite"
)

// Publication creates intents transactionally. Dispatch is retryable; failed sync jobs require explicit retry.
func recordAutoSync(c *db.Conn, kb, item, revision, owner string) error {
	rs, e := c.Query("SELECT data FROM sync_target WHERE kb=?", kb)
	if e != nil {
		return e
	}
	for _, r := range rs {
		var t SyncTarget
		if e = json.Unmarshal([]byte(db.String(r, "data")), &t); e != nil {
			return e
		}
		if !t.AutoSync {
			continue
		}
		id := core.Hash([]string{t.ID, item, revision})
		if e = c.Exec("INSERT OR IGNORE INTO auto_sync_outbox(id,kb,target,item,revision,owner,created) VALUES(?,?,?,?,?,?,?)", id, kb, t.ID, item, revision, owner, time.Now().Unix()); e != nil {
			return e
		}
	}
	return nil
}
func (s *Store) DispatchAutoSync(ctx context.Context) error {
	var rows []db.Row
	e := s.DB.Do(ctx, func(c *db.Conn) error {
		var err error
		rows, err = c.Query("SELECT * FROM auto_sync_outbox ORDER BY created,id LIMIT 100")
		return err
	})
	if e != nil {
		return e
	}
	for _, r := range rows {
		target, item, revision := db.String(r, "target"), db.String(r, "item"), db.String(r, "revision")
		t, err := s.SyncTarget(ctx, target)
		if err != nil {
			return err
		}
		k, err := s.Knowledge(ctx, item, "")
		if err != nil {
			return err
		}
		if t.AutoSync && k.Published == revision && k.State != "blocked" && k.State != "archived" {
			_, err = s.Enqueue(ctx, db.String(r, "kb"), "sync", db.String(r, "owner"), "auto-sync:"+db.String(r, "id"), map[string]any{"item_id": item, "revision": revision, "target_id": target})
			if err != nil {
				return err
			}
		}
		if err = s.DB.Do(ctx, func(c *db.Conn) error { return c.Exec("DELETE FROM auto_sync_outbox WHERE id=?", db.String(r, "id")) }); err != nil {
			return err
		}
	}
	return nil
}
