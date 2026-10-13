package tests

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
	"github.com/BlueSkyXN/XRAG/adapters"
	"github.com/BlueSkyXN/XRAG/core"
	"github.com/BlueSkyXN/XRAG/internal/build"
	db "github.com/BlueSkyXN/XRAG/internal/sqlite"
	"github.com/BlueSkyXN/XRAG/internal/store"
)

type gcBackend7 struct {
	mu                sync.Mutex
	collection        string
	origin, kb, model string
	exists            bool
	count             int
	alias             bool
	foreign           bool
	failDelete        bool
	failDescriptor    bool
	requests          []string
	deleted           int
}

func (g *gcBackend7) serve(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.requests = append(g.requests, r.Method+" "+r.URL.RequestURI())
	response := any(map[string]any{"result": true})
	switch {
	case r.Method == "GET" && r.URL.Path == "/collections/"+g.collection+"/exists":
		response = map[string]any{"result": map[string]any{"exists": g.exists}}
	case r.Method == "GET" && r.URL.Path == "/collections/xrag_demo_meta/points/"+core.UUID(g.collection):
		origin := g.origin
		if g.foreign {
			origin = "someone_else"
		}
		response = map[string]any{"result": map[string]any{"payload": map[string]any{"origin_id": origin, "kb": g.kb, "model_identity": g.model, "physical_collection": g.collection}}}
	case r.Method == "GET" && r.URL.Path == "/aliases":
		aliases := []any{}
		if g.alias {
			aliases = append(aliases, map[string]any{"alias_name": "do-not-touch", "collection_name": g.collection})
		}
		response = map[string]any{"result": map[string]any{"aliases": aliases}}
	case r.Method == "POST" && r.URL.Path == "/collections/"+g.collection+"/points/count":
		response = map[string]any{"result": map[string]any{"count": g.count}}
	case r.Method == "DELETE" && r.URL.Path == "/collections/"+g.collection:
		if g.failDelete {
			http.Error(w, "unavailable", 503)
			return
		}
		g.exists = false
		g.deleted++
	case r.Method == "POST" && r.URL.Path == "/collections/xrag_demo_meta/points/delete":
		if g.failDescriptor {
			http.Error(w, "unavailable", 503)
			return
		}
	case r.Method == "POST" && r.URL.Path == "/collections/"+g.collection+"/points/delete":
	default:
		http.Error(w, "unexpected path "+r.URL.RequestURI(), 404)
		return
	}
	json.NewEncoder(w).Encode(response)
}
func setupGC7(t *testing.T) (*store.Store, *build.Builder, *gcBackend7, adapters.Config, core.Document) {
	t.Helper()
	s, _ := setup(t)
	g := &gcBackend7{origin: s.Origin, kb: "demo", exists: true}
	ts := httptest.NewServer(http.HandlerFunc(g.serve))
	t.Cleanup(ts.Close)
	cfg := adapters.Config{Qdrant: &adapters.Endpoint{URL: ts.URL}, Embedding: &adapters.Endpoint{URL: ts.URL, Model: "retired", Dimension: 2, Revision: "r1"}}
	q := adapters.Qdrant{Origin: s.Origin, Endpoint: *cfg.Qdrant, Embed: &adapters.Embedder{Config: *cfg.Embedding}}
	g.collection = q.Collection("demo")
	g.model = cfg.Embedding.SemanticID()
	d := parsed(t, "old.md", "old evidence")
	must(t, s.Stage(ctx, d))
	must(t, s.BindProjections(ctx, d, store.ProjectionBinding{Vectors: []adapters.Config{cfg}}))
	must(t, s.Publish(ctx, d, store.Job{}, "", ""))
	b := &build.Builder{Store: s}
	return s, b, g, cfg, d
}
func retireGC7(t *testing.T, s *store.Store, d core.Document) {
	t.Helper()
	pub(t, s, "old.md", "new evidence")
	must(t, s.DB.Tx(ctx, true, func(c *db.Conn) error {
		if e := c.Exec("DELETE FROM fts WHERE chunk_id IN(SELECT id FROM chunk WHERE edition=?)", d.Edition); e != nil {
			return e
		}
		if e := c.Exec("DELETE FROM chunk WHERE edition=?", d.Edition); e != nil {
			return e
		}
		if e := c.Exec("DELETE FROM edition WHERE id=?", d.Edition); e != nil {
			return e
		}
		if e := c.Exec("DELETE FROM projection_gc WHERE edition=?", d.Edition); e != nil {
			return e
		}
		return c.Exec("UPDATE vector_registry SET retired_at=? WHERE state='retired'", time.Now().Add(-45*24*time.Hour).Unix())
	}))
}
func TestV7VectorGCPreviewDoesNotContactBackend(t *testing.T) {
	s, b, g, _, d := setupGC7(t)
	retireGC7(t, s, d)
	p, e := b.CollectVectorGenerations(ctx, 30, false)
	must(t, e)
	if len(g.requests) != 0 || len(p.Entries) != 1 || p.Entries[0].Decision != "eligible_locally" || p.RemoteChecked {
		t.Fatal(p, g.requests)
	}
}
func TestV7VectorGCPreservesEditionHolders(t *testing.T) {
	_, b, g, _, _ := setupGC7(t)
	p, e := b.CollectVectorGenerations(ctx, 1, true)
	must(t, e)
	if p.Removed != 0 || p.Entries[0].Decision != "held_by_editions" || len(g.requests) > 0 {
		t.Fatal(p)
	}
}
func TestV7VectorGCPreservesDesiredConfiguration(t *testing.T) {
	s, b, g, cfg, d := setupGC7(t)
	retireGC7(t, s, d)
	b.Config = cfg
	p, e := b.CollectVectorGenerations(ctx, 1, true)
	must(t, e)
	if p.Entries[0].Decision != "selected_by_configuration" || len(g.requests) > 0 {
		t.Fatal(p)
	}
}
func TestV7VectorGCRetentionStartsAtLastHolderRemoval(t *testing.T) {
	s, b, g, _, d := setupGC7(t)
	retireGC7(t, s, d)
	must(t, s.DB.Do(ctx, func(c *db.Conn) error { return c.Exec("UPDATE vector_registry SET retired_at=?", time.Now().Unix()) }))
	p, e := b.CollectVectorGenerations(ctx, 1, true)
	must(t, e)
	if p.Entries[0].Decision != "retention_window" || len(g.requests) > 0 {
		t.Fatal(p)
	}
}
func TestV7VectorGCDropsOnlyEmptyUnaliasedOwnedGeneration(t *testing.T) {
	s, b, g, _, d := setupGC7(t)
	retireGC7(t, s, d)
	p, e := b.CollectVectorGenerations(ctx, 30, true)
	must(t, e)
	if p.Removed != 1 || g.deleted != 1 || g.exists {
		t.Fatal(p, g)
	}
	gs, e := s.VectorGenerations(ctx)
	must(t, e)
	if len(gs) != 0 {
		t.Fatal(gs)
	}
	p, e = b.CollectVectorGenerations(ctx, 30, true)
	must(t, e)
	if p.Removed != 0 || g.deleted != 1 {
		t.Fatal(p)
	}
}
func TestV7VectorGCRefusesAlias(t *testing.T) {
	s, b, g, _, d := setupGC7(t)
	retireGC7(t, s, d)
	g.alias = true
	p, e := b.CollectVectorGenerations(ctx, 30, true)
	code(t, e, "COLLECTION_IN_USE")
	if p.Removed != 0 || g.deleted != 0 {
		t.Fatal(p)
	}
}
func TestV7VectorGCRefusesResidualPoints(t *testing.T) {
	s, b, g, _, d := setupGC7(t)
	retireGC7(t, s, d)
	g.count = 1
	_, e := b.CollectVectorGenerations(ctx, 30, true)
	code(t, e, "COLLECTION_NOT_EMPTY")
	if g.deleted != 0 {
		t.Fatal("deleted non-empty")
	}
}
func TestV7VectorGCRefusesForeignDescriptor(t *testing.T) {
	s, b, g, _, d := setupGC7(t)
	retireGC7(t, s, d)
	g.foreign = true
	_, e := b.CollectVectorGenerations(ctx, 30, true)
	code(t, e, "REF_ORIGIN_MISMATCH")
	if g.deleted != 0 {
		t.Fatal("deleted foreign")
	}
}
func TestV7VectorGCDeleteFailureKeepsRegistry(t *testing.T) {
	s, b, g, _, d := setupGC7(t)
	retireGC7(t, s, d)
	g.failDelete = true
	p, e := b.CollectVectorGenerations(ctx, 30, true)
	code(t, e, "BACKEND_UNAVAILABLE")
	if p.Removed != 0 || !g.exists {
		t.Fatal(p)
	}
	gs, e := s.VectorGenerations(ctx)
	must(t, e)
	if len(gs) != 1 || gs[0].State != "deleting" {
		t.Fatal(gs)
	}
}
func TestV7VectorGCRetryAfterDescriptorFailure(t *testing.T) {
	s, b, g, _, d := setupGC7(t)
	retireGC7(t, s, d)
	g.failDescriptor = true
	_, e := b.CollectVectorGenerations(ctx, 30, true)
	code(t, e, "BACKEND_UNAVAILABLE")
	if g.deleted != 1 || g.exists {
		t.Fatal(g)
	}
	g.failDescriptor = false
	p, e := b.CollectVectorGenerations(ctx, 30, true)
	must(t, e)
	if p.Removed != 1 || g.deleted != 1 {
		t.Fatal(p, g)
	}
}
func TestV7VectorGCRejectsPendingJob(t *testing.T) {
	s, b, g, _, d := setupGC7(t)
	retireGC7(t, s, d)
	_, e := s.Enqueue(ctx, "demo", "build", "tester", "", map[string]any{})
	must(t, e)
	_, e = b.CollectVectorGenerations(ctx, 30, true)
	code(t, e, "WORKSPACE_BUSY")
	if len(g.requests) > 0 {
		t.Fatal(g.requests)
	}
}
func TestV7ExternalEditionPruneUsesPhysicalNameOnce(t *testing.T) {
	s, b, g, _, d := setupGC7(t)
	pub(t, s, "old.md", "new content")
	must(t, s.DB.Do(ctx, func(c *db.Conn) error {
		return c.Exec("UPDATE edition_retirement SET retired_at=? WHERE edition=?", time.Now().Add(-40*24*time.Hour).Unix(), d.Edition)
	}))
	p, e := b.PruneExternal(ctx, 30, true, false)
	must(t, e)
	if p.RemovedEditions != 1 {
		t.Fatal(p)
	}
	expected := "POST /collections/" + g.collection + "/points/delete?wait=true"
	found := false
	for _, r := range g.requests {
		if r == expected {
			found = true
		}
		if strings.Count(r, "xrag_demo_") > 1 && !strings.Contains(r, "xrag_demo_meta") {
			t.Fatal(r)
		}
	}
	if !found {
		t.Fatal(g.requests)
	}
	gs, e := s.VectorGenerations(ctx)
	must(t, e)
	if len(gs) != 1 || gs[0].RetiredAt == 0 {
		t.Fatal(gs)
	}
}
func TestV7CollectionExistenceMalformedResponseIsNotAbsence(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{}`)) }))
	defer ts.Close()
	q := adapters.Qdrant{Endpoint: adapters.Endpoint{URL: ts.URL}}
	_, e := q.CollectionExists(ctx, "test")
	code(t, e, "BACKEND_UNAVAILABLE")
}
