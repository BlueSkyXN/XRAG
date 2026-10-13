package store

import (
	"context"
	"encoding/json"
	"time"
	"github.com/BlueSkyXN/XRAG/core"
	"github.com/BlueSkyXN/XRAG/evaluation"
	db "github.com/BlueSkyXN/XRAG/internal/sqlite"
)

func (s *Store) SaveEvalSet(ctx context.Context, set evaluation.Set) (evaluation.Set, error) {
	if e := evaluation.ValidateSet(set); e != nil {
		return set, e
	}
	// Each import is an immutable dataset version; changes create a new dataset id.
	set.ID = core.ID("evs_")
	set.Revision = core.Hash([]any{set.KB, set.Name, set.Family, set.Purpose, set.Cases})
	e := s.DB.Tx(ctx, true, func(c *db.Conn) error {
		if e := registerSplits(c, set); e != nil {
			return e
		}
		return c.Exec("INSERT INTO eval_set(id,kb,name,revision,data,created) VALUES(?,?,?,?,?,?)", set.ID, set.KB, set.Name, set.Revision, core.JSON(set), time.Now().Unix())
	})
	return set, e
}
func (s *Store) EvalSets(ctx context.Context, kbs []string) ([]evaluation.Set, error) {
	out := []evaluation.Set{}
	e := s.DB.Do(ctx, func(c *db.Conn) error {
		r, e := c.Query("SELECT data FROM eval_set ORDER BY created DESC,id DESC LIMIT 200")
		if e != nil {
			return e
		}
		for _, row := range r {
			var v evaluation.Set
			if e = json.Unmarshal([]byte(db.String(row, "data")), &v); e != nil {
				return e
			}
			if core.Subset([]string{v.KB}, kbs) && readableEvalLineage(c, s.Origin, v.ID) == nil {
				out = append(out, v)
			}
		}
		return nil
	})
	return out, e
}
func (s *Store) EvalSet(ctx context.Context, id string) (evaluation.Set, error) {
	var v evaluation.Set
	e := s.DB.Do(ctx, func(c *db.Conn) error {
		r, e := c.Query("SELECT data FROM eval_set WHERE id=?", id)
		if e != nil {
			return e
		}
		if len(r) == 0 {
			return core.Err("NOT_FOUND", "evaluation set not found", 404)
		}
		if e := readableEvalLineage(c, s.Origin, id); e != nil {
			return e
		}
		return json.Unmarshal([]byte(db.String(r[0], "data")), &v)
	})
	return v, e
}
