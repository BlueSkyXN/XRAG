package store

import (
	"context"
	"encoding/json"
	"time"
	"github.com/BlueSkyXN/XRAG/core"
	db "github.com/BlueSkyXN/XRAG/internal/sqlite"
	"github.com/BlueSkyXN/XRAG/research"
)

func (s *Store) TaskProgress(ctx context.Context, j Job, epoch int64, ob research.Observation) error {
	return s.DB.Tx(ctx, true, func(c *db.Conn) error {
		if err := fence(c, j); err != nil {
			return err
		}
		rows, err := c.Query("SELECT result FROM job WHERE id=?", j.ID)
		if err != nil {
			return err
		}
		var old struct {
			Observations []research.Observation `json:"observations"`
		}
		if len(rows) > 0 {
			if err = json.Unmarshal([]byte(db.String(rows[0], "result")), &old); err != nil {
				return err
			}
		}
		if len(old.Observations) >= 16 {
			return core.Err("RESULT_LIMIT", "too many task observations", 422)
		}
		old.Observations = append(old.Observations, ob)
		return c.Exec("UPDATE job SET result=? WHERE id=? AND token=?", core.JSON(map[string]any{"kind": j.Kind, "withdrawal_epoch": epoch, "observations": old.Observations, "state": "running"}), j.ID, j.Token)
	})
}

// Keep the actual immutable result windows, not the whole search cache. The
// declared seven-day lifetime remains subordinate to source withdrawal.
func (s *Store) RetainTaskResults(ctx context.Context, j Job, result research.Result) (int64, error) {
	until := time.Now().Add(7 * 24 * time.Hour).Unix()
	ids := map[string]bool{}
	for _, e := range result.Evidence {
		r, err := core.DecodeRef(e.Ref)
		if err != nil {
			return 0, err
		}
		if r.ResultID != "" {
			ids[r.ResultID] = true
		}
	}
	for _, o := range result.Observations {
		if o.Result.ResultID != "" {
			ids[o.Result.ResultID] = true
		}
	}
	err := s.DB.Tx(ctx, true, func(c *db.Conn) error {
		if err := fence(c, j); err != nil {
			return err
		}
		es, err := c.Query("SELECT CAST(v AS INTEGER) epoch FROM meta WHERE k='epoch'")
		if err != nil {
			return err
		}
		if len(es) != 1 || db.Int(es[0], "epoch") != result.Epoch {
			return core.Err("VIEW_INVALIDATED", "source withdrawal invalidates task evidence", 409)
		}
		for id := range ids {
			rows, err := c.Query("SELECT owner,expires FROM cache WHERE id=?", id)
			if err != nil {
				return err
			}
			if len(rows) != 1 || db.String(rows[0], "owner") != j.Owner || db.Int(rows[0], "expires") < time.Now().Unix() {
				return core.Err("RESULT_EXPIRED", "cannot promise expired task evidence", 410)
			}
			if err = c.Exec("UPDATE cache SET expires=MAX(expires,?) WHERE id=?", until, id); err != nil {
				return err
			}
		}
		return nil
	})
	return until, err
}
