package tests

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
	"github.com/BlueSkyXN/XRAG/core"
	"github.com/BlueSkyXN/XRAG/evaluation"
	"github.com/BlueSkyXN/XRAG/internal/housekeeping"
	db "github.com/BlueSkyXN/XRAG/internal/sqlite"
	"github.com/BlueSkyXN/XRAG/internal/store"
)

type noFullSnapshot struct{ *store.Store }

func (r noFullSnapshot) Snapshot(context.Context, []string) (core.View, error) {
	return core.View{}, errors.New("full Snapshot must not be called")
}
func indexedOnly(s *store.Store) *core.Engine {
	return &core.Engine{Repo: noFullSnapshot{s}, Mode: "server"}
}
func admin6() store.Principal {
	return store.Principal{ID: "admin-test", Role: "admin", KBs: []string{"*"}}
}
func rows6(t *testing.T, s *store.Store, q string, args ...any) []db.Row {
	t.Helper()
	var rs []db.Row
	must(t, s.DB.Do(ctx, func(c *db.Conn) error { var e error; rs, e = c.Query(q, args...); return e }))
	return rs
}
func exec6(t *testing.T, s *store.Store, q string, args ...any) {
	t.Helper()
	must(t, s.DB.Do(ctx, func(c *db.Conn) error { return c.Exec(q, args...) }))
}

