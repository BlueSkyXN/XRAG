package adapters

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/BlueSkyXN/XRAG/core"
	"sort"
	"strings"
	"time"
)

var directMetadataFields = []string{"chunk_id", "chunk_key", "kb", "path", "edition_id", "file_hash", "title", "heading_path", "char_start", "char_end", "line_start", "line_end", "fields", "generation", "origin_id", "physical_collection", "query_mentions", "vector_state", "graph_state"}

type directPoint struct {
	ID      string          `json:"id"`
	Score   float64         `json:"score"`
	Payload json.RawMessage `json:"payload"`
}

func chunkDocument(c core.Chunk) core.Document {
	return core.Document{KB: c.KB, Path: c.Path, Edition: c.Edition, FileHash: c.FileHash, Title: c.Title, Fields: c.Fields, Kind: "doc", Chunks: []core.Chunk{c}}
}

func (d *Direct) checkIdentity(ctx context.Context) (string, error) {
	d.identityMu.Lock()
	defer d.identityMu.Unlock()
	if d.Q == nil {
		if d.Neo == nil || d.Origin == "" {
			return "", core.Err("REF_ORIGIN_MISMATCH", "Neo4j direct requires an explicit origin_id", 409)
		}
		return d.Origin, nil
	}
	desc, e := d.Q.Descriptor(ctx, d.Collection)
	if e != nil {
		return "", e
	}
	origin, _ := desc["origin_id"].(string)
	if origin == "" || desc["kb"] != d.KB || (d.Origin != "" && d.Origin != origin) {
		return "", core.Err("REF_ORIGIN_MISMATCH", "direct collection identity changed", 409)
	}
	if d.Q.Embed != nil && desc["model_identity"] != d.Q.Embed.Config.SemanticID() {
		return "", core.Err("MODEL_MISMATCH", "query embedding does not match the fixed collection", 409)
	}
	// The direct profile normally contains origin_id. Empty profile identity can
	// be discovered, but no URL or database is chosen from returned content.
	if d.Origin == "" {
		d.Origin = origin
	}
	if d.Q.Origin == "" {
		d.Q.Origin = origin
	}
	if d.Q.Origin != origin {
		return "", core.Err("REF_ORIGIN_MISMATCH", "query binding identity changed", 409)
	}
	if d.Neo != nil {
		if d.Neo.Origin == "" {
			d.Neo.Origin = origin
		}
		if d.Neo.Origin != origin {
			return "", core.Err("REF_ORIGIN_MISMATCH", "graph binding identity changed", 409)
		}
	}
	return origin, nil
}
func (d *Direct) point(p directPoint) (core.Chunk, error) {
	var c core.Chunk
	var m map[string]any
	if json.Unmarshal(p.Payload, &c) != nil || json.Unmarshal(p.Payload, &m) != nil {
		return c, core.Err("BACKEND_UNAVAILABLE", "invalid point payload", 503)
	}
	if c.ID == "" || c.ID != p.ID || c.KB != d.KB || c.Edition == "" || c.Path == "" || c.FileHash == "" || m["origin_id"] != d.Origin || m["physical_collection"] != d.Collection {
		return c, core.Err("REF_ORIGIN_MISMATCH", "point/collection identity mismatch", 409)
	}
	return c, nil
}
func (d *Direct) fetchChunks(ctx context.Context, ids []string, text bool) ([]core.Chunk, error) {
	out := []core.Chunk{}
	if len(ids) == 0 {
		return out, nil
	}
	if d.Q == nil {
		return d.Neo.ChunksByID(ctx, d.KB, ids)
	}
	payload := any(directMetadataFields)
	if text {
		payload = true
	}
	for start := 0; start < len(ids); start += 256 {
		end := min(start+256, len(ids))
		var res struct {
			Result []directPoint `json:"result"`
		}
		if e := d.Q.call(ctx, "POST", "/collections/"+d.Collection+"/points", map[string]any{"ids": ids[start:end], "with_payload": payload, "with_vector": false}, &res); e != nil {
			return nil, e
		}
		allowed := map[string]bool{}
		seen := map[string]bool{}
		for _, id := range ids[start:end] {
			allowed[id] = true
		}
		for _, p := range res.Result {
			if !allowed[p.ID] || seen[p.ID] {
				return nil, core.Err("BACKEND_UNAVAILABLE", "unexpected/duplicate returned point", 503)
			}
			seen[p.ID] = true
			c, e := d.point(p)
			if e != nil {
				return nil, e
			}
			out = append(out, c)
		}
	}
	return out, nil
}
func (d *Direct) OriginID() string { d.identityMu.Lock(); defer d.identityMu.Unlock(); return d.Origin }
func (d *Direct) CurrentReference(ctx context.Context, kb, path string) (core.Ref, error) {
	return core.Ref{}, core.Err("MODE_UNSUPPORTED", "direct exposes chunks by ref, not complete files by path", 422)
}
func (d *Direct) HydrateItems(ctx context.Context, items []core.Item) ([]core.Item, error) {
	if _, e := d.checkIdentity(ctx); e != nil {
		return nil, e
	}
	ids := []string{}
	for _, it := range items {
		if it.ChunkID != "" {
			ids = append(ids, it.ChunkID)
		}
	}
	cs, e := d.fetchChunks(ctx, core.Unique(ids), true)
	if e != nil {
		return nil, e
	}
	by := map[string]core.Chunk{}
	for _, c := range cs {
		by[c.ID] = c
	}
	out := append([]core.Item{}, items...)
	for i, it := range out {
		if it.ChunkID == "" {
			continue
		}
		c, ok := by[it.ChunkID]
		if !ok {
			return nil, core.Err("REF_GONE", "saved result point was removed", 410)
		}
		ref, err := core.DecodeRef(it.Ref)
		if err != nil {
			return nil, err
		}
		if ref.Origin != d.Origin || c.KB != ref.KB || c.Path != ref.Path || c.Edition != ref.Edition || c.FileHash != ref.FileHash || (ref.Generation != "" && ref.Generation != c.Generation) {
			return nil, core.Err("REF_IDENTITY_MISMATCH", "saved point identity changed", 409)
		}
		out[i].Text = c.Text
		avail := !c.TextMissing
		out[i].TextAvailable = &avail
	}
	return out, nil
}
func (d *Direct) ViewFromReferences(ctx context.Context, refs []core.Ref) (core.View, error) {
	origin, e := d.checkIdentity(ctx)
	v := core.View{Origin: origin, Mode: "direct", Compact: true, At: time.Now(), Documents: []core.Document{}}
	if e != nil {
		return v, e
	}
	ids := []string{}
	expected := map[string]core.Ref{}
	for _, r := range refs {
		if r.Origin != origin || r.KB != d.KB || r.ChunkID == "" {
			return v, core.Err("REF_ORIGIN_MISMATCH", "scope is not a chunk scope in this binding", 409)
		}
		ids = append(ids, r.ChunkID)
		expected[r.ChunkID] = r
	}
	cs, e := d.fetchChunks(ctx, core.Unique(ids), false)
	if e != nil {
		return v, e
	}
	if len(cs) != len(expected) {
		return v, core.Err("HANDLE_STALE", "saved scope lost a point", 409)
	}
	for _, c := range cs {
		r := expected[c.ID]
		if c.Path != r.Path || c.Edition != r.Edition || c.FileHash != r.FileHash {
			return v, core.Err("REF_IDENTITY_MISMATCH", "scope point changed", 409)
		}
		v.Documents = append(v.Documents, chunkDocument(c))
	}
	return v, nil
}
func (d *Direct) QueryView(ctx context.Context, kbs []string, refs []core.Ref, wiki bool) (core.View, error) {
	if !core.Subset(kbs, []string{d.KB}) {
		return core.View{}, core.Err("PERMISSION_DENIED", "KB not configured", 403)
	}
	if refs != nil {
		return d.ViewFromReferences(ctx, refs)
	}
	origin, e := d.checkIdentity(ctx)
	v := core.View{Origin: origin, Mode: "direct", Compact: true, At: time.Now(), Documents: []core.Document{}}
	if e != nil {
		return v, e
	}
	if d.Q == nil {
		_, cs, e := d.Neo.ReadChunks(ctx, d.KB)
		for _, c := range cs {
			v.Documents = append(v.Documents, chunkDocument(c))
		}
		return v, e
	}
	var offset any
	seenOffsets := map[string]bool{}
	seenPoints := map[string]bool{}
	for {
		var res struct {
			Result struct {
				Points []directPoint `json:"points"`
				Next   any           `json:"next_page_offset"`
			} `json:"result"`
		}
		body := map[string]any{"filter": map[string]any{"must": []any{map[string]any{"key": "kb", "match": map[string]any{"value": d.KB}}, map[string]any{"key": "origin_id", "match": map[string]any{"value": origin}}}}, "limit": 256, "with_payload": directMetadataFields, "with_vector": false}
		if offset != nil {
			body["offset"] = offset
		}
		if e = d.Q.call(ctx, "POST", "/collections/"+d.Collection+"/points/scroll", body, &res); e != nil {
			return v, e
		}
		for _, p := range res.Result.Points {
			if seenPoints[p.ID] {
				return v, core.Err("BACKEND_UNAVAILABLE", "duplicate point in live scroll", 503)
			}
			seenPoints[p.ID] = true
			c, er := d.point(p)
			if er != nil {
				return v, er
			}
			if c.MentionNames == nil {
				return v, core.Err("PAYLOAD_SCHEMA_UNSUPPORTED", "rebuild with demo8 metadata before compact direct graph queries", 409)
			}
			v.Documents = append(v.Documents, chunkDocument(c))
			if len(v.Documents) > 200000 {
				return v, core.Err("VIEW_TOO_LARGE", "direct metadata exceeds 200000 points; narrow the binding", 422)
			}
		}
		if res.Result.Next == nil {
			break
		}
		key := fmt.Sprint(res.Result.Next)
		if seenOffsets[key] {
			return v, core.Err("BACKEND_UNAVAILABLE", "repeating backend cursor", 503)
		}
		seenOffsets[key] = true
		offset = res.Result.Next
	}
	return v, nil
}
func (d *Direct) CompactSemantic() {}
func directFilter(q core.Request, kb, origin string) (map[string]any, error) {
	must := []any{map[string]any{"key": "kb", "match": map[string]any{"value": kb}}, map[string]any{"key": "origin_id", "match": map[string]any{"value": origin}}}
	for _, f := range q.Filters {
		if (f.Field != "path" && f.Field != "kb" && f.Field != "title" && !strings.HasPrefix(f.Field, "fields.")) || (f.Op != "eq" && f.Op != "in") {
			return nil, core.Err("FILTER_UNSUPPORTED", "native direct supports eq/in; use scoped metadata route for other filters", 422)
		}
		match := map[string]any{"value": f.Value}
		if values, ok := f.Value.([]any); ok {
			if len(values) == 0 {
				return nil, core.Err("INVALID_ARGUMENT", "empty match list", 400)
			}
			for _, value := range values {
				if _, ok := value.(string); !ok {
					return nil, core.Err("FILTER_UNSUPPORTED", "native any requires string values", 422)
				}
			}
			match = map[string]any{"any": values}
		}
		must = append(must, map[string]any{"key": f.Field, "match": match})
	}
	return map[string]any{"must": must}, nil
}
func (d *Direct) CanSelectIndex(action string, q core.Request) bool {
	if d.Q == nil || q.Within != "" {
		return false
	}
	if action != "search.keyword" && action != "search.semantic" && action != "+ask" {
		return false
	}
	if action == "+ask" && (core.Bool(q.Graph) || core.Bool(q.External) || core.Bool(q.Rerank)) {
		return false
	}
	for _, f := range q.Filters {
		if f.Op != "eq" && f.Op != "in" {
			return false
		}
		switch f.Value.(type) {
		case string, bool:
		case []any:
			for _, value := range f.Value.([]any) {
				if _, ok := value.(string); !ok {
					return false
				}
			}
		default:
			return false
		}
	}
	return true
}
func (d *Direct) SelectIndex(ctx context.Context, action string, q core.Request, capacity int) (core.IndexSelection, error) {
	if action == "+ask" {
		return d.selectAsk(ctx, q, capacity)
	}
	return d.selectIndex(ctx, action, q, capacity, nil)
}

