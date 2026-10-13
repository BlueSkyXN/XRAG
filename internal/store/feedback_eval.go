package store

import (
	"context"
	"encoding/json"
	"strings"
	"time"
	"github.com/BlueSkyXN/XRAG/core"
	"github.com/BlueSkyXN/XRAG/evaluation"
	db "github.com/BlueSkyXN/XRAG/internal/sqlite"
)

type EvalDraft struct {
	ID         string          `json:"id"`
	KB         string          `json:"kb"`
	FeedbackID string          `json:"feedback_id"`
	Case       evaluation.Case `json:"case"`
	Status     string          `json:"status"`
	Revision   int64           `json:"revision"`
	Owner      string          `json:"owner"`
	Note       string          `json:"note"`
	Created    int64           `json:"created"`
	Updated    int64           `json:"updated"`
}

var feedbackEvalSchema = []string{
	`CREATE TABLE IF NOT EXISTS eval_draft(id TEXT PRIMARY KEY,kb TEXT NOT NULL,feedback_id TEXT NOT NULL,data TEXT NOT NULL,status TEXT NOT NULL,revision INTEGER NOT NULL,owner TEXT NOT NULL,created INTEGER NOT NULL,updated INTEGER NOT NULL)`,
	`CREATE INDEX IF NOT EXISTS eval_draft_kb ON eval_draft(kb,created,id)`,
	`CREATE TABLE IF NOT EXISTS eval_case_lineage(set_id TEXT NOT NULL,draft_id TEXT NOT NULL,feedback_id TEXT NOT NULL,PRIMARY KEY(set_id,draft_id))`,
	`CREATE TABLE IF NOT EXISTS eval_question_split(kb TEXT NOT NULL,family TEXT NOT NULL,question_hash TEXT NOT NULL,purpose TEXT NOT NULL,PRIMARY KEY(kb,family,question_hash))`,
}

// A draft derived from withdrawn material is not a bypass around that withdrawal.
func feedbackReadable(c *db.Conn, origin, id string) error {
	rows, e := c.Query("SELECT ref FROM feedback WHERE id=?", id)
	if e != nil {
		return e
	}
	if len(rows) != 1 {
		return core.Err("NOT_FOUND", "feedback missing", 404)
	}
	ref, e := core.DecodeRef(db.String(rows[0], "ref"))
	if e != nil {
		return e
	}
	if ref.Origin != origin {
		return core.Err("ORIGIN_MISMATCH", "feedback belongs to another origin", 409)
	}
	rows, e = c.Query("SELECT f.withdrawn,e.state FROM file f LEFT JOIN edition e ON e.id=? AND e.kb=f.kb AND e.path=f.path WHERE f.kb=? AND f.path=?", ref.Edition, ref.KB, ref.Path)
	if e != nil {
		return e
	}
	if len(rows) != 1 || db.Int(rows[0], "withdrawn") != 0 {
		return core.Err("REF_WITHDRAWN", "feedback source withdrawn", 410)
	}
	if db.String(rows[0], "state") == "" {
		return core.Err("REF_GONE", "feedback version no longer retained", 410)
	}
	return nil
}
func redactEvalDraft(d EvalDraft) EvalDraft {
	d.Status = "unavailable"
	d.Case = evaluation.Case{ID: d.Case.ID}
	d.Note = "Original feedback material is no longer available."
	return d
}

