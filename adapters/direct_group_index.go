package adapters

import (
	"context"
	"github.com/BlueSkyXN/XRAG/core"
	"time"
)

func (g *DirectGroup) CompactSemantic() {}
func (g *DirectGroup) OriginID() string {
	for _, d := range g.Readers {
		if origin := d.OriginID(); origin != "" {
			return origin
		}
	}
	return ""
}
func (g *DirectGroup) CurrentReference(ctx context.Context, kb, path string) (core.Ref, error) {
	return core.Ref{}, core.Err("MODE_UNSUPPORTED", "direct requires an immutable chunk ref", 422)
}
func (g *DirectGroup) CanSelectIndex(action string, q core.Request) bool {
	if len(q.Kbs) == 0 {
		return false
	}
	for _, kb := range q.Kbs {
		d := g.Readers[kb]
		if d == nil || !d.CanSelectIndex(action, q) {
			return false
		}
	}
	return true
}
func (g *DirectGroup) SelectIndex(ctx context.Context, action string, q core.Request, limit int) (core.IndexSelection, error) {
	out := core.IndexSelection{Items: []core.Item{}, Dependencies: []core.Ref{}, Unit: "chunks", Basis: "candidate", Execution: "direct_native_query_then_page_hydration"}
	m := core.Unknown("chunks", "live backend query")
	out.Metric = &m
	for _, kb := range core.Unique(q.Kbs) {
		d := g.Readers[kb]
		if d == nil {
			return out, core.Err("PERMISSION_DENIED", "direct KB not configured", 403)
		}
		part := q
		part.Kbs = []string{kb}
		p, e := d.SelectIndex(ctx, action, part, limit)
		if e != nil {
			return out, e
		}
		if out.Origin != "" && out.Origin != p.Origin {
			return out, core.Err("REF_ORIGIN_MISMATCH", "direct group contains different authorities", 409)
		}
		out.Origin = p.Origin
		out.Approximate = out.Approximate || p.Approximate
		out.Notes = append(out.Notes, p.Notes...)
		for i, it := range p.Items {
			if (action == "search.semantic" || action == "+ask") && it.Ranking != "none" {
				it.Score = 1 / (60 + float64(i+1))
				it.Ranking = "direct_model_rank_fusion"
			}
			out.Items = append(out.Items, it)
		}
		out.SelectedTotal += p.SelectedTotal
	}
	sortDirectItems(out.Items)
	if action == "search.semantic" && len(out.Items) > 200 {
		out.Items = out.Items[:200]
		out.SelectedTotal = len(out.Items)
	}
	if len(out.Items) > limit {
		out.Items = out.Items[:limit]
	}
	for _, it := range out.Items {
		r, _ := core.DecodeRef(it.Ref)
		out.Dependencies = append(out.Dependencies, r)
	}
	out.Notes = core.Unique(out.Notes)
	return out, nil
}
func (g *DirectGroup) HydrateItems(ctx context.Context, items []core.Item) ([]core.Item, error) {
	out := append([]core.Item{}, items...)
	groups := map[string][]int{}
	for i, it := range items {
		if it.ChunkID != "" {
			groups[it.KB] = append(groups[it.KB], i)
		}
	}
	for kb, indexes := range groups {
		d := g.Readers[kb]
		if d == nil {
			return nil, core.Err("REF_ORIGIN_MISMATCH", "original direct binding is unavailable", 409)
		}
		selected := []core.Item{}
		for _, i := range indexes {
			selected = append(selected, items[i])
		}
		p, e := d.HydrateItems(ctx, selected)
		if e != nil {
			return nil, e
		}
		for j, i := range indexes {
			out[i] = p[j]
		}
	}
	return out, nil
}
func (g *DirectGroup) ViewFromReferences(ctx context.Context, refs []core.Ref) (core.View, error) {
	v := core.View{Mode: "direct", Compact: true, At: time.Now(), Documents: []core.Document{}}
	by := map[string][]core.Ref{}
	for _, r := range refs {
		by[r.KB] = append(by[r.KB], r)
	}
	for kb, rr := range by {
		d := g.Readers[kb]
		if d == nil {
			return v, core.Err("PERMISSION_DENIED", "direct binding unavailable", 403)
		}
		p, e := d.ViewFromReferences(ctx, rr)
		if e != nil {
			return v, e
		}
		if v.Origin != "" && v.Origin != p.Origin {
			return v, core.Err("REF_ORIGIN_MISMATCH", "different direct authorities", 409)
		}
		v.Origin = p.Origin
		v.Documents = append(v.Documents, p.Documents...)
	}
	return v, nil
}
func (g *DirectGroup) QueryView(ctx context.Context, kbs []string, refs []core.Ref, wiki bool) (core.View, error) {
	if refs != nil {
		return g.ViewFromReferences(ctx, refs)
	}
	v := core.View{Mode: "direct", Compact: true, At: time.Now(), Documents: []core.Document{}}
	for _, kb := range core.Unique(kbs) {
		d := g.Readers[kb]
		if d == nil {
			return v, core.Err("PERMISSION_DENIED", "KB not configured", 403)
		}
		p, e := d.QueryView(ctx, []string{kb}, nil, wiki)
		if e != nil {
			return v, e
		}
		if v.Origin != "" && v.Origin != p.Origin {
			return v, core.Err("REF_ORIGIN_MISMATCH", "different direct authorities", 409)
		}
		v.Origin = p.Origin
		v.Documents = append(v.Documents, p.Documents...)
	}
	return v, nil
}
func (g *DirectGroup) Discover(ctx context.Context, kbs []string) (map[string]any, error) {
	bindings := []map[string]any{}
	origin := ""
	for _, kb := range core.Unique(kbs) {
		d := g.Readers[kb]
		if d == nil {
			return nil, core.Err("PERMISSION_DENIED", "KB not configured", 403)
		}
		o, e := d.checkIdentity(ctx)
		if e != nil {
			return nil, e
		}
		if origin != "" && origin != o {
			return nil, core.Err("REF_ORIGIN_MISMATCH", "different direct authorities", 409)
		}
		origin = o
		bindings = append(bindings, map[string]any{"kb": kb, "text": d.Q != nil, "graph": d.Neo != nil, "physical_collection": d.Collection})
	}
	return map[string]any{"bindings": bindings, "origin_id": origin, "consistency": "backend_live", "metadata_only": true}, nil
}

func (g *DirectGroup) QuerySupports(ctx context.Context, action string, q core.Request, v core.View, chunks map[string]core.Chunk) ([]core.Support, error) {
	// Multiple bindings may contain the same name. Resolve the start globally
	// before fetching any adjacency, not once per backend.
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
	id := ""
	for kb := range directParts(v) {
		d := g.Readers[kb]
		if d == nil || d.Neo == nil {
			return nil, core.Err("CAPABILITY_UNAVAILABLE", "graph backend missing", 422)
		}
		found, err := d.Neo.ResolveEntity(ctx, kb, seed)
		if err != nil {
			return nil, err
		}
		if found != "" {
			if id != "" && id != found {
				return nil, core.Err("AMBIGUOUS_ENTITY", "use entity_id across KBs", 409)
			}
			id = found
		}
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
		out := []core.Support{}
		for kb, part := range directParts(v) {
			d := g.Readers[kb]
			if d == nil || d.Neo == nil {
				return nil, core.Err("CAPABILITY_UNAVAILABLE", "graph binding unavailable", 422)
			}
			ss, err := d.Neo.Frontier(ctx, part, front, dir, remaining-len(out))
			if err != nil {
				return nil, err
			}
			out = append(out, ss...)
		}
		return out, nil
	})
}
