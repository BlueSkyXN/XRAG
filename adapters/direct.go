package adapters

import (
	"context"
	"sync"
	"time"
	"github.com/BlueSkyXN/XRAG/core"
)

// Direct reads an explicitly named XRAG Demo physical collection. It never reads Server SQLite.
// The full schema/broad database compatibility promised by V0.7 is not claimed here.
type Direct struct {
	identityMu sync.Mutex
	Q          *Qdrant
	Collection string
	KB         string
	Origin     string
	CacheDir   string
	Neo        *Neo4j
}

func (d *Direct) Snapshot(ctx context.Context, kbs []string) (core.View, error) {
	v := core.View{Mode: "direct", At: time.Now(), Documents: []core.Document{}}
	origin, err := d.checkIdentity(ctx)
	if err != nil {
		return v, err
	}
	v.Origin = origin
	if !core.Subset(kbs, []string{d.KB}) {
		return v, core.Err("PERMISSION_DENIED", "direct profile is bound to one KB", 403)
	}
	if d.Q == nil {
		if d.Neo == nil {
			return v, core.Err("CAPABILITY_UNAVAILABLE", "direct requires a vector or graph backend", 422)
		}
		_, cs, err := d.Neo.ReadChunks(ctx, d.KB)
		if err != nil {
			return v, err
		}
		v.Origin = origin
		editions := map[string]int{}
		for _, c := range cs {
			idx, ok := editions[c.Edition]
			if !ok {
				idx = len(v.Documents)
				editions[c.Edition] = idx
				v.Documents = append(v.Documents, core.Document{KB: c.KB, Path: c.Path, Edition: c.Edition, Title: c.Title, FileHash: c.FileHash, Kind: "doc", Chunks: []core.Chunk{}})
			}
			v.Documents[idx].Chunks = append(v.Documents[idx].Chunks, c)
		}
		return v, nil
	}
	cs, o, e := d.Q.Scroll(ctx, d.Collection)
	if e != nil {
		return v, e
	}
	if len(cs) > 0 && o != origin {
		return v, core.Err("REF_ORIGIN_MISMATCH", "payload/descriptor origin mismatch", 409)
	}
	v.Origin = origin
	byEdition := map[string]int{}
	for _, c := range cs {
		idx, ok := byEdition[c.Edition]
		if !ok {
			idx = len(v.Documents)
			byEdition[c.Edition] = idx
			v.Documents = append(v.Documents, core.Document{KB: c.KB, Path: c.Path, Edition: c.Edition, FileHash: c.FileHash, Title: c.Title, Fields: c.Fields, Kind: "doc", Chunks: []core.Chunk{}})
		}
		v.Documents[idx].Chunks = append(v.Documents[idx].Chunks, c)
	}
	return v, nil
}
func (d *Direct) Epoch(ctx context.Context) (int64, error) { return 0, ctx.Err() }
func (d *Direct) Read(ctx context.Context, r core.Ref) (core.Document, *core.Chunk, string, error) {
	if r.ChunkID == "" {
		return core.Document{}, nil, "", core.Err("MODE_UNSUPPORTED", "direct exposes chunks, not complete original files", 422)
	}
	origin, err := d.checkIdentity(ctx)
	if err != nil {
		return core.Document{}, nil, "", err
	}
	if origin != r.Origin || r.KB != d.KB {
		return core.Document{}, nil, "", core.Err("REF_ORIGIN_MISMATCH", "reference does not belong to this direct binding", 409)
	}
	cs, err := d.fetchChunks(ctx, []string{r.ChunkID}, true)
	if err != nil {
		return core.Document{}, nil, "", err
	}
	if len(cs) != 1 {
		return core.Document{}, nil, "", core.Err("REF_GONE", "referenced point is no longer present", 410)
	}
	c := cs[0]
	if c.ID != r.ChunkID || c.Edition != r.Edition || c.Path != r.Path || c.FileHash != r.FileHash || (r.Generation != "" && c.Generation != r.Generation) {
		return core.Document{}, nil, "", core.Err("REF_IDENTITY_MISMATCH", "point identity changed", 409)
	}
	doc := chunkDocument(c)
	return doc, &c, "backend_live", nil
}
func (d *Direct) Lexical(ctx context.Context, v core.View, q string) ([]core.Ranked, error) { // Query Qdrant payload filtering, not a local relevance ranking.
	if d.Q == nil {
		return nil, core.Err("CAPABILITY_UNAVAILABLE", "Neo4j-only profile provides graph operations, not text search", 422)
	}
	var result struct {
		Result struct {
			Points []struct {
				ID string `json:"id"`
			} `json:"points"`
			Next any `json:"next_page_offset"`
		} `json:"result"`
	}
	rs := []core.Ranked{}
	var offset any
	for {
		body := map[string]any{"filter": map[string]any{"must": []any{map[string]any{"key": "text", "match": map[string]any{"text": q}}}}, "limit": 256, "with_payload": false}
		if offset != nil {
			body["offset"] = offset
		}
		result.Result.Next = nil
		if e := d.Q.call(ctx, "POST", "/collections/"+d.Collection+"/points/scroll", body, &result); e != nil {
			return nil, e
		}
		for _, p := range result.Result.Points {
			rs = append(rs, core.Ranked{ID: p.ID})
		}
		if len(rs) > 10000 {
			return nil, core.Err("RESULT_TOO_LARGE", "direct text filter exceeds limit", 422)
		}
		if result.Result.Next == nil {
			break
		}
		offset = result.Result.Next
	}
	return rs, nil
}
func (d *Direct) PutCache(ctx context.Context, id string, c core.Cache) error {
	return (&Local{CacheDir: d.CacheDir}).PutCache(ctx, id, c)
}
func (d *Direct) GetCache(ctx context.Context, id string) (core.Cache, error) {
	return (&Local{CacheDir: d.CacheDir}).GetCache(ctx, id)
}
func (d *Direct) ValidateView(ctx context.Context, v core.View) error {
	refs := []core.Ref{}
	for _, doc := range v.Documents {
		for _, ch := range doc.Chunks {
			ref, _ := core.DecodeRef(core.ChunkItem(ch, v.Origin).Ref)
			refs = append(refs, ref)
		}
	}
	_, err := d.ViewFromReferences(ctx, refs)
	return err
}
func (d *Direct) Label() string { return "qdrant_cosine" }
func (d *Direct) Search(ctx context.Context, v core.View, q string) ([]core.Ranked, error) {
	if d.Q == nil || d.Q.Embed == nil {
		return nil, core.Err("CAPABILITY_UNAVAILABLE", "direct semantic needs an embedding model", 422)
	}
	if d.Collection != d.Q.Collection(d.KB) {
		return nil, core.Err("MODEL_MISMATCH", "unexpected physical collection", 409)
	}
	return d.Q.Search(ctx, v, q)
}

func (d *Direct) Supports(ctx context.Context, v core.View) ([]core.Support, error) {
	if d.Neo == nil {
		return nil, core.Err("CAPABILITY_UNAVAILABLE", "graph backend is not configured", 422)
	}
	return d.Neo.Supports(ctx, v)
}
