package tests

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
	"github.com/BlueSkyXN/XRAG/core"
	"github.com/BlueSkyXN/XRAG/internal/store"
)

type referenceRepository struct{ core.Repository } // force original generic full-view path for comparison
func TestDemo6ResourceProbe(t *testing.T) {
	output := os.Getenv("XRAG_LOAD_REPORT")
	if output == "" {
		t.Skip("opt-in synthetic resource measurement")
	}
	s, e := setup(t)
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
			d.Chunks[j].Vector = make([]float64, 128)
		}
		must(t, s.Stage(ctx, d))
		must(t, s.Publish(ctx, d, store.Job{}, "", ""))
	}
	type row struct {
		Path      string  `json:"path"`
		Iteration int     `json:"iteration"`
		MS        float64 `json:"milliseconds"`
		Allocated uint64  `json:"allocated_bytes"`
		Total     int     `json:"total"`
		Returned  int     `json:"returned"`
	}
	rows := []row{}
	engines := []*core.Engine{e, {Repo: referenceRepository{s}, Mode: "server"}}
	for it := 0; it < 3; it++ {
		for k, eng := range engines {
			runtime.GC()
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			start := time.Now()
			r := run(t, eng, "search.keyword", core.Request{Q: "MEASURE_NEEDLE", Limit: 10, SaveScope: "none"})
			elapsed := time.Since(start)
			runtime.ReadMemStats(&after)
			label := "indexed"
			if k == 1 {
				label = "generic_view"
			}
			rows = append(rows, row{label, it, elapsed.Seconds() * 1000, after.TotalAlloc - before.TotalAlloc, *r.Total.Value, r.Returned})
		}
	}
	b, er := json.MarshalIndent(map[string]any{"scope": "single-process synthetic SQLite keyword query; Go total allocations, not peak RSS or real retrieval quality", "documents": 600, "blocks_per_document": 12, "vector_dimension": 128, "rows": rows}, "", "  ")
	must(t, er)
	must(t, os.WriteFile(output, b, 0600))
}
