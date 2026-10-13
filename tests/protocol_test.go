package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"github.com/BlueSkyXN/XRAG/adapters"
	"github.com/BlueSkyXN/XRAG/contracts"
	"github.com/BlueSkyXN/XRAG/core"
	"github.com/BlueSkyXN/XRAG/internal/build"
	"github.com/BlueSkyXN/XRAG/internal/server"
	"github.com/BlueSkyXN/XRAG/internal/store"
	"github.com/BlueSkyXN/XRAG/protocols/mcp"
)

func TestHTTPRolesAndSchemaRejectUnknownArguments(t *testing.T) {
	s, _ := setup(t)
	pub(t, s, "a.md", "日志")
	reader, x := s.CreateKey(ctx, "reader", "reader", []string{"demo"})
	must(t, x)
	app := server.New(s, &build.Builder{Store: s}, nil)
	ts := httptest.NewServer(app)
	defer ts.Close()
	call := func(method, path, body, key string) int {
		r, _ := http.NewRequest(method, ts.URL+path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if key != "" {
			r.Header.Set("Authorization", "Bearer "+key)
		}
		res, x := http.DefaultClient.Do(r)
		must(t, x)
		defer res.Body.Close()
		_, _ = io.Copy(io.Discard, res.Body)
		return res.StatusCode
	}
	if n := call("POST", "/v1/search/keyword", `{"kbs":["demo"],"q":"日志"}`, reader); n != 200 {
		t.Fatal(n)
	}
	if n := call("POST", "/v1/search/keyword", `{"kbs":["demo"],"q":"日志","arbitrary":true}`, reader); n != 400 {
		t.Fatal(n)
	}
	if n := call("POST", "/v1/kbs/demo/builds", `{}`, reader); n != 403 {
		t.Fatal(n)
	}
	if n := call("POST", "/v1/search/keyword", `{"kbs":["demo"],"q":"日志"}`, ""); n != 401 {
		t.Fatal(n)
	}
	if n := call("GET", "/v1/kbs/demo/knowledge", ``, reader); n != 403 {
		t.Fatal(n)
	}
}
func TestMCPStructureAndErrors(t *testing.T) {
	s, _ := setup(t)
	pub(t, s, "a.md", "日志")
	key, x := s.CreateKey(ctx, "r", "reader", []string{"demo"})
	must(t, x)
	p, x := s.Authenticate(ctx, key)
	must(t, x)
	app := server.New(s, &build.Builder{Store: s}, nil)
	m := mcp.Server{Graph: true, Toolsets: "core,files,graph,utility", Execute: func(c context.Context, a string, b []byte) (any, error) { return app.Execute(c, p, a, b) }}
	for _, tool := range m.Tools() {
		if tool["outputSchema"] == nil || tool["inputSchema"] == nil {
			t.Fatal(tool)
		}
	}
	result := m.Handle(ctx, []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"search_keyword","arguments":{"kbs":["demo"],"q":"日志"}}}`)).(map[string]any)["result"].(map[string]any)
	if result["isError"] != false || result["structuredContent"] == nil {
		t.Fatal(result)
	}
	result = m.Handle(ctx, []byte(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"files_read","arguments":{"kbs":["demo"],"ref":"invalid"}}}`)).(map[string]any)["result"].(map[string]any)
	if result["isError"] != true {
		t.Fatal(result)
	}
}
func TestMCPStdioRoundtrip(t *testing.T) {
	m := mcp.Server{}
	input := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}` + "\n" + `{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n" + `{"jsonrpc":"2.0","id":2,"method":"tools/list"}` + "\n"
	var out bytes.Buffer
	must(t, m.Stdio(ctx, strings.NewReader(input), &out))
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatal(out.String())
	}
	for _, line := range lines {
		if !json.Valid([]byte(line)) {
			t.Fatal(line)
		}
	}
}
func TestSecretReferencesMissingAndRotation(t *testing.T) {
	t.Setenv("XRAG_TEST_SECRET", "first")
	s, x := adapters.Secret("env:XRAG_TEST_SECRET")
	must(t, x)
	if s != "first" {
		t.Fatal(s)
	}
	t.Setenv("XRAG_TEST_SECRET", "second")
	s, x = adapters.Secret("env:XRAG_TEST_SECRET")
	must(t, x)
	if s != "second" {
		t.Fatal(s)
	}
	p := filepath.Join(t.TempDir(), "key")
	must(t, os.WriteFile(p, []byte("value\n"), 0600))
	s, x = adapters.Secret("file:" + p)
	must(t, x)
	if s != "value" {
		t.Fatal(s)
	}
	_, x = adapters.Secret("env:ABSENT_XRAG_TEST_CREDENTIAL")
	code(t, x, "CREDENTIAL_UNAVAILABLE")
	_, x = adapters.Secret("plaintext")
	code(t, x, "INVALID_ARGUMENT")
}
func TestEmbeddingHTTPFixtureDimensionAndIndexValidation(t *testing.T) {
	var invalid atomic.Bool
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if invalid.Load() {
			io.WriteString(w, `{"data":[{"index":0,"embedding":[1]}]}`)
		} else {
			io.WriteString(w, `{"data":[{"index":0,"embedding":[1,0]}],"usage":{"total_tokens":3,"fixture":true}}`)
		}
	}))
	defer ts.Close()
	ep := adapters.Endpoint{URL: ts.URL, Model: "fixture-only", Dimension: 2, Revision: "scripted"}
	v, _, x := (&adapters.Embedder{Config: ep}).Embed(ctx, []string{"日志"}, true)
	must(t, x)
	if len(v[0]) != 2 {
		t.Fatal(v)
	}
	invalid.Store(true)
	_, _, x = (&adapters.Embedder{Config: ep}).Embed(ctx, []string{"日志"}, true)
	code(t, x, "MODEL_MISMATCH")
}
func TestEmbeddingCacheReusesActualInputOnMetadataChange(t *testing.T) {
	s, e := setup(t)
	var calls atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		io.WriteString(w, `{"data":[{"index":0,"embedding":[1,0]}],"usage":{"fixture":true}}`)
	}))
	defer ts.Close()
	b := build.Builder{Store: s, Config: adapters.Config{Embedding: &adapters.Endpoint{URL: ts.URL, Model: "fixture", Dimension: 2, Revision: "scripted"}}}
	root := t.TempDir()
	must(t, s.AddSource(ctx, store.Source{KB: "demo", ID: "src", Root: root, Mount: "docs", Kind: "path"}))
	for _, field := range []string{"A", "B"} {
		must(t, os.WriteFile(filepath.Join(root, "a.md"), []byte("---\nproduct: "+field+"\n---\n# Same\n日志正文\n"), 0600))
		j := claim(t, s, "build", map[string]any{})
		res, x := b.RunBuild(ctx, j)
		must(t, x)
		if res.(map[string]any)["failed"].(int) > 0 {
			t.Fatal(res)
		}
		must(t, s.Finish(ctx, j, res, nil))
	}
	if calls.Load() != 1 {
		t.Fatal("cache not reused", calls.Load())
	}
	r := run(t, e, "search.keyword", core.Request{Q: "日志", Filters: []core.Filter{{Field: "fields.product", Op: "eq", Value: "B"}}})
	if r.Returned != 1 {
		t.Fatal("new payload not visible", r)
	}
}
func TestSemanticProfileMismatch(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"data":[{"index":0,"embedding":[1,0]}]}`)
	}))
	defer ts.Close()
	s, e := setup(t)
	d := parsed(t, "a.md", "日志")
	d.Chunks[0].Vector = []float64{1, 0}
	d.Chunks[0].Generation = "another-model"
	must(t, s.Stage(ctx, d))
	must(t, s.Publish(ctx, d, store.Job{}, "", ""))
	e.Semantic = &adapters.ExactSemantic{Embed: &adapters.Embedder{Config: adapters.Endpoint{URL: ts.URL, Model: "fixture", Dimension: 2}}}
	_, x := e.Run(ctx, "search.semantic", core.Request{Kbs: []string{"demo"}, Q: "日志"}, "tester")
	code(t, x, "MODEL_MISMATCH")
}
func TestQdrantAdapterUsesPinnedCollectionFixture(t *testing.T) {
	paths := []string{}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		switch {
		case strings.HasSuffix(r.URL.Path, "/embeddings"):
			io.WriteString(w, `{"data":[{"index":0,"embedding":[1,0]}]}`)
		case strings.HasSuffix(r.URL.Path, "/exists"):
			io.WriteString(w, `{"result":{"exists":false}}`)
		case strings.HasSuffix(r.URL.Path, "/points/query"):
			io.WriteString(w, `{"result":{"points":[]}}`)
		default:
			io.WriteString(w, `{"result":{"status":"completed"},"status":"ok"}`)
		}
	}))
	defer ts.Close()
	ep := adapters.Endpoint{URL: ts.URL, Model: "fixture", Dimension: 2, Revision: "fixed"}
	q := &adapters.Qdrant{Endpoint: adapters.Endpoint{URL: ts.URL}, Embed: &adapters.Embedder{Config: ep}, Origin: "origin-A"}
	d := parsed(t, "a.md", "日志")
	d.Chunks[0].Vector = []float64{1, 0}
	d.Chunks[0].Generation = ep.SemanticID()
	must(t, q.Write(ctx, d))
	_, x := q.Search(ctx, core.View{Origin: "origin-A", Documents: []core.Document{d}}, "日志")
	must(t, x)
	want := "/collections/" + q.Collection("demo") + "/points/query"
	if !strings.Contains(strings.Join(paths, "\n"), want) {
		t.Fatal(paths)
	}
	other := *q
	other.Origin = "origin-B"
	if other.Collection("demo") == q.Collection("demo") {
		t.Fatal("origins share physical collection")
	}
}
func TestNeo4jAdapterRejects202QueryErrors(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(202)
		io.WriteString(w, `{"errors":[{"code":"Neo.ClientError.Statement.SyntaxError","message":"fixture"}]}`)
	}))
	defer ts.Close()
	n := adapters.Neo4j{Config: adapters.NeoConfig{URL: ts.URL, Database: "neo4j", User: "test"}, Origin: "o"}
	x := n.Write(ctx, parsed(t, "a.md", "[[A]] --uses--> [[B]]"))
	code(t, x, "BACKEND_UNAVAILABLE")
}
func TestNeo4jWriteContainsIDsButNoText(t *testing.T) {
	raw := ""
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		raw = string(b)
		io.WriteString(w, `{"data":{"fields":[],"values":[]}}`)
	}))
	defer ts.Close()
	n := adapters.Neo4j{Config: adapters.NeoConfig{URL: ts.URL, Database: "neo4j", User: "test"}, Origin: "o"}
	d := parsed(t, "a.md", "# unique-secret-body\n[[A]] --uses--> [[B]]")
	must(t, n.Write(ctx, d))
	if strings.Contains(raw, "unique-secret-body") || strings.Contains(raw, `"evidence"`) {
		t.Fatal(raw)
	}
	if !strings.Contains(raw, "support_id") {
		t.Fatal("missing support identity")
	}
}
func TestExtractorRealSubprocessNoDatabaseAccess(t *testing.T) {
	p := filepath.Join(t.TempDir(), "extract.py")
	must(t, os.WriteFile(p, []byte(`import sys,json
