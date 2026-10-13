package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"github.com/BlueSkyXN/XRAG/adapters"
	"github.com/BlueSkyXN/XRAG/core"
	"github.com/BlueSkyXN/XRAG/internal/build"
	"github.com/BlueSkyXN/XRAG/internal/cli"
	"github.com/BlueSkyXN/XRAG/internal/server"
	"github.com/BlueSkyXN/XRAG/protocols/mcp"
	"github.com/BlueSkyXN/XRAG/research"
)

func readPlanner8(_ context.Context, s research.State) (research.Decision, error) {
	switch len(s.Observations) {
	case 0:
		return research.Decision{Action: "search.keyword", Arguments: core.Request{Q: "E203"}}, nil
	case 1:
		if len(s.Observations[0].Result.Items) == 0 {
			return research.Decision{Finish: true, Unresolved: []string{"未找到相关资料"}}, nil
		}
		return research.Decision{Action: "files.read", Arguments: core.Request{Ref: s.Observations[0].Result.Items[0].Ref}}, nil
	default:
		for _, e := range s.Evidence {
			if e.Kind != "aggregate" {
				return research.Decision{Finish: true, Findings: []research.Finding{{Claim: "日志资料说明了 E203 的处理条件。", EvidenceIDs: []string{e.ID}}}}, nil
			}
		}
	}
	return research.Decision{}, fmt.Errorf("missing doc evidence")
}
func TestDemo8ResearchActuallySearchesReadsAndCites(t *testing.T) {
	s, e := setup(t)
	pub(t, s, "a.md", "# 日志\nE203 重启日志服务前先保存现场。")
	r := research.Runner{Engine: e, Owner: "tester", Planner: research.PlannerFunc(readPlanner8)}
	res, err := r.Run(ctx, research.Task{KB: "demo", Goal: "E203处理"})
	must(t, err)
	if res.State != "succeeded" || len(res.Observations) != 2 || len(res.Findings) != 1 || len(res.Findings[0].EvidenceRefs) != 1 {
		t.Fatal(res)
	}
}
func TestDemo8ResearchExactCountHasTypedEvidence(t *testing.T) {
	s, e := setup(t)
	for i := 0; i < 37; i++ {
		pub(t, s, fmt.Sprintf("%d.md", i), "# 日志\nCOUNT203")
	}
	p := research.PlannerFunc(func(_ context.Context, st research.State) (research.Decision, error) {
		if len(st.Observations) == 0 {
			return research.Decision{Action: "search.keyword", Arguments: core.Request{Q: "COUNT203"}}, nil
		}
		return research.Decision{Finish: true, Findings: []research.Finding{{Claim: "本范围匹配 {n}。", EvidenceIDs: []string{"e1"}, Quantities: []research.Quantity{{Slot: "n", EvidenceID: "e1", Metric: "total"}}}}}, nil
	})
	res, err := (research.Runner{Engine: e, Planner: p, Owner: "tester"}).Run(ctx, research.Task{KB: "demo", Goal: "完整匹配数量"})
	must(t, err)
	if !strings.Contains(res.Findings[0].Claim, "37 chunks") || len(res.Observations[0].Result.Items) != 5 || *res.Evidence[0].Metric.Value != 37 {
		t.Fatal(res)
	}
}
func TestDemo8ResearchUnknownEvidenceFails(t *testing.T) {
	s, e := setup(t)
	pub(t, s, "a.md", "# one\nhello")
	p := research.PlannerFunc(func(context.Context, research.State) (research.Decision, error) {
		return research.Decision{Finish: true, Findings: []research.Finding{{Claim: "invented", EvidenceIDs: []string{"e88"}}}}, nil
	})
	_, err := (research.Runner{Engine: e, Planner: p}).Run(ctx, research.Task{KB: "demo", Goal: "test"})
	code(t, err, "EVIDENCE_UNKNOWN")
}
func TestDemo8ResearchRejectsRecursiveAndWriteActions(t *testing.T) {
	s, e := setup(t)
	pub(t, s, "a.md", "# one\nhello")
	for _, action := range []string{"research.submit", "scope.expand", "knowledge.submit", "shell"} {
		p := research.PlannerFunc(func(context.Context, research.State) (research.Decision, error) {
			return research.Decision{Action: action}, nil
		})
		_, err := (research.Runner{Engine: e, Planner: p}).Run(ctx, research.Task{KB: "demo", Goal: "test"})
		code(t, err, "ACTION_NOT_ALLOWED")
	}
}
func TestDemo8ResearchCannotReplaceScopeOrReadForeignRef(t *testing.T) {
	s, e := setup(t)
	d := pub(t, s, "other/a.md", "# private\nE203")
	pub(t, s, "selected/a.md", "# selected\nE203")
	bad := core.ChunkItem(d.Chunks[0], s.Origin).Ref
	p := research.PlannerFunc(func(context.Context, research.State) (research.Decision, error) {
		return research.Decision{Action: "files.read", Arguments: core.Request{Ref: bad}}, nil
	})
	_, err := (research.Runner{Engine: e, Planner: p}).Run(ctx, research.Task{KB: "demo", Goal: "test", Path: "selected"})
	code(t, err, "EVIDENCE_UNKNOWN")
}
func TestDemo8ResearchFixedScopeExcludesOtherDirectory(t *testing.T) {
	s, e := setup(t)
	pub(t, s, "selected/a.md", "# selected\nE203 yes")
	pub(t, s, "other/a.md", "# other\nE203 do not include")
	p := research.PlannerFunc(func(_ context.Context, st research.State) (research.Decision, error) {
		if len(st.Observations) > 0 {
			if *st.Observations[0].Result.Total.Value != 1 {
				t.Fatal(st)
			}
			for _, it := range st.Observations[0].Result.Items {
				if !strings.HasPrefix(it.Path, "selected/") {
					t.Fatal(it)
				}
			}
		}
		return readPlanner8(ctx, st)
	})
	_, err := (research.Runner{Engine: e, Planner: p, Owner: "tester"}).Run(ctx, research.Task{KB: "demo", Goal: "E203", Path: "selected"})
	must(t, err)
}
func TestDemo8ResearchStepBudgetStopsWithoutFakeFindings(t *testing.T) {
	s, e := setup(t)
	pub(t, s, "a.md", "# log\nE203")
	res, err := (research.Runner{Engine: e, Planner: research.PlannerFunc(readPlanner8), Owner: "tester"}).Run(ctx, research.Task{KB: "demo", Goal: "E203", MaxSteps: 1})
	must(t, err)
	if res.StopReason != "step_budget" || len(res.Findings) != 0 || res.State != "partial" {
		t.Fatal(res)
	}
}
func TestDemo8ResearchWithdrawDuringPlanningDropsEvidence(t *testing.T) {
	s, e := setup(t)
	pub(t, s, "a.md", "# log\nE203")
	p := research.PlannerFunc(func(c context.Context, st research.State) (research.Decision, error) {
		if len(st.Observations) == 1 {
			must(t, s.Withdraw(c, "demo", "a.md"))
		}
		return readPlanner8(c, st)
	})
	res, err := (research.Runner{Engine: e, Planner: p, Owner: "tester"}).Run(ctx, research.Task{KB: "demo", Goal: "E203"})
	code(t, err, "VIEW_INVALIDATED")
	if len(res.Evidence) != 0 || len(res.Observations) != 0 {
		t.Fatal(res)
	}
}
func TestDemo8QuantitiesRejectIncompleteAndFabricatedMetric(t *testing.T) {
	m := core.Count(37, "chunks", "imported scope")
	m.CalculationComplete = false
	ev := []research.Evidence{{ID: "e1", Ref: "r", Kind: "aggregate", Metric: &m}}
	f := []research.Finding{{Claim: "{n}", EvidenceIDs: []string{"e1"}, Quantities: []research.Quantity{{Slot: "n", EvidenceID: "e1", Metric: "total"}}}}
	_, err := research.RenderFindings(f, ev)
	code(t, err, "QUANTITY_UNSUPPORTED")
	m.CalculationComplete = true
	f[0].Quantities[0].Metric = "customers"
	_, err = research.RenderFindings(f, ev)
	code(t, err, "QUANTITY_UNSUPPORTED")
}
func TestDemo8RecipeExecutesRealRetrievalWithoutChat(t *testing.T) {
	s, e := setup(t)
	pub(t, s, "a.md", "# log\nE203 actual source")
	rec := research.Recipe{Version: "xrag.recipe/v1", Name: "lookup", Steps: []research.RecipeStep{{Action: "search.keyword", Arguments: core.Request{Q: "$input"}}, {Action: "files.read", Arguments: core.Request{Ref: "$last.ref"}}}}
	p, err := research.RecipePlanner(rec)
	must(t, err)
	res, err := (research.Runner{Engine: e, Planner: p, Owner: "tester"}).Run(ctx, research.Task{KB: "demo", Goal: "E203"})
	must(t, err)
	if len(res.Observations) != 2 || !strings.Contains(core.JSON(res), "actual source") {
		t.Fatal(res)
	}
}
func TestDemo8RecipeYAMLIsRealParserRejectsAliases(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "recipe.yaml")
	must(t, os.WriteFile(file, []byte("version: xrag.recipe/v1\nname: test\nsteps:\n  - action: search.keyword\n    arguments: {q: '$input'}\n"), 0600))
	r, err := cli.LoadRecipe(ctx, file)
	must(t, err)
	if len(r.Steps) != 1 {
		t.Fatal(r)
	}
	must(t, os.WriteFile(file, []byte("version: xrag.recipe/v1\nname: test\nsteps: &s []\nextra: *s\n"), 0600))
	_, err = cli.LoadRecipe(ctx, file)
	code(t, err, "RECIPE_FORMAT_INVALID")
}