func (s *Store) FeedbackInfo(ctx context.Context, id string) (FeedbackInfo, error) {
	var f FeedbackInfo
	e := s.DB.Do(ctx, func(c *db.Conn) error {
		rs, e := c.Query("SELECT f.*,COALESCE(st.status,'open') status FROM feedback f LEFT JOIN feedback_state st ON st.feedback_id=f.id WHERE f.id=?", id)
		if e != nil {
			return e
		}
		if len(rs) != 1 {
			return core.Err("NOT_FOUND", "feedback not found", 404)
		}
		r := rs[0]
		f = FeedbackInfo{ID: id, KB: db.String(r, "kb"), Ref: db.String(r, "ref"), Label: db.String(r, "label"), Note: db.String(r, "note"), Owner: db.String(r, "owner"), Status: db.String(r, "status")}
		return nil
	})
	return f, e
}
func (s *Store) CreateEvalDraft(ctx context.Context, p Principal, feedbackID, question string) (EvalDraft, error) {
	d := EvalDraft{}
	if !RoleAtLeast(p, "maintainer") {
		return d, core.Err("PERMISSION_DENIED", "maintainer required", 403)
	}
	f, e := s.FeedbackInfo(ctx, feedbackID)
	if e != nil {
		return d, e
	}
	if _, e = s.Scope(ctx, p, []string{f.KB}); e != nil {
		return d, e
	}
	ref, e := core.DecodeRef(f.Ref)
	if e != nil {
		return d, e
	}
	if _, _, _, e = s.Read(ctx, ref); e != nil {
		return d, e
	}
	question = strings.TrimSpace(question)
	if question == "" || len(question) > 2000 {
		return d, core.Err("INVALID_ARGUMENT", "provide the original user question explicitly", 400)
	}
	d = EvalDraft{ID: core.ID("edraft_"), KB: f.KB, FeedbackID: f.ID, Status: "draft", Owner: p.ID, Case: evaluation.Case{ID: "feedback-" + core.Hash(f.ID)[:12], Question: question, Expected: []evaluation.Expected{}}, Created: time.Now().Unix(), Updated: time.Now().Unix()}
	// Feedback says whether one result was useful. It does not automatically label
	// the complete query as a positive or an empty-result example.
	if f.Label == "useful" {
		d.Case.Expected = []evaluation.Expected{{Path: ref.Path}}
	}
	d.Note = "Expected targets are suggestions until a maintainer explicitly approves the question and expected results."
	e = s.DB.Do(ctx, func(c *db.Conn) error {
		return c.Exec("INSERT INTO eval_draft VALUES(?,?,?,?,?,0,?,?,?)", d.ID, d.KB, d.FeedbackID, core.JSON(d), d.Status, d.Owner, d.Created, d.Updated)
	})
	return d, e
}
func (s *Store) EvalDraft(ctx context.Context, id string) (EvalDraft, error) {
	var d EvalDraft
	e := s.DB.Do(ctx, func(c *db.Conn) error {
		rs, e := c.Query("SELECT data FROM eval_draft WHERE id=?", id)
		if e != nil {
			return e
		}
		if len(rs) != 1 {
			return core.Err("NOT_FOUND", "evaluation draft not found", 404)
		}
		if e = json.Unmarshal([]byte(db.String(rs[0], "data")), &d); e != nil {
			return e
		}
		if e = feedbackReadable(c, s.Origin, d.FeedbackID); e != nil {
			d = redactEvalDraft(d)
		}
		return nil
	})
	return d, e
}
func (s *Store) EvalDrafts(ctx context.Context, kbs []string) ([]EvalDraft, error) {
	out := []EvalDraft{}
	e := s.DB.Do(ctx, func(c *db.Conn) error {
		rs, e := c.Query("SELECT data FROM eval_draft WHERE kb IN(SELECT value FROM json_each(?)) ORDER BY created DESC,id DESC LIMIT 200", core.JSON(kbs))
		if e != nil {
			return e
		}
		for _, r := range rs {
			var d EvalDraft
			if e = json.Unmarshal([]byte(db.String(r, "data")), &d); e != nil {
				return e
			}
			if er := feedbackReadable(c, s.Origin, d.FeedbackID); er != nil {
				d = redactEvalDraft(d)
			}
			out = append(out, d)
		}
		return nil
	})
	return out, e
}
func (s *Store) ApproveEvalDraft(ctx context.Context, p Principal, id string, expected int64, question evaluation.Case, note string) (EvalDraft, error) {
	d, e := s.EvalDraft(ctx, id)
	if e != nil {
		return d, e
	}
	if !RoleAtLeast(p, "maintainer") {
		return d, core.Err("PERMISSION_DENIED", "maintainer required", 403)
	}
	if _, e = s.Scope(ctx, p, []string{d.KB}); e != nil {
		return d, e
	}
	if d.Status == "unavailable" {
		return d, core.Err("REF_WITHDRAWN", "feedback source is unavailable", 410)
	}
	question.ID = d.Case.ID
	if e = evaluation.ValidateSet(evaluation.Set{KB: d.KB, Name: "approval", Cases: []evaluation.Case{question}}); e != nil {
		return d, e
	}
	if len(note) > 8000 {
		return d, core.Err("INVALID_ARGUMENT", "note too long", 400)
	}
	for _, target := range question.Expected {
		r, er := s.CurrentReference(ctx, d.KB, target.Path)
		if er != nil {
			return d, er
		}
		doc, _, _, er := s.Read(ctx, r)
		if er != nil {
			return d, er
		}
		if target.Anchor != "" && !strings.Contains(doc.Body, target.Anchor) {
			return d, core.Err("EVAL_DATASET_INVALID", "expected anchor absent from published document", 422)
		}
	}
	d.Case = question
	d.Note = note
	d.Status = "approved"
	d.Revision = expected + 1
	d.Updated = time.Now().Unix()
	e = s.DB.Tx(ctx, true, func(c *db.Conn) error {
		if e := feedbackReadable(c, s.Origin, d.FeedbackID); e != nil {
			return e
		}
		if er := c.Exec("UPDATE eval_draft SET data=?,status=?,revision=?,updated=? WHERE id=? AND revision=?", core.JSON(d), d.Status, d.Revision, d.Updated, id, expected); er != nil {
			return er
		}
		if c.Changes() != 1 {
			return core.Err("VERSION_CONFLICT", "draft changed; reload before approving", 409)
		}
		return nil
	})
	return d, e
}

