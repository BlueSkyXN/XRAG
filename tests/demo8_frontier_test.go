package tests

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"github.com/BlueSkyXN/XRAG/adapters"
	"github.com/BlueSkyXN/XRAG/core"
)

func frontier8(t *testing.T) (core.View, map[string]core.Chunk, []core.Support) {
	t.Helper()
	d := parsed(t, "links.md", "# 关系\n[[A]] --uses--> [[B]]\n[[B]] --uses--> [[C]]\n[[X]] --uses--> [[Y]]")
	d.Supports = core.ExplicitSupports(d)
	v := core.View{Origin: "o8", Documents: []core.Document{d}}
	cs := map[string]core.Chunk{}
	for _, c := range d.Chunks {
		cs[c.ID] = c
	}
	return v, cs, d.Supports
}
func incident8(ss []core.Support, front []string, dir string) []core.Support {
	out := []core.Support{}
	for _, s := range ss {
		a, b := core.EntityID(s.KB, s.From), core.EntityID(s.KB, s.To)
		for _, f := range front {
			if ((dir == "out" || dir == "both") && a == f) || ((dir == "in" || dir == "both") && b == f) {
				out = append(out, s)
				break
			}
		}
	}
	return out
}
func TestDemo8FrontierOnlyReachedEntities(t *testing.T) {
	v, cs, ss := frontier8(t)
	fronts := [][]string{}
	out, e := core.ExpandFrontier(ctx, "graph.path", core.Request{FromID: "A", ToID: "C", Direction: "out", MaxHops: 2}, v, cs, func(_ context.Context, f []string, dir string, n int) ([]core.Support, error) {
		fronts = append(fronts, append([]string{}, f...))
		return incident8(ss, f, dir), nil
	})
	must(t, e)
	if len(out) != 2 || len(fronts) != 2 || fronts[1][0] != core.EntityID("demo", "B") {
		t.Fatal(out, fronts)
	}
}
func TestDemo8FrontierReverseDirection(t *testing.T) {
	v, cs, ss := frontier8(t)
	out, e := core.ExpandFrontier(ctx, "graph.neighbors", core.Request{Name: "C", Depth: 1, Direction: "in"}, v, cs, func(_ context.Context, f []string, d string, n int) ([]core.Support, error) {
		return incident8(ss, f, d), nil
	})
	must(t, e)
	if len(out) != 1 || out[0].From != "B" {
		t.Fatal(out)
	}
}
func TestDemo8FrontierRejectsUnrelatedEdge(t *testing.T) {
	v, cs, ss := frontier8(t)
	_, e := core.ExpandFrontier(ctx, "graph.path", core.Request{FromID: "A", MaxHops: 1, Direction: "out"}, v, cs, func(context.Context, []string, string, int) ([]core.Support, error) { return ss, nil })
	code(t, e, "BACKEND_FILTER_MISMATCH")
}
func TestDemo8FrontierRejectsOutOfScopeSupport(t *testing.T) {
	v, cs, ss := frontier8(t)
	bad := ss[0]
	bad.ChunkID = "outside"
	_, e := core.ExpandFrontier(ctx, "graph.path", core.Request{FromID: "A", MaxHops: 1}, v, cs, func(context.Context, []string, string, int) ([]core.Support, error) { return []core.Support{bad}, nil })
	code(t, e, "BACKEND_FILTER_MISMATCH")
}
func TestDemo8FrontierRejectsUnpublishedEdition(t *testing.T) {
	v, cs, ss := frontier8(t)
	bad := ss[0]
	bad.Edition = "pending"
	_, e := core.ExpandFrontier(ctx, "graph.path", core.Request{FromID: "A", MaxHops: 1}, v, cs, func(context.Context, []string, string, int) ([]core.Support, error) { return []core.Support{bad}, nil })
	code(t, e, "BACKEND_FILTER_MISMATCH")
}
func TestDemo8FrontierHonorsCancellation(t *testing.T) {
	v, cs, _ := frontier8(t)
	c, cancel := context.WithCancel(ctx)
	cancel()
	_, e := core.ExpandFrontier(c, "graph.path", core.Request{FromID: "A"}, v, cs, func(context.Context, []string, string, int) ([]core.Support, error) {
		t.Fatal("called backend after cancellation")
		return nil, nil
	})
	if e != context.Canceled {
		t.Fatal(e)
	}
}
func TestDemo8FrontierDuplicateEvidenceNotCountedTwice(t *testing.T) {
	v, cs, ss := frontier8(t)
	one := incident8(ss, []string{core.EntityID("demo", "A")}, "out")
	out, e := core.ExpandFrontier(ctx, "graph.path", core.Request{FromID: "A", MaxHops: 1}, v, cs, func(context.Context, []string, string, int) ([]core.Support, error) { return append(one, one...), nil })
	must(t, e)
	if len(out) != 1 {
		t.Fatal(out)
	}
}
func TestDemo8NeoFrontierPredicatesBeforeLimit(t *testing.T) {
	v, _, _ := frontier8(t)
	var statement string
	var params map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b map[string]any
		json.NewDecoder(r.Body).Decode(&b)
		statement, _ = b["statement"].(string)
		params, _ = b["parameters"].(map[string]any)
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"fields": []string{}, "values": [][]any{}}})
	}))
	defer ts.Close()
	n := adapters.Neo4j{Origin: v.Origin, Config: adapters.NeoConfig{URL: ts.URL, Database: "neo4j"}}
	_, e := n.Frontier(ctx, v, []string{core.EntityID("demo", "A")}, "out", 20)
	must(t, e)
	if !strings.Contains(statement, "s.chunk_id IN $chunks") || !strings.Contains(statement, "a.entity_id IN $front") || strings.Index(statement, "WHERE") > strings.Index(statement, "LIMIT") {
		t.Fatal(statement)
	}
	if params["authority"] != "o8" || len(params["front"].([]any)) != 1 {
		t.Fatal(params)
	}
}
func TestDemo8DirectConcurrentReadIdentity(t *testing.T) {
	e, _, _ := direct8(t, 3)
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, er := e.Run(ctx, "search.semantic", core.Request{Kbs: []string{"demo"}, Q: "needle", Limit: 1, SaveScope: "none"}, "reader")
			errs <- er
		}()
	}
	wg.Wait()
	close(errs)
	for er := range errs {
		must(t, er)
	}
}
