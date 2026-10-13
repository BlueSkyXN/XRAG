package tests

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
	"github.com/BlueSkyXN/XRAG/core"
	"github.com/BlueSkyXN/XRAG/internal/build"
	"github.com/BlueSkyXN/XRAG/internal/server"
	"github.com/BlueSkyXN/XRAG/internal/store"
	"github.com/BlueSkyXN/XRAG/research"
)

var interrupted9 = errors.New("fixture: process interrupted just after checkpoint commit")

func savedTask9(t *testing.T, phase string, step int) (*store.Store, *build.Builder, store.Job, *research.Checkpoint, string) {
	t.Helper()
	s, eng := setup(t)
	pub(t, s, "a.md", "# log\nE203 original content")
	b := &build.Builder{Store: s}
	rec := research.Recipe{Version: "xrag.recipe/v1", Name: "resume-test", Steps: []research.RecipeStep{{Action: "search.keyword", Arguments: core.Request{Q: "$input"}}, {Action: "files.read", Arguments: core.Request{Ref: "$last.ref"}}}}
	task := research.Task{KB: "demo", Goal: "E203", MaxSteps: 3, MaxSeconds: 120}
	q := build.InvestigationRequest{Task: task, Recipe: &rec}
	j, e := s.Enqueue(ctx, "demo", "recipe", "tester", "stable-idempotency", q)
	must(t, e)
	j, ok, e := s.Claim(ctx)
	must(t, e)
	if !ok {
		t.Fatal("no task")
	}
	fp, e := b.InvestigationFingerprint(ctx, "demo")
	must(t, e)
	planner, e := research.RecipePlanner(rec)
	must(t, e)
	runner := research.Runner{DeterministicPlanner: true, Engine: eng, Planner: planner, Owner: "tester", Save: func(c context.Context, cp *research.Checkpoint) error {
		if e := s.SaveTaskCheckpoint(c, j, fp, cp); e != nil {
			return e
		}
		if cp.Phase == phase && cp.NextStep == step {
			return interrupted9
		}
		return nil
	}}
	result, e := runner.Run(ctx, task)
	if !errors.Is(e, interrupted9) {
		t.Fatalf("expected interruption at %s/%d, got %v", phase, step, e)
	}
	must(t, s.Finish(ctx, j, result, e))
	cp, got, e := s.TaskCheckpoint(ctx, j.ID)
	must(t, e)
	if cp == nil || got != fp {
		t.Fatal("missing checkpoint")
	}
	return s, b, j, cp, fp
}
func TestDemo9ResumeRecipeUsesSavedScopeAndSameTask(t *testing.T) {
	s, b, j, cp, fp := savedTask9(t, "ready", 1)
	oldScope := cp.Result.Scope
	// A normal update must not replace the saved scope or first query's ref.
	pub(t, s, "a.md", "# log\nE203 NEW content must not replace historical evidence")
	meter := &store.Meter{Store: s, KB: "demo", Run: j.ID, Owner: j.Owner}
	call, e := meter.Reserve(ctx, "chat", 90, 10)
	must(t, e)
	must(t, meter.Complete(ctx, call, map[string]any{"prompt_tokens": 90, "completion_tokens": 10, "total_tokens": 100}, true))
	resumed, e := s.ResumeInvestigation(ctx, j.ID, fp)
	must(t, e)
	again, e := s.ResumeInvestigation(ctx, j.ID, fp)
	must(t, e)
	if resumed.ID != j.ID || again.ID != j.ID || resumed.Started != j.Started || resumed.Deadline != j.Deadline {
		t.Fatal(resumed, j)
	}
	next, ok, e := s.Claim(ctx)
	must(t, e)
	if !ok || next.ID != j.ID || next.Attempt != 2 {
		t.Fatal(next)
	}
	b.Execute(ctx, next)
	done, e := s.Job(ctx, j.ID)
	must(t, e)
	if done.State != "succeeded" {
		t.Fatal(done.Error, string(done.Result))
	}
	var result research.Result
	must(t, json.Unmarshal(done.Result, &result))
	if result.Scope != oldScope || len(result.Observations) != 2 || !strings.Contains(core.JSON(result.Evidence), "original content") || strings.Contains(core.JSON(result.Evidence), "NEW content") {
		t.Fatal(core.JSON(result))
	}
	rows := rows6(t, s, "SELECT charged_tokens,state FROM budget_call WHERE id=?", call)
	if len(rows) != 1 || rows[0]["charged_tokens"] != int64(100) {
		t.Fatal(rows)
	}
}
func TestDemo9CheckpointAfterPlanningDoesNotRepeatPlanner(t *testing.T) {
	s, b, j, cp, fp := savedTask9(t, "planned", 0)
	eng, e := b.EngineFor(ctx, []string{"demo"})
	must(t, e)
	calls := 0
	runner := research.Runner{Engine: eng, Owner: j.Owner, Initial: cp, Planner: research.PlannerFunc(func(c context.Context, st research.State) (research.Decision, error) {
		calls++
		return readPlanner8(c, st)
	})}
	result, e := runner.Run(ctx, cp.Task)
	must(t, e)
	if calls != 2 || len(result.Observations) != 2 {
		t.Fatal(calls, result)
	}
	_, e = s.ResumeInvestigation(ctx, j.ID, fp)
	must(t, e)
}
func TestDemo9CheckpointAfterReadOnlyDispatchCanReplay(t *testing.T) {
	_, b, j, cp, _ := savedTask9(t, "executing", 1)
	eng, e := b.EngineFor(ctx, []string{"demo"})
	must(t, e)
	result, e := (research.Runner{Engine: eng, Owner: j.Owner, Initial: cp, Planner: research.PlannerFunc(readPlanner8)}).Run(ctx, cp.Task)
	must(t, e)
	if len(result.Observations) != 2 || len(result.Findings) != 1 {
		t.Fatal(result)
	}
}
func TestDemo9CompleteCheckpointNeedsNoPlannerOrToolCalls(t *testing.T) {
	s, b, j, _, fp := savedTask9(t, "complete", 3)
	_, e := s.ResumeInvestigation(ctx, j.ID, fp)
	must(t, e)
	next, ok, e := s.Claim(ctx)
	must(t, e)
	if !ok {
		t.Fatal("no resume")
	}
	b.Execute(ctx, next)
	done, e := s.Job(ctx, j.ID)
	must(t, e)
	if done.State != "succeeded" || !strings.Contains(string(done.Result), "recipe_completed") {
		t.Fatal(done)
	}
}
func TestDemo9ResumeRejectsUnknownPaidPlanning(t *testing.T) {
	s, _, j, cp, fp := savedTask9(t, "planning", 0)
	cp.DeterministicPlanner = false
	exec6(t, s, "UPDATE research_checkpoint SET data=? WHERE job_id=?", core.JSON(cp), j.ID)
	_, e := s.ResumeInvestigation(ctx, j.ID, fp)
	code(t, e, "RESUME_OUTCOME_UNKNOWN")
}
func TestDemo9ResumeRejectsPotentialPaidToolReplay(t *testing.T) {
	s, _, j, cp, fp := savedTask9(t, "executing", 0)
	for _, action := range []string{"search.semantic", "+ask", "graph.path", "search.external"} {
		cp.Pending.Action = action
		exec6(t, s, "UPDATE research_checkpoint SET data=? WHERE job_id=?", core.JSON(cp), j.ID)
		_, e := s.ResumeInvestigation(ctx, j.ID, fp)
		code(t, e, "RESUME_OUTCOME_UNKNOWN")
	}
}
func TestDemo9ResumeRequiresOriginalConfiguration(t *testing.T) {
	s, _, j, _, _ := savedTask9(t, "ready", 1)
	_, e := s.ResumeInvestigation(ctx, j.ID, "another-config")
	code(t, e, "TASK_CONFIGURATION_CHANGED")
}
func TestDemo9ResumeRejectsWithdrawnSources(t *testing.T) {
	s, _, j, _, fp := savedTask9(t, "ready", 1)
	must(t, s.Withdraw(ctx, "demo", "a.md"))
	_, e := s.ResumeInvestigation(ctx, j.ID, fp)
	code(t, e, "VIEW_INVALIDATED")
}
func TestDemo9ResumeCannotResetExpiredExecutionDeadline(t *testing.T) {
	s, _, j, _, fp := savedTask9(t, "ready", 1)
	exec6(t, s, "UPDATE job SET deadline=? WHERE id=?", time.Now().Unix()-1, j.ID)
	_, e := s.ResumeInvestigation(ctx, j.ID, fp)
	code(t, e, "TASK_DEADLINE_EXCEEDED")
}
func TestDemo9ResumeRejectsUserCancellation(t *testing.T) {
	s, _, j, _, fp := savedTask9(t, "ready", 1)
	exec6(t, s, "UPDATE job SET state='cancelled',cancel=1 WHERE id=?", j.ID)
	_, e := s.ResumeInvestigation(ctx, j.ID, fp)
	code(t, e, "TASK_NOT_RESUMABLE")
}
func TestDemo9ResumeRejectsExpiredScope(t *testing.T) {
	s, _, j, cp, fp := savedTask9(t, "ready", 1)
	exec6(t, s, "UPDATE cache SET expires=? WHERE id=?", time.Now().Unix()-1, cp.Result.Scope)
	_, e := s.ResumeInvestigation(ctx, j.ID, fp)
	code(t, e, "RESULT_EXPIRED")
}
func TestDemo9ResumeCannotTakeAnActiveLease(t *testing.T) {
	s, _, j, _, fp := savedTask9(t, "ready", 1)
	exec6(t, s, "UPDATE job SET state='running',lease=? WHERE id=?", time.Now().Unix()+100, j.ID)
	_, e := s.ResumeInvestigation(ctx, j.ID, fp)
	code(t, e, "TASK_NOT_RESUMABLE")
}
func TestDemo9ResumeExpiredLeaseFencesOldWorker(t *testing.T) {
	s, _, j, cp, fp := savedTask9(t, "ready", 1)
	exec6(t, s, "UPDATE job SET state='running',lease=? WHERE id=?", time.Now().Unix()-1, j.ID)
	_, e := s.ResumeInvestigation(ctx, j.ID, fp)
	must(t, e)
	next, ok, e := s.Claim(ctx)
	must(t, e)
	if !ok {
		t.Fatal("not claimed")
	}
	e = s.SaveTaskCheckpoint(ctx, j, fp, cp)
	code(t, e, "JOB_FENCE_LOST")
	if next.Token == j.Token {
		t.Fatal("same lease")
	}
}
func TestDemo9ResumeHasNoCheckpointCannotInventState(t *testing.T) {
	s, _ := setup(t)
	j, e := s.Enqueue(ctx, "demo", "recipe", "tester", "", map[string]any{"max_seconds": 120})
	must(t, e)
	j, _, e = s.Claim(ctx)
	must(t, e)
	must(t, s.Finish(ctx, j, nil, interrupted9))
	_, e = s.ResumeInvestigation(ctx, j.ID, "f")
	code(t, e, "CHECKPOINT_MISSING")
}
func TestDemo9CheckpointRetentionOutlastsTaskDeadline(t *testing.T) {
	s, _, j, cp, _ := savedTask9(t, "ready", 1)
	c, e := s.GetCache(ctx, cp.Result.Scope)
	must(t, e)
	if c.Expires < j.Deadline+300 {
		t.Fatal(c.Expires, j.Deadline)
	}
}
func TestDemo9CheckpointRejectsStaleRevision(t *testing.T) {
	s, _, j, cp, fp := savedTask9(t, "ready", 1)
	_, e := s.ResumeInvestigation(ctx, j.ID, fp)
	must(t, e)
	next, _, e := s.Claim(ctx)
	must(t, e)
	cp.Revision--
	e = s.SaveTaskCheckpoint(ctx, next, fp, cp)
	code(t, e, "CHECKPOINT_CONFLICT")
}
func TestDemo9ResumeAPIDoesNotTakeAnotherCallersTask(t *testing.T) {
	h := productSetup(t)
	_, token, e := h.s.CreateManagedKey(ctx, "reader", "reader", []string{"demo"}, 0)
	must(t, e)
	j, e := h.s.Enqueue(ctx, "demo", "recipe", "other-owner", "", map[string]any{})
	must(t, e)
	status, body := h.request(t, "POST", "/v1/tasks/resume", map[string]any{"id": j.ID}, token)
	if status != 403 {
		t.Fatal(status, string(body))
	}
}
func TestDemo9ResumePlannerCannotChangeTask(t *testing.T) {
	_, b, j, cp, _ := savedTask9(t, "ready", 1)
	eng, e := b.EngineFor(ctx, []string{"demo"})
	must(t, e)
	task := cp.Task
	task.Goal = "another"
	_, e = (research.Runner{Engine: eng, Planner: research.PlannerFunc(readPlanner8), Owner: j.Owner, Initial: cp}).Run(ctx, task)
	code(t, e, "CHECKPOINT_INVALID")
}

func TestDemo9ResumeCheckpointMetadataCannotUnhideWithdrawnResult(t *testing.T) {
	s, b, j, _, fp := savedTask9(t, "ready", 1)
	must(t, s.Withdraw(ctx, "demo", "a.md"))
	app := server.New(s, b, nil)
	result, _, e := app.ExecuteTask(ctx, store.Principal{ID: j.Owner, Role: "reader", KBs: []string{"demo"}}, "task.status", []byte(core.JSON(map[string]any{"id": j.ID})), "")
	must(t, e)
	got := result.(store.Job)
	if strings.Contains(string(got.Result), "original content") || strings.Contains(string(got.Result), "observations") || !strings.Contains(string(got.Result), "VIEW_INVALIDATED") {
		t.Fatal(string(got.Result))
	}
	_, e = s.ResumeInvestigation(ctx, j.ID, fp)
	code(t, e, "VIEW_INVALIDATED")
}
