package store

import (
	"context"
	"encoding/json"
	"time"
	"github.com/BlueSkyXN/XRAG/core"
	db "github.com/BlueSkyXN/XRAG/internal/sqlite"
)

type MaintenancePolicy struct {
	ContentEvery    int  `json:"content_every_seconds"`
	ContentDays     int  `json:"content_keep_days"`
	ContentExternal bool `json:"content_external"`
	Enabled         bool `json:"enabled"`
	BackupEvery     int  `json:"backup_every_seconds"`
	BackupKeep      int  `json:"backup_keep"`
	RecordsEvery    int  `json:"records_every_seconds"`
	RecordsDays     int  `json:"records_keep_days"`
	BatchSize       int  `json:"batch_size"`
}
type MaintenanceSetting struct {
	Revision    int64             `json:"revision"`
	Policy      MaintenancePolicy `json:"policy"`
	NextBackup  int64             `json:"next_backup_at"`
	NextContent int64             `json:"next_content_at"`
	NextRecords int64             `json:"next_records_at"`
}
type MaintenanceRun struct {
	ID        string            `json:"id"`
	State     string            `json:"state"`
	Automatic bool              `json:"automatic"`
	Backup    bool              `json:"backup"`
	Content   bool              `json:"content"`
	Records   bool              `json:"records"`
	Policy    MaintenancePolicy `json:"policy"`
	Owner     string            `json:"owner"`
	Created   int64             `json:"created"`
	Started   int64             `json:"started"`
	Finished  int64             `json:"finished"`
	Token     string            `json:"-"`
	Result    json.RawMessage   `json:"result"`
	Error     string            `json:"error,omitempty"`
}

var housekeepingSchema = []string{
	`CREATE TABLE IF NOT EXISTS maintenance_policy(id INTEGER PRIMARY KEY CHECK(id=1),revision INTEGER NOT NULL,data TEXT NOT NULL,next_backup INTEGER NOT NULL,next_records INTEGER NOT NULL,next_content INTEGER NOT NULL DEFAULT 0)`,
	`CREATE TABLE IF NOT EXISTS maintenance_run(id TEXT PRIMARY KEY,state TEXT NOT NULL,automatic INTEGER NOT NULL,owner TEXT NOT NULL,payload TEXT NOT NULL,created INTEGER NOT NULL,started INTEGER NOT NULL DEFAULT 0,finished INTEGER NOT NULL DEFAULT 0,lease INTEGER NOT NULL DEFAULT 0,token TEXT NOT NULL DEFAULT '',result TEXT NOT NULL DEFAULT '{}',error TEXT NOT NULL DEFAULT '')`,
	`CREATE UNIQUE INDEX IF NOT EXISTS maintenance_one_running ON maintenance_run(state) WHERE state='running'`,
	`CREATE TABLE IF NOT EXISTS automatic_backup(id TEXT PRIMARY KEY,run_id TEXT NOT NULL,created INTEGER NOT NULL)`,
	`CREATE TABLE IF NOT EXISTS job_terminal(id TEXT PRIMARY KEY,finished INTEGER NOT NULL)`,
	`CREATE TRIGGER IF NOT EXISTS job_terminal_stamp AFTER UPDATE OF state ON job WHEN new.state IN('succeeded','partial','failed','cancelled','expired') AND old.state<>new.state BEGIN INSERT INTO job_terminal(id,finished) VALUES(new.id,CAST(strftime('%s','now') AS INTEGER)) ON CONFLICT(id) DO UPDATE SET finished=excluded.finished; END`,
	`CREATE TABLE IF NOT EXISTS job_tombstone(id TEXT PRIMARY KEY,kb TEXT NOT NULL,owner TEXT NOT NULL,kind TEXT NOT NULL,state TEXT NOT NULL,finished INTEGER NOT NULL)`,
	`CREATE TABLE IF NOT EXISTS budget_rollup(kb TEXT NOT NULL,run TEXT NOT NULL,day TEXT NOT NULL,operation TEXT NOT NULL,state TEXT NOT NULL,calls INTEGER NOT NULL,tokens INTEGER NOT NULL,cost INTEGER NOT NULL,PRIMARY KEY(kb,run,day,operation,state))`,
}

