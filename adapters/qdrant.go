package adapters

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/BlueSkyXN/XRAG/core"
	"sort"
	"strings"
)

func SortRanks(rs []core.Ranked) {
	sort.Slice(rs, func(i, j int) bool {
		if rs[i].Score == rs[j].Score {
			return rs[i].ID < rs[j].ID
		}
		return rs[i].Score > rs[j].Score
	})
}

type Qdrant struct {
	Endpoint Endpoint
	Embed    *Embedder
	Origin   string
}

func (q *Qdrant) headers() (map[string]string, error) {
	key, e := Secret(q.Endpoint.Key)
	h := map[string]string{}
	if key != "" {
		h["api-key"] = key
	}
	return h, e
}
func (q *Qdrant) call(ctx context.Context, method, path string, body, out any) error {
	h, e := q.headers()
	if e != nil {
		return e
	}
	return Call(ctx, method, strings.TrimRight(q.Endpoint.URL, "/")+path, h, body, out)
}
func (q *Qdrant) Collection(kb string) string {
	return q.legacyCollection(kb) + "_" + core.LexicalSchema
}
func (q *Qdrant) legacyCollection(kb string) string {
	return "xrag_demo_" + core.Hash(q.Origin)[:8] + "_" + kb + "_" + q.Embed.Config.SemanticID()[:12]
}
func (q *Qdrant) ensure(ctx context.Context, name string, dim int, lexical bool) error {
	var res struct {
		Result struct {
			Exists bool `json:"exists"`
		} `json:"result"`
	}
	if e := q.call(ctx, "GET", "/collections/"+name+"/exists", nil, &res); e != nil {
		return e
	}
	if !res.Result.Exists {
		body := map[string]any{"vectors": map[string]any{"size": dim, "distance": "Cosine"}}
		if lexical {
			body["sparse_vectors"] = map[string]any{core.LexicalVectorName: map[string]any{"modifier": "idf"}}
		}
		return q.call(ctx, "PUT", "/collections/"+name, body, nil)
	}
	return nil
}
func (q *Qdrant) Write(ctx context.Context, d core.Document) error {
	collection := q.Collection(d.KB)
	if e := q.ensure(ctx, collection, q.Embed.Config.Dimension, true); e != nil {
		return e
	}
	if e := q.call(ctx, "PUT", "/collections/"+collection+"/index?wait=true", map[string]any{"field_name": "text", "field_schema": map[string]any{"type": "text", "tokenizer": "word", "lowercase": true}}, nil); e != nil {
		return e
	}
	if e := q.ensure(ctx, "xrag_demo_meta", 1, false); e != nil {
		return e
	}
	descriptor := map[string]any{"physical_collection": collection, "payload_schema": "demo8", "model_identity": q.Embed.Config.SemanticID(), "model": q.Embed.Config.Model, "dimension": q.Embed.Config.Dimension, "revision": q.Embed.Config.Revision, "origin_id": q.Origin, "kb": d.KB, "doc_prefix": q.Embed.Config.DocPrefix, "query_prefix": q.Embed.Config.QueryPrefix}
	descriptor["lexical_schema"] = core.LexicalSchema
	descriptor["lexical_avgdl"] = core.LexicalAverageLength
	if e := q.call(ctx, "PUT", "/collections/xrag_demo_meta/points?wait=true", map[string]any{"points": []any{map[string]any{"id": core.UUID(collection), "vector": []float64{1}, "payload": descriptor}}}, nil); e != nil {
		return e
	}
	pts := []any{}
	for _, c := range d.Chunks {
		if len(c.Vector) == 0 {
			continue
		}
		payload := map[string]any{}
		_ = json.Unmarshal([]byte(core.JSON(c)), &payload)
		delete(payload, "vector")
		payload["origin_id"] = q.Origin
		payload["physical_collection"] = collection
		payload["query_mentions"] = append([]string{}, core.Mentions(c)...)
		pts = append(pts, map[string]any{"id": c.ID, "vector": map[string]any{"": c.Vector, core.LexicalVectorName: core.LexicalSparse(c.Title+" "+c.Text, false)}, "payload": payload})
	}
	if len(pts) == 0 {
		return nil
	}
	return q.call(ctx, "PUT", "/collections/"+collection+"/points?wait=true", map[string]any{"points": pts}, nil)
}
func (q *Qdrant) CompactSemantic() {}
func (q *Qdrant) Label() string    { return "qdrant_cosine" }
func (q *Qdrant) Search(ctx context.Context, v core.View, text string) ([]core.Ranked, error) {
	return q.searchCollection(ctx, v, text, "")
}
func (q *Qdrant) searchCollection(ctx context.Context, v core.View, text, physical string) ([]core.Ranked, error) {
	vec, _, e := q.Embed.Embed(ctx, []string{text}, true)
	if e != nil {
		return nil, e
	}
	perKB := map[string][]string{}
	for _, d := range v.Documents {
		for _, c := range d.Chunks {
			if c.Generation != q.Embed.Config.SemanticID() {
				return nil, core.Err("MODEL_MISMATCH", "active file uses a different embedding profile", 409)
			}
			perKB[d.KB] = append(perKB[d.KB], c.ID)
		}
	}
	out := []core.Ranked{}
	for kb, ids := range perKB {
		collection := physical
		if collection == "" {
			collection = q.Collection(kb)
		}
		for start := 0; start < len(ids); start += 500 {
			end := start + 500
			if end > len(ids) {
				end = len(ids)
			}
			var res struct {
				Result struct {
					Points []struct {
						ID    string  `json:"id"`
						Score float64 `json:"score"`
					} `json:"points"`
				} `json:"result"`
			}
			e = q.call(ctx, "POST", "/collections/"+collection+"/points/query", map[string]any{"query": vec[0], "filter": map[string]any{"must": []any{map[string]any{"has_id": ids[start:end]}}}, "limit": 200, "with_payload": false}, &res)
			if e != nil {
				return nil, e
			}
			for _, p := range res.Result.Points {
				out = append(out, core.Ranked{ID: p.ID, Score: p.Score})
			}
		}
	}
	SortRanks(out)
	if len(out) > 200 {
		out = out[:200]
	}
	return out, nil
}
func (q *Qdrant) Descriptor(ctx context.Context, collection string) (map[string]any, error) {
	var res struct {
		Result struct {
			Payload map[string]any `json:"payload"`
		} `json:"result"`
	}
	e := q.call(ctx, "GET", "/collections/xrag_demo_meta/points/"+core.UUID(collection), nil, &res)
	if e != nil {
		return nil, e
	}
	if res.Result.Payload["physical_collection"] != collection {
		return nil, core.Err("MODEL_MISMATCH", "descriptor does not identify requested physical collection", 409)
	}
	return res.Result.Payload, nil
}
func (q *Qdrant) Scroll(ctx context.Context, collection string) ([]core.Chunk, string, error) {
	out := []core.Chunk{}
	var offset any
	origin := ""
	for {
		var res struct {
			Result struct {
				Points []struct {
					Payload json.RawMessage `json:"payload"`
				} `json:"points"`
				Next any `json:"next_page_offset"`
			} `json:"result"`
		}
		req := map[string]any{"limit": 256, "with_payload": true, "with_vector": false}
		if offset != nil {
			req["offset"] = offset
		}
		if e := q.call(ctx, "POST", "/collections/"+collection+"/points/scroll", req, &res); e != nil {
			return nil, "", e
		}
		for _, pt := range res.Result.Points {
			var c core.Chunk
			if e := json.Unmarshal(pt.Payload, &c); e != nil {
				return nil, "", e
			}
			var m map[string]any
			_ = json.Unmarshal(pt.Payload, &m)
			if c.ID == "" || c.Edition == "" {
				return nil, "", core.Err("MODEL_MISMATCH", "not an XRAG payload", 409)
			}
			if x, ok := m["origin_id"].(string); ok {
				if origin != "" && origin != x {
					return nil, "", core.Err("REF_ORIGIN_MISMATCH", "mixed data origins in collection", 409)
				}
				origin = x
			}
			out = append(out, c)
		}
		if len(out) > 10000 {
			return nil, "", core.Err("VIEW_TOO_LARGE", "direct mode limits scroll to 10000 chunks", 422)
		}
		if res.Result.Next == nil {
			break
		}
		if fmt.Sprint(res.Result.Next) == fmt.Sprint(offset) {
			return nil, "", core.Err("BACKEND_UNAVAILABLE", "non-advancing scroll cursor", 503)
		}
		offset = res.Result.Next
	}
	return out, origin, nil
}

func (q *Qdrant) PublishAlias(ctx context.Context, kb string) error {
	alias := "xrag_" + core.Hash(q.Origin)[:8] + "_" + kb
	var res struct {
		Result struct {
			Aliases []struct {
				Alias string `json:"alias_name"`
			}
		} `json:"result"`
	}
	if e := q.call(ctx, "GET", "/aliases", nil, &res); e != nil {
		return e
	}
	actions := []any{}
	for _, a := range res.Result.Aliases {
		if a.Alias == alias {
			actions = append(actions, map[string]any{"delete_alias": map[string]any{"alias_name": alias}})
		}
	}
	actions = append(actions, map[string]any{"create_alias": map[string]any{"alias_name": alias, "collection_name": q.Collection(kb)}})
	return q.call(ctx, "POST", "/collections/aliases", map[string]any{"actions": actions}, nil)
}
func (q *Qdrant) DeleteEdition(ctx context.Context, kb, edition string) error {
	return q.call(ctx, "POST", "/collections/"+q.Collection(kb)+"/points/delete?wait=true", map[string]any{"filter": map[string]any{"must": []any{map[string]any{"key": "edition_id", "match": map[string]any{"value": edition}}}}}, nil)
}
