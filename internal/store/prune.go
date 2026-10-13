package store

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"
	"github.com/BlueSkyXN/XRAG/core"
	"github.com/BlueSkyXN/XRAG/internal/oplock"
	db "github.com/BlueSkyXN/XRAG/internal/sqlite"
)

type PruneReport struct {
	NextAfter               string   `json:"next_after,omitempty"`
	CycleComplete           bool     `json:"cycle_complete"`
	Online                  bool     `json:"online"`
	DryRun                  bool     `json:"dry_run"`
	KeepDays                int      `json:"keep_days"`
	CandidateEditions       int      `json:"candidate_editions"`
	ProtectedEditions       int      `json:"protected_editions"`
	EligibleEditions        []string `json:"eligible_editions"`
	RemovedEditions         int      `json:"removed_editions"`
	ExpiredCache            int      `json:"expired_cache"`
	DropModelCache          bool     `json:"drop_model_cache"`
	ModelEntries            int      `json:"model_cache_entries"`
	PhysicalFileShrinks     bool     `json:"physical_file_shrinks"`
	ExternalBackendsCleaned bool     `json:"external_backends_cleaned"`
}

// PruneHistory is an OFFLINE operation. Caller must hold exclusive RuntimeLock.
// It conservatively keeps all editions referenced by persistent knowledge,
// feedback, evaluations, unfinished jobs, or unexpired cache/history.
func (s *Store) PruneHistory(ctx context.Context, keepDays int, apply, dropModel bool) (PruneReport, error) {
	return s.pruneHistory(ctx, keepDays, apply, dropModel, 0, "")
}

