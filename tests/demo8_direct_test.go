package tests

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"github.com/BlueSkyXN/XRAG/adapters"
	"github.com/BlueSkyXN/XRAG/core"
)

// queryBackend8 is an explicitly controlled protocol fixture, not Qdrant.
type queryBackend8 struct {
	mu         sync.Mutex
	points     map[string]map[string]any
	desc       map[string]any
	collection string
	requests   []map[string]any
	fullReads  int
	bad        bool
	repeat     bool
}

func (f *queryBackend8) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	body := map[string]any{}
	if r.Method == "POST" {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	record := map[string]any{"path": r.URL.Path, "method": r.Method, "body": body}
	f.requests = append(f.requests, record)
	if strings.Contains(r.URL.Path, "xrag_demo_meta/points/") {
		json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"payload": f.desc}})
		return
	}
	if strings.HasSuffix(r.URL.Path, "/embeddings") {
		io.WriteString(w, `{"data":[{"index":0,"embedding":[1,0]}],"usage":{"prompt_tokens":1,"total_tokens":1}}`)
		return
	}
	ids := []string{}
	if strings.HasSuffix(r.URL.Path, "/points") {
		for _, id := range body["ids"].([]any) {
			ids = append(ids, id.(string))
		}
		if body["with_payload"] == true {
			f.fullReads += len(ids)
		}
	} else {
		for id := range f.points {
			ids = append(ids, id)
		}
		sort.Strings(ids)
	}
	filter, _ := body["filter"].(map[string]any)
	matched := []map[string]any{}
	for _, id := range ids {
		p, ok := f.points[id]
		if !ok {
			continue
		}
		matches := true
		clauses, _ := filter["must"].([]any)
		for _, v := range clauses {
			cond := v.(map[string]any)
			key, _ := cond["key"].(string)
			mt, _ := cond["match"].(map[string]any)
			actual := p[key]
			if strings.HasPrefix(key, "fields.") {
				actual = p["fields"].(map[string]any)[strings.TrimPrefix(key, "fields.")]
			}
			if value, ok := mt["value"]; ok && fmt.Sprint(actual) != fmt.Sprint(value) {
				matches = false
			}
			if values, ok := mt["any"].([]any); ok {
				one := false
				for _, value := range values {
					one = one || fmt.Sprint(actual) == fmt.Sprint(value)
				}
				matches = matches && one
			}
			if text, ok := mt["text"].(string); ok && !strings.Contains(fmt.Sprint(p["text"]), text) {
				matches = false
			}
		}
		if !matches {
			continue
		}
		payload := p
		if fields, ok := body["with_payload"].([]any); ok {
			payload = map[string]any{}
			for _, k := range fields {
				if value, yes := p[k.(string)]; yes {
					payload[k.(string)] = value
				}
			}
		}
		matched = append(matched, map[string]any{"id": id, "payload": payload, "score": float64(len(ids) - len(matched))})
	}
	if f.bad && len(matched) > 0 {
		copy := map[string]any{}
		for k, v := range matched[0]["payload"].(map[string]any) {
			copy[k] = v
		}
		copy["origin_id"] = "alien"
		matched[0]["payload"] = copy
	}
	if strings.HasSuffix(r.URL.Path, "/points") {
		json.NewEncoder(w).Encode(map[string]any{"result": matched})
		return
	}
	var next any
	if n, ok := body["limit"].(float64); ok && len(matched) > int(n) {
		next = matched[int(n)-1]["id"]
		matched = matched[:int(n)]
	}
	if f.repeat {
		next = "repeat"
	}
	if strings.HasSuffix(r.URL.Path, "/points/query") {
		json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"points": matched}})
		return
	}
	json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"points": matched, "next_page_offset": next}})
}
func direct8(t *testing.T, n int) (*core.Engine, *adapters.Direct, *queryBackend8) {
	t.Helper()
	f := &queryBackend8{points: map[string]map[string]any{}}
	ts := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(ts.Close)
	ep := adapters.Endpoint{URL: ts.URL, Model: "fixture-model", Dimension: 2, Revision: "fixed-v1"}
	q := &adapters.Qdrant{Endpoint: adapters.Endpoint{URL: ts.URL}, Embed: &adapters.Embedder{Config: ep}, Origin: "origin8"}
	f.collection = q.Collection("demo")
	f.desc = map[string]any{"physical_collection": f.collection, "model_identity": ep.SemanticID(), "origin_id": "origin8", "kb": "demo", "payload_schema": "demo8"}
	for i := 0; i < n; i++ {
		d := parsed(t, fmt.Sprintf("file-%03d.md", i), fmt.Sprintf("# 文档\nneedle 检索 %03d", i))
		c := d.Chunks[0]
		c.Generation = ep.SemanticID()
		c.MentionNames = []string{}
		p := map[string]any{}
		must(t, json.Unmarshal([]byte(core.JSON(c)), &p))
		p["origin_id"] = "origin8"
		p["physical_collection"] = f.collection
		f.points[c.ID] = p
	}
	d := &adapters.Direct{Q: q, Collection: f.collection, KB: "demo", Origin: "origin8", CacheDir: t.TempDir()}
	return &core.Engine{Repo: d, Mode: "direct", Semantic: d}, d, f
}
func TestDemo8DirectSemanticNoCorpusScrollAndPageHydration(t *testing.T) {
	e, _, f := direct8(t, 37)
	r := run(t, e, "search.semantic", core.Request{Q: "needle", Limit: 10, SaveScope: "none"})
	if r.Returned != 10 || r.Total.Value != nil || !r.Coverage.Approximate {
		t.Fatal(r)
	}
	if f.fullReads != 10 {
		t.Fatalf("full reads %d", f.fullReads)
	}
	for _, q := range f.requests {
		if strings.HasSuffix(q["path"].(string), "/scroll") {
			t.Fatal("semantic scanned corpus")
		}
	}
	r = run(t, e, "results.page", core.Request{Cursor: r.NextCursor, Limit: 10})
	if r.Returned != 10 || f.fullReads != 20 {
		t.Fatal(r, f.fullReads)
	}
}
func TestDemo8DirectKeywordFiltersBeforeWindow(t *testing.T) {
	e, _, f := direct8(t, 37)
	r := run(t, e, "search.keyword", core.Request{Q: "needle", Filters: []core.Filter{{Field: "path", Op: "eq", Value: "file-020.md"}}, Limit: 10})
	if r.Returned != 1 || r.Items[0].Path != "file-020.md" || f.fullReads != 1 {
		t.Fatal(r, f.fullReads)
	}
}
func TestDemo8DirectReadFetchesOnlyRequestedPoint(t *testing.T) {
	e, _, f := direct8(t, 20)
	r := run(t, e, "search.semantic", core.Request{Q: "needle", Limit: 1})
	f.requests = nil
	f.fullReads = 0
	rr := run(t, e, "files.read", core.Request{Ref: r.Items[0].Ref})
	data := rr.Data.(core.ReadData)
	if !strings.Contains(data.Text, "needle") || f.fullReads != 1 {
		t.Fatal(rr, f.fullReads)
	}
	for _, q := range f.requests {
		if strings.HasSuffix(q["path"].(string), "scroll") {
			t.Fatal(q)
		}
	}
}
func TestDemo8DirectWrongOriginRejected(t *testing.T) {
	e, _, f := direct8(t, 2)
	f.bad = true
	_, err := e.Run(ctx, "search.semantic", core.Request{Kbs: []string{"demo"}, Q: "needle"}, "tester")
	code(t, err, "REF_ORIGIN_MISMATCH")
}
func TestDemo8DirectModelMismatchBeforeEmbedding(t *testing.T) {
	e, _, f := direct8(t, 2)
	f.desc["model_identity"] = "changed"
	_, err := e.Run(ctx, "search.semantic", core.Request{Kbs: []string{"demo"}, Q: "needle"}, "tester")
	code(t, err, "MODEL_MISMATCH")
	for _, q := range f.requests {
		if strings.Contains(q["path"].(string), "embedding") {
			t.Fatal(q)
		}
	}
}
func TestDemo8DirectPointMissingOnSecondPage(t *testing.T) {
	e, _, f := direct8(t, 3)
	r := run(t, e, "search.semantic", core.Request{Q: "needle", Limit: 1})
	f.mu.Lock()
	f.points = map[string]map[string]any{}
	f.mu.Unlock()
	_, err := e.Run(ctx, "results.page", core.Request{Kbs: []string{"demo"}, Cursor: r.NextCursor}, "tester")
	code(t, err, "REF_GONE")
}
func TestDemo8DirectNativeAskIsNotAnAgent(t *testing.T) {
	e, _, f := direct8(t, 20)
	r := run(t, e, "+ask", core.Request{Q: "needle", Limit: 5})
	if r.Returned != 5 || r.Strategy["execution"] != "direct_native_combination_then_page_hydration" {
		t.Fatal(r)
	}
	if f.fullReads != 5 {
		t.Fatal(f.fullReads)
	}
	for _, q := range f.requests {
		if strings.HasSuffix(q["path"].(string), "/scroll") {
			b := q["body"].(map[string]any)
			if b["filter"] == nil {
				t.Fatal("unfiltered scan")
			}
		}
	}
}
func TestDemo8DirectScopeLoadsMetadataOnly(t *testing.T) {
	e, d, f := direct8(t, 6)
	r := run(t, e, "search.semantic", core.Request{Q: "needle", Limit: 1, SaveScope: "required"})
	f.fullReads = 0
	ref := decode(t, r.Items[0].Ref)
	v, err := d.ViewFromReferences(ctx, []core.Ref{ref})
	must(t, err)
	if !v.Compact || len(v.Documents) != 1 || v.Documents[0].Chunks[0].Text != "" || f.fullReads != 0 {
		t.Fatal(v)
	}
}
func TestDemo8DirectUnknownTotalNotZero(t *testing.T) {
	e, _, _ := direct8(t, 2)
	r := run(t, e, "search.keyword", core.Request{Q: "absent"})
	if r.Total.Value != nil || r.Total.Accuracy != "unknown" || r.Returned != 0 {
		t.Fatal(r)
	}
}
func TestDemo8DirectMetadataProjectionRejectsLegacyPayload(t *testing.T) {
	_, d, f := direct8(t, 1)
	for _, p := range f.points {
		delete(p, "query_mentions")
	}
	_, err := d.QueryView(ctx, []string{"demo"}, nil, false)
	code(t, err, "PAYLOAD_SCHEMA_UNSUPPORTED")
}