func defaultMaintenance() MaintenancePolicy {
	return MaintenancePolicy{ContentDays: 30, BackupKeep: 7, RecordsDays: 30, BatchSize: 200}
}
func ValidateMaintenance(p MaintenancePolicy) error {
	for _, n := range []int{p.BackupEvery, p.RecordsEvery, p.ContentEvery} {
		if n < 0 || (n > 0 && n < 60) || n > 604800 {
			return core.Err("INVALID_ARGUMENT", "maintenance interval is 0 or 60..604800 seconds", 400)
		}
	}
	if p.BackupKeep < 1 || p.BackupKeep > 15 || p.RecordsDays < 1 || p.RecordsDays > 36500 || p.BatchSize < 1 || p.BatchSize > 1000 {
		return core.Err("INVALID_ARGUMENT", "backup_keep 1..15, records_keep_days 1..36500, batch_size 1..1000", 400)
	}
	if p.ContentEvery > 0 && (p.ContentDays < 1 || p.ContentDays > 36500) {
		return core.Err("INVALID_ARGUMENT", "content_keep_days must be 1..36500", 400)
	}
	if p.Enabled && p.BackupEvery == 0 && p.RecordsEvery == 0 && p.ContentEvery == 0 {
		return core.Err("INVALID_ARGUMENT", "enabled maintenance requires at least one schedule", 400)
	}
	return nil
}
func maintenanceSetting(c *db.Conn) (MaintenanceSetting, error) {
	s := MaintenanceSetting{Policy: defaultMaintenance()}
	rs, e := c.Query("SELECT * FROM maintenance_policy WHERE id=1")
	if e != nil || len(rs) == 0 {
		return s, e
	}
	s.Revision = db.Int(rs[0], "revision")
	s.NextBackup = db.Int(rs[0], "next_backup")
	s.NextRecords = db.Int(rs[0], "next_records")
	s.NextContent = db.Int(rs[0], "next_content")
	e = json.Unmarshal([]byte(db.String(rs[0], "data")), &s.Policy)
	return s, e
}
func (s *Store) MaintenanceSetting(ctx context.Context) (MaintenanceSetting, error) {
	var v MaintenanceSetting
	e := s.DB.Do(ctx, func(c *db.Conn) error { var err error; v, err = maintenanceSetting(c); return err })
	return v, e
}
func (s *Store) SetMaintenance(ctx context.Context, expected int64, p MaintenancePolicy) (MaintenanceSetting, error) {
	out := MaintenanceSetting{}
	if e := ValidateMaintenance(p); e != nil {
		return out, e
	}
	e := s.DB.Tx(ctx, true, func(c *db.Conn) error {
		old, e := maintenanceSetting(c)
		if e != nil {
			return e
		}
		if old.Revision != expected {
			return core.Err("VERSION_CONFLICT", "maintenance settings changed", 409)
		}
		now := time.Now().Unix()
		out = MaintenanceSetting{Revision: expected + 1, Policy: p}
		if p.Enabled && p.BackupEvery > 0 {
			out.NextBackup = now + int64(p.BackupEvery)
		}
		if p.Enabled && p.RecordsEvery > 0 {
			out.NextRecords = now + int64(p.RecordsEvery)
		}
		if p.ContentDays == 0 {
			p.ContentDays = 30
			out.Policy.ContentDays = 30
		}
		if p.Enabled && p.ContentEvery > 0 {
			out.NextContent = now + int64(p.ContentEvery)
		}
		if e = c.Exec("INSERT INTO maintenance_policy VALUES(1,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET revision=excluded.revision,data=excluded.data,next_backup=excluded.next_backup,next_records=excluded.next_records,next_content=excluded.next_content", out.Revision, core.JSON(p), out.NextBackup, out.NextRecords, out.NextContent); e != nil {
			return e
		}
		if !p.Enabled {
			return c.Exec("UPDATE maintenance_run SET state='cancelled',finished=?,error='automatic maintenance disabled' WHERE state='queued' AND automatic=1", now)
		}
		return nil
	})
	return out, e
}
func maintenanceRow(r db.Row) (MaintenanceRun, error) {
	var x MaintenanceRun
	e := json.Unmarshal([]byte(db.String(r, "payload")), &x)
	x.ID = db.String(r, "id")
	x.State = db.String(r, "state")
	x.Automatic = db.Int(r, "automatic") != 0
	x.Owner = db.String(r, "owner")
	x.Created = db.Int(r, "created")
	x.Started = db.Int(r, "started")
	x.Finished = db.Int(r, "finished")
	x.Token = db.String(r, "token")
	x.Result = json.RawMessage(db.String(r, "result"))
	x.Error = db.String(r, "error")
	return x, e
}
func insertMaintenance(c *db.Conn, x MaintenanceRun) (MaintenanceRun, error) {
	x.ID = core.ID("mr_")
	x.State = "queued"
	x.Created = time.Now().Unix()
	x.Result = json.RawMessage(`{}`)
	e := c.Exec("INSERT INTO maintenance_run(id,state,automatic,owner,payload,created) VALUES(?,'queued',?,?,?,?)", x.ID, x.Automatic, x.Owner, core.JSON(x), x.Created)
	return x, e
}
func (s *Store) QueueMaintenance(ctx context.Context, owner string, backup, records bool, contentOpt ...bool) (MaintenanceRun, error) {
	var x MaintenanceRun
	content := len(contentOpt) > 0 && contentOpt[0]
	if !backup && !records && !content {
		return x, core.Err("INVALID_ARGUMENT", "select backup, records and/or content", 400)
	}
	e := s.DB.Tx(ctx, true, func(c *db.Conn) error {
		cfg, e := maintenanceSetting(c)
		if e != nil {
			return e
		}
		busy, e := c.Query("SELECT id FROM maintenance_run WHERE state IN('queued','running') LIMIT 1")
		if e != nil {
			return e
		}
		if len(busy) > 0 {
			return core.Err("WORKSPACE_BUSY", "another maintenance run is pending", 409)
		}
		x, e = insertMaintenance(c, MaintenanceRun{Owner: owner, Backup: backup, Records: records, Content: content, Policy: cfg.Policy})
		return e
	})
	return x, e
}
func (s *Store) ScheduleMaintenance(ctx context.Context, now time.Time) error {
	return s.DB.Tx(ctx, true, func(c *db.Conn) error {
		cfg, e := maintenanceSetting(c)
		if e != nil {
			return e
		}
		if !cfg.Policy.Enabled {
			return nil
		}
		backup := cfg.NextBackup > 0 && cfg.NextBackup <= now.Unix()
		records := cfg.NextRecords > 0 && cfg.NextRecords <= now.Unix()
		content := cfg.NextContent > 0 && cfg.NextContent <= now.Unix()
		if !backup && !records && !content {
			return nil
		}
		rs, e := c.Query("SELECT id FROM maintenance_run WHERE state IN('queued','running') LIMIT 1")
		if e != nil {
			return e
		}
		if len(rs) > 0 {
			return nil
		}
		if _, e = insertMaintenance(c, MaintenanceRun{Owner: "maintenance-schedule", Automatic: true, Backup: backup, Records: records, Content: content, Policy: cfg.Policy}); e != nil {
			return e
		}
		if backup {
			cfg.NextBackup = now.Unix() + int64(cfg.Policy.BackupEvery)
		}
		if records {
			cfg.NextRecords = now.Unix() + int64(cfg.Policy.RecordsEvery)
		}
		if content {
			cfg.NextContent = now.Unix() + int64(cfg.Policy.ContentEvery)
		}
		return c.Exec("UPDATE maintenance_policy SET next_backup=?,next_records=?,next_content=? WHERE id=1", cfg.NextBackup, cfg.NextRecords, cfg.NextContent)
	})
}
func (s *Store) MaintenanceRuns(ctx context.Context) ([]MaintenanceRun, error) {
	out := []MaintenanceRun{}
	e := s.DB.Do(ctx, func(c *db.Conn) error {
		rs, e := c.Query("SELECT * FROM maintenance_run ORDER BY created DESC,id DESC LIMIT 100")
		if e != nil {
			return e
		}
		for _, r := range rs {
			x, e := maintenanceRow(r)
			if e != nil {
				return e
			}
			out = append(out, x)
		}
		return nil
	})
	return out, e
}
func (s *Store) ClaimMaintenance(ctx context.Context) (MaintenanceRun, bool, error) {
	var x MaintenanceRun
	found := false
	e := s.DB.Tx(ctx, true, func(c *db.Conn) error {
		now := time.Now().Unix()
		if e := c.Exec("UPDATE maintenance_run SET state='failed',finished=?,error='interrupted: maintenance lease expired' WHERE state='running' AND lease<=?", now, now); e != nil {
			return e
		}
		rs, e := c.Query("SELECT id FROM maintenance_run WHERE state='running'")
		if e != nil || len(rs) > 0 {
			return e
		}
		rs, e = c.Query("SELECT * FROM maintenance_run WHERE state='queued' ORDER BY created,id LIMIT 1")
		if e != nil || len(rs) == 0 {
			return e
		}
		x, e = maintenanceRow(rs[0])
		if e != nil {
			return e
		}
		x.Token = core.ID("mt_")
		x.State = "running"
		x.Started = now
		e = c.Exec("UPDATE maintenance_run SET state='running',token=?,started=?,lease=? WHERE id=? AND state='queued'", x.Token, now, now+600, x.ID)
		found = e == nil
		return e
	})
	return x, found, e
}
func (s *Store) FinishMaintenance(ctx context.Context, x MaintenanceRun, result any, runErr error) error {
	state, msg := "succeeded", ""
	if runErr != nil {
		state = "failed"
		msg = runErr.Error()
	}
	return s.DB.Do(ctx, func(c *db.Conn) error {
		return c.Exec("UPDATE maintenance_run SET state=?,finished=?,result=?,error=?,lease=0 WHERE id=? AND token=? AND state='running'", state, time.Now().Unix(), core.JSON(result), msg, x.ID, x.Token)
	})
}

