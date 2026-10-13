package store

import (
	"context"
	"encoding/json"
	"time"
	"github.com/BlueSkyXN/XRAG/core"
	db "github.com/BlueSkyXN/XRAG/internal/sqlite"
	"github.com/BlueSkyXN/XRAG/research"
)

var checkpointSchema = []string{
	`CREATE TABLE IF NOT EXISTS research_checkpoint(job_id TEXT PRIMARY KEY NOT NULL,revision INTEGER NOT NULL,fingerprint TEXT NOT NULL,data TEXT NOT NULL,updated INTEGER NOT NULL,FOREIGN KEY(job_id) REFERENCES job(id) ON DELETE CASCADE)`,
}

func (s *Store) TaskCheckpoint(ctx context.Context, id string) (*research.Checkpoint, string, error) {
	var cp *research.Checkpoint
	var fingerprint string
	e := s.DB.Do(ctx, func(c *db.Conn) error {
		rows, e := c.Query("SELECT data,fingerprint FROM research_checkpoint WHERE job_id=?", id)
		if e != nil || len(rows) == 0 {
			return e
		}
		cp = &research.Checkpoint{}
		fingerprint = db.String(rows[0], "fingerprint")
		return json.Unmarshal([]byte(db.String(rows[0], "data")), cp)
	})
	return cp, fingerprint, e
}
func checkpointCacheIDs(cp *research.Checkpoint) map[string]bool {
	ids := map[string]bool{cp.Result.Scope: true}
	for _, o := range cp.Result.Observations {
		if o.Result.ResultID != "" {
			ids[o.Result.ResultID] = true
		}
	}
	for _, ev := range cp.Result.Evidence {
		if r, e := core.DecodeRef(ev.Ref); e == nil && r.ResultID != "" {
			ids[r.ResultID] = true
		}
	}
	return ids
}
func (s *Store) SaveTaskCheckpoint(ctx context.Context, j Job, fp string, cp *research.Checkpoint) error {
	if cp == nil || cp.Version != "xrag.checkpoint/v1" || cp.Task.KB != j.KB || len(core.JSON(cp)) > 600000 {
		return core.Err("CHECKPOINT_INVALID", "bounded same-KB checkpoint required", 409)
	}
	next := *cp
	next.Revision++
	e := s.DB.Tx(ctx, true, func(c *db.Conn) error {
		if e := fence(c, j); e != nil {
			return e
		}
		ep, e := epoch(c)
		if e != nil {
			return e
		}
		if ep != cp.Result.Epoch {
			return core.Err("VIEW_INVALIDATED", "withdrawal invalidates checkpoint", 409)
		}
		rs, e := c.Query("SELECT revision,fingerprint FROM research_checkpoint WHERE job_id=?", j.ID)
		if e != nil {
			return e
		}
		if len(rs) == 0 && cp.Revision != 0 || len(rs) > 0 && (db.Int(rs[0], "revision") != cp.Revision || db.String(rs[0], "fingerprint") != fp) {
			return core.Err("CHECKPOINT_CONFLICT", "checkpoint or task configuration changed", 409)
		}
		for id := range checkpointCacheIDs(cp) {
			rows, e := c.Query("SELECT owner,expires FROM cache WHERE id=?", id)
			if e != nil {
				return e
			}
			if len(rows) != 1 || db.String(rows[0], "owner") != j.Owner || db.Int(rows[0], "expires") <= time.Now().Unix() {
				return core.Err("RESULT_EXPIRED", "saved scope or result is no longer available", 410)
			}
			if e = c.Exec("UPDATE cache SET expires=MAX(expires,?) WHERE id=?", j.Deadline+300, id); e != nil {
				return e
			}
		}
		if e = c.Exec("INSERT INTO research_checkpoint VALUES(?,?,?,?,?) ON CONFLICT(job_id) DO UPDATE SET revision=excluded.revision,data=excluded.data,updated=excluded.updated", j.ID, next.Revision, fp, core.JSON(next), time.Now().Unix()); e != nil {
			return e
		}
		visible := map[string]any{}
		if e = json.Unmarshal([]byte(core.JSON(cp.Result)), &visible); e != nil {
			return e
		}
		visible["state"] = "running"
		visible["checkpoint"] = map[string]any{"revision": next.Revision, "phase": cp.Phase, "next_step": cp.NextStep}
		return c.Exec("UPDATE job SET result=? WHERE id=? AND token=?", core.JSON(visible), j.ID, j.Token)
	})
	if e == nil {
		cp.Revision = next.Revision
	}
	return e
}

