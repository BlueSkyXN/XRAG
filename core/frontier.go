package core

import (
	"context"
	"sort"
)

// FrontierGraph restricts remote graph reads before local path assembly. The
// supplied View and chunks already have the requested publication and filters.
type FrontierGraph interface {
	QuerySupports(context.Context, string, Request, View, map[string]Chunk) ([]Support, error)
}

// ExpandFrontier fetches only outgoing/incoming adjacency for each reached
// frontier. It never relaxes the caller's chunk scope. A budget failure is an
// error, not a silently shortened relation catalog.
func ExpandFrontier(ctx context.Context, action string, q Request, v View, chunks map[string]Chunk, fetch func(context.Context, []string, string, int) ([]Support, error)) ([]Support, error) {
	if action != "graph.path" && action != "graph.neighbors" {
		return nil, Err("MODE_UNSUPPORTED", "frontier applies to paths/neighbors", 422)
	}
	dir := q.Direction
	if dir == "" {
		dir = "both"
	}
	if dir != "both" && dir != "in" && dir != "out" {
		return nil, Err("INVALID_ARGUMENT", "invalid direction", 400)
	}
	hops := q.MaxHops
	seed := q.FromID
	if action == "graph.neighbors" {
		hops = q.Depth
		seed = q.ID
		if seed == "" {
			seed = q.Name
		}
		if seed == "" {
			seed = q.Q
		}
	}
	if hops == 0 {
		hops = 2
	}
	if hops < 1 || hops > 5 {
		return nil, Err("INVALID_ARGUMENT", "depth must be 1..5", 400)
	}
	entities := map[string]string{}
	for _, c := range chunks {
		for _, name := range Mentions(c) {
			entities[EntityID(c.KB, name)] = name
		}
	}
	for _, d := range v.Documents {
		for _, s := range d.Supports {
			if _, ok := chunks[s.ChunkID]; ok {
				entities[EntityID(s.KB, s.From)] = s.From
				entities[EntityID(s.KB, s.To)] = s.To
			}
		}
	}
	start := ""
	if _, ok := entities[seed]; ok {
		start = seed
	} else {
		for id, name := range entities {
			if name == seed {
				if start != "" && start != id {
					return nil, Err("AMBIGUOUS_ENTITY", "name exists in multiple KBs; use entity_id", 409)
				}
				start = id
			}
		}
	}
	// The caller may know an ID from an earlier response even when metadata has
	// no explicit textual mentions. It is still filtered by the scope at fetch.
	if start == "" && len(seed) > 3 && seed[:3] == "en_" {
		start = seed
	}
	if start == "" {
		return []Support{}, nil
	}
	front := []string{start}
	expanded := map[string]bool{}
	seen := map[string]bool{}
	out := []Support{}
	for depth := 0; depth < hops && len(front) > 0; depth++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		fs := map[string]bool{}
		for _, id := range front {
			fs[id] = true
			expanded[id] = true
		}
		ss, err := fetch(ctx, front, dir, 20000-len(out))
		if err != nil {
			return nil, err
		}
		next := map[string]bool{}
		for _, s := range ss {
			c, ok := chunks[s.ChunkID]
			if !ok || s.KB != c.KB || s.Edition != c.Edition || s.Path != c.Path {
				return nil, Err("BACKEND_FILTER_MISMATCH", "graph support outside published/filter scope", 503)
			}
			a, b := EntityID(s.KB, s.From), EntityID(s.KB, s.To)
			incident := false
			if (dir == "out" || dir == "both") && fs[a] {
				incident = true
				if !expanded[b] {
					next[b] = true
				}
			}
			if (dir == "in" || dir == "both") && fs[b] {
				incident = true
				if !expanded[a] {
					next[a] = true
				}
			}
			if !incident {
				return nil, Err("BACKEND_FILTER_MISMATCH", "non-frontier edge returned", 503)
			}
			if !seen[s.ID] {
				out = append(out, s)
				seen[s.ID] = true
				if len(out) > 20000 {
					return nil, Err("GRAPH_BUDGET_EXCEEDED", "frontier support budget", 422)
				}
			}
		}
		front = front[:0]
		for id := range next {
			front = append(front, id)
		}
		sort.Strings(front)
		if len(front) > 5000 {
			return nil, Err("GRAPH_BUDGET_EXCEEDED", "frontier exceeds 5000 entities", 422)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}