r=json.loads(sys.stdin.readline())
print(json.dumps({'id':r['id'],'result':{'relations':[{'from':'A','to':'B','rel':'uses','evidence':r['text']}]}}))
`), 0600))
	d := parsed(t, "a.md", "A uses B.")
	ss, x := adapters.Extract(ctx, []string{"python3", "-I", p}, "fixture-script", d.Chunks[0])
	must(t, x)
	if len(ss) != 1 || ss[0].Origin != "plugin" {
		t.Fatal(ss)
	}
}
func TestWikiConfiguredModelProducesOnlyDraft(t *testing.T) {
	s, e := setup(t)
	pub(t, s, "a.md", "# 日志\n日志是运行记录。\n")
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"choices":[{"message":{"content":"固定脚本模型生成的测试综述 [1]。"}}],"usage":{"fixture":true,"total_tokens":12}}`)
	}))
	defer ts.Close()
	b := build.Builder{Store: s, Config: adapters.Config{Chat: &adapters.Endpoint{URL: ts.URL, Model: "fixture"}}}
	j := claim(t, s, "wiki", map[string]any{"topic": "日志"})
	res, x := b.RunWiki(ctx, j)
	must(t, x)
	if res == nil {
		t.Fatal("no draft")
	}
	r := run(t, e, "search.keyword", core.Request{Q: "固定脚本模型"})
	if r.Returned != 0 {
		t.Fatal("Wiki draft leaked")
	}
	items, x := s.KnowledgeList(ctx, []string{"demo"})
	must(t, x)
	if len(items) != 1 || items[0].State != "draft" {
		t.Fatal(items)
	}
}
func TestWikiNoModelNoFakeContent(t *testing.T) {
	s, _ := setup(t)
	b := build.Builder{Store: s}
	j := claim(t, s, "wiki", map[string]any{"topic": "日志"})
	_, x := b.RunWiki(ctx, j)
	code(t, x, "CAPABILITY_UNAVAILABLE")
	ks, x := s.KnowledgeList(ctx, []string{"demo"})
	must(t, x)
	if len(ks) != 0 {
		t.Fatal(ks)
	}
}
func TestContractRegistryIsSingleActionSource(t *testing.T) {
	if len(contracts.All) != 16 {
		t.Fatal(len(contracts.All))
	}
	seen := map[string]bool{}
	for _, a := range contracts.All {
		if seen[a.Tool] {
			t.Fatal("duplicate tool", a.Tool)
		}
		seen[a.Tool] = true
		if len(contracts.Input(a)) == 0 {
			t.Fatal(a.Action)
		}
	}
	if fmt.Sprint(contracts.Output()["type"]) != "object" {
		t.Fatal(contracts.Output())
	}
}

