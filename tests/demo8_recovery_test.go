package tests

import (
	"encoding/json"
	"fmt"
	"github.com/BlueSkyXN/XRAG/adapters"
	"github.com/BlueSkyXN/XRAG/core"
	"github.com/BlueSkyXN/XRAG/internal/build"
	"github.com/BlueSkyXN/XRAG/internal/maintenance"
	"github.com/BlueSkyXN/XRAG/internal/store"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

type recoveryBackend8 struct {
	mu          sync.Mutex
	collections map[string]bool
	points      map[string]map[string]any
	descriptors map[string]any
	supports    map[string]any
	chunks      map[string]any
	calls       int
	modelCalls  int
	failWrite   bool
	occupied    bool
}

func (f *recoveryBackend8) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	body := map[string]any{}
	if r.Method == "POST" || r.Method == "PUT" {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	result := any(map[string]any{"result": true})
	switch {
	case strings.Contains(r.URL.Path, "embeddings") || strings.Contains(r.URL.Path, "chat"):
		f.modelCalls++
		http.Error(w, "model calls forbidden in recovery", 500)
		return
	case strings.HasSuffix(r.URL.Path, "/exists"):
		result = map[string]any{"result": map[string]any{"exists": f.collections[parts[1]] || f.occupied}}
	case r.Method == "PUT" && len(parts) == 2:
		f.collections[parts[1]] = true
	case r.Method == "PUT" && strings.HasSuffix(r.URL.Path, "/index"):
	case r.Method == "PUT" && len(parts) >= 3 && parts[2] == "points":
		if f.failWrite && parts[1] != "xrag_demo_meta" {
			http.Error(w, "controlled outage", 503)
			return
		}
		for _, v := range body["points"].([]any) {
			p := v.(map[string]any)
			if parts[1] == "xrag_demo_meta" {
				f.descriptors[p["id"].(string)] = p
			} else {
				f.points[p["id"].(string)] = p
			}
		}
	case strings.HasSuffix(r.URL.Path, "query/v2"):
		statement := body["statement"].(string)
		par := body["parameters"].(map[string]any)
		if strings.Contains(statement, "RETURN count(n)") {
			n := 0
			if f.occupied {
				n = 1
			}
			result = map[string]any{"data": map[string]any{"values": [][]any{{n}}}}
		} else {
			for _, v := range par["rows"].([]any) {
				row := v.(map[string]any)
				if strings.Contains(statement, "XragDemoSupport") {
					f.supports[row["id"].(string)] = row
				} else {
					f.chunks[row["id"].(string)] = row
				}
			}
			result = map[string]any{"data": map[string]any{"values": []any{}}}
		}
	default:
		http.Error(w, "unexpected "+r.URL.Path, 404)
		return
	}
	json.NewEncoder(w).Encode(result)
}
func recovery8(t *testing.T) (*store.Store, *build.Builder, build.RecoveryTargets, *recoveryBackend8, core.Document) {
	t.Helper()
	s, _ := setup(t)
	d := parsed(t, "a.md", "# Evidence\n[[A]] --uses--> [[B]]\noriginal")
	cfg := adapters.Config{Embedding: &adapters.Endpoint{URL: "http://127.0.0.1:1", Model: "saved-fixture", Dimension: 2, Revision: "fixed"}, Qdrant: &adapters.Endpoint{URL: "http://127.0.0.1:2"}, Neo4j: &adapters.NeoConfig{URL: "http://127.0.0.1:3", Database: "neo4j"}}
	for i := range d.Chunks {
		d.Chunks[i].Vector = []float64{1, .25}
		d.Chunks[i].Generation = cfg.Embedding.SemanticID()
		d.Chunks[i].VectorState = "ready"
	}
	must(t, s.SaveGeneration(ctx, "demo", cfg))
	must(t, s.Stage(ctx, d))
	must(t, s.BindProjections(ctx, d, store.ProjectionBinding{Vectors: []adapters.Config{cfg}, Neo4j: cfg.Neo4j}))
	must(t, s.Publish(ctx, d, store.Job{}, "", ""))
	snap := filepath.Join(t.TempDir(), "snapshot.sqlite")
	must(t, s.Backup(ctx, snap))
	dest := filepath.Join(t.TempDir(), "restored")
	_, err := maintenance.Restore(ctx, snap, dest)
	must(t, err)
	restored, err := store.Open(dest)
	must(t, err)
	t.Cleanup(func() { restored.Close() })
	f := &recoveryBackend8{collections: map[string]bool{}, points: map[string]map[string]any{}, descriptors: map[string]any{}, supports: map[string]any{}, chunks: map[string]any{}}
	ts := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(ts.Close)
	targets := build.RecoveryTargets{Bindings: map[string]build.RecoveryTarget{"demo": {Qdrant: &adapters.Endpoint{URL: ts.URL}, Neo4j: &adapters.NeoConfig{URL: ts.URL, Database: "neo4j", User: "test"}}}}
	return restored, &build.Builder{Store: restored}, targets, f, d
}
func TestDemo8RecoveryPreviewLocalOnly(t *testing.T) {
	_, b, target, f, d := recovery8(t)
	p, e := b.RecoverProjections(ctx, target, false)
	must(t, e)
	if !p.DryRun || p.Editions != 1 || p.VectorPoints != len(d.Chunks) || f.calls != 0 {
		t.Fatal(p, f.calls)
	}
}
func TestDemo8RecoveryReplaysSavedVectorsAndGraphWithoutModels(t *testing.T) {
	s, b, target, f, d := recovery8(t)
	p, e := b.RecoverProjections(ctx, target, true)
	must(t, e)
	if p.State != "succeeded" || p.Completed != 1 || p.ModelCalls != 0 || f.modelCalls != 0 {
		t.Fatal(p)
	}
	if len(f.points) != len(d.Chunks) || len(f.supports) != len(d.Supports) {
		t.Fatal(len(f.points), len(f.supports))
	}
	for _, c := range d.Chunks {
		vectors := f.points[c.ID]["vector"].(map[string]any)
		v := vectors[""].([]any)
		if core.JSON(vectors[core.LexicalVectorName]) != core.JSON(core.LexicalSparse(c.Title+" "+c.Text, false)) {
			t.Fatal(vectors)
		}
		if v[0] != float64(1) || v[1] != .25 {
			t.Fatal(v)
		}
	}
	cfg, e := s.Generation(ctx, "demo", d.Chunks[0].Generation)
	must(t, e)
	if cfg.Qdrant.URL != target.Bindings["demo"].Qdrant.URL {
		t.Fatal(cfg)
	}
	_, ch, _, e := s.Read(ctx, decode(t, core.ChunkItem(d.Chunks[0], s.Origin).Ref))
	must(t, e)
	if ch == nil || ch.Text != d.Chunks[0].Text {
		t.Fatal(ch)
	}
	must(t, s.CheckRecoveryComplete(ctx))
	before := f.calls
	p, e = b.RecoverProjections(ctx, target, true)
	must(t, e)
	if p.State != "succeeded" || f.calls != before {
		t.Fatal("completed recovery unexpectedly executed", p, f.calls)
	}
}
func TestDemo8RecoveryRejectsOccupiedTargetBeforeWriting(t *testing.T) {
	_, b, target, f, _ := recovery8(t)
	f.occupied = true
	_, e := b.RecoverProjections(ctx, target, true)
	code(t, e, "RECOVERY_TARGET_NOT_EMPTY")
	if len(f.points) > 0 || len(f.supports) > 0 {
		t.Fatal("occupied target overwritten")
	}
}
func TestDemo8RecoveryFailureCanResumeAndBlocksServing(t *testing.T) {
	s, b, target, f, _ := recovery8(t)
	f.failWrite = true
	p, e := b.RecoverProjections(ctx, target, true)
	if e == nil || p.State != "failed" {
		t.Fatal(p, e)
	}
	code(t, s.CheckRecoveryComplete(ctx), "RECOVERY_INCOMPLETE")
	f.failWrite = false
	p, e = b.RecoverProjections(ctx, target, true)
	must(t, e)
	if p.State != "succeeded" || p.Completed != 1 || f.modelCalls != 0 {
		t.Fatal(p)
	}
	must(t, s.CheckRecoveryComplete(ctx))
}
func TestDemo8RecoveryRequiresRestoredCopy(t *testing.T) {
	s, _ := setup(t)
	_, e := (&build.Builder{Store: s}).RecoverProjections(ctx, build.RecoveryTargets{}, false)
	code(t, e, "RESTORED_COPY_REQUIRED")
}
func TestDemo8RecoveryDoesNotReplayWithdrawnData(t *testing.T) {
	s, b, target, f, d := recovery8(t)
	must(t, s.Withdraw(ctx, "demo", d.Path))
	_, e := b.RecoverProjections(ctx, target, true)
	code(t, e, "INVALID_ARGUMENT")
	if f.calls != 0 {
		t.Fatal("withdrawn data reached target")
	}
}
func TestDemo8RecoveryUnknownTargetFieldRejected(t *testing.T) {
	_, e := build.DecodeRecoveryTargets([]byte(`{"bindings":{},"exec":"do something"}`))
	if e == nil {
		t.Fatal("unknown field accepted")
	}
}
func TestDemo8RecoveryResumeCannotChangeTarget(t *testing.T) {
	_, b, target, f, _ := recovery8(t)
	f.failWrite = true
	_, e := b.RecoverProjections(ctx, target, true)
	if e == nil {
		t.Fatal("expected controlled failure")
	}
	other := target
	other.Bindings = map[string]build.RecoveryTarget{"demo": {Qdrant: &adapters.Endpoint{URL: "http://127.0.0.1:9"}}}
	_, e = b.RecoverProjections(ctx, other, true)
	code(t, e, "VERSION_CONFLICT")
}
func TestDemo8RecoveryReportsPartialBackendSelection(t *testing.T) {
	s, b, target, f, d := recovery8(t)
	target.Bindings["demo"] = build.RecoveryTarget{Neo4j: target.Bindings["demo"].Neo4j}
	p, e := b.RecoverProjections(ctx, target, true)
	must(t, e)
	if p.VectorPoints != 0 || len(f.points) != 0 || len(f.supports) == 0 {
		t.Fatal(p)
	}
	cfg, e := s.Generation(ctx, "demo", d.Chunks[0].Generation)
	must(t, e)
	if cfg.Qdrant.URL != "http://127.0.0.1:2" {
		t.Fatal(fmt.Sprint(cfg))
	}
}