// ResumeInvestigation explicitly requeues the same logical task. Budget rows,
// first_started_at, execution_deadline_at and content identity are never reset.
func (s *Store) ResumeInvestigation(ctx context.Context, id, fp string) (Job, error) {
	var out Job
	e := s.DB.Tx(ctx, true, func(c *db.Conn) error {
		rows, e := c.Query("SELECT * FROM job WHERE id=?", id)
		if e != nil {
			return e
		}
		if len(rows) != 1 {
			return core.Err("NOT_FOUND", "task not found", 404)
		}
		out = jobRow(rows[0])
		now := time.Now().Unix()
		if out.Kind != "research" && out.Kind != "recipe" {
			return core.Err("TASK_NOT_RESUMABLE", "only investigations and recipes have checkpoints", 409)
		}
		if out.Cancel || out.State == "cancelled" {
			return core.Err("TASK_NOT_RESUMABLE", "cancelled tasks are not resumed", 409)
		}
		if out.Started == 0 || out.Deadline <= now {
			return core.Err("TASK_DEADLINE_EXCEEDED", "original execution deadline has expired; submit a new task explicitly", 409)
		}
		if out.State == "queued" && out.Attempt > 0 {
			return nil
		} // repeating the same resume cannot create another task
		if out.State != "failed" && !(out.State == "running" && out.Lease <= now) {
			return core.Err("TASK_NOT_RESUMABLE", "only failed/interrupted tasks can be resumed", 409)
		}
		saved, e := c.Query("SELECT fingerprint,data FROM research_checkpoint WHERE job_id=?", id)
		if e != nil {
			return e
		}
		if len(saved) != 1 {
			return core.Err("CHECKPOINT_MISSING", "task has no durable checkpoint", 409)
		}
		if db.String(saved[0], "fingerprint") != fp {
			return core.Err("TASK_CONFIGURATION_CHANGED", "restore the original task configuration or submit a new task", 409)
		}
		var cp research.Checkpoint
		if e = json.Unmarshal([]byte(db.String(saved[0], "data")), &cp); e != nil {
			return e
		}
		if e = cp.CanResume(); e != nil {
			return e
		}
		ep, e := epoch(c)
		if e != nil {
			return e
		}
		if cp.Result.Epoch != ep {
			return core.Err("VIEW_INVALIDATED", "withdrawal invalidates the task's original scope", 409)
		}
		for cacheID := range checkpointCacheIDs(&cp) {
			r, e := c.Query("SELECT owner,expires FROM cache WHERE id=?", cacheID)
			if e != nil {
				return e
			}
			if len(r) != 1 || db.String(r[0], "owner") != out.Owner || db.Int(r[0], "expires") <= now {
				return core.Err("RESULT_EXPIRED", "original task scope or results expired", 410)
			}
		}
		if e = c.Exec("UPDATE job SET state='queued',error='',lease=0,token='',queue_expires=deadline WHERE id=?", id); e != nil {
			return e
		}
		if e = c.Exec("DELETE FROM job_terminal WHERE id=?", id); e != nil {
			return e
		}
		out.State = "queued"
		out.Error = ""
		out.Token = ""
		out.Lease = 0
		return nil
	})
	return out, e
}