// SaveEvalSetFromDrafts makes a new immutable set. The user, not the feedback
// label or an LLM, supplies the final expectations at approval time.
func (s *Store) SaveEvalSetFromDrafts(ctx context.Context, p Principal, kb, name, family, purpose string, ids []string) (evaluation.Set, error) {
	out := evaluation.Set{KB: kb, Name: name, Family: family, Purpose: purpose, Cases: []evaluation.Case{}}
	if !RoleAtLeast(p, "maintainer") {
		return out, core.Err("PERMISSION_DENIED", "maintainer required", 403)
	}
	if _, e := s.Scope(ctx, p, []string{kb}); e != nil {
		return out, e
	}
	if len(ids) < 1 || len(ids) > 100 || len(ids) != len(core.Unique(ids)) {
		return out, core.Err("INVALID_ARGUMENT", "choose 1..100 distinct approved drafts", 400)
	}
	drafts := []EvalDraft{}
	for _, id := range ids {
		d, e := s.EvalDraft(ctx, id)
		if e != nil {
			return out, e
		}
		if d.KB != kb {
			return out, core.Err("PERMISSION_DENIED", "draft belongs to another KB", 403)
		}
		if d.Status != "approved" {
			return out, core.Err("EVAL_DRAFT_UNAPPROVED", "approve expected results before dataset publication", 409)
		}
		drafts = append(drafts, d)
		out.Cases = append(out.Cases, d.Case)
	}
	if e := evaluation.ValidateSet(out); e != nil {
		return out, e
	}
	out.ID = core.ID("evs_")
	out.Revision = core.Hash([]any{out.KB, out.Name, out.Family, out.Purpose, out.Cases})
	e := s.DB.Tx(ctx, true, func(c *db.Conn) error {
		for _, d := range drafts {
			if e := feedbackReadable(c, s.Origin, d.FeedbackID); e != nil {
				return e
			}
			rs, e := c.Query("SELECT revision,status FROM eval_draft WHERE id=?", d.ID)
			if e != nil {
				return e
			}
			if len(rs) != 1 || db.Int(rs[0], "revision") != d.Revision || db.String(rs[0], "status") != "approved" {
				return core.Err("VERSION_CONFLICT", "approved draft changed during publication", 409)
			}
		}
		if e := registerSplits(c, out); e != nil {
			return e
		}
		if e := c.Exec("INSERT INTO eval_set VALUES(?,?,?,?,?,?)", out.ID, kb, name, out.Revision, core.JSON(out), time.Now().Unix()); e != nil {
			return e
		}
		for _, d := range drafts {
			if e := c.Exec("INSERT INTO eval_case_lineage VALUES(?,?,?)", out.ID, d.ID, d.FeedbackID); e != nil {
				return e
			}
		}
		return nil
	})
	return out, e
}
func registerSplits(c *db.Conn, set evaluation.Set) error {
	family := set.Family
	if family == "" {
		family = "default"
	}
	purpose := set.Purpose
	if purpose == "" {
		purpose = "development"
	}
	for _, x := range set.Cases {
		hash := core.Hash(strings.ToLower(strings.Join(strings.Fields(x.Question), " ")))
		rs, e := c.Query("SELECT purpose FROM eval_question_split WHERE kb=? AND family=? AND question_hash=?", set.KB, family, hash)
		if e != nil {
			return e
		}
		if len(rs) > 0 && db.String(rs[0], "purpose") != purpose {
			return core.Err("EVAL_SPLIT_OVERLAP", "same normalized question cannot appear in development and holdout sets of one family", 409)
		}
		if e = c.Exec("INSERT OR IGNORE INTO eval_question_split VALUES(?,?,?,?)", set.KB, family, hash, purpose); e != nil {
			return e
		}
	}
	return nil
}

func readableEvalLineage(c *db.Conn, origin, id string) error {
	rows, e := c.Query("SELECT DISTINCT feedback_id FROM eval_case_lineage WHERE set_id=?", id)
	if e != nil {
		return e
	}
	for _, r := range rows {
		if e = feedbackReadable(c, origin, db.String(r, "feedback_id")); e != nil {
			return e
		}
	}
	return nil
}