func TestIndexedKeywordDoesNotLoadCorpus(t *testing.T) {
	s, _ := setup(t)
	pub(t, s, "hit.md", "# Hit\nNEEDLEXYZ unique document\n")
	for i := 0; i < 25; i++ {
		pub(t, s, fmt.Sprintf("other%d.md", i), "# unrelated\n"+strings.Repeat("large irrelevant payload ", 100))
	}
	e := indexedOnly(s)
	e.MaxItems = 3
	r := run(t, e, "search.keyword", core.Request{Q: "NEEDLEXYZ", Trace: true})
	if r.Returned != 1 || *r.Total.Value != 1 || !strings.Contains(r.Items[0].Text, "unique") {
		t.Fatal(r)
	}
	c, er := s.GetCache(ctx, r.ResultID)
	must(t, er)
	if strings.Contains(string(c.Data), "large irrelevant payload") || strings.Contains(string(c.Data), "unique document") {
		t.Fatal("cache copied full text")
	}
	if r.Strategy["execution"] != "indexed_metadata_then_page_hydration" {
		t.Fatal(r.Strategy)
	}
}
func TestIndexedTreeIsMetadataOnly(t *testing.T) {
	s, _ := setup(t)
	pub(t, s, "a.md", "# label\nBODY_PRIVATE\n")
	r := run(t, indexedOnly(s), "files.tree", core.Request{SaveScope: "required"})
	if r.Returned != 1 || r.Items[0].Text != "" {
		t.Fatal(r)
	}
	c, er := s.GetCache(ctx, r.Handle)
	must(t, er)
	if strings.Contains(string(c.Data), "BODY_PRIVATE") {
		t.Fatal("scope copied body")
	}
	x := run(t, indexedOnly(s), "handles.describe", core.Request{ID: r.Handle})
	if x.Data == nil {
		t.Fatal(x)
	}
}
func TestIndexedSavedScopeCanQueryGraph(t *testing.T) {
	s, _ := setup(t)
	pub(t, s, "f1.md", "# f1\n[[A]] and [[B]]\n")
	pub(t, s, "f2.md", "# f2\n[[A]] --uses--> [[B]]\n")
	e := indexedOnly(s)
	r := run(t, e, "files.tree", core.Request{Filters: []core.Filter{{Field: "path", Op: "eq", Value: "f1.md"}}, SaveScope: "required"})
	g := run(t, e, "graph.neighbors", core.Request{Name: "A", Within: r.Handle})
	if g.Returned != 0 {
		t.Fatal(g)
	}
}
func TestIndexedBoundedWindowStillHasExactTotal(t *testing.T) {
	s, _ := setup(t)
	for i := 0; i < 9; i++ {
		pub(t, s, fmt.Sprintf("%d.md", i), "# Common\nBOUNDEDXYZ\n")
	}
	e := indexedOnly(s)
	e.MaxItems = 4
	r := run(t, e, "search.keyword", core.Request{Q: "BOUNDEDXYZ", Limit: 2, SaveScope: "auto"})
	if *r.Total.Value != 9 || r.CacheComplete || r.Handle != "" {
		t.Fatal(r)
	}
	r = run(t, e, "results.page", core.Request{ResultID: r.ResultID, Cursor: r.NextCursor, Limit: 2})
	if r.NextCursor != "" || r.Returned != 2 {
		t.Fatal(r)
	}
	_, er := e.Run(ctx, "search.keyword", core.Request{Kbs: []string{"demo"}, Q: "BOUNDEDXYZ", SaveScope: "required"}, "tester")
	code(t, er, "HANDLE_TOO_LARGE")
}
func TestIndexedPerFilePreservesExactCount(t *testing.T) {
	s, _ := setup(t)
	pub(t, s, "a.md", "## one\nCOUNTXYZ\n## two\nCOUNTXYZ\n## three\nCOUNTXYZ\n")
	r := run(t, indexedOnly(s), "search.keyword", core.Request{Q: "COUNTXYZ", PerFile: 1})
	if *r.Total.Value != 3 || r.Returned != 1 {
		t.Fatal(r)
	}
}
func TestIndexedContinuationReadsPriorEdition(t *testing.T) {
	s, _ := setup(t)
	pub(t, s, "a.md", "## one\nPAGEXYZ oldone\n## two\nPAGEXYZ oldtwo\n")
	e := indexedOnly(s)
	r := run(t, e, "search.keyword", core.Request{Q: "PAGEXYZ", Limit: 1})
	pub(t, s, "a.md", "# changed\nPAGEXYZ totallynew\n")
	p := run(t, e, "results.page", core.Request{ResultID: r.ResultID, Cursor: r.NextCursor, Limit: 1})
	if p.Returned != 1 || strings.Contains(p.Items[0].Text, "totallynew") {
		t.Fatal(p)
	}
}
func TestIndexedWithdrawalInvalidatesPage(t *testing.T) {
	s, _ := setup(t)
	pub(t, s, "a.md", "# xyz\nNEEDLE\n")
	e := indexedOnly(s)
	r := run(t, e, "search.keyword", core.Request{Q: "NEEDLE"})
	must(t, s.Withdraw(ctx, "demo", "a.md"))
	_, er := e.Run(ctx, "results.page", core.Request{Kbs: []string{"demo"}, ResultID: r.ResultID}, "tester")
	code(t, er, "VIEW_INVALIDATED")
}
func TestIndexedPathReadAvoidsCorpus(t *testing.T) {
	s, _ := setup(t)
	pub(t, s, "a.md", "# document\n原文阅读\n")
	r := run(t, indexedOnly(s), "files.read", core.Request{Path: "a.md"})
	if !strings.Contains(r.Data.(core.ReadData).Text, "原文阅读") {
		t.Fatal(r)
	}
}
func TestIndexedFilterMatchesOriginalSemantics(t *testing.T) {
	s, _ := setup(t)
	pub(t, s, "a.md", "---\nproduct: A\n---\n# common\nFILTERXYZ\n")
	pub(t, s, "b.md", "---\nproduct: B\n---\n# common\nFILTERXYZ\n")
	r := run(t, indexedOnly(s), "search.keyword", core.Request{Q: "FILTERXYZ", Filters: []core.Filter{{Field: "fields.product", Op: "eq", Value: "A"}}})
	if r.Returned != 1 || r.Items[0].Path != "a.md" {
		t.Fatal(r)
	}
}
func TestIndexedEmptyCorpusAndMissingMatch(t *testing.T) {
	s, _ := setup(t)
	e := indexedOnly(s)
	r := run(t, e, "search.keyword", core.Request{Q: "none"})
	if r.Returned != 0 || *r.Total.Value != 0 {
		t.Fatal(r)
	}
	pub(t, s, "a.md", "# Apple\napple\n")
	r = run(t, e, "search.keyword", core.Request{Q: "notpresent"})
	if *r.Total.Value != 0 {
		t.Fatal(r)
	}
}
func TestIndexedSummaryEvidenceCanBeSaved(t *testing.T) {
	s, _ := setup(t)
	pub(t, s, "a.md", "# counting\nCOUNTXYZ\n")
	r := run(t, indexedOnly(s), "search.keyword", core.Request{Q: "COUNTXYZ"})
	k, er := s.SaveKnowledge(store.WithActor(ctx, "tester"), store.Knowledge{KB: "demo", Title: "count", Body: "One matching chunk", Refs: []string{r.SummaryRef}})
	must(t, er)
	if k.ID == "" {
		t.Fatal(k)
	}
}
func TestSQLiteWaitingRequestHonorsCancellation(t *testing.T) {
	s, _ := setup(t)
	held := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() { done <- s.DB.Do(ctx, func(c *db.Conn) error { close(held); <-release; return nil }) }()
	<-held
	c, cancel := context.WithTimeout(ctx, 25*time.Millisecond)
	defer cancel()
	start := time.Now()
	er := s.DB.Do(c, func(*db.Conn) error { return errors.New("should not run") })
	close(release)
	must(t, <-done)
	if !errors.Is(er, context.DeadlineExceeded) || time.Since(start) > time.Second {
		t.Fatalf("%v %v", er, time.Since(start))
	}
}
func TestSQLiteEachStopsWithoutAccumulation(t *testing.T) {
	s, _ := setup(t)
	stop := errors.New("enough")
	n := 0
	er := s.DB.Do(ctx, func(c *db.Conn) error {
		return c.Each("WITH RECURSIVE n(v) AS(VALUES(1) UNION ALL SELECT v+1 FROM n WHERE v<100000) SELECT v FROM n", nil, func(db.Row) error {
			n++
			if n == 3 {
				return stop
			}
			return nil
		})
	})
	if !errors.Is(er, stop) || n != 3 {
		t.Fatal(er, n)
	}
}