func (d *Direct) selectIndex(ctx context.Context, action string, q core.Request, capacity int, ids []string) (core.IndexSelection, error) {
	out := core.IndexSelection{Items: []core.Item{}, Dependencies: []core.Ref{}, Unit: "chunks", Basis: "candidate", Execution: "backend_query_then_page_hydration", Notes: []string{"Direct reads a live backend, including retained/staging points; it does not inherit Server publication or withdrawal state."}}
	origin, e := d.checkIdentity(ctx)
	out.Origin = origin
	if e != nil {
		return out, e
	}
	if strings.TrimSpace(q.Q) == "" {
		return out, core.Err("INVALID_ARGUMENT", "q is required", 400)
	}
	filter, e := directFilter(q, d.KB, origin)
	if e != nil {
		return out, e
	}
	if ids != nil {
		filter["must"] = append(filter["must"].([]any), map[string]any{"has_id": ids})
	}
	var pts []directPoint
	metric := core.Unknown("chunks", "live backend query")
	out.Metric = &metric
	if action == "search.lexical" {
		vec := core.LexicalSparse(q.Q, true)
		if len(vec.Indices) == 0 {
			return out, core.Err("INVALID_ARGUMENT", "query needs at least one term", 400)
		}
		var res struct {
			Result struct {
				Points []directPoint `json:"points"`
			} `json:"result"`
		}
		e = d.Q.call(ctx, "POST", "/collections/"+d.Collection+"/points/query", map[string]any{"using": core.LexicalVectorName, "query": vec, "filter": filter, "limit": min(200, capacity), "with_payload": directMetadataFields, "with_vector": false}, &res)
		if e != nil {
			return out, e
		}
		pts = res.Result.Points
	} else if action == "search.semantic" {
		if d.Q.Embed == nil {
			return out, core.Err("CAPABILITY_UNAVAILABLE", "direct semantic requires its matching embedding model", 422)
		}
		vec, _, er := d.Q.Embed.Embed(ctx, []string{q.Q}, true)
		if er != nil {
			return out, er
		}
		var res struct {
			Result struct {
				Points []directPoint `json:"points"`
			} `json:"result"`
		}
		e = d.Q.call(ctx, "POST", "/collections/"+d.Collection+"/points/query", map[string]any{"query": vec[0], "filter": filter, "limit": min(200, capacity), "with_payload": directMetadataFields, "with_vector": false}, &res)
		if e != nil {
			return out, e
		}
		pts = res.Result.Points
		out.Approximate = true
	} else {
		must := filter["must"].([]any)
		filter["must"] = append(must, map[string]any{"key": "text", "match": map[string]any{"text": q.Q}})
		var offset any
		seen := map[string]bool{}
		for len(pts) <= capacity {
			var res struct {
				Result struct {
					Points []directPoint `json:"points"`
					Next   any           `json:"next_page_offset"`
				} `json:"result"`
			}
			body := map[string]any{"filter": filter, "limit": min(256, capacity+1-len(pts)), "with_payload": directMetadataFields, "with_vector": false}
			if offset != nil {
				body["offset"] = offset
			}
			if e = d.Q.call(ctx, "POST", "/collections/"+d.Collection+"/points/scroll", body, &res); e != nil {
				return out, e
			}
			pts = append(pts, res.Result.Points...)
			if res.Result.Next == nil {
				break
			}
			key := fmt.Sprint(res.Result.Next)
			if seen[key] {
				return out, core.Err("BACKEND_UNAVAILABLE", "repeating backend cursor", 503)
			}
			seen[key] = true
			offset = res.Result.Next
		}
	}
	allowed := map[string]bool{}
	for _, id := range ids {
		allowed[id] = true
	}
	seen := map[string]bool{}
	per := map[string]int{}
	for _, p := range pts {
		if ids != nil && !allowed[p.ID] {
			return out, core.Err("BACKEND_FILTER_MISMATCH", "backend returned a candidate outside the requested scope", 503)
		}
		if seen[p.ID] {
			return out, core.Err("BACKEND_UNAVAILABLE", "duplicate candidate", 503)
		}
		seen[p.ID] = true
		c, er := d.point(p)
		if er != nil {
			return out, er
		}
		ok, er := core.MatchesFilters(c, q.Filters)
		if er != nil {
			return out, er
		}
		if !ok {
			return out, core.Err("BACKEND_FILTER_MISMATCH", "backend returned a candidate outside the requested filter", 503)
		}
		perKey := c.KB + ":" + c.Path
		if q.PerFile > 0 && per[perKey] >= q.PerFile {
			continue
		}
		per[perKey]++
		it := core.ChunkItem(c, origin)
		it.Text = ""
		it.Score = p.Score
		it.Ranking = "none"
		if action == "search.semantic" {
			it.Ranking = "qdrant_cosine"
		} else if action == "search.lexical" {
			it.Ranking = "qdrant_bm25"
		}
		out.Items = append(out.Items, it)
	}
	out.SelectedTotal = len(out.Items)
	if len(pts) > capacity {
		out.SelectedTotal = max(out.SelectedTotal, capacity+1)
	}
	if len(out.Items) > capacity {
		out.Items = out.Items[:capacity]
	}
	if action == "search.lexical" {
		sortDirectItems(out.Items)
	}
	if action == "search.keyword" {
		sort.Slice(out.Items, func(i, j int) bool { return out.Items[i].ChunkID < out.Items[j].ChunkID })
	}
	for _, it := range out.Items {
		ref, _ := core.DecodeRef(it.Ref)
		out.Dependencies = append(out.Dependencies, ref)
	}
	return out, nil
}