// MarkAutomaticBackup only tags backups made by maintenance. Manual backups are never rotated by this policy.
func (s *Store) MarkAutomaticBackup(ctx context.Context, id, run string) error {
	return s.DB.Do(ctx, func(c *db.Conn) error {
		return c.Exec("INSERT OR IGNORE INTO automatic_backup VALUES(?,?,?)", id, run, time.Now().Unix())
	})
}
func (s *Store) RotateAutomaticBackups(ctx context.Context, keep int) ([]string, error) {
	ids := []string{}
	if keep < 1 {
		return ids, core.Err("INVALID_ARGUMENT", "keep at least one automatic backup", 400)
	}
	e := s.DB.Do(ctx, func(c *db.Conn) error {
		rs, e := c.Query("SELECT a.id FROM automatic_backup a JOIN backup_record b ON b.id=a.id ORDER BY b.created DESC,a.rowid DESC LIMIT -1 OFFSET ?", keep)
		for _, r := range rs {
			ids = append(ids, db.String(r, "id"))
		}
		return e
	})
	if e != nil {
		return nil, e
	}
	removed := []string{}
	for _, id := range ids {
		if e = s.RemoveBackup(ctx, id); e != nil {
			return removed, e
		}
		removed = append(removed, id)
	}
	return removed, nil
}