func TestDirectAdapterCarriesOriginIntoSemanticQueryFixture(t *testing.T) {
	var physical, origin = "", "direct-origin"
	var hitPath string
	ep := adapters.Endpoint{Model: "fixture", Dimension: 2, Revision: "r1"}
	q := &adapters.Qdrant{Embed: &adapters.Embedder{Config: ep}, Origin: origin}
	physical = q.Collection("demo")
	d := parsed(t, "a.md", "日志")
	c := d.Chunks[0]
	c.Generation = ep.SemanticID()
	payload := map[string]any{}
	must(t, json.Unmarshal([]byte(core.JSON(c)), &payload))
	payload["origin_id"] = origin
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "xrag_demo_meta/points/"):
			fmt.Fprintf(w, `{"result":{"payload":{"physical_collection":%q,"origin_id":%q,"kb":"demo","model_identity":%q}}}`, physical, origin, ep.SemanticID())
		case strings.HasSuffix(r.URL.Path, "points/scroll"):
			json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"points": []any{map[string]any{"payload": payload}}, "next_page_offset": nil}})
		case strings.HasSuffix(r.URL.Path, "embeddings"):
			io.WriteString(w, `{"data":[{"index":0,"embedding":[1,0]}]}`)
		case strings.HasSuffix(r.URL.Path, "points/query"):
			hitPath = r.URL.Path
			fmt.Fprintf(w, `{"result":{"points":[{"id":%q,"score":1}]}}`, c.ID)
		default:
			t.Errorf("unexpected %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer ts.Close()
	ep.URL = ts.URL
	q = &adapters.Qdrant{Endpoint: adapters.Endpoint{URL: ts.URL}, Embed: &adapters.Embedder{Config: ep}}
	direct := &adapters.Direct{Q: q, KB: "demo", Collection: physical, CacheDir: t.TempDir()}
	v, x := direct.Snapshot(ctx, []string{"demo"})
	must(t, x)
	_, x = q.Search(ctx, v, "日志")
	must(t, x)
	if hitPath != "/collections/"+physical+"/points/query" {
		t.Fatal(hitPath)
	}
}
