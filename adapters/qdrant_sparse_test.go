package adapters

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/BlueSkyXN/XRAG/core"
)

func TestQdrantSparseWriteProtocol(t *testing.T) {
	seen := map[string]map[string]any{}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			_, _ = w.Write([]byte(`{"result":{"exists":false}}`))
			return
		}
		body := map[string]any{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		seen[r.URL.Path] = body
		_, _ = w.Write([]byte(`{"result":true}`))
	}))
	defer ts.Close()
	q := &Qdrant{Endpoint: Endpoint{URL: ts.URL}, Embed: &Embedder{Config: Endpoint{Model: "fixture", Dimension: 2}}, Origin: "test"}
	doc := core.Document{KB: "demo", Chunks: []core.Chunk{{ID: core.UUID("test"), Title: "hello", Text: "hello", Vector: []float64{1, 0}}}}
	if err := q.Write(context.Background(), doc); err != nil {
		t.Fatal(err)
	}
	name := q.Collection("demo")
	if !strings.HasSuffix(name, "_"+core.LexicalSchema) {
		t.Fatal(name)
	}
	created := seen["/collections/"+name]
	if core.JSON(created["sparse_vectors"]) != `{"xrag_terms":{"modifier":"idf"}}` {
		t.Fatal(created)
	}
	if core.JSON(created["vectors"]) != `{"distance":"Cosine","size":2}` {
		t.Fatal(created)
	}
	if seen["/collections/xrag_demo_meta"]["sparse_vectors"] != nil {
		t.Fatal("descriptor collection needs only dense vector")
	}
	points := seen["/collections/"+name+"/points"]["points"].([]any)
	vectors := points[0].(map[string]any)["vector"].(map[string]any)
	if core.JSON(vectors[""]) != "[1,0]" || core.JSON(vectors[core.LexicalVectorName]) != core.JSON(core.LexicalSparse("hello hello", false)) {
		t.Fatal(vectors)
	}
	meta := seen["/collections/xrag_demo_meta/points"]["points"].([]any)[0].(map[string]any)["payload"].(map[string]any)
	if meta["lexical_schema"] != core.LexicalSchema || meta["lexical_avgdl"] != core.LexicalAverageLength {
		t.Fatal(meta)
	}
}