// PruneContentBatch requires an exclusive operation lease, while processes remain running.
func (s *Store) PruneContentBatch(ctx context.Context, days, batch int, after string, apply bool) (PruneReport, error) {
	if !oplock.Exclusive(ctx, s.Dir) {
		return PruneReport{}, core.Err("CONTENT_LEASE_REQUIRED", "exclusive content lease required", 409)
	}
	if batch < 1 || batch > 1000 {
		return PruneReport{}, core.Err("INVALID_ARGUMENT", "batch must be 1..1000", 400)
	}
	return s.pruneHistory(ctx, days, apply, false, batch, after)
}
func (s *Store) pruneHistory(ctx context.Context, keepDays int, apply, dropModel bool, batch int, after string) (PruneReport, error) {
	out := PruneReport{DryRun: !apply, KeepDays: keepDays, DropModelCache: dropModel, EligibleEditions: []string{}}
	if keepDays < 1 || keepDays > 36500 {
		return out, core.Err("INVALID_ARGUMENT", "keep-days must be 1..36500", 400)
	}
	out.Online = batch > 0
	now := time.Now().Unix()
	cut := now - int64(keepDays)*86400
	e := s.DB.Tx(ctx, apply, func(c *db.Conn) error {
		busy, e := c.Query("SELECT id FROM job WHERE state IN ('queued','running') AND cancel=0 LIMIT 1")
		if e != nil {
			return e
		}
		if len(busy) > 0 && batch == 0 {
			return core.Err("WORKSPACE_BUSY", "finish/cancel pending jobs before pruning", 409)
		}
		limit := 100001
		if batch > 0 {
			limit = batch + 1
		}
		rows, e := c.Query("SELECT e.id,e.data FROM edition e JOIN edition_retirement r ON r.edition=e.id WHERE r.retired_at<=? AND e.id>? AND NOT EXISTS(SELECT 1 FROM file f WHERE f.active=e.id AND f.withdrawn=0) ORDER BY e.id LIMIT ?", cut, after, limit)
		if e != nil {
			return e
		}
		if len(rows) > 100000 {
			return core.Err("MAINTENANCE_LIMIT", "over 100000 retired editions; narrow maintenance offline", 422)
		}
		out.CycleComplete = true
		if batch > 0 && len(rows) > batch {
			rows = rows[:batch]
			out.NextAfter = db.String(rows[len(rows)-1], "id")
			out.CycleComplete = false
		}
		candidates := map[string]bool{}
		protected := map[string]bool{}
		for _, r := range rows {
			candidates[db.String(r, "id")] = true
		}
		out.CandidateEditions = len(rows)
		var protect func(any)
		protect = func(v any) {
			switch x := v.(type) {
			case string:
				if candidates[x] {
					protected[x] = true
				}
				if strings.HasPrefix(x, "xr1:") {
					if ref, e := core.DecodeRef(x); e == nil && ref.Origin == s.Origin && candidates[ref.Edition] {
						protected[ref.Edition] = true
					}
				}
			case []any:
				for _, a := range x {
					protect(a)
				}
			case map[string]any:
				for _, a := range x {
					protect(a)
				}
			}
		}
		scans := []string{
			"SELECT data FROM evidence_archive", "SELECT data FROM knowledge_revision", "SELECT ref AS data FROM feedback",
			"SELECT data FROM eval_set", "SELECT payload AS data FROM job", "SELECT result AS data FROM job WHERE kind<>'build'",
			"SELECT data FROM cache WHERE expires>" + strconv.FormatInt(now, 10), "SELECT data FROM query_record WHERE expires>" + strconv.FormatInt(now, 10),
		}
		for _, q := range scans {
			rs, e := c.Query(q + " LIMIT 100001")
			if e != nil {
				return e
			}
			if len(rs) > 100000 {
				return core.Err("MAINTENANCE_LIMIT", "too many owner records; no deletion performed", 422)
			}
			for _, r := range rs {
				raw := db.String(r, "data")
				if len(raw) > 32<<20 {
					return core.Err("MAINTENANCE_LIMIT", "oversized owner record", 422)
				}
				var v any
				if strings.HasPrefix(raw, "xr1:") {
					protect(raw)
				} else if e = json.Unmarshal([]byte(raw), &v); e != nil {
					return core.Err("MAINTENANCE_BLOCKED", "cannot interpret owner reference data; no deletion performed", 409)
				} else {
					protect(v)
				}
			}
		}
		for _, r := range rows {
			id := db.String(r, "id")
			if protected[id] {
				out.ProtectedEditions++
				continue
			}
			var d core.Document
			if e = json.Unmarshal([]byte(db.String(r, "data")), &d); e != nil {
				return e
			}
			// An external projection is not removed without its independently verified
			// deletion adapter. Refuse apply rather than silently orphaning live data.
			bindings, be := c.Query("SELECT data,state FROM projection_gc WHERE edition=?", id)
			if be != nil {
				return be
			}
			needsExternal := false
			hasRecord := len(bindings) > 0
			if hasRecord {
				var b ProjectionBinding
				if e = json.Unmarshal([]byte(db.String(bindings[0], "data")), &b); e != nil {
					return e
				}
				needsExternal = (b.Neo4j != nil || len(b.Vectors) > 0) && db.String(bindings[0], "state") != "remote_deleted"
			}
			if apply && needsExternal {
				return core.Err("EXTERNAL_CLEANUP_REQUIRED", "use --external to delete the recorded remote projection first", 409)
			}
			for _, ch := range d.Chunks {
				if !hasRecord && ch.Generation != "" && apply {
					return core.Err("EXTERNAL_CLEANUP_REQUIRED", "candidate has external vector generation; this command does not delete remote projections", 409)
				}
			}
			out.EligibleEditions = append(out.EligibleEditions, id)
		}
		rs, e := c.Query("SELECT COUNT(*) n FROM cache WHERE expires<=?", now)
		if e != nil {
			return e
		}
		out.ExpiredCache = int(db.Int(rs[0], "n"))
		rs, e = c.Query("SELECT COUNT(*) n FROM model_cache")
		if e != nil {
			return e
		}
		out.ModelEntries = int(db.Int(rs[0], "n"))
		if !apply {
			return nil
		}
		// Whole plan and its application stay in one offline transaction.
		for _, id := range out.EligibleEditions {
			if e = c.Exec("DELETE FROM fts WHERE chunk_id IN (SELECT id FROM chunk WHERE edition=?)", id); e != nil {
				return e
			}
			if e = c.Exec("DELETE FROM chunk WHERE edition=?", id); e != nil {
				return e
			}
			if e = c.Exec("DELETE FROM edition WHERE id=?", id); e != nil {
				return e
			}
			if e = c.Exec("DELETE FROM edition_retirement WHERE edition=?", id); e != nil {
				return e
			}
			if e = c.Exec("DELETE FROM projection_gc WHERE edition=?", id); e != nil {
				return e
			}
			out.RemovedEditions++
		}
		if e = c.Exec("DELETE FROM cache WHERE expires<=?", now); e != nil {
			return e
		}
		if e = c.Exec("DELETE FROM query_record WHERE expires<=?", now); e != nil {
			return e
		}
		if dropModel {
			if e = c.Exec("DELETE FROM model_cache"); e != nil {
				return e
			}
		}
		return nil
	})
	if e != nil {
		out.RemovedEditions = 0
	}
	return out, e
}
