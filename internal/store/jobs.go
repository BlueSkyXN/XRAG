package store

import (
	"context"
	"encoding/json"
	"time"
	"github.com/BlueSkyXN/XRAG/core"
	"github.com/BlueSkyXN/XRAG/evaluation"
	db "github.com/BlueSkyXN/XRAG/internal/sqlite"
	"github.com/BlueSkyXN/XRAG/research"
)

type Job struct {
	ID       string          `json:"id"`
	KB       string          `json:"kb"`
	Kind     string          `json:"kind"`
	State    string          `json:"state"`
	Payload  json.RawMessage `json:"payload"`
	Result   json.RawMessage `json:"result"`
	Error    string          `json:"error,omitempty"`
	Owner    string          `json:"owner"`
	Created  int64           `json:"created"`
	Started  int64           `json:"first_started_at"`
	Deadline int64           `json:"execution_deadline_at"`
	Lease    int64           `json:"lease_until"`
	Token    string          `json:"-"`
	Attempt  int             `json:"attempt"`
	Cancel   bool            `json:"cancel_requested"`
}

func jobRow(r db.Row) Job {
	return Job{db.String(r, "id"), db.String(r, "kb"), db.String(r, "kind"), db.String(r, "state"), json.RawMessage(db.String(r, "payload")), json.RawMessage(db.String(r, "result")), db.String(r, "error"), db.String(r, "owner"), db.Int(r, "created"), db.Int(r, "started"), db.Int(r, "deadline"), db.Int(r, "lease"), db.String(r, "token"), int(db.Int(r, "attempt")), db.Int(r, "cancel") != 0}
}
func (s *Store) Enqueue(ctx context.Context, kb, kind, owner, key string, payload any) (Job, error) {
	var j Job
	fingerprint := core.Hash(payload)
	e := s.DB.Tx(ctx, true, func(c *db.Conn) error {
		if key != "" {
			rs, e := c.Query("SELECT fingerprint,job_id FROM idempotency WHERE owner=? AND operation=? AND key=?", owner, kb+":"+kind, key)
			if e != nil {
				return e
			}
			if len(rs) > 0 {
				if db.String(rs[0], "fingerprint") != fingerprint {
					return core.Err("IDEMPOTENCY_CONFLICT", "same key with different request", 409)
				}
				rs, e = c.Query("SELECT * FROM job WHERE id=?", db.String(rs[0], "job_id"))
				if e == nil && len(rs) == 0 {
					return core.Err("JOB_EXPIRED", "this idempotent request already completed; its detailed record was retired", 410)
				}
				if e == nil {
					j = jobRow(rs[0])
				}
				return e
			}
		}
		// Coalesce queued build intents, preserving full rebuilds. Publish/wiki/eval remain distinct.
		if kind == "build" {
			rs, e := c.Query("SELECT * FROM job WHERE kb=? AND kind='build' AND state='queued' ORDER BY created LIMIT 1", kb)
			if e != nil {
				return e
			}
			if len(rs) > 0 {
				j = jobRow(rs[0])
				var old, next map[string]any
				_ = json.Unmarshal(j.Payload, &old)
				_ = json.Unmarshal([]byte(core.JSON(payload)), &next)
				if old == nil {
					old = map[string]any{}
				}
				// All builds scan the entire KB. A full intent is never lost while triggers coalesce.
				om, _ := old["mode"].(string)
				nm, _ := next["mode"].(string)
				if nm == "full" || om == "full" {
					old["mode"] = "full"
				} else if nm != "" && om != nm {
					old["mode"] = "full"
				} else if nm != "" {
					old["mode"] = nm
				}
				if old["mode"] == nil {
					old["mode"] = "incremental"
				}
				if next["trigger"] != nil {
					old["last_trigger"] = next["trigger"]
				}
				j.Payload = json.RawMessage(core.JSON(old))
				if e = c.Exec("UPDATE job SET payload=? WHERE id=? AND state='queued'", string(j.Payload), j.ID); e != nil {
					return e
				}
			}
		}
		if j.ID == "" {
			now := time.Now().Unix()
			j = Job{ID: core.ID("job_"), KB: kb, Kind: kind, State: "queued", Payload: json.RawMessage(core.JSON(payload)), Result: json.RawMessage(`{}`), Owner: owner, Created: now}
			if e := c.Exec("INSERT INTO job(id,kb,kind,state,payload,owner,created,queue_expires) VALUES(?,?,?,?,?,?,?,?)", j.ID, kb, kind, "queued", string(j.Payload), owner, now, now+86400); e != nil {
				return e
			}
		}
		if key != "" {
			return c.Exec("INSERT INTO idempotency(owner,operation,key,fingerprint,job_id) VALUES(?,?,?,?,?)", owner, kb+":"+kind, key, fingerprint, j.ID)
		}
		return nil
	})
	return j, e
}
func (s *Store) Claim(ctx context.Context) (Job, bool, error) {
	j := Job{}
	found := false
	e := s.DB.Tx(ctx, true, func(c *db.Conn) error {
		now := time.Now().Unix()
		if e := c.Exec("UPDATE job SET state='failed',error='interrupted: worker lease expired; investigations may use explicit resume after checking their checkpoint',lease=0 WHERE state='running' AND lease<=?", now); e != nil {
			return e
		}
		if e := c.Exec("UPDATE call_usage SET state='outcome_unknown' WHERE state='sent' AND job_id IN (SELECT id FROM job WHERE state='failed')"); e != nil {
			return e
		}
		if e := c.Exec("UPDATE job SET state='expired',error='queue timeout' WHERE state='queued' AND queue_expires<=?", now); e != nil {
			return e
		}
		if e := c.Exec("UPDATE job SET state='cancelled' WHERE state='queued' AND cancel=1"); e != nil {
			return e
		}
		rs, e := c.Query("SELECT j.* FROM job j WHERE j.state='queued' AND j.cancel=0 AND NOT EXISTS(SELECT 1 FROM job running WHERE running.kb=j.kb AND running.state='running') ORDER BY j.created,j.id LIMIT 1")
		if e != nil || len(rs) == 0 {
			return e
		}
		j = jobRow(rs[0])
		j.State = "running"
		j.Token = core.ID("lease_")
		j.Lease = now + 60
		j.Attempt++
		if j.Started == 0 {
			j.Started = now
			j.Deadline = now + 600
			if j.Kind == "research" || j.Kind == "recipe" {
				var task research.Task
				if json.Unmarshal(j.Payload, &task) == nil && task.MaxSeconds >= 1 && task.MaxSeconds <= 600 {
					j.Deadline = now + int64(task.MaxSeconds)
				}
			}
		}
		if e = c.Exec("UPDATE job SET state='running',token=?,lease=?,attempt=?,started=?,deadline=? WHERE id=?", j.Token, j.Lease, j.Attempt, j.Started, j.Deadline, j.ID); e != nil {
			return e
		}
		found = true
		return nil
	})
	return j, found, e
}
func (s *Store) Renew(ctx context.Context, j Job) error {
	return s.DB.Tx(ctx, true, func(c *db.Conn) error {
		if e := fence(c, j); e != nil {
			return e
		}
		return c.Exec("UPDATE job SET lease=? WHERE id=? AND token=?", time.Now().Unix()+60, j.ID, j.Token)
	})
}
func (s *Store) Finish(ctx context.Context, j Job, result any, runErr error) error {
	return s.DB.Tx(ctx, true, func(c *db.Conn) error {
		rs, e := c.Query("SELECT token,cancel,state FROM job WHERE id=?", j.ID)
		if e != nil {
			return e
		}
		if len(rs) != 1 || db.String(rs[0], "token") != j.Token || db.String(rs[0], "state") != "running" {
			return core.Err("JOB_FENCE_LOST", "cannot finish a replaced job", 409)
		}
		state, msg := "succeeded", ""
		if m, ok := result.(map[string]any); ok {
			if n, ok := m["failed"].(int); ok && n > 0 {
				state = "partial"
			}
		}
		if result, ok := result.(research.Result); ok && result.State == "partial" {
			state = "partial"
		}
		if report, ok := result.(evaluation.Report); ok {
			for _, sum := range report.Summaries {
				if sum.Errors > 0 {
					state = "partial"
				}
			}
		}
		if runErr != nil {
			state = "failed"
			msg = runErr.Error()
		}
		if db.Int(rs[0], "cancel") != 0 {
			state = "cancelled"
		}
		return c.Exec("UPDATE job SET state=?,error=?,result=?,lease=0 WHERE id=? AND token=?", state, msg, core.JSON(result), j.ID, j.Token)
	})
}
func (s *Store) Job(ctx context.Context, id string) (Job, error) {
	j := Job{}
	e := s.DB.Do(ctx, func(c *db.Conn) error {
		r, e := c.Query("SELECT * FROM job WHERE id=?", id)
		if e != nil {
			return e
		}
		if len(r) == 0 {
			old, err := c.Query("SELECT id FROM job_tombstone WHERE id=?", id)
			if err != nil {
				return err
			}
			if len(old) > 0 {
				return core.Err("JOB_EXPIRED", "completed job details were retired", 410)
			}
			return core.Err("NOT_FOUND", "job not found", 404)
		}
		j = jobRow(r[0])
		return nil
	})
	return j, e
}
func (s *Store) Jobs(ctx context.Context, kbs []string) ([]Job, error) {
	out := []Job{}
	e := s.DB.Do(ctx, func(c *db.Conn) error {
		r, e := c.Query("SELECT * FROM job ORDER BY created DESC,id DESC LIMIT 200")
		for _, row := range r {
			j := jobRow(row)
			if core.Subset([]string{j.KB}, kbs) {
				out = append(out, j)
			}
		}
		return e
	})
	return out, e
}
func (s *Store) Cancel(ctx context.Context, id string) error {
	return s.DB.Do(ctx, func(c *db.Conn) error {
		return c.Exec("UPDATE job SET cancel=1 WHERE id=? AND state IN ('running','queued')", id)
	})
}
func (s *Store) CheckJob(ctx context.Context, j Job) error {
	return s.DB.Do(ctx, func(c *db.Conn) error { return fence(c, j) })
}
func (s *Store) UsageStart(ctx context.Context, j Job, kind string) (string, error) {
	id := core.ID("call_")
	e := s.DB.Tx(ctx, true, func(c *db.Conn) error {
		if e := fence(c, j); e != nil {
			return e
		}
		r, e := c.Query("SELECT count(*) n FROM call_usage WHERE job_id=?", j.ID)
		if e != nil {
			return e
		}
		if db.Int(r[0], "n") >= 200 {
			return core.Err("BUDGET_EXHAUSTED", "job cap: 200 external calls per job", 422)
		}
		return c.Exec("INSERT INTO call_usage(id,job_id,kind,state,usage,created) VALUES(?,?,?,'sent','{}',?)", id, j.ID, kind, time.Now().Unix())
	})
	return id, e
}
func (s *Store) UsageEnd(ctx context.Context, id string, usage any, known bool) error {
	state := "known"
	if !known {
		state = "outcome_unknown"
	}
	return s.DB.Do(ctx, func(c *db.Conn) error {
		return c.Exec("UPDATE call_usage SET state=?,usage=? WHERE id=?", state, core.JSON(usage), id)
	})
}