func TestMaintenanceDefaultsOff(t *testing.T) {
	s, _ := setup(t)
	v, er := s.MaintenanceSetting(ctx)
	must(t, er)
	if v.Policy.Enabled {
		t.Fatal(v)
	}
	must(t, s.ScheduleMaintenance(ctx, time.Now().Add(time.Hour)))
	xs, er := s.MaintenanceRuns(ctx)
	must(t, er)
	if len(xs) > 0 {
		t.Fatal(xs)
	}
}
func TestMaintenanceSchedulingAndDisable(t *testing.T) {
	s, _ := setup(t)
	v, _ := s.MaintenanceSetting(ctx)
	v.Policy.Enabled = true
	v.Policy.BackupEvery = 60
	v, er := s.SetMaintenance(ctx, 0, v.Policy)
	must(t, er)
	future := time.Now().Add(61 * time.Second)
	must(t, s.ScheduleMaintenance(ctx, future))
	must(t, s.ScheduleMaintenance(ctx, future))
	xs, _ := s.MaintenanceRuns(ctx)
	if len(xs) != 1 || xs[0].State != "queued" {
		t.Fatal(xs)
	}
	v.Policy.Enabled = false
	_, er = s.SetMaintenance(ctx, v.Revision, v.Policy)
	must(t, er)
	xs, _ = s.MaintenanceRuns(ctx)
	if xs[0].State != "cancelled" {
		t.Fatal(xs)
	}
}
func TestMaintenancePolicyCASAndValidation(t *testing.T) {
	s, _ := setup(t)
	v, _ := s.MaintenanceSetting(ctx)
	_, er := s.SetMaintenance(ctx, 1, v.Policy)
	code(t, er, "VERSION_CONFLICT")
	v.Policy.BackupEvery = 1
	_, er = s.SetMaintenance(ctx, 0, v.Policy)
	code(t, er, "INVALID_ARGUMENT")
}
func TestMaintenanceBackupsProtectManualCopies(t *testing.T) {
	s, _ := setup(t)
	manual, er := s.CreateBackup(ctx)
	must(t, er)
	v, _ := s.MaintenanceSetting(ctx)
	v.Policy.BackupKeep = 1
	_, er = s.SetMaintenance(ctx, 0, v.Policy)
	must(t, er)
	c := housekeeping.Controller{Store: s}
	for i := 0; i < 2; i++ {
		_, er = s.QueueMaintenance(ctx, "admin", true, false)
		must(t, er)
		must(t, c.Tick(ctx))
	}
	xs, er := s.Backups(ctx)
	must(t, er)
	if len(xs) != 2 {
		t.Fatal(xs)
	}
	found := false
	for _, x := range xs {
		if x.ID == manual.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("manual copy rotated")
	}
	runs, _ := s.MaintenanceRuns(ctx)
	for _, r := range runs {
		if r.State != "succeeded" {
			t.Fatal(r)
		}
	}
}
func TestMaintenanceConcurrentRunRejected(t *testing.T) {
	s, _ := setup(t)
	_, er := s.QueueMaintenance(ctx, "admin", true, false)
	must(t, er)
	_, er = s.QueueMaintenance(ctx, "admin", false, true)
	code(t, er, "WORKSPACE_BUSY")
	r, ok, er := s.ClaimMaintenance(ctx)
	must(t, er)
	if !ok {
		t.Fatal("not claimed")
	}
	_, ok, er = s.ClaimMaintenance(ctx)
	must(t, er)
	if ok {
		t.Fatal("double claim")
	}
	must(t, s.FinishMaintenance(ctx, r, map[string]bool{"ok": true}, nil))
}
func TestMaintenanceInterruptedRunNotSilentlyReplayed(t *testing.T) {
	s, _ := setup(t)
	_, er := s.QueueMaintenance(ctx, "admin", true, false)
	must(t, er)
	r, ok, er := s.ClaimMaintenance(ctx)
	must(t, er)
	if !ok {
		t.Fatal(r)
	}
	exec6(t, s, "UPDATE maintenance_run SET lease=0 WHERE id=?", r.ID)
	_, ok, er = s.ClaimMaintenance(ctx)
	must(t, er)
	if ok {
		t.Fatal("replayed uncertain backup")
	}
	xs, _ := s.MaintenanceRuns(ctx)
	if xs[0].State != "failed" {
		t.Fatal(xs)
	}
}
func TestRecordRotationKeepsUnknownAndBudgetTotals(t *testing.T) {
	s, _ := setup(t)
	m := store.Meter{Store: s, KB: "demo", Run: "r6", Owner: "admin", Policy: store.BudgetPolicy{MaxCalls: 2}}
	a, er := m.Reserve(ctx, "chat", 10, 10)
	must(t, er)
	must(t, m.Complete(ctx, a, map[string]any{"prompt_tokens": 10, "completion_tokens": 5}, true))
	b, er := m.Reserve(ctx, "chat", 10, 10)
	must(t, er)
	must(t, m.Complete(ctx, b, nil, false))
	old := time.Now().Add(-72 * time.Hour)
	exec6(t, s, "UPDATE budget_call SET created=?,day=?", old.Unix(), old.UTC().Format("2006-01-02"))
	r, er := s.RotateRecords(ctx, 1, 100, false)
	must(t, er)
	if r.Calls != 1 || r.UnknownRetained != 1 {
		t.Fatal(r)
	}
	r, er = s.RotateRecords(ctx, 1, 100, true)
	must(t, er)
	if len(rows6(t, s, "SELECT * FROM budget_call")) != 1 || len(rows6(t, s, "SELECT * FROM budget_rollup")) != 1 {
		t.Fatal(r)
	}
	_, er = m.Reserve(ctx, "chat", 1, 1)
	code(t, er, "BUDGET_EXCEEDED")
	rep, er := s.BudgetReport(ctx, "demo")
	must(t, er)
	if !rep.(map[string]any)["totals_include_archived_rollups"].(bool) {
		t.Fatal(rep)
	}
}
func TestRecordRotationDoesNotDeleteContents(t *testing.T) {
	s, e := setup(t)
	pub(t, s, "a.md", "# kept\nCONTENTXYZ\n")
	_, er := s.RotateRecords(ctx, 1, 100, true)
	must(t, er)
	r := run(t, e, "search.keyword", core.Request{Q: "CONTENTXYZ"})
	if r.Returned != 1 {
		t.Fatal(r)
	}
}
func TestRecordRotationRetainsIdempotencyAndCompletionAge(t *testing.T) {
	s, _ := setup(t)
	payload := map[string]string{"x": "y"}
	j, er := s.Enqueue(ctx, "demo", "wiki", "admin", "key6", payload)
	must(t, er)
	exec6(t, s, "UPDATE job SET state='succeeded',created=? WHERE id=?", time.Now().Add(-100*time.Hour).Unix(), j.ID)
	r, er := s.RotateRecords(ctx, 1, 100, true)
	must(t, er)
	if r.Jobs != 0 {
		t.Fatal("recent finish removed", r)
	}
	exec6(t, s, "UPDATE job_terminal SET finished=? WHERE id=?", time.Now().Add(-72*time.Hour).Unix(), j.ID)
	r, er = s.RotateRecords(ctx, 1, 100, true)
	must(t, er)
	if r.Jobs != 1 {
		t.Fatal(r)
	}
	_, er = s.Enqueue(ctx, "demo", "wiki", "admin", "key6", payload)
	code(t, er, "JOB_EXPIRED")
	_, er = s.Enqueue(ctx, "demo", "wiki", "admin", "key6", map[string]string{"x": "z"})
	code(t, er, "IDEMPOTENCY_CONFLICT")
	_, er = s.Job(ctx, j.ID)
	code(t, er, "JOB_EXPIRED")
}
func TestRestoredCopyDisablesScheduledMaintenance(t *testing.T) {
	s, _ := setup(t)
	v, _ := s.MaintenanceSetting(ctx)
	v.Policy.Enabled = true
	v.Policy.BackupEvery = 60
	_, er := s.SetMaintenance(ctx, 0, v.Policy)
	must(t, er)
	_, er = s.PrepareRestoredCopy(ctx)
	must(t, er)
	v, er = s.MaintenanceSetting(ctx)
	must(t, er)
	if v.Policy.Enabled || v.NextBackup != 0 {
		t.Fatal(v)
	}
}

