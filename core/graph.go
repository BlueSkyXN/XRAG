package core

import (
	"sort"
	"strings"
)

func GraphQuery(action string, q Request, v View, chunks map[string]Chunk, ss []Support) ([]Item, string, bool, error) {
	if len(ss) > 20000 {
		return nil, "supports", false, Err("GRAPH_BUDGET_EXCEEDED", "raw support set exceeds 20000; narrow the query scope", 422)
	}
	entities := map[string]Item{}
	mentionIDs := map[string]map[string]bool{}
	addMention := func(kb, name, id string) {
		eid := EntityID(kb, name)
		it := entities[eid]
		it.Kind = "entity"
		it.KB = kb
		it.EntityID = eid
		it.Name = name
		if mentionIDs[eid] == nil {
			mentionIDs[eid] = map[string]bool{}
		}
		mentionIDs[eid][id] = true
		it.Mentions = len(mentionIDs[eid])
		entities[eid] = it
	}
	for id, c := range chunks {
		for _, n := range Mentions(c) {
			addMention(c.KB, n, id)
		}
	}
	for _, s := range ss {
		addMention(s.KB, s.From, s.ChunkID)
		addMention(s.KB, s.To, s.ChunkID)
	}
	list := []Item{}
	for _, it := range entities {
		list = append(list, it)
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].Mentions != list[j].Mentions {
			return list[i].Mentions > list[j].Mentions
		}
		return list[i].EntityID < list[j].EntityID
	})
	if action == "graph.entity" {
		out := []Item{}
		for _, it := range list {
			if q.Name == "" || strings.Contains(strings.ToLower(it.Name), strings.ToLower(q.Name)) {
				out = append(out, it)
			}
		}
		return out, "entities", true, nil
	}
	resolve := func(x string) (string, error) {
		if _, ok := entities[x]; ok {
			return x, nil
		}
		ids := []string{}
		for id, it := range entities {
			if it.Name == x {
				ids = append(ids, id)
			}
		}
		if len(ids) > 1 {
			return "", Err("AMBIGUOUS_ENTITY", "same name exists in multiple knowledge bases; use entity_id", 409)
		}
		if len(ids) == 1 {
			return ids[0], nil
		}
		return "", nil
	}
	seed := q.ID
	if seed == "" {
		seed = q.Name
	}
	if seed == "" {
		seed = q.Q
	}
	if action == "graph.path" {
		seed = q.FromID
	}
	from, er := resolve(seed)
	if er != nil {
		return nil, "", false, er
	}
	if from == "" {
		unit := "items"
		switch action {
		case "graph.path":
			unit = "paths"
		case "graph.neighbors":
			unit = "relations"
		case "graph.chunks":
			unit = "chunks"
		}
		return []Item{}, unit, true, nil
	}
	if action == "graph.chunks" || action == "+context" {
		out := []Item{}
		if action == "+context" {
			out = append(out, entities[from])
		}
		for id := range mentionIDs[from] {
			out = append(out, ChunkItem(chunks[id], v.Origin))
		}
		sortItems(out)
		if action == "graph.chunks" {
			return out, "chunks", true, nil
		}
		neighbors, _, complete, err := GraphQuery("graph.neighbors", Request{ID: from, Depth: 1, Direction: "both", MaxExpansions: q.MaxExpansions}, v, chunks, ss)
		out = append(out, neighbors...)
		for _, d := range v.Documents {
			if d.Kind != "wiki" {
				continue
			}
			if !strings.Contains(strings.ToLower(d.Title+" "+d.Body), strings.ToLower(entities[from].Name)) {
				continue
			}
			for _, c := range d.Chunks {
				if _, ok := chunks[c.ID]; ok {
					it := ChunkItem(c, v.Origin)
					it.Kind = "wiki"
					out = append(out, it)
					break
				}
			}
		}
		return out, "items", complete, err
	}
	target := ""
	if action == "graph.path" {
		target, er = resolve(q.ToID)
		if er != nil {
			return nil, "", false, er
		}
		if target == "" {
			return []Item{}, "paths", true, nil
		}
	}
	if q.Direction == "" {
		q.Direction = "both"
	}
	if q.Direction != "both" && q.Direction != "in" && q.Direction != "out" {
		return nil, "", false, Err("INVALID_ARGUMENT", "direction must be in, out, or both", 400)
	}
	hops := q.MaxHops
	if action == "graph.neighbors" {
		hops = q.Depth
	}
	if hops == 0 {
		hops = 2
	}
	if hops < 1 || hops > 5 {
		return nil, "", false, Err("INVALID_ARGUMENT", "graph depth must be 1..5", 400)
	}
	budget := q.MaxExpansions
	if budget == 0 {
		budget = 5000
	}
	type edge struct {
		a, b string
		s    []Support
	}
	group := map[string]*edge{}
	for _, s := range ss {
		a, b := EntityID(s.KB, s.From), EntityID(s.KB, s.To)
		key := a + "/" + s.Relation + "/" + b
		if group[key] == nil {
			group[key] = &edge{a: a, b: b}
		}
		group[key].s = append(group[key].s, s)
	}
	keys := []string{}
	for k := range group {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	adj := map[string][]string{}
	for _, key := range keys {
		ed := group[key]
		if q.Direction == "out" || q.Direction == "both" {
			adj[ed.a] = append(adj[ed.a], key)
		}
		if (q.Direction == "in" || q.Direction == "both") && (q.Direction != "both" || ed.a != ed.b) {
			adj[ed.b] = append(adj[ed.b], key)
		}
	}
	type walk struct {
		node  string
		nodes []string
		edges []Support
	}
	front := []walk{{from, []string{from}, nil}}
	out := []Item{}
	seenEdges := map[string]bool{}
	expansions := 0
	complete := true
	for depth := 0; depth < hops && len(front) > 0; depth++ {
		next := []walk{}
		for _, w := range front {
			for _, key := range adj[w.node] {
				ed := group[key]
				dest := ""
				if (q.Direction == "out" || q.Direction == "both") && ed.a == w.node {
					dest = ed.b
				}
				if (q.Direction == "in" || q.Direction == "both") && ed.b == w.node {
					dest = ed.a
				}
				if dest == "" {
					continue
				}
				expansions++
				if expansions > budget {
					complete = false
					break
				}
				cycle := false
				for _, n := range w.nodes {
					if n == dest {
						cycle = true
					}
				}
				if cycle {
					continue
				}
				edges := append(append([]Support{}, w.edges...), ed.s...)
				nodes := append(append([]string{}, w.nodes...), dest)
				refs := []string{}
				for _, s := range edges {
					refs = append(refs, ChunkItem(chunks[s.ChunkID], v.Origin).Ref)
				}
				if action == "graph.path" && dest == target {
					out = append(out, Item{Kind: "path", Nodes: nodes, Edges: edges, Refs: Unique(refs), Supports: len(edges)})
				}
				if action == "graph.neighbors" && !seenEdges[key] {
					seenEdges[key] = true
					names := ed.s[0]
					out = append(out, Item{Kind: "relation", KB: names.KB, From: names.From, To: names.To, Relation: names.Relation, Supports: len(ed.s), Edges: ed.s, Refs: Unique(refs)})
				}
				next = append(next, walk{dest, nodes, edges})
			}
			if !complete {
				break
			}
		}
		if !complete || action == "graph.path" && len(out) > 0 {
			break
		}
		front = next
	}
	if action == "graph.path" {
		return out, "paths", complete, nil
	}
	return out, "relations", complete, nil
}
