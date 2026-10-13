package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"github.com/BlueSkyXN/XRAG/adapters"
	"github.com/BlueSkyXN/XRAG/core"
	"github.com/BlueSkyXN/XRAG/internal/build"
	db "github.com/BlueSkyXN/XRAG/internal/sqlite"
	"github.com/BlueSkyXN/XRAG/internal/store"
)

type queryOnly7 struct{ *store.Store }

func (q queryOnly7) Snapshot(context.Context, []string) (core.View, error) {
	return core.View{}, fmt.Errorf("full Snapshot must not run")
}
func compactEngine7(s *store.Store) *core.Engine {
	return &core.Engine{Repo: queryOnly7{s}, Mode: "server"}
}
func vectorDoc7(t *testing.T, s *store.Store, ep adapters.Endpoint, path, body string, vec []float64) core.Document {
	t.Helper()
	d := parsed(t, path, body)
	for i := range d.Chunks {
		d.Chunks[i].Generation = ep.SemanticID()
		d.Chunks[i].Vector = vec
		d.Chunks[i].VectorState = "ready"
	}
	must(t, s.SaveGeneration(ctx, "demo", adapters.Config{Embedding: &ep}))
	must(t, s.Stage(ctx, d))
	must(t, s.Publish(ctx, d, store.Job{}, "", ""))
	return d
}
func TestV7CompactProjectionOmitsCorpusTextAndVectors(t *testing.T) {
	s, _ := setup(t)
	d := parsed(t, "a.md", "# T\nBODY_DO_NOT_LOAD [[A]] --uses--> [[B]]")
	for i := range d.Chunks {
		d.Chunks[i].Vector = []float64{.5, .6}
	}
	must(t, s.Stage(ctx, d))
	must(t, s.Publish(ctx, d, store.Job{}, "", ""))
	v, e := s.QueryView(ctx, []string{"demo"}, nil, false)
	must(t, e)
	if !v.Compact || v.Documents[0].Body != "" || v.Documents[0].Chunks[0].Text != "" || len(v.Documents[0].Chunks[0].Vector) != 0 {
		t.Fatal(core.JSON(v))
	}
	if len(core.Mentions(v.Documents[0].Chunks[0])) != 2 {
		t.Fatal(v)
	}
}
func TestV7CompactGraphHydratesOnlyRequestedPage(t *testing.T) {
	s, _ := setup(t)
	pub(t, s, "a.md", "# A\n[[Seed]] --uses--> [[X]]\nFIRST_BODY")
	pub(t, s, "b.md", "# B\n[[Seed]] --uses--> [[Y]]\nSECOND_BODY")
	e := compactEngine7(s)
	r := run(t, e, "graph.chunks", core.Request{Name: "Seed", Limit: 1, SaveScope: "required"})
	if r.Strategy["execution"] != "metadata_projection_then_page_hydration" || r.Items[0].Text == "" {
		t.Fatal(r)
	}
	c, err := s.GetCache(ctx, r.ResultID)
	must(t, err)
	// Graph supports legitimately keep their own excerpts, but unrelated body
	// paragraphs do not belong in the saved candidate window.
	if strings.Contains(string(c.Data), "SECOND_BODY") || strings.Contains(string(c.Data), "FIRST_BODY") {
		t.Fatal("saved result copied source bodies")
	}
	next := run(t, e, "results.page", core.Request{ResultID: r.ResultID, Cursor: r.NextCursor, Limit: 1})
	if next.Returned != 1 || next.Items[0].Text == "" || next.Items[0].Ref == r.Items[0].Ref {
		t.Fatal(next)
	}
}
func TestV7CompactGraphSavedRangeKeepsSupportFilter(t *testing.T) {
	s, _ := setup(t)
	pub(t, s, "f1.md", "[[A]] and [[B]]")
	pub(t, s, "f2.md", "[[A]] --uses--> [[B]]")
	e := compactEngine7(s)
	h := run(t, e, "files.tree", core.Request{Filters: []core.Filter{{Field: "path", Op: "eq", Value: "f1.md"}}, SaveScope: "required"})
	r := run(t, e, "graph.path", core.Request{FromID: "A", ToID: "B", Within: h.Handle})
	if r.Returned != 0 {
		t.Fatal(r)
	}
}
func TestV7CompactPageKeepsRetiredEdition(t *testing.T) {
	s, _ := setup(t)
	pub(t, s, "a.md", "# A\n[[Seed]] and oldtext\n## B\n[[Seed]] and oldlater")
	e := compactEngine7(s)
	r := run(t, e, "graph.chunks", core.Request{Name: "Seed", Limit: 1})
	pub(t, s, "a.md", "# New\n[[Seed]] and newtext")
	p := run(t, e, "results.page", core.Request{ResultID: r.ResultID, Cursor: r.NextCursor, Limit: 1})
	if !strings.Contains(p.Items[0].Text, "oldlater") {
		t.Fatal(p)
	}
}
func TestV7CompactScopeWithdrawalStillInvalidates(t *testing.T) {
	s, _ := setup(t)
	pub(t, s, "a.md", "[[A]] --uses--> [[B]]")
	e := compactEngine7(s)
	r := run(t, e, "graph.chunks", core.Request{Name: "A", SaveScope: "required"})
	must(t, s.Withdraw(ctx, "demo", "a.md"))
	_, err := e.Run(ctx, "graph.path", core.Request{Kbs: []string{"demo"}, Within: r.Handle, FromID: "A", ToID: "B"}, "tester")
	code(t, err, "VIEW_INVALIDATED")
}
func TestV7CompactQueryRejectsForgedReferenceIdentity(t *testing.T) {
	s, _ := setup(t)
	d := pub(t, s, "a.md", "hello")
	_, e := s.QueryView(ctx, []string{"demo"}, []core.Ref{{Origin: s.Origin, KB: "demo", Path: "other.md", Edition: d.Edition, FileHash: d.FileHash}}, false)
	code(t, e, "REF_GONE")
}
func TestV7StreamingSemanticRanksAndHydrates(t *testing.T) {
	s, _ := setup(t)
	ts, calls := embed5(t)
	ep := adapters.Endpoint{URL: ts.URL, Model: "m1", Dimension: 2, Revision: "r1"}
	vectorDoc7(t, s, ep, "a.md", "# A\nVECTOR_BEST", []float64{1, 0})
	vectorDoc7(t, s, ep, "b.md", "# B\nVECTOR_WORST", []float64{0, 1})
	b := &build.Builder{Store: s, Config: adapters.Config{Embedding: &ep}}
	e, err := b.EngineFor(ctx, []string{"demo"})
	must(t, err)
	e.Repo = queryOnly7{s}
	r := run(t, e, "search.semantic", core.Request{Q: "hello", Limit: 1, Trace: true})
	if calls.Load() != 1 || r.Items[0].Path != "a.md" || !strings.Contains(r.Items[0].Text, "VECTOR_BEST") || r.Coverage.Approximate != true {
		t.Fatal(r, calls.Load())
	}
	cc, err := s.GetCache(ctx, r.ResultID)
	must(t, err)
	if strings.Contains(string(cc.Data), "VECTOR_WORST") {
		t.Fatal("stored corpus text")
	}
}
func TestV7StreamingSemanticHonorsCandidateScope(t *testing.T) {
	s, _ := setup(t)
	ts, _ := embed5(t)
	ep := adapters.Endpoint{URL: ts.URL, Model: "m1", Dimension: 2}
	vectorDoc7(t, s, ep, "a.md", "# A\nSCOPE_FIRST", []float64{1, 0})
	vectorDoc7(t, s, ep, "b.md", "# B\nSCOPE_SECOND", []float64{0, 1})
	b := &build.Builder{Store: s, Config: adapters.Config{Embedding: &ep}}
	e, er := b.EngineFor(ctx, []string{"demo"})
	must(t, er)
	e.Repo = queryOnly7{s}
	h := run(t, e, "search.keyword", core.Request{Q: "SCOPE_SECOND", SaveScope: "required"})
	r := run(t, e, "search.semantic", core.Request{Q: "hello", Within: h.Handle})
	if r.Returned != 1 || r.Items[0].Path != "b.md" {
		t.Fatal(r)
	}
}
func TestV7CompactHybridHydratesBeforeRerank(t *testing.T) {
	s, _ := setup(t)
	pub(t, s, "a.md", "# A\nRANK_NEEDLE_A")
	pub(t, s, "b.md", "# B\nRANK_NEEDLE_B")
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var q struct {
			Documents []string `json:"documents"`
		}
		must(t, json.NewDecoder(r.Body).Decode(&q))
		if len(q.Documents) != 2 || q.Documents[0] == "" || q.Documents[1] == "" {
			t.Error("reranker received empty candidate text", q)
		}
		json.NewEncoder(w).Encode(map[string]any{"results": []any{map[string]any{"index": 1, "relevance_score": .9}, map[string]any{"index": 0, "relevance_score": .1}}})
	}))
	defer ts.Close()
	on := true
	b := &build.Builder{Store: s, Config: adapters.Config{Rerank: &adapters.Endpoint{URL: ts.URL, Model: "rerank"}}}
	e, err := b.EngineFor(ctx, []string{"demo"})
	must(t, err)
	e.Repo = queryOnly7{s}
	r := run(t, e, "+ask", core.Request{Q: "RANK", Rerank: &on})
	if r.Returned != 2 || r.Items[0].Text == "" {
		t.Fatal(r)
	}
}
func TestV7LegacySemanticReceivesFullView(t *testing.T) {
	s, _ := setup(t)
	ts, _ := embed5(t)
	ep := adapters.Endpoint{URL: ts.URL, Model: "m1", Dimension: 2}
	vectorDoc7(t, s, ep, "a.md", "present", []float64{1, 0})
	e := &core.Engine{Repo: s, Mode: "server", Semantic: &adapters.ExactSemantic{Embed: &adapters.Embedder{Config: ep}}}
	r := run(t, e, "search.semantic", core.Request{Q: "hello"})
	if r.Returned != 1 || r.Items[0].Text == "" {
		t.Fatal(r)
	}
}
func TestV7StreamingTopKDeterministicTie(t *testing.T) {
	w := core.RankWindow{Limit: 3}
	for _, id := range []string{"z", "b", "a", "c", "d"} {
		w.Add(core.Ranked{ID: id, Score: 1})
	}
	out := w.Sorted()
	if len(out) != 3 || out[0].ID != "a" || out[2].ID != "c" {
		t.Fatal(out)
	}
}
func TestV7VectorStreamDoesNotReadEditionBody(t *testing.T) {
	s, _ := setup(t)
	ts, _ := embed5(t)
	ep := adapters.Endpoint{URL: ts.URL, Model: "m1", Dimension: 2}
	d := vectorDoc7(t, s, ep, "a.md", "body", []float64{1, 0})
	v, e := s.QueryView(ctx, []string{"demo"}, nil, false)
	must(t, e)
	// Corrupt only the irrelevant canonical edition JSON after the metadata
	// read: streamed vector computation must not decode that document body.
	must(t, s.DB.Do(ctx, func(c *db.Conn) error { return c.Exec("DROP TRIGGER edition_catalog_update") }))
	must(t, s.DB.Do(ctx, func(c *db.Conn) error { return c.Exec("UPDATE edition SET data='not-json' WHERE id=?", d.Edition) }))
	rs, e := s.VectorTop(ctx, v, []float64{1, 0}, 200)
	must(t, e)
	if len(rs) != 1 {
		t.Fatal(rs)
	}
}
func TestV7CompactContextRetainsWikiSubstringMatch(t *testing.T) {
	s, _ := setup(t)
	pub(t, s, "a.md", "[[A]] --uses--> [[B]]")
	d := parsed(t, "w.md", "# knowledge\nA appears here without bracket markup")
	d.Kind = "wiki"
	must(t, s.Stage(ctx, d))
	must(t, s.Publish(ctx, d, store.Job{}, "", ""))
	r := run(t, compactEngine7(s), "+context", core.Request{Name: "A"})
	found := false
	for _, it := range r.Items {
		if it.Kind == "wiki" && it.Text != "" {
			found = true
		}
	}
	if !found {
		t.Fatal(r)
	}
}
func TestV7RawGraphBudgetCoversWholeQuery(t *testing.T) {
	ss := make([]core.Support, 20001)
	_, _, _, e := core.GraphQuery("graph.entity", core.Request{}, core.View{}, map[string]core.Chunk{}, ss)
	code(t, e, "GRAPH_BUDGET_EXCEEDED")
}
func TestV7MetadataCapacityIndependentOfResultWindow(t *testing.T) {
	s, _ := setup(t)
	for i := 0; i < 12; i++ {
		pub(t, s, fmt.Sprintf("d%d.md", i), fmt.Sprintf("[[A%d]] --uses--> [[B%d]]", i, i))
	}
	e := compactEngine7(s)
	e.MaxItems = 3
	r := run(t, e, "graph.neighbors", core.Request{Name: "A1"})
	if r.Returned != 1 {
		t.Fatal(r)
	}
}

type supportReader7 struct{ Items []core.Support }

func (g supportReader7) Supports(context.Context, core.View) ([]core.Support, error) {
	return g.Items, nil
}
func TestV7HybridGraphSupplementAlsoLimitsRawSupports(t *testing.T) {
	s, _ := setup(t)
	d := pub(t, s, "a.md", "[[A]] --uses--> [[B]]")
	ss := make([]core.Support, 20001)
	for i := range ss {
		ss[i] = core.Support{ID: fmt.Sprint(i), KB: "demo", ChunkID: d.Chunks[0].ID, From: "A", To: "B", Relation: "uses"}
	}
	e := compactEngine7(s)
	e.Graph = supportReader7{Items: ss}
	truth := true
	_, err := e.Run(ctx, "+ask", core.Request{Kbs: []string{"demo"}, Q: "A", Graph: &truth}, "tester")
	code(t, err, "GRAPH_BUDGET_EXCEEDED")
}