type RecordsReport struct {
	DryRun             bool `json:"dry_run"`
	KeepDays           int  `json:"keep_days"`
	Jobs               int  `json:"jobs"`
	Calls              int  `json:"settled_calls"`
	UnknownRetained    int  `json:"unknown_calls_retained"`
	ExpiredResults     int  `json:"expired_results"`
	QueryRecords       int  `json:"query_records"`
	MaintenanceRecords int  `json:"maintenance_records"`
	More               bool `json:"more_batches"`
	RemovedContent     bool `json:"removed_content"`
}

// Only operational records are rotated online. Source editions, evidence,
// feedback, evaluation datasets, unknown charges and idempotency keys are kept.
func (s *Store) RotateRecords(ctx context.Context, days, batch int, apply bool) (RecordsReport, error) {
	out := RecordsReport{DryRun: !apply, KeepDays: days}
	if days < 1 || days > 36500 || batch < 1 || batch > 1000 {
		return out, core.Err("INVALID_ARGUMENT", "invalid retention bounds", 400)
	}
	cut := time.Now().Add(-time.Duration(days) * 24 * time.Hour).Unix()
	today := time.Now().UTC().Format("2006-01-02")
	e := s.DB.Tx(ctx, apply, func(c *db.Conn) error {
		unknown, e := c.Query("SELECT COUNT(*) n FROM budget_call WHERE state<>'completed' AND created<?", cut)
		if e != nil {
			return e
		}
		out.UnknownRetained = int(db.Int(unknown[0], "n"))
		calls, e := c.Query(`SELECT b.* FROM budget_call b WHERE b.state='completed' AND b.created<? AND b.day<? AND NOT EXISTS(SELECT 1 FROM job j WHERE j.id=b.run AND j.state IN('queued','running')) ORDER BY b.created,b.id LIMIT ?`, cut, today, batch+1)
		if e != nil {
			return e
		}
		out.More = len(calls) > batch
		if len(calls) > batch {
			calls = calls[:batch]
		}
		out.Calls = len(calls)
		jobs, e := c.Query(`SELECT j.*,t.finished FROM job j JOIN job_terminal t ON t.id=j.id WHERE t.finished<? AND j.state IN('succeeded','partial','failed','cancelled','expired') AND NOT EXISTS(SELECT 1 FROM budget_call b WHERE b.run=j.id AND b.state<>'completed') AND NOT EXISTS(SELECT 1 FROM call_usage u WHERE u.job_id=j.id AND u.state IN('sent','outcome_unknown')) ORDER BY t.finished,j.id LIMIT ?`, cut, batch+1)
		if e != nil {
			return e
		}
		out.More = out.More || len(jobs) > batch
		if len(jobs) > batch {
			jobs = jobs[:batch]
		}
		out.Jobs = len(jobs)
		rows, e := c.Query("SELECT id FROM cache WHERE expires<=? ORDER BY expires,id LIMIT ?", time.Now().Unix(), batch+1)
		if e != nil {
			return e
		}
		out.More = out.More || len(rows) > batch
		if len(rows) > batch {
			rows = rows[:batch]
		}
		out.ExpiredResults = len(rows)
		qr, e := c.Query("SELECT id FROM query_record WHERE expires<=? ORDER BY expires,id LIMIT ?", time.Now().Unix(), batch+1)
		if e != nil {
			return e
		}
		mr, e := c.Query("SELECT id FROM maintenance_run WHERE finished>0 AND finished<? AND state NOT IN('queued','running') ORDER BY finished,id LIMIT ?", cut, batch+1)
		if e != nil {
			return e
		}
		out.More = out.More || len(qr) > batch || len(mr) > batch
		out.QueryRecords = min(len(qr), batch)
		out.MaintenanceRecords = min(len(mr), batch)
		if !apply {
			return nil
		}
		for _, r := range calls {
			if e = c.Exec(`INSERT INTO budget_rollup VALUES(?,?,?,?,?,1,?,?) ON CONFLICT(kb,run,day,operation,state) DO UPDATE SET calls=calls+1,tokens=tokens+excluded.tokens,cost=cost+excluded.cost`, db.String(r, "kb"), db.String(r, "run"), db.String(r, "day"), db.String(r, "operation"), db.String(r, "state"), db.Int(r, "charged_tokens"), db.Int(r, "charged_cost")); e != nil {
				return e
			}
			if e = c.Exec("DELETE FROM budget_call WHERE id=? AND state='completed'", db.String(r, "id")); e != nil {
				return e
			}
		}
		for _, r := range jobs {
			id := db.String(r, "id")
			if e = c.Exec("INSERT OR IGNORE INTO job_tombstone VALUES(?,?,?,?,?,?)", id, db.String(r, "kb"), db.String(r, "owner"), db.String(r, "kind"), db.String(r, "state"), db.Int(r, "finished")); e != nil {
				return e
			}
			for _, q := range []string{"DELETE FROM build_detail WHERE job_id=?", "DELETE FROM job_trigger WHERE job_id=?", "DELETE FROM call_usage WHERE job_id=?", "DELETE FROM job_terminal WHERE id=?", "DELETE FROM job WHERE id=?"} {
				if e = c.Exec(q, id); e != nil {
					return e
				}
			}
		}
		for _, r := range rows {
			if e = c.Exec("DELETE FROM cache WHERE id=?", db.String(r, "id")); e != nil {
				return e
			}
		}
		if e = c.Exec("DELETE FROM query_record WHERE id IN(SELECT id FROM query_record WHERE expires<=? ORDER BY expires,id LIMIT ?)", time.Now().Unix(), batch); e != nil {
			return e
		}
		// Keep a bounded recent maintenance activity list; it contains no model usage.
		return c.Exec("DELETE FROM maintenance_run WHERE id IN(SELECT id FROM maintenance_run WHERE finished>0 AND finished<? AND state NOT IN('queued','running') ORDER BY finished,id LIMIT ?)", cut, batch)
	})
	return out, e
}

// Continue a scheduled record sweep in bounded batches. A newer/disabled policy
// wins; an explicit one-shot request is never turned into an endless schedule.
func (s *Store) ContinueRecords(ctx context.Context, run MaintenanceRun) error {
	if !run.Automatic {
		return nil
	}
	return s.DB.Tx(ctx, true, func(c *db.Conn) error {
		setting, e := maintenanceSetting(c)
		if e != nil {
			return e
		}
		if !setting.Policy.Enabled || setting.Policy.RecordsEvery == 0 || core.JSON(setting.Policy) != core.JSON(run.Policy) {
			return nil
		}
		next := time.Now().Add(5 * time.Second).Unix()
		return c.Exec("UPDATE maintenance_policy SET next_records=MIN(next_records,?) WHERE id=1", next)
	})
}
