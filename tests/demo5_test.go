package tests

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"github.com/BlueSkyXN/XRAG/adapters"
	"github.com/BlueSkyXN/XRAG/contracts"
	"github.com/BlueSkyXN/XRAG/core"
	"github.com/BlueSkyXN/XRAG/internal/build"
	"github.com/BlueSkyXN/XRAG/internal/cli"
	"github.com/BlueSkyXN/XRAG/internal/server"
	db "github.com/BlueSkyXN/XRAG/internal/sqlite"
	"github.com/BlueSkyXN/XRAG/internal/store"
	"github.com/BlueSkyXN/XRAG/protocols/mcp"
)

func setopts(t *testing.T, s *store.Store, k store.KBSettings) store.KBSettings {
	t.Helper()
	old, e := s.KBSettings(ctx, "demo")
	must(t, e)
	k, e = s.SetKBSettings(ctx, "demo", k, old.Revision)
	must(t, e)
	return k
}
func call5(t *testing.T, app http.Handler, key, method, path string, value any) (int, map[string]any) {
	t.Helper()
	var body string
	if value != nil {
		body = core.JSON(value)
	}
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+key)
	w := httptest.NewRecorder()
	app.ServeHTTP(w, r)
	v := map[string]any{}
	_ = json.Unmarshal(w.Body.Bytes(), &v)
	return w.Code, v
}
func admin5(t *testing.T, s *store.Store) string {
	t.Helper()
	k, e := s.CreateKey(ctx, "admin", "admin", nil)
	must(t, e)
	return k
}
func embed5(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	n := &atomic.Int32{}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			Input []string `json:"input"`
			Model string   `json:"model"`
		}
		must(t, json.NewDecoder(r.Body).Decode(&b))
		n.Add(1)
		data := []any{}
		for i := range b.Input {
			vec := []float64{1, 0}
			if b.Model == "m2" {
				vec = []float64{0, 1}
			}
			data = append(data, map[string]any{"index": i, "embedding": vec})
		}
		json.NewEncoder(w).Encode(map[string]any{"data": data, "usage": map[string]any{"prompt_tokens": 10, "total_tokens": 10}})
	}))
	t.Cleanup(ts.Close)
	return ts, n
}
func TestV5ExpansionReappliesOutputFilters(t *testing.T) {
	s, e := setup(t)
	pub(t, s, "A.md", "[[A]] 和 [[B]]。")
	pub(t, s, "B.md", "[[A]] --uses--> [[B]]")
	seed := run(t, e, "search.keyword", core.Request{Q: "A", Filters: []core.Filter{{Field: "path", Op: "eq", Value: "A.md"}}, SaveScope: "required"})
	out := run(t, e, "scope.expand", core.Request{Within: seed.Handle, Filters: []core.Filter{{Field: "path", Op: "eq", Value: "A.md"}}})
	for _, it := range out.Items {
		if it.Path != "A.md" {
			t.Fatalf("escaped filters: %s", core.JSON(out))
		}
	}
}
func TestV5GraphCountIsRelationsNotSupports(t *testing.T) {
	s, e := setup(t)
	pub(t, s, "A.md", "[[A]] --uses--> [[B]]")
	pub(t, s, "B.md", "[[A]] --uses--> [[B]]")
	r := run(t, e, "graph.neighbors", core.Request{Name: "A"})
	if r.Total.Unit != "relations" || *r.Total.Value != 1 || r.Items[0].Supports != 2 {
		t.Fatal(core.JSON(r))
	}
}
func TestV5GrepReadsAcrossChunkBoundary(t *testing.T) {
	s, e := setup(t)
	body := strings.Repeat("x", 1190) + "EDGE_LEFT\nEDGE_RIGHT"
	pub(t, s, "A.md", body)
	r := run(t, e, "files.grep", core.Request{Pattern: "EDGE_LEFT\\nEDGE_RIGHT"})
	if r.Total.Unit != "matches" || *r.Total.Value != 1 {
		t.Fatal(core.JSON(r))
	}
	read := run(t, e, "files.read", core.Request{Ref: r.Items[0].Ref})
	if !strings.Contains(core.JSON(read), "EDGE_LEFT") {
		t.Fatal(read)
	}
}
func TestV5GrepNeverJoinsDisconnectedScope(t *testing.T) {
	s, e := setup(t)
	pub(t, s, "A.md", "# A\nhello\n# B\nnot selected\n# C\nworld\n")
	r := run(t, e, "search.keyword", core.Request{Q: "hello", SaveScope: "required"})
	g := run(t, e, "files.grep", core.Request{Within: r.Handle, Pattern: "world"})
	if *g.Total.Value != 0 {
		t.Fatal(g)
	}
}
func TestV5KeywordPerFileKeepsExactTotal(t *testing.T) {
	s, e := setup(t)
	pub(t, s, "A.md", strings.Repeat("日志\n", 1500))
	r := run(t, e, "search.keyword", core.Request{Q: "日志", PerFile: 1})
	if r.Returned != 1 || *r.Total.Value <= 1 {
		t.Fatal(r)
	}
}
func TestV5DiscoverDoesNotReadFullContent(t *testing.T) {
	s, e := setup(t)
	pub(t, s, "A.md", "unique-body-marker")
	r := run(t, e, "kb.discover", core.Request{})
	if strings.Contains(core.JSON(r), "unique-body-marker") {
		t.Fatal("discover exposed body")
	}
}
func TestV5KBSettingsCASAndChunkFields(t *testing.T) {
	s, _ := setup(t)
	k := setopts(t, s, store.KBSettings{ChunkMax: 200, Fields: map[string]string{"active": "boolean"}})
	_, e := s.SetKBSettings(ctx, "demo", k, 0)
	code(t, e, "VERSION_CONFLICT")
	b, e := (&build.Builder{Store: s}).ForKB(ctx, "demo")
	must(t, e)
	d := parsed(t, "A.md", "---\nactive: true\n---\n正文")
	must(t, b.Prepare(ctx, &d, store.Job{}))
	if d.Fields["active"] != true {
		t.Fatal(d.Fields)
	}
}
func TestV5KBRemovalReservesSlugAndWithdraws(t *testing.T) {
	s, _ := setup(t)
	d := pub(t, s, "A.md", "正文")
	must(t, s.DeleteKB(ctx, "demo"))
	_, e := s.KBSettings(ctx, "demo")
	code(t, e, "NOT_FOUND")
	_, _, _, e = s.Read(ctx, core.Ref{Origin: s.Origin, KB: "demo", Path: "A.md", Edition: d.Edition, ChunkID: d.Chunks[0].ID})
	code(t, e, "REF_WITHDRAWN")
	if e = s.CreateKB(ctx, "demo", "different"); e == nil {
		t.Fatal("deleted authority reused")
	}
}
func TestV5ManagementHTTPCRUD(t *testing.T) {
	s, _ := setup(t)
	a := server.New(s, &build.Builder{Store: s}, nil)
	key := admin5(t, s)
	status, _ := call5(t, a, key, "PATCH", "/v1/kbs/demo", map[string]any{"name": "新名称"})
	if status != 200 {
		t.Fatal(status)
	}
	status, v := call5(t, a, key, "PUT", "/v1/kbs/demo/settings", map[string]any{"expected_revision": 0, "settings": map[string]any{"chunk_max_chars": 250}})
	if status != 200 {
		t.Fatal(status, v)
	}
	status, v = call5(t, a, key, "DELETE", "/v1/kbs/demo", map[string]any{"confirm": true})
	if status != 200 {
		t.Fatal(status, v)
	}
}
func TestV5DisableAndDeleteSource(t *testing.T) {
	s, _ := setup(t)
	root := t.TempDir()
	must(t, os.WriteFile(filepath.Join(root, "a.md"), []byte("正文"), 0600))
	must(t, s.AddSource(ctx, store.Source{KB: "demo", ID: "a", Kind: "path", Root: root, Mount: "a"}))
	b := &build.Builder{Store: s}
	j := claim(t, s, "build", map[string]any{"mode": "incremental"})
	b.Execute(ctx, j)
	d, ok, e := s.Current(ctx, "demo", "a/a.md")
	must(t, e)
	if !ok {
		t.Fatal("missing")
	}
	must(t, s.ChangeSource(ctx, "demo", "a", nil, store.SourceOptions{Disabled: true}, 0, false))
	src, e := s.Source(ctx, "demo", "a")
	must(t, e)
	if !src.Options.Disabled {
		t.Fatal(src)
	}
	must(t, s.ChangeSource(ctx, "demo", "a", nil, store.SourceOptions{}, 1, true))
	_, _, _, e = s.Read(ctx, core.Ref{Origin: s.Origin, KB: "demo", Path: d.Path, Edition: d.Edition, ChunkID: d.Chunks[0].ID, FileHash: d.FileHash})
	must(t, e)
}
func TestV5SourcePatternsOnlyPublishSelectedFiles(t *testing.T) {
	s, _ := setup(t)
	root := t.TempDir()
	for _, f := range []string{"a.md", "b.md"} {
		must(t, os.WriteFile(filepath.Join(root, f), []byte(f), 0600))
	}
	must(t, s.AddSource(ctx, store.Source{KB: "demo", ID: "a", Kind: "path", Root: root, Mount: "a"}))
	must(t, s.ChangeSource(ctx, "demo", "a", nil, store.SourceOptions{Include: []string{"a.md"}}, 0, false))
	b := &build.Builder{Store: s}
	j := claim(t, s, "build", map[string]any{})
	b.Execute(ctx, j)
	v, e := s.Snapshot(ctx, []string{"demo"})
	must(t, e)
	if len(v.Documents) != 1 || v.Documents[0].Path != "a/a.md" {
		t.Fatal(v)
	}
}
func TestV5ProjectionBuildDoesNotReadSources(t *testing.T) {
	s, _ := setup(t)
	pub(t, s, "A.md", "[[A]] --uses--> [[B]]")
	b := &build.Builder{Store: s}
	j := claim(t, s, "build", map[string]any{"mode": "graph_only"})
	b.Execute(ctx, j)
	job, e := s.Job(ctx, j.ID)
	must(t, e)
	if job.State != "succeeded" {
		t.Fatal(job.Error)
	}
	r := run(t, b.Engine(), "graph.path", core.Request{FromID: "A", ToID: "B"})
	if r.Returned == 0 {
		t.Fatal(r)
	}
}
func TestV5VectorGenerationMigrationKeepsOldModelUntilPublish(t *testing.T) {
	s, _ := setup(t)
	pub(t, s, "A.md", "日志")
	ts, n := embed5(t)
	setopts(t, s, store.KBSettings{Providers: &adapters.Config{Embedding: &adapters.Endpoint{URL: ts.URL, Model: "m1", Dimension: 2}}})
	b := &build.Builder{Store: s}
	j := claim(t, s, "build", map[string]any{"mode": "vectors_only"})
	b.Execute(ctx, j)
	old, e := s.Snapshot(ctx, []string{"demo"})
	must(t, e)
	oldGen := old.Documents[0].Chunks[0].Generation
	if oldGen == "" {
		t.Fatal("no vector")
	}
	setopts(t, s, store.KBSettings{Providers: &adapters.Config{Embedding: &adapters.Endpoint{URL: ts.URL, Model: "m2", Dimension: 2}}})
	r := run(t, b.Engine(), "search.semantic", core.Request{Q: "日志"})
	if r.Returned == 0 {
		t.Fatal(r)
	}
	j = claim(t, s, "build", map[string]any{"mode": "vectors_only"})
	b.Execute(ctx, j)
	v, e := s.Snapshot(ctx, []string{"demo"})
	must(t, e)
	if v.Documents[0].Chunks[0].Generation == oldGen || n.Load() < 3 {
		t.Fatal(v, n.Load())
	}
	oldr, e := core.DecodeRef(core.ChunkItem(old.Documents[0].Chunks[0], s.Origin).Ref)
	must(t, e)
	_, _, _, e = s.Read(ctx, oldr)
	must(t, e)
}
func TestV5ProjectionFailureLeavesAllOldEditions(t *testing.T) {
	s, _ := setup(t)
	pub(t, s, "A.md", "A")
	pub(t, s, "B.md", "B")
	before, e := s.Snapshot(ctx, []string{"demo"})
	must(t, e)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) }))
	defer ts.Close()
	setopts(t, s, store.KBSettings{Providers: &adapters.Config{Embedding: &adapters.Endpoint{URL: ts.URL, Model: "bad", Dimension: 2}}})
	b := &build.Builder{Store: s}
	j := claim(t, s, "build", map[string]any{"mode": "vectors_only"})
	b.Execute(ctx, j)
	after, e := s.Snapshot(ctx, []string{"demo"})
	must(t, e)
	if core.JSON(before.Documents) != core.JSON(after.Documents) {
		t.Fatal("partial model migration became visible")
	}
}
func TestV5GraphOnlyKeepsExistingVectors(t *testing.T) {
	s, _ := setup(t)
	pub(t, s, "A.md", "[[A]] --uses--> [[B]]")
	ts, n := embed5(t)
	setopts(t, s, store.KBSettings{Providers: &adapters.Config{Embedding: &adapters.Endpoint{URL: ts.URL, Model: "m1", Dimension: 2}}})
	b := &build.Builder{Store: s}
	j := claim(t, s, "build", map[string]any{"mode": "vectors_only"})
	b.Execute(ctx, j)
	calls := n.Load()
	j = claim(t, s, "build", map[string]any{"mode": "graph_only"})
	b.Execute(ctx, j)
	v, e := s.Snapshot(ctx, []string{"demo"})
	must(t, e)
	if n.Load() != calls || len(v.Documents[0].Chunks[0].Vector) != 2 {
		t.Fatal("graph rebuild re-embedded or lost vector")
	}
}
func TestV5LLMExtractorPreservesEvidenceAndCaches(t *testing.T) {
	s, _ := setup(t)
	var n atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		text := `{"entities":[{"name":"甲","type":"设备"},{"name":"乙","type":"组件"}],"relations":[{"from":"甲","to":"乙","rel":"使用","evidence":"甲使用乙。"}]}`
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": text}}}, "usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 20}})
	}))
	defer ts.Close()
	setopts(t, s, store.KBSettings{Graph: adapters.GraphOptions{Mode: "llm", Model: &adapters.Endpoint{URL: ts.URL, Model: "extract"}}})
	b, e := (&build.Builder{Store: s}).ForKB(ctx, "demo")
	must(t, e)
	d := parsed(t, "A.md", "甲使用乙。")
	must(t, b.Prepare(ctx, &d, store.Job{}))
	if len(d.Supports) != 1 || d.Supports[0].Origin != "llm" {
		t.Fatal(d.Supports)
	}
	d2 := parsed(t, "A.md", "甲使用乙。")
	must(t, b.Prepare(ctx, &d2, store.Job{}))
	if n.Load() != 1 || len(d2.Supports) != 1 || d.Supports[0].ID == d2.Supports[0].ID {
		t.Fatal("cache/mapping incorrect")
	}
}
func TestV5LLMEmptyEvidenceRejected(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		text := `{"entities":[{"name":"A","type":"x"},{"name":"B","type":"x"}],"relations":[{"from":"A","to":"B","rel":"uses","evidence":""}]}`
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": text}}}})
	}))
	defer ts.Close()
	d := parsed(t, "A.md", "A uses B")
	ss, _, e := adapters.ExtractLLM(ctx, adapters.Endpoint{URL: ts.URL, Model: "m"}, adapters.GraphOptions{}, d.Chunks[0])
	must(t, e)
	if len(ss) != 0 {
		t.Fatal(ss)
	}
}
func TestV5AggregateEvidenceSurvivesResultExpiry(t *testing.T) {
	s, e := setup(t)
	pub(t, s, "A.md", "日志")
	r := run(t, e, "search.keyword", core.Request{Q: "日志"})
	if r.SummaryRef == "" {
		t.Fatal("no aggregate ref")
	}
	k, err := s.SaveKnowledge(store.WithActor(ctx, "tester"), store.Knowledge{KB: "demo", Title: "统计", Body: "已导入匹配块为1", Refs: []string{r.SummaryRef}})
	must(t, err)
	must(t, s.DB.Do(ctx, func(c *db.Conn) error { return c.Exec("DELETE FROM cache WHERE id=?", r.ResultID) }))
	rr := run(t, e, "files.read", core.Request{Ref: k.Refs[0]})
	if !strings.Contains(core.JSON(rr), "aggregate") {
		t.Fatal(rr)
	}
}
func TestV5PathEvidenceCanPublishAndWithdraw(t *testing.T) {
	s, e := setup(t)
	pub(t, s, "A.md", "[[A]] --uses--> [[B]]")
	r := run(t, e, "graph.path", core.Request{FromID: "A", ToID: "B"})
	k, err := s.SaveKnowledge(store.WithActor(ctx, "tester"), store.Knowledge{KB: "demo", Title: "路径", Body: "A与B有资料支持的路径。", Refs: []string{r.Items[0].Ref}})
	must(t, err)
	b := &build.Builder{Store: s}
	j := claim(t, s, "publish", map[string]any{"id": k.ID, "revision": k.Draft})
	b.Execute(ctx, j)
	job, err := s.Job(ctx, j.ID)
	must(t, err)
	if job.State != "succeeded" {
		t.Fatal(job.Error)
	}
	must(t, s.Withdraw(ctx, "demo", "A.md"))
	_, err = e.Run(ctx, "files.read", core.Request{Kbs: []string{"demo"}, Ref: r.Items[0].Ref}, "tester")
	code(t, err, "REF_WITHDRAWN")
}
func TestV5EvidenceRejectsAnotherCallersCache(t *testing.T) {
	s, e := setup(t)
	pub(t, s, "A.md", "日志")
	r := run(t, e, "search.keyword", core.Request{Q: "日志"})
	_, err := s.SaveKnowledge(store.WithActor(ctx, "another"), store.Knowledge{KB: "demo", Title: "计数", Body: "1", Refs: []string{r.SummaryRef}})
	code(t, err, "PERMISSION_DENIED")
}
func TestV5BudgetEnforcesBeforeSendAndPersists(t *testing.T) {
	s, _ := setup(t)
	m := &store.Meter{Store: s, KB: "demo", Run: "r", Policy: store.BudgetPolicy{MaxCalls: 1}}
	id, e := m.Reserve(ctx, "chat", 20, 20)
	must(t, e)
	_, e = m.Reserve(ctx, "chat", 1, 1)
	code(t, e, "BUDGET_EXCEEDED")
	must(t, m.Complete(ctx, id, map[string]any{"prompt_tokens": 10, "completion_tokens": 3}, true))
	report, e := s.BudgetReport(ctx, "demo")
	must(t, e)
	if !strings.Contains(core.JSON(report), "completed") {
		t.Fatal(report)
	}
}
func TestV5ConcurrentBudgetReservationsRespectLimit(t *testing.T) {
	s, _ := setup(t)
	m := &store.Meter{Store: s, KB: "demo", Run: "r", Policy: store.BudgetPolicy{MaxCalls: 1}}
	var ok atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := m.Reserve(ctx, "chat", 1, 1)
			if e == nil {
				ok.Add(1)
			}
		}()
	}
	wg.Wait()
	if ok.Load() != 1 {
		t.Fatal(ok.Load())
	}
}
func TestV5BudgetUnknownDoesNotReleaseReservation(t *testing.T) {
	s, _ := setup(t)
	m := &store.Meter{Store: s, KB: "demo", Run: "r", Policy: store.BudgetPolicy{MaxTokens: 30}}
	id, e := m.Reserve(ctx, "chat", 10, 20)
	must(t, e)
	must(t, m.Complete(ctx, id, nil, false))
	_, e = m.Reserve(ctx, "chat", 1, 1)
	code(t, e, "BUDGET_EXCEEDED")
}
func TestV5CostLimitRequiresRate(t *testing.T) {
	s, _ := setup(t)
	m := &store.Meter{Store: s, KB: "demo", Run: "r", Policy: store.BudgetPolicy{MaxCost: 10}}
	_, e := m.Reserve(ctx, "chat", 1, 1)
	code(t, e, "BUDGET_PRICE_MISSING")
}
func TestV5MCPWriteIsOptInAndDoesNotPublish(t *testing.T) {
	s, _ := setup(t)
	d := pub(t, s, "A.md", "日志")
	key, e := s.CreateKey(ctx, "contrib", "contributor", []string{"demo"})
	must(t, e)
	p, e := s.Authenticate(ctx, key)
	must(t, e)
	app := server.New(s, &build.Builder{Store: s}, nil)
	m := mcp.Server{AllowWrite: true, Toolsets: "core", Execute: func(c context.Context, a string, b []byte) (any, error) { return app.Execute(c, p, a, b) }}
	for _, tool := range m.Tools() {
		if tool["name"] == "knowledge_submit" {
			t.Fatal("write enabled by default")
		}
	}
	m.Toolsets = "write"
	args := map[string]any{"kb": "demo", "title": "测试", "body_md": "未发布草稿", "refs": []string{core.ChunkItem(d.Chunks[0], s.Origin).Ref}}
	out := m.Handle(ctx, []byte(core.JSON(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": "knowledge_submit", "arguments": args}}))).(map[string]any)["result"].(map[string]any)
	if out["isError"] != false {
		t.Fatal(out)
	}
	if strings.Contains(core.JSON(out), `"published_revision":"kr_`) {
		t.Fatal("published via MCP")
	}
}
func TestV5MCPReaderCannotWriteEvenIfToolFlagWrong(t *testing.T) {
	s, _ := setup(t)
	key, e := s.CreateKey(ctx, "r", "reader", []string{"demo"})
	must(t, e)
	p, e := s.Authenticate(ctx, key)
	must(t, e)
	a := server.New(s, &build.Builder{Store: s}, nil)
	_, _, e = a.ExecuteWrite(ctx, p, "knowledge.submit", []byte(`{}`))
	code(t, e, "PERMISSION_DENIED")
}
func TestV5WebAndMCPDisableAreIndependent(t *testing.T) {
	s, _ := setup(t)
	a := server.New(s, &build.Builder{Store: s}, nil)
	a.DisableWeb = true
	a.DisableMCP = true
	key := admin5(t, s)
	for _, u := range []string{"/", "/mcp"} {
		status, _ := call5(t, a, key, "GET", u, nil)
		if status != 404 {
			t.Fatal(u, status)
		}
	}
	status, _ := call5(t, a, key, "GET", "/v1/kbs", nil)
	if status != 200 {
		t.Fatal(status)
	}
}
func TestV5ExternalSearchAndReadAcrossRealHTTPApps(t *testing.T) {
	up, _ := setup(t)
	pub(t, up, "up.md", "外部文档日志")
	key, e := up.CreateKey(ctx, "peer", "reader", []string{"demo"})
	must(t, e)
	t.Setenv("XRAG_TEST_PEER5", key)
	ts := httptest.NewServer(server.New(up, &build.Builder{Store: up}, nil))
	defer ts.Close()
	s, _ := setup(t)
	setopts(t, s, store.KBSettings{Providers: &adapters.Config{External: []adapters.ExternalConfig{{ID: "peer", URL: ts.URL, Key: "env:XRAG_TEST_PEER5", Kbs: []string{"demo"}}}}})
	b := &build.Builder{Store: s}
	r := run(t, b.Engine(), "search.external", core.Request{Q: "日志"})
	if r.Returned != 1 || r.Items[0].External == nil {
		t.Fatal(r)
	}
	read := run(t, b.Engine(), "files.read", core.Request{Ref: r.Items[0].Ref})
	if !strings.Contains(core.JSON(read), "外部文档") {
		t.Fatal(read)
	}
}
func TestV5ExternalReferenceRejectsBindingRetarget(t *testing.T) {
	p := adapters.ExternalConfig{ID: "x", URL: "http://localhost:1", Kbs: []string{"a"}}
	ref := core.ExternalReference{Provider: p.ID, Revision: p.Revision(), UpstreamRef: "ref"}
	p.URL = "http://localhost:2"
	_, e := (&adapters.External{Configs: []adapters.ExternalConfig{p}}).ReadExternal(ctx, ref)
	code(t, e, "REF_ORIGIN_MISMATCH")
}
func TestV5DirectNeoOnlyExplicitNoText(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		must(t, json.NewDecoder(r.Body).Decode(&req))
		statement := req["statement"].(string)
		rows := []any{}
		if strings.Contains(statement, "RETURN {chunk_id:") {
			rows = []any{[]any{map[string]any{"kb": "demo", "path": "a.md", "edition_id": "ed1", "chunk_id": "c1"}}}
		} else if strings.Contains(statement, "RETURN e.entity_id") {
			rows = []any{[]any{core.EntityID("demo", "A")}}
		} else if strings.Contains(statement, "support_id:s.support_id") {
			rows = []any{[]any{map[string]any{"support_id": "s1", "kb": "demo", "path": "a.md", "edition_id": "ed1", "chunk_id": "c1", "from": "A", "to": "B", "relation": "uses"}}}
		}
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"values": rows}})
	}))
	defer ts.Close()
	e, err := cli.LocalEngine(cli.Profile{Type: "direct", KBs: []string{"demo"}, Origin: "fixture-origin", Neo4j: &adapters.NeoConfig{URL: ts.URL, Database: "neo4j", User: "u"}}, t.TempDir())
	must(t, err)
	r := run(t, e, "graph.path", core.Request{FromID: "A", ToID: "B"})
	if r.Returned != 1 {
		t.Fatal(core.JSON(r))
	}
	_, err = e.Run(ctx, "files.read", core.Request{Kbs: []string{"demo"}, Ref: r.Items[0].Refs[0]}, "tester")
	code(t, err, "TEXT_UNAVAILABLE")
}
func TestV5DirectMultiKBCanInstantiateWithoutNetwork(t *testing.T) {
	e, err := cli.LocalEngine(cli.Profile{Type: "direct", KBs: []string{"a", "b"}, Bindings: []cli.DirectBinding{{KB: "a", Origin: "x", Neo4j: &adapters.NeoConfig{URL: "http://localhost:1", Database: "neo4j"}}, {KB: "b", Origin: "x", Neo4j: &adapters.NeoConfig{URL: "http://localhost:1", Database: "neo4j"}}}}, t.TempDir())
	must(t, err)
	if e.Graph == nil {
		t.Fatal("no graph")
	}
}
func TestV5NewContractsExposeExternalAndWrite(t *testing.T) {
	if _, ok := contracts.Find("search.external"); !ok {
		t.Fatal("missing external")
	}
	for _, a := range contracts.WriteActions {
		if contracts.Input(a) == nil || contracts.WriteOutput(a) == nil {
			t.Fatal(a)
		}
	}
}
func TestV5AbsoluteBudgetRecordsDoNotExpireWithResultCache(t *testing.T) {
	s, _ := setup(t)
	m := &store.Meter{Store: s, KB: "demo", Run: "r"}
	id, e := m.Reserve(ctx, "embedding", 10, 0)
	must(t, e)
	must(t, m.Complete(ctx, id, map[string]any{"prompt_tokens": 10}, true))
	must(t, s.DB.Do(ctx, func(c *db.Conn) error { return c.Exec("DELETE FROM cache WHERE expires<?", time.Now().Unix()+1) }))
	r, e := s.BudgetReport(ctx, "demo")
	must(t, e)
	if !strings.Contains(core.JSON(r), id) {
		t.Fatal("billing purged with result cache")
	}
}

