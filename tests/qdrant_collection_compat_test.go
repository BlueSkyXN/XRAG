package tests

import (
	"encoding/json"
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

func TestServerSemanticReadsMixedLegacyAndSparseCollections(t *testing.T) {
	s, _ := setup(t)
	points := map[string]map[string]bool{}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/embeddings" {
			json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"index": 0, "embedding": []float64{1, 0}}}})
			return
		}
		ids, ok := points[r.URL.Path]
		if !ok {
			http.Error(w, "collection not found", http.StatusNotFound)
			return
		}
		var q struct {
			Filter struct {
				Must []struct {
					IDs []string `json:"has_id"`
				} `json:"must"`
			} `json:"filter"`
		}
		if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
			t.Error(err)
		}
		matches := []any{}
		for _, filter := range q.Filter.Must {
			for _, id := range filter.IDs {
				if ids[id] {
					matches = append(matches, map[string]any{"id": id, "score": 1})
				}
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"points": matches}})
	}))
	defer ts.Close()
	cfg := adapters.Config{Embedding: &adapters.Endpoint{URL: ts.URL, Model: "fixture", Dimension: 2}, Qdrant: &adapters.Endpoint{URL: ts.URL}}
	q := adapters.Qdrant{Origin: s.Origin, Endpoint: *cfg.Qdrant, Embed: &adapters.Embedder{Config: *cfg.Embedding}}
	modern := q.Collection("demo")
	legacy := strings.TrimSuffix(modern, "_"+core.LexicalSchema)
	must(t, s.SaveGeneration(ctx, "demo", cfg))
	b := &build.Builder{Store: s, Config: cfg}
	for i, collection := range []string{legacy, modern} {
		d := parsed(t, collection+".md", "preserved vector evidence")
		ids := map[string]bool{}
		for j := range d.Chunks {
			d.Chunks[j].Generation = cfg.Embedding.SemanticID()
			d.Chunks[j].Vector = []float64{1, 0}
			d.Chunks[j].VectorState = "ready"
			ids[d.Chunks[j].ID] = true
		}
		points["/collections/"+collection+"/points/query"] = ids
		must(t, s.Stage(ctx, d))
		must(t, s.BindProjections(ctx, d, store.ProjectionBinding{Vectors: []adapters.Config{cfg}}))
		if i == 0 {
			// Recreate the persisted binding written before sparse collection names.
			must(t, s.DB.Do(ctx, func(c *db.Conn) error {
				if err := c.Exec("UPDATE projection_generation SET registry_id='legacy' WHERE edition=?", d.Edition); err != nil {
					return err
				}
				return c.Exec("UPDATE vector_registry SET id='legacy',physical=? WHERE physical=?", legacy, modern)
			}))
		}
		must(t, s.Publish(ctx, d, store.Job{}, "", ""))
		engine, err := b.EngineFor(ctx, []string{"demo"})
		must(t, err)
		result := run(t, engine, "search.semantic", core.Request{Q: "evidence", Limit: 10})
		if result.Returned != i+1 {
			t.Fatalf("after %d collections: got %d results, want %d", i+1, result.Returned, i+1)
		}
	}
}