// A strict scripted HTTP Chat provider drives the actual job/Engine. It tests
// integration, not semantic intelligence or any commercial model.
func taskBackend8(t *testing.T) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		json.NewDecoder(r.Body).Decode(&b)
		var st research.State
		if len(b.Messages) != 2 {
			t.Error("messages")
			return
		}
		json.Unmarshal([]byte(b.Messages[1].Content), &st)
		d, err := readPlanner8(ctx, st)
		if err != nil {
			t.Error(err)
		}
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": core.JSON(d)}}}, "usage": map[string]any{"prompt_tokens": 12, "completion_tokens": 5, "total_tokens": 17}})
	}))
	t.Cleanup(ts.Close)
	return ts
}
func TestDemo8ResearchRealJobRetainsEvidenceAndFixedDeadline(t *testing.T) {
	s, _ := setup(t)
	pub(t, s, "a.md", "# log\nE203 source condition")
	ts := taskBackend8(t)
	b := build.Builder{Store: s, Config: adapters.Config{Chat: &adapters.Endpoint{URL: ts.URL, Model: "scripted-fixture"}}}
	q := build.InvestigationRequest{Task: research.Task{KB: "demo", Goal: "E203", MaxSteps: 8, MaxSeconds: 120}}
	j, err := s.Enqueue(ctx, "demo", "research", "tester", "one", q)
	must(t, err)
	again, err := s.Enqueue(ctx, "demo", "research", "tester", "one", q)
	must(t, err)
	if j.ID != again.ID {
		t.Fatal("duplicate")
	}
	j, ok, err := s.Claim(ctx)
	must(t, err)
	if !ok || j.Deadline-j.Started != 120 {
		t.Fatal(j)
	}
	b.Execute(ctx, j)
	end, err := s.Job(ctx, j.ID)
	must(t, err)
	if end.State != "succeeded" {
		t.Fatal(end, string(end.Result))
	}
	var result research.Result
	must(t, json.Unmarshal(end.Result, &result))
	if result.ReferencesExpireAt < time.Now().Add(6*24*time.Hour).Unix() {
		t.Fatal(result)
	}
	for _, e := range result.Evidence {
		r := decode(t, e.Ref)
		if r.ResultID != "" {
			c, err := s.GetCache(ctx, r.ResultID)
			must(t, err)
			if c.Expires < result.ReferencesExpireAt {
				t.Fatal(c)
			}
		}
	}
	calls := rows6(t, s, "SELECT count(*) n FROM budget_call WHERE run=?", j.ID)
	if len(calls) != 1 || calls[0]["n"] != int64(3) {
		t.Fatal(calls)
	}
}
func TestDemo8TaskAPIRefusesModelMissingAndRecipeStillWorks(t *testing.T) {
	h := productSetup(t)
	pub(t, h.s, "a.md", "# log\nE203")
	status, data := h.request(t, "POST", "/v1/research", map[string]any{"kb": "demo", "goal": "E203"}, h.key)
	if status != 422 || !strings.Contains(string(data), "CAPABILITY_UNAVAILABLE") {
		t.Fatal(status, string(data))
	}
	r := research.Recipe{Version: "xrag.recipe/v1", Name: "x", Steps: []research.RecipeStep{{Action: "search.keyword", Arguments: core.Request{Q: "$input"}}}}
	status, data = h.request(t, "POST", "/v1/recipes/run", build.InvestigationRequest{Task: research.Task{KB: "demo", Goal: "E203"}, Recipe: &r}, h.key)
	if status != 202 {
		t.Fatal(status, string(data))
	}
	j, ok, err := h.s.Claim(ctx)
	must(t, err)
	if !ok {
		t.Fatal("no job")
	}
	h.app.Builder.Execute(ctx, j)
	status, data = h.request(t, "GET", "/v1/jobs/"+j.ID, nil, h.key)
	if status != 200 || !strings.Contains(string(data), "recipe_completed") {
		t.Fatal(status, string(data))
	}
	must(t, h.s.Withdraw(ctx, "demo", "a.md"))
	status, data = h.request(t, "GET", "/v1/jobs/"+j.ID, nil, h.key)
	if status != 200 || !strings.Contains(string(data), "VIEW_INVALIDATED") {
		t.Fatal(status, string(data))
	}
}
func TestDemo8MCPTasksOptInAndActualStatus(t *testing.T) {
	m := mcp.Server{AllowTasks: true, Toolsets: "core"}
	for _, tool := range m.Tools() {
		if tool["name"] == "research_submit" {
			t.Fatal("not opt in")
		}
	}
	m.Toolsets = "research"
	if len(m.Tools()) != 5 {
		t.Fatal(m.Tools())
	}
	h := productSetup(t)
	m.Execute = func(c context.Context, a string, b []byte) (any, error) {
		v, _, e := h.app.ExecuteTask(c, admin6(), a, b, "")
		return v, e
	}
	res := m.Handle(ctx, []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"research_submit","arguments":{"kb":"demo","goal":"test"}}}`))
	if !strings.Contains(core.JSON(res), "CAPABILITY_UNAVAILABLE") {
		t.Fatal(res)
	}
}
func TestDemo8TaskReadCannotExposeAnotherOwnersJob(t *testing.T) {
	h := productSetup(t)
	_, token, err := h.s.CreateManagedKey(ctx, "another", "reader", []string{"demo"}, 0)
	must(t, err)
	j, err := h.s.Enqueue(ctx, "demo", "recipe", "owner-not-reader", "", map[string]any{})
	must(t, err)
	code, b := h.request(t, "POST", "/v1/tasks/status", map[string]any{"id": j.ID}, token)
	if code != 403 {
		t.Fatal(code, string(b))
	}
}
func TestDemo8TaskProgressReadableOnlyUntilWithdrawal(t *testing.T) {
	s, _ := setup(t)
	pub(t, s, "a.md", "# doc\nE203")
	key, err := s.CreateKey(ctx, "admin", "admin", nil)
	must(t, err)
	app := server.New(s, &build.Builder{Store: s}, nil)
	ts := httptest.NewServer(app)
	defer ts.Close()
	h := productTest{s, app, ts.URL, key}
	j, err := s.Enqueue(ctx, "demo", "recipe", "x", "", map[string]any{})
	must(t, err)
	j, _, err = s.Claim(ctx)
	must(t, err)
	ep, err := s.Epoch(ctx)
	must(t, err)
	must(t, s.TaskProgress(ctx, j, ep, research.Observation{Step: 1, Action: "files.read", Result: core.Response{Data: "marker-secret-content"}}))
	must(t, s.Withdraw(ctx, "demo", "a.md"))
	status, b := h.request(t, "GET", "/v1/jobs/"+j.ID, nil, key)
	if status != 200 || strings.Contains(string(b), "marker-secret-content") {
		t.Fatal(status, string(b))
	}
}