func feedbackDraft6(t *testing.T, s *store.Store, label, q string) store.EvalDraft {
	t.Helper()
	d := pub(t, s, "a.md", "# Reference\nNEEDLE expected passage\n")
	must(t, s.Feedback(ctx, "demo", core.ChunkItem(d.Chunks[0], s.Origin).Ref, label, "comment", "admin-test"))
	fs, er := s.Feedbacks(ctx, admin6(), []string{"demo"}, "", 20, 0)
	must(t, er)
	ed, er := s.CreateEvalDraft(ctx, admin6(), fs.Items[0].ID, q)
	must(t, er)
	return ed
}
func TestFeedbackDoesNotAutomaticallyBecomeGroundTruth(t *testing.T) {
	s, _ := setup(t)
	d := feedbackDraft6(t, s, "irrelevant", "original question")
	if d.Status != "draft" || len(d.Case.Expected) != 0 || d.Case.ExpectedEmpty {
		t.Fatal(d)
	}
	_, er := s.SaveEvalSetFromDrafts(ctx, admin6(), "demo", "set", "family", "development", []string{d.ID})
	code(t, er, "EVAL_DRAFT_UNAPPROVED")
}
func TestFeedbackApprovalAndImmutableDataset(t *testing.T) {
	s, _ := setup(t)
	d := feedbackDraft6(t, s, "useful", "original question")
	c := d.Case
	c.Expected = []evaluation.Expected{{Path: "a.md", Anchor: "expected passage"}}
	d, er := s.ApproveEvalDraft(ctx, admin6(), d.ID, 0, c, "human accepted")
	must(t, er)
	set, er := s.SaveEvalSetFromDrafts(ctx, admin6(), "demo", "set", "family", "development", []string{d.ID})
	must(t, er)
	c.Question = "edited question"
	_, er = s.ApproveEvalDraft(ctx, admin6(), d.ID, 1, c, "edited")
	must(t, er)
	saved, er := s.EvalSet(ctx, set.ID)
	must(t, er)
	if saved.Cases[0].Question != "original question" {
		t.Fatal(saved)
	}
	if len(rows6(t, s, "SELECT * FROM eval_case_lineage WHERE set_id=?", set.ID)) != 1 {
		t.Fatal("no lineage")
	}
}
func TestFeedbackApprovalRequiresExactRevisionAndValidTarget(t *testing.T) {
	s, _ := setup(t)
	d := feedbackDraft6(t, s, "useful", "question")
	_, er := s.ApproveEvalDraft(ctx, admin6(), d.ID, 20, d.Case, "")
	code(t, er, "VERSION_CONFLICT")
	c := d.Case
	c.Expected[0].Anchor = "not in doc"
	_, er = s.ApproveEvalDraft(ctx, admin6(), d.ID, 0, c, "")
	code(t, er, "EVAL_DATASET_INVALID")
}
func TestEvaluationSplitCannotLeakSameNormalizedQuestion(t *testing.T) {
	s, _ := setup(t)
	set := evaluation.Set{KB: "demo", Name: "dev", Family: "release1", Purpose: "development", Cases: []evaluation.Case{{ID: "q1", Question: "A QUESTION", ExpectedEmpty: true}}}
	_, er := s.SaveEvalSet(ctx, set)
	must(t, er)
	set.Name = "hold"
	set.Purpose = "holdout"
	set.Cases[0].Question = " a   question "
	_, er = s.SaveEvalSet(ctx, set)
	code(t, er, "EVAL_SPLIT_OVERLAP")
	set.Family = "release2"
	_, er = s.SaveEvalSet(ctx, set)
	must(t, er)
}
func TestEvaluationDuplicateQuestionRejected(t *testing.T) {
	er := evaluation.ValidateSet(evaluation.Set{KB: "demo", Name: "set", Cases: []evaluation.Case{{ID: "a", Question: "Same", ExpectedEmpty: true}, {ID: "b", Question: " same ", ExpectedEmpty: true}}})
	code(t, er, "EVAL_DUPLICATE_QUESTION")
}
func TestFeedbackWithdrawalRedactsAndBlocksPromotion(t *testing.T) {
	s, _ := setup(t)
	d := feedbackDraft6(t, s, "useful", "question from withdrawn material")
	d, er := s.ApproveEvalDraft(ctx, admin6(), d.ID, 0, d.Case, "contains note")
	must(t, er)
	must(t, s.Withdraw(ctx, "demo", "a.md"))
	x, er := s.EvalDraft(ctx, d.ID)
	must(t, er)
	if x.Status != "unavailable" || x.Case.Question != "" {
		t.Fatal(x)
	}
	xs, er := s.EvalDrafts(ctx, []string{"demo"})
	must(t, er)
	if len(xs) != 1 || xs[0].Case.Question != "" {
		t.Fatal(xs)
	}
	_, er = s.ApproveEvalDraft(ctx, admin6(), d.ID, 1, d.Case, "")
	code(t, er, "REF_WITHDRAWN")
	_, er = s.SaveEvalSetFromDrafts(ctx, admin6(), "demo", "set", "fam", "development", []string{d.ID})
	code(t, er, "EVAL_DRAFT_UNAPPROVED")
}
func TestFeedbackReaderCannotCreateEvaluation(t *testing.T) {
	s, _ := setup(t)
	_, er := s.CreateEvalDraft(ctx, store.Principal{ID: "reader", Role: "reader", KBs: []string{"demo"}}, "x", "question")
	code(t, er, "PERMISSION_DENIED")
}
func TestDiagnosticsNeverRerunsQuery(t *testing.T) {
	s, e := setup(t)
	pub(t, s, "a.md", "# doc\nEXPECTEDXYZ\n")
	q := core.Request{Kbs: []string{"demo"}, Q: "EXPECTEDXYZ", Trace: true}
	r := run(t, e, "search.keyword", q)
	ep, er := s.Epoch(ctx)
	must(t, er)
	must(t, s.RecordQuery(ctx, admin6(), q, r, ep))
	d, er := s.Diagnose(ctx, admin6(), r.QueryID, "demo", "a.md", "EXPECTEDXYZ")
	must(t, er)
	if d["reran_query"].(bool) || d["model_calls"].(int) != 0 || !d["returned_on_recorded_page"].(bool) {
		t.Fatal(d)
	}
	b, _ := json.Marshal(d)
	if !strings.Contains(string(b), "historical_execution") {
		t.Fatal(d)
	}
}