var _ = io.Discard

func TestV5EmptyWorkspaceNavigationAndCreation(t *testing.T) {
	h := productSetup(t)
	n, b := h.request(t, "DELETE", "/v1/kbs/demo", map[string]any{"confirm": true}, h.key)
	if n != 200 {
		t.Fatal(n, string(b))
	}
	for _, path := range []string{"/v1/system", "/v1/overview", "/v1/kbs"} {
		n, b = h.request(t, "GET", path, nil, h.key)
		if n != 200 {
			t.Fatal(path, n, string(b))
		}
	}
	n, b = h.request(t, "POST", "/v1/search/keyword", map[string]any{"q": "no database"}, h.key)
	if n < 400 {
		t.Fatal("empty scope accepted", string(b))
	}
	n, b = h.request(t, "POST", "/v1/kbs", map[string]any{"id": "new", "name": "新知识库"}, h.key)
	if n != 201 {
		t.Fatal(n, string(b))
	}
}
func TestV5MCPUsesPerKBModelConfiguration(t *testing.T) {
	h := productSetup(t)
	_, err := h.s.SetKBSettings(ctx, "demo", store.KBSettings{Providers: &adapters.Config{Embedding: &adapters.Endpoint{URL: "http://127.0.0.1:1", Model: "not-called", Dimension: 2}}}, 0)
	must(t, err)
	n, b := h.request(t, "POST", "/mcp", map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list"}, h.key)
	if n != 200 || !strings.Contains(string(b), `"search_semantic"`) {
		t.Fatal(n, string(b))
	}
}
func TestV5MaintainerCanEditNonProviderSettingsAfterAdminConfig(t *testing.T) {
	h := productSetup(t)
	k, err := h.s.SetKBSettings(ctx, "demo", store.KBSettings{Providers: &adapters.Config{Chat: &adapters.Endpoint{URL: "http://127.0.0.1:1", Model: "not-called"}}}, 0)
	must(t, err)
	_, key, err := h.s.CreateManagedKey(ctx, "maintenance", "maintainer", []string{"demo"}, 0)
	must(t, err)
	k.ChunkMax = 1000
	n, b := h.request(t, "PUT", "/v1/kbs/demo/settings", map[string]any{"expected_revision": k.Revision, "settings": k}, key)
	if n != 200 {
		t.Fatal(n, string(b))
	}
	k.Providers = nil
	k.Revision++
	n, b = h.request(t, "PUT", "/v1/kbs/demo/settings", map[string]any{"expected_revision": k.Revision, "settings": k}, key)
	if n != 403 {
		t.Fatal(n, string(b))
	}
}
func TestV5PluginMetadataDoesNotStartCommands(t *testing.T) {
	h := productSetup(t)
	h.app.Builder.Config.Plugins = map[string]adapters.PluginSpec{"parser-a": {Kind: "parser", Version: "v1", Protocol: "xrag.parser/v1", Command: []string{"does-not-exist"}}, "parser-b": {Kind: "parser", Version: "v2", Protocol: "xrag.parser/v1", Command: []string{"other-not-exist"}}}
	n, b := h.request(t, "GET", "/v1/system", nil, h.key)
	if n != 200 || strings.Contains(string(b), "does-not-exist") || !strings.Contains(string(b), "parser-a") {
		t.Fatal(n, string(b))
	}
	_, err := h.s.SetKBSettings(ctx, "demo", store.KBSettings{ParserPlugin: "parser-b"}, 0)
	must(t, err)
	bb, err := h.app.Builder.ForKB(ctx, "demo")
	must(t, err)
	if bb.Config.ParserCommand[0] != "other-not-exist" || bb.Config.ParserRevision != "v2" {
		t.Fatal(bb.Config)
	}
}
