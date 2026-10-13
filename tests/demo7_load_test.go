package tests

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
	"github.com/BlueSkyXN/XRAG/adapters"
	"github.com/BlueSkyXN/XRAG/core"
	"github.com/BlueSkyXN/XRAG/internal/build"
	"github.com/BlueSkyXN/XRAG/internal/store"
)

// Opt-in synthetic execution experiment, not a semantic-model quality test.
func TestDemo7ResourceProbe(t *testing.T) {
	output := os.Getenv("XRAG_COMPACT_REPORT")
	if output == "" {
		t.Skip("opt-in compact query resource measurement")
	}
	s, _ := setup(t)
	vector := make([]float64, 128)
	vector[0] = 1
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"index": 0, "embedding": vector}}, "usage": map[string]int{"prompt_tokens": 1, "total_tokens": 1}})
	}))
	defer ts.Close()
	ep := adapters.Endpoint{URL: ts.URL, Model: "synthetic-query-fixture", Dimension: 128, Revision: "r1"}
	cfg := adapters.Config{Embedding: &ep}
	must(t, s.SaveGeneration(ctx, "demo", cfg))
	chunks := 0
	for i := 0; i < 600; i++ {
		var b strings.Builder
		for n := 0; n < 12; n++ {
			fmt.Fprintf(&b, "## section %d\n", n)
			if i%200 == 0 && n == 0 {
				b.WriteString("MEASURE_NEEDLE ")
			}
			b.WriteString(strings.Repeat("ordinary unrelated document payload ", 25))
			b.WriteString("\n")
		}
		d := parsed(t, fmt.Sprintf("file%04d.md", i), b.String())
		for j := range d.Chunks {
			vec := make([]float64, 128)
			vec[0] = .1
			vec[1] = float64(i+1)/100 + float64(j)/1000
			if i%200 == 0 && j == 0 {
				vec[0] = 1
				vec[1] = float64(i) / 10000
			}
			d.Chunks[j].Vector = vec
			d.Chunks[j].Generation = ep.SemanticID()
			d.Chunks[j].VectorState = "ready"
		}
		must(t, s.Stage(ctx, d))
		must(t, s.Publish(ctx, d, store.Job{}, "", ""))
		chunks += len(d.Chunks)
	}
	builder := &build.Builder{Store: s, Config: cfg}
	compact, err := builder.EngineFor(ctx, []string{"demo"})
	must(t, err)
	full, err := builder.EngineFor(ctx, []string{"demo"})
	must(t, err)
	full.Repo = referenceRepository{s}
	type row struct {
		Action    string   `json:"action"`
		Path      string   `json:"path"`
		Iteration int      `json:"iteration"`
		MS        float64  `json:"milliseconds"`
		Allocated uint64   `json:"allocated_bytes"`
		IDs       []string `json:"first_page_ids"`
		Returned  int      `json:"returned"`
	}
	rows := []row{}
	for _, action := range []string{"search.semantic", "+ask"} {
		var expected []string
		for i := 0; i < 3; i++ {
			for j, eng := range []*core.Engine{full, compact} {
				runtime.GC()
				var a, z runtime.MemStats
				runtime.ReadMemStats(&a)
				start := time.Now()
				resp := run(t, eng, action, core.Request{Q: "MEASURE_NEEDLE", Limit: 3, SaveScope: "none"})
				elapsed := time.Since(start)
				runtime.ReadMemStats(&z)
				ids := []string{}
				for _, x := range resp.Items {
					ids = append(ids, x.ChunkID)
				}
				if expected == nil {
					expected = ids
				}
				if core.JSON(ids) != core.JSON(expected) {
					t.Fatalf("different first page: %s %s %s", action, core.JSON(expected), core.JSON(ids))
				}
				label := "full_view"
				if j == 1 {
					label = "compact_projection"
				}
				rows = append(rows, row{action, label, i, elapsed.Seconds() * 1000, z.TotalAlloc - a.TotalAlloc, ids, resp.Returned})
			}
		}
	}
	b, e := json.MarshalIndent(map[string]any{"scope": "single-process synthetic SQLite + local exact cosine; fixed vectors and controlled embedding endpoint; Go allocations, not peak RSS or model quality", "documents": 600, "chunks": chunks, "dimension": 128, "repetitions": 3, "first_pages_equal": true, "rows": rows}, "", "  ")
	must(t, e)
	must(t, os.WriteFile(output, b, 0600))
}