func TestAutomaticRetentionContinuesOnlyWithSameEnabledPolicy(t *testing.T) {
	s, _ := setup(t)
	v, _ := s.MaintenanceSetting(ctx)
	v.Policy.Enabled = true
	v.Policy.RecordsEvery = 86400
	v, er := s.SetMaintenance(ctx, 0, v.Policy)
	must(t, er)
	run := store.MaintenanceRun{Automatic: true, Policy: v.Policy}
	must(t, s.ContinueRecords(ctx, run))
	got, er := s.MaintenanceSetting(ctx)
	must(t, er)
	if got.NextRecords > time.Now().Add(10*time.Second).Unix() {
		t.Fatal(got)
	}
	v.Policy.Enabled = false
	v, er = s.SetMaintenance(ctx, v.Revision, v.Policy)
	must(t, er)
	must(t, s.ContinueRecords(ctx, run))
	got, er = s.MaintenanceSetting(ctx)
	must(t, er)
	if got.NextRecords != 0 {
		t.Fatal(got)
	}
}
func TestWithdrawnFeedbackDatasetCannotBeReadOrRun(t *testing.T) {
	s, _ := setup(t)
	d := feedbackDraft6(t, s, "useful", "question")
	d, er := s.ApproveEvalDraft(ctx, admin6(), d.ID, 0, d.Case, "")
	must(t, er)
	set, er := s.SaveEvalSetFromDrafts(ctx, admin6(), "demo", "set", "f", "development", []string{d.ID})
	must(t, er)
	must(t, s.Withdraw(ctx, "demo", "a.md"))
	_, er = s.EvalSet(ctx, set.ID)
	code(t, er, "REF_WITHDRAWN")
	sets, er := s.EvalSets(ctx, []string{"demo"})
	must(t, er)
	if len(sets) != 0 {
		t.Fatal(sets)
	}
}

func TestFreshInstallRejectsUnknownStorageInsteadOfEmptySearch(t *testing.T) {
	s, _ := setup(t)
	exec6(t, s, "DELETE FROM meta WHERE k='store_schema'")
	other, er := store.Open(s.Dir)
	if other != nil {
		other.Close()
	}
	code(t, er, "STORAGE_VERSION_UNSUPPORTED")
}