// selectAsk uses backend candidates, never an unfiltered corpus scroll. Optional
// graph/rerank branches deliberately retain the shared planner until selected.
func (d *Direct) selectAsk(ctx context.Context, q core.Request, capacity int) (core.IndexSelection, error) {
	part := q
	part.PerFile = 0
	lex, err := d.selectLexical(ctx, part, min(capacity, 200), nil)
	if err != nil {
		return lex, err
	}
	out := lex
	out.Execution = "direct_native_combination_then_page_hydration"
	out.Dependencies = nil
	out.Items = nil
	for i := range lex.Items {
		lex.Items[i].Routes = []string{"keyword"}
	}
	routes := [][]core.Item{lex.Items}
	if d.Q.Embed != nil {
		sem, e := d.SelectIndex(ctx, "search.semantic", part, min(capacity, 200))
		if e != nil {
			return out, e
		}
		out.Approximate = true
		for i := range sem.Items {
			sem.Items[i].Routes = []string{"semantic"}
		}
		routes = append(routes, sem.Items)
	}
	out.Items = core.FuseRanked(routes)
	per := q.PerFile
	if per == 0 {
		per = 3
	}
	counts := map[string]int{}
	kept := []core.Item{}
	for _, it := range out.Items {
		k := it.KB + ":" + it.Path
		if counts[k] < per {
			kept = append(kept, it)
			counts[k]++
		}
	}
	out.Items = kept
	out.SelectedTotal = len(kept)
	if len(out.Items) > capacity {
		out.Items = out.Items[:capacity]
	}
	for _, it := range out.Items {
		r, _ := core.DecodeRef(it.Ref)
		out.Dependencies = append(out.Dependencies, r)
	}
	return out, nil
}

func (d *Direct) QuerySupports(ctx context.Context, action string, q core.Request, v core.View, chunks map[string]core.Chunk) ([]core.Support, error) {
	if d.Neo == nil {
		return nil, core.Err("CAPABILITY_UNAVAILABLE", "Neo4j required", 422)
	}
	seed := q.FromID
	if action == "graph.neighbors" {
		seed = q.ID
		if seed == "" {
			seed = q.Name
		}
		if seed == "" {
			seed = q.Q
		}
	}
	id, err := d.Neo.ResolveEntity(ctx, d.KB, seed)
	if err != nil {
		return nil, err
	}
	if id == "" {
		return []core.Support{}, nil
	}
	if action == "graph.path" {
		q.FromID = id
	} else {
		q.ID = id
	}
	return core.ExpandFrontier(ctx, action, q, v, chunks, func(ctx context.Context, front []string, dir string, remaining int) ([]core.Support, error) {
		return d.Neo.Frontier(ctx, v, front, dir, remaining)
	})
}
