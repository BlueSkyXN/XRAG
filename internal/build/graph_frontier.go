package build

import (
	"context"
	"github.com/BlueSkyXN/XRAG/adapters"
	"github.com/BlueSkyXN/XRAG/core"
)

func (g scopedGraph) QuerySupports(ctx context.Context, action string, q core.Request, v core.View, chunks map[string]core.Chunk) ([]core.Support, error) {
	groups := map[string]core.View{}
	endpoints := map[string]*adapters.NeoConfig{}
	local := []core.Support{}
	for _, d := range v.Documents {
		b, _, _, err := g.Builder.Store.ProjectionBinding(ctx, d.Edition)
		if err != nil && core.AsError(err).Code != "NOT_FOUND" {
			return nil, err
		}
		if err != nil || b.Neo4j == nil {
			for _, s := range d.Supports {
				if _, ok := chunks[s.ChunkID]; ok {
					local = append(local, s)
				}
			}
			continue
		}
		key := core.Hash(b.Neo4j)
		part := groups[key]
		part.Origin = v.Origin
		part.Documents = append(part.Documents, d)
		groups[key] = part
		endpoints[key] = b.Neo4j
	}
	return core.ExpandFrontier(ctx, action, q, v, chunks, func(ctx context.Context, front []string, dir string, remaining int) ([]core.Support, error) {
		fs := map[string]bool{}
		for _, id := range front {
			fs[id] = true
		}
		out := []core.Support{}
		for _, s := range local {
			a, b := core.EntityID(s.KB, s.From), core.EntityID(s.KB, s.To)
			if ((dir == "out" || dir == "both") && fs[a]) || ((dir == "in" || dir == "both") && fs[b]) {
				out = append(out, s)
			}
		}
		if len(out) > remaining {
			return nil, core.Err("GRAPH_BUDGET_EXCEEDED", "local frontier limit", 422)
		}
		for key, part := range groups {
			n := adapters.Neo4j{Config: *endpoints[key], Origin: v.Origin}
			ss, err := n.Frontier(ctx, part, front, dir, remaining-len(out))
			if err != nil {
				return nil, err
			}
			out = append(out, ss...)
		}
		return out, nil
	})
}
