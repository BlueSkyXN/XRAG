package core

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

type Engine struct {
	Repo     Repository
	Semantic Semantic
	Graph    GraphReader
	Rerank   Reranker
	Mode     string
	MaxItems int
	External ExternalSearch
}
type scopeData struct {
	Refs  []Ref    `json:"refs,omitempty"`
	Files bool     `json:"files,omitempty"`
	View  View     `json:"view"`
	IDs   []string `json:"ids"`
	Basis string   `json:"basis"`
}
type resultData struct {
	ViewEpoch    int64    `json:"view_epoch"`
	Lazy         bool     `json:"lazy,omitempty"`
	Response     Response `json:"response"`
	Items        []Item   `json:"items"`
	Dependencies []Ref    `json:"dependencies,omitempty"`
	Request      Request  `json:"request"`
}

func (e *Engine) capacity() int {
	if e.MaxItems > 0 {
		return e.MaxItems
	}
	return 20000
}
func Subset(a, b []string) bool {
	set := map[string]bool{}
	for _, v := range b {
		set[v] = true
	}
	for _, v := range a {
		if !set[v] {
			return false
		}
	}
	return true
}
func (e *Engine) load(ctx context.Context, id, kind, owner string, kbs []string) (Cache, error) {
	c, err := e.Repo.GetCache(ctx, id)
	if err != nil {
		return c, err
	}
	if c.Kind != kind || c.Owner != owner {
		return c, Err("PERMISSION_DENIED", "this result belongs to another request identity", 403)
	}
	if time.Now().Unix() >= c.Expires {
		return c, Err(strings.ToUpper(kind)+"_EXPIRED", "saved object expired; make an explicit new query", 410)
	}
	if !Subset(c.Kbs, kbs) {
		return c, Err("PERMISSION_DENIED", "saved scope exceeds current access", 403)
	}
	epoch, err := e.Repo.Epoch(ctx)
	if err != nil {
		return c, err
	}
	if epoch != c.Epoch {
		return c, Err("VIEW_INVALIDATED", "a source was withdrawn after this result was created", 409)
	}
	return c, nil
}
func (e *Engine) Run(ctx context.Context, action string, q Request, owner string) (Response, error) {
	if l, ok := e.Repo.(OperationLeaser); ok {
		leaseCtx, release, err := l.BeginOperation(ctx)
		if err != nil {
			return Response{}, err
		}
		defer release()
		ctx = leaseCtx
	}
	start := time.Now()
	resp := Response{SchemaVersion: "0.7-demo", QueryID: ID("q_"), Action: action, Mode: e.Mode, Kbs: q.Kbs, Items: []Item{}, Notes: []string{}, CacheComplete: true, Strategy: map[string]any{"name": action, "graph": false, "rerank": false, "external": false}}
	if q.Limit == 0 {
		q.Limit = 10
	}
	if q.MaxChars == 0 {
		q.MaxChars = 8000
	}
	if q.Limit < 1 || q.Limit > 100 || q.MaxChars < 1 || q.MaxChars > 200000 || q.MaxHops > 5 || q.MaxExpansions > 100000 || q.Offset < 0 || q.Length < 0 {
		return resp, Err("INVALID_ARGUMENT", "limit or range outside allowed bounds", 400)
	}
	if Bool(q.Graph) && (e.Mode == "local" || (e.Mode == "direct" && e.Graph == nil)) {
		return resp, Err("CAPABILITY_UNAVAILABLE", "selected mode has no graph reader", 422)
	}
	if action == "results.page" {
		return e.Page(ctx, q, owner)
	}
	if action == "handles.describe" {
		c, er := e.load(ctx, q.ID, "handle", owner, q.Kbs)
		if er != nil {
			return resp, er
		}
		var h scopeData
		if er = json.Unmarshal(c.Data, &h); er != nil {
			return resp, er
		}
		resp.Data = map[string]any{"handle": q.ID, "members": scopeMembers(h), "set_semantics": h.Basis, "expires_at": c.Expires}
		resp.Total = Count(scopeMembers(h), scopeUnit(h), h.Basis)
		resp.Coverage = Coverage{ScopeBasis: h.Basis, Consistency: consistency(e.Mode)}
		return resp, nil
	}
	if action == "files.read" {
		return e.read(ctx, q, owner, resp)
	}
	if action == "kb.discover" {
		if m, ok := e.Repo.(MetadataReader); ok {
			data, err := m.Discover(ctx, q.Kbs)
			if err != nil {
				return resp, err
			}
			data["version"] = Version
			data["semantic"] = e.Semantic != nil
			data["graph"] = e.Mode == "server" || e.Graph != nil
			data["external"] = e.External != nil
			data["investigation_in_core"] = false
			resp.Data = data
			resp.Total = Unknown("files", "registered metadata")
			resp.Coverage = Coverage{Consistency: consistency(e.Mode), ScopeBasis: "corpus"}
			return resp, nil
		}
	}
	nativeIndex := action == "search.keyword" || action == "files.tree"
	if n, ok := e.Repo.(NativeIndexSelector); ok {
		nativeIndex = n.CanSelectIndex(action, q)
	}
	if ir, ok := e.Repo.(IndexedReader); ok && q.Within == "" && nativeIndex {
		return e.indexed(ctx, action, q, owner, resp, ir)
	}
	var view View
	var err error
	qr, canCompact := e.Repo.(CompactReader)
	_, canHydrate := e.Repo.(IndexedReader)
	canCompact = canCompact && canHydrate && action != "files.grep"
	if canCompact && e.Semantic != nil && (action == "search.semantic" || action == "+ask") {
		_, canCompact = e.Semantic.(CompactSemantic)
	}
	if q.Within == "" {
		if canCompact {
			view, err = qr.QueryView(ctx, q.Kbs, nil, action == "+context")
		} else {
			view, err = e.Repo.Snapshot(ctx, q.Kbs)
		}
		if err != nil {
			return resp, err
		}
	}
	resp.Coverage = Coverage{Consistency: consistency(e.Mode), ScopeBasis: "corpus"}
	allowed := map[string]bool{}
	basis := "corpus"
	if q.Within != "" {
		c, er := e.load(ctx, q.Within, "handle", owner, q.Kbs)
		if er != nil {
			return resp, er
		}
		var h scopeData
		if er = json.Unmarshal(c.Data, &h); er != nil {
			return resp, er
		}
		if len(h.Refs) > 0 {
			ir, ok := e.Repo.(IndexedReader)
			if !ok {
				return resp, Err("CAPABILITY_UNAVAILABLE", "saved scope requires indexed reader", 422)
			}
			if canCompact {
				h.View, er = qr.QueryView(ctx, q.Kbs, h.Refs, action == "+context")
			} else {
				h.View, er = ir.ViewFromReferences(ctx, h.Refs)
			}
			if er != nil {
				return resp, er
			}
			if h.Files {
				for _, d := range h.View.Documents {
					for _, ch := range d.Chunks {
						h.IDs = append(h.IDs, ch.ID)
					}
				}
			}
		}
		if validator, ok := e.Repo.(interface {
			ValidateView(context.Context, View) error
		}); ok {
			if er := validator.ValidateView(ctx, h.View); er != nil {
				return resp, er
			}
		}
		view = h.View
		basis = h.Basis
		for _, id := range h.IDs {
			allowed[id] = true
		}
	}
	chunks := map[string]Chunk{}
	files := map[string]Document{}
	allKB := map[string]bool{}
	for _, d := range view.Documents {
		allKB[d.KB] = true
		files[d.KB+":"+d.Path] = d
		for _, c := range d.Chunks {
			if q.Within != "" && !allowed[c.ID] {
				continue
			}
			matches := true
			for _, f := range q.Filters {
				ok, er := MatchFilter(c, f)
				if er != nil {
					return resp, er
				}
				if !ok {
					matches = false
					break
				}
			}
			if matches {
				chunks[c.ID] = c
			}
		}
	}
	// Reject unknown registered fields rather than returning a misleading empty result.
	for _, f := range q.Filters {
		if strings.HasPrefix(f.Field, "fields.") {
			found := false
			for _, d := range view.Documents {
				if _, ok := d.Fields[strings.TrimPrefix(f.Field, "fields.")]; ok {
					found = true
				}
			}
			if !found {
				return resp, Err("FILTER_UNSUPPORTED", "unknown field: "+f.Field, 422)
			}
		}
	}
	if !view.Compact && len(chunks) > e.capacity() {
		return resp, Err("VIEW_TOO_LARGE", "Demo view exceeds configured item limit; narrow the knowledge bases", 422)
	}
	resp.Coverage.ScopeBasis = basis
	selected := []string{}
	uncappedTotal := -1
	items := []Item{}
	unit := "chunks"
	complete := true
	trace := []Step{{Stage: "published_view", Count: len(chunks), Note: "file editions selected before retrieval"}}
	if view.Compact {
		resp.Strategy["execution"] = "metadata_projection_then_page_hydration"
		trace = append(trace, Step{Stage: "query_projection", Count: len(chunks), Note: "chunk metadata and graph supports; corpus text and vectors are not in the View"})
	}
	appendRanks := func(rs []Ranked, label string) []Item {
		out := []Item{}
		for _, r := range rs {
			if c, ok := chunks[r.ID]; ok {
				it := ChunkItem(c, view.Origin)
				it.Score = r.Score
				it.Ranking = label
				out = append(out, it)
			}
		}
		return out
	}
	switch action {
	case "kb.discover":
		fields := map[string]bool{}
		n := 0
		for _, d := range view.Documents {
			for k := range d.Fields {
				fields[k] = true
			}
			n += len(d.Chunks)
		}
		fs := []string{}
		for k := range fields {
			fs = append(fs, k)
		}
		sort.Strings(fs)
		resp.Data = map[string]any{"version": Version, "origin_id": view.Origin, "documents": len(view.Documents), "chunks": n, "fields": fs, "semantic": e.Semantic != nil, "graph": e.Mode == "server" || e.Graph != nil, "graph_backend": "explicit supports, or configured Neo4j", "keyword": map[string]any{"server": "SQLite FTS5 BM25, character/bigram fallback", "local": "scan heuristic", "direct": "Qdrant payload text filtering"}[e.Mode], "knowledge_write": e.Mode == "server", "investigation_in_core": false, "wiki_generation": "requires configured chat endpoint", "mode_limits": "MD/TXT; +ask retrieves, does not answer"}
		resp.Total = Count(len(view.Documents), "files", basis)
		return resp, nil
	case "files.tree":
		unit = "files"
		keys := []string{}
		for k := range files {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			d := files[k]
			ids := []string{}
			for _, c := range d.Chunks {
				if _, ok := chunks[c.ID]; ok {
					ids = append(ids, c.ID)
				}
			}
			if len(ids) == 0 {
				continue
			}
			selected = append(selected, ids...)
			items = append(items, Item{Kind: "file", KB: d.KB, Path: d.Path, Edition: d.Edition, FileHash: d.FileHash, Title: d.Title, Fields: d.Fields, Ref: EncodeRef(Ref{Origin: view.Origin, KB: d.KB, Path: d.Path, Edition: d.Edition, FileHash: d.FileHash})})
		}
	case "files.grep":
		pattern := q.Pattern
		if pattern == "" {
			pattern = q.Q
		}
		if pattern == "" || len(pattern) > 512 {
			return resp, Err("INVALID_ARGUMENT", "a regex of 1..512 bytes is required", 400)
		}
		re, er := regexp.Compile(pattern)
		if er != nil {
			return resp, Err("INVALID_ARGUMENT", "invalid RE2 regular expression", 400)
		}
		unit = "matches"
		for _, d := range view.Documents {
			spans := allowedSpans(d, chunks)
			text := []rune(d.Body)
			for _, span := range spans {
				sub := string(text[span[0]:span[1]])
				for _, hit := range re.FindAllStringIndex(sub, -1) {
					lo := span[0] + utf8.RuneCountInString(sub[:hit[0]])
					hi := span[0] + utf8.RuneCountInString(sub[:hit[1]])
					if hi == lo {
						continue
					}
					if len(items) >= e.capacity() {
						return resp, Err("RESULT_TOO_LARGE", "grep match count exceeds result capacity; narrow scope", 422)
					}
					ref := Ref{Origin: view.Origin, KB: d.KB, Path: d.Path, Edition: d.Edition, FileHash: d.FileHash, RangeStart: &lo, RangeEnd: &hi}
					line := strings.Count(string(text[:lo]), "\n") + 1
					items = append(items, Item{Kind: "match", KB: d.KB, Path: d.Path, Edition: d.Edition, FileHash: d.FileHash, Title: d.Title, Text: string(text[lo:hi]), LineStart: line, LineEnd: line + strings.Count(string(text[lo:hi]), "\n"), CharStart: lo, CharEnd: hi, Ref: EncodeRef(ref)})
					for _, c := range d.Chunks {
						if _, ok := chunks[c.ID]; ok && c.Start < hi && c.End > lo {
							selected = append(selected, c.ID)
						}
					}
				}
			}
		}
		sortItems(items)
	case "search.keyword", "search.semantic", "+ask":
		if strings.TrimSpace(q.Q) == "" {
			return resp, Err("INVALID_ARGUMENT", "q is required", 400)
		}
		if action == "search.semantic" {
			if e.Semantic == nil {
				return resp, Err("CAPABILITY_UNAVAILABLE", "configure an embedding endpoint before semantic search", 422)
			}
			rs, er := e.Semantic.Search(ctx, subsetView(view, chunks), q.Q)
			if er != nil {
				return resp, er
			}
			items = appendRanks(rs, e.Semantic.Label())
			complete = false
			resp.Coverage.Approximate = true
		} else {
			rs, er := e.Repo.Lexical(ctx, view, q.Q)
			if er != nil {
				return resp, er
			}
			items = appendRanks(rs, "bm25")
			if e.Mode == "local" {
				for i := range items {
					items[i].Ranking = "scan_heuristic"
				}
			}
			if e.Mode == "direct" {
				complete = false
				for i := range items {
					items[i].Ranking = "none"
				}
			}
			trace = append(trace, Step{Stage: "keyword", Count: len(items), IDs: itemIDs(items, 30)})
			if action == "+ask" {
				complete = false
				if view.Compact && len(items) > 200 {
					items = items[:200]
					trace = append(trace, Step{Stage: "keyword_candidate_window", Count: len(items), Note: "hybrid retrieval uses at most 200 lexical candidates; not an exhaustive result"})
				}
				routes := [][]Item{items}
				if e.Semantic != nil {
					rs, er := e.Semantic.Search(ctx, subsetView(view, chunks), q.Q)
					if er != nil {
						resp.Coverage.ExecutionPartial = true
						resp.Notes = append(resp.Notes, "semantic branch unavailable: "+AsError(er).Code)
					} else {
						sem := appendRanks(rs, e.Semantic.Label())
						routes = append(routes, sem)
						trace = append(trace, Step{Stage: "semantic", Count: len(sem), IDs: itemIDs(sem, 30)})
					}
				} else {
					resp.Notes = append(resp.Notes, "Semantic is not configured; this run uses lexical retrieval.")
				}
				if Bool(q.Graph) {
					if e.Mode == "local" {
						return resp, Err("MODE_UNSUPPORTED", "local mode has no graph index", 422)
					}
					supports, er := e.supports(ctx, view, chunks)
					if er != nil {
						return resp, er
					}
					g := []Item{}
					seen := map[string]bool{}
					for _, s := range supports {
						if strings.Contains(strings.ToLower(q.Q), strings.ToLower(s.From)) || strings.Contains(strings.ToLower(q.Q), strings.ToLower(s.To)) {
							if !seen[s.ChunkID] {
								g = append(g, ChunkItem(chunks[s.ChunkID], view.Origin))
								seen[s.ChunkID] = true
							}
						}
					}
					sortItems(g)
					routes = append(routes, g)
					resp.Strategy["graph"] = true
					trace = append(trace, Step{Stage: "graph_supplement", Count: len(g)})
				}
				if Bool(q.External) {
					if e.External == nil {
						return resp, Err("CAPABILITY_UNAVAILABLE", "no external search provider configured", 422)
					}
					if q.Within != "" {
						return resp, Err("FILTER_UNSUPPORTED", "within is a local scope; external federation cannot widen it", 422)
					}
					ext, er := e.External.SearchExternal(ctx, q)
					if er != nil {
						return resp, er
					}
					routes = append(routes, ext)
					resp.Strategy["external"] = true
					resp.Coverage.Consistency = "mixed_live"
					trace = append(trace, Step{Stage: "external", Count: len(ext)})
				}
				items = Fuse(routes)
				resp.Strategy["ranked_routes"] = len(routes)
				if e.Mode == "direct" { // direct lexical is deliberately not fused as a relevance-ranked route
					ranked := [][]Item{}
					for _, r := range routes {
						if len(r) > 0 && r[0].Ranking != "none" {
							ranked = append(ranked, r)
						}
					}
					items = Fuse(ranked)
					seen := map[string]bool{}
					for _, it := range items {
						seen[it.ChunkID] = true
					}
					for _, it := range routes[0] {
						if !seen[it.ChunkID] {
							items = append(items, it)
						}
					}
				}
				trace = append(trace, Step{Stage: "fusion", Count: len(items), IDs: itemIDs(items, 30)})
				if Bool(q.Rerank) {
					if e.Rerank == nil {
						return resp, Err("CAPABILITY_UNAVAILABLE", "reranker is not configured", 422)
					}
					if view.Compact {
						if len(items) > 200 {
							items = items[:200]
						}
						items, er = e.Repo.(IndexedReader).HydrateItems(ctx, items)
						if er != nil {
							return resp, er
						}
					}
					items, er = e.Rerank.Rank(ctx, q.Q, items)
					if er != nil {
						return resp, er
					}
					resp.Strategy["rerank"] = true
					trace = append(trace, Step{Stage: "rerank", Count: len(items), IDs: itemIDs(items, 100)})
				}
				if q.PerFile == 0 {
					q.PerFile = 3
				}
				counts := map[string]int{}
				kept := []Item{}
				for _, it := range items {
					key := it.KB + ":" + it.Path
					if counts[key] < q.PerFile {
						kept = append(kept, it)
						counts[key]++
					}
				}
				trace = append(trace, Step{Stage: "per_file", Count: len(kept), Note: fmt.Sprintf("removed %d candidates by per-file limit", len(items)-len(kept)), IDs: itemIDs(kept, 100)})
				items = kept
			}
		}
	case "graph.entity", "graph.neighbors", "graph.path", "graph.chunks", "+context":
		if e.Mode == "local" {
			return resp, Err("MODE_UNSUPPORTED", "local mode scans documents; it does not construct graph indexes", 422)
		}
		if e.Mode == "direct" && e.Graph == nil {
			return resp, Err("CAPABILITY_UNAVAILABLE", "direct graph queries require a Neo4j connection", 422)
		}
		var ss []Support
		var er error
		if fg, ok := e.Graph.(FrontierGraph); ok && (action == "graph.path" || action == "graph.neighbors") {
			ss, er = fg.QuerySupports(ctx, action, q, subsetView(view, chunks), chunks)
			trace = append(trace, Step{Stage: "graph_frontier", Count: len(ss), Note: "adjacency fetched per reached frontier; support filters precede traversal"})
		} else {
			ss, er = e.supports(ctx, view, chunks)
		}
		if er != nil {
			return resp, er
		}
		items, unit, complete, er = GraphQuery(action, q, view, chunks, ss)
		if er != nil {
			return resp, er
		}
		if action == "graph.path" {
			resp.Notes = append(resp.Notes, "Paths are bounded by the requested hop/expansion limits, not an exhaustive business relation catalog.")
		}
		if !complete {
			resp.Coverage.ExecutionPartial = true
			resp.Notes = append(resp.Notes, "Bounded graph traversal: not a claim about every path in the graph.")
		}
		for _, it := range items {
			for _, s := range it.Edges {
				selected = append(selected, s.ChunkID)
			}
		}
	case "search.external":
		if e.External == nil {
			return resp, Err("CAPABILITY_UNAVAILABLE", "no external search provider configured", 422)
		}
		if q.Within != "" {
			return resp, Err("FILTER_UNSUPPORTED", "local scope handles cannot be applied to a different retrieval service", 422)
		}
		var er error
		items, er = e.External.SearchExternal(ctx, q)
		if er != nil {
			return resp, er
		}
		unit = "external_results"
		complete = false
		resp.Coverage.Consistency = "upstream_live"
		resp.Strategy["external"] = true
	case "scope.expand":
		if q.Within == "" {
			return resp, Err("INVALID_ARGUMENT", "within is required for expansion", 400)
		}
		names := map[string]bool{}
		for _, c := range chunks {
			for _, n := range Mentions(c) {
				names[n] = true
			}
		}
		var newView View
		var er error
		if canCompact {
			newView, er = qr.QueryView(ctx, q.Kbs, nil, false)
		} else {
			newView, er = e.Repo.Snapshot(ctx, q.Kbs)
		}
		if er != nil {
			return resp, er
		}
		chunks = map[string]Chunk{}
		for _, d := range newView.Documents {
			for _, c := range d.Chunks {
				matches, er := MatchesFilters(c, q.Filters)
				if er != nil {
					return resp, er
				}
				if !matches {
					continue
				}
				for _, n := range Mentions(c) {
					if names[n] {
						chunks[c.ID] = c
						break
					}
				}
			}
		}
		for _, c := range chunks {
			items = append(items, ChunkItem(c, newView.Origin))
		}
		sortItems(items)
		view = newView
		basis = "expanded"
		resp.Coverage.ScopeBasis = basis
		resp.Notes = append(resp.Notes, "Scope was explicitly expanded to mentions in the selected knowledge bases.")
	default:
		return resp, Err("MODE_UNSUPPORTED", "unknown or unimplemented action: "+action, 422)
	}
	if action == "search.keyword" && q.PerFile > 0 {
		uncappedTotal = len(items)
		counts := map[string]int{}
		kept := []Item{}
		for _, it := range items {
			key := it.KB + ":" + it.Path
			if counts[key] < q.PerFile {
				kept = append(kept, it)
				counts[key]++
			}
		}
		if len(kept) < len(items) {
			resp.Notes = append(resp.Notes, "per_file limits selected results, not the complete keyword count")
		}
		items = kept
	}
	if len(items) > e.capacity() {
		return resp, Err("RESULT_TOO_LARGE", "Demo result cache capacity exceeded; narrow the scope", 422)
	}
	for _, it := range items {
		if it.ChunkID != "" {
			selected = append(selected, it.ChunkID)
		}
		if it.Kind == "entity" {
			for id, c := range chunks {
				for _, name := range Mentions(c) {
					if EntityID(c.KB, name) == it.EntityID {
						selected = append(selected, id)
					}
				}
			}
		}
	}
	selected = Unique(selected)
	if complete {
		resp.Total = Count(len(items), unit, basis)
		if uncappedTotal >= 0 {
			resp.Total = Count(uncappedTotal, unit, basis)
		}
	} else {
		resp.Total = Unknown(unit, basis)
	}
	if (action == "+ask" || action == "search.semantic") && basis == "corpus" {
		basis = "candidate"
	}
	if q.RequireComplete && !complete {
		return resp, Err("INCOMPLETE_RESULT", "this action cannot promise a complete corpus result", 422)
	}
	resultID := ID("rs_")
	for i := range items {
		if items[i].Ref == "" {
			items[i].Ref = EncodeRef(Ref{Origin: view.Origin, ResultID: resultID, Index: i})
		}
	}
	if q.SaveScope != "none" && len(selected) > 0 {
		if len(selected) > e.capacity() {
			if q.SaveScope == "required" {
				return resp, Err("HANDLE_TOO_LARGE", "scope exceeds saved member limit", 422)
			}
		} else {
			h := scopeData{View: view, IDs: selected, Basis: basis}
			if view.Compact {
				h.View = View{}
				for _, id := range selected {
					if c, ok := chunks[id]; ok {
						h.Refs = append(h.Refs, Ref{Origin: view.Origin, KB: c.KB, Path: c.Path, Edition: c.Edition, FileHash: c.FileHash, ChunkID: c.ID})
					}
				}
			}
			c := Cache{Kind: "handle", Owner: owner, Expires: time.Now().Add(24 * time.Hour).Unix(), Epoch: view.Epoch, Kbs: q.Kbs, Data: json.RawMessage(JSON(h))}
			resp.Handle = ID("h_")
			if er := e.Repo.PutCache(ctx, resp.Handle, c); er != nil {
				return resp, er
			}
		}
	}
	epoch, er := e.Repo.Epoch(ctx)
	if er != nil {
		return resp, er
	}
	if epoch != view.Epoch {
		return resp, Err("VIEW_INVALIDATED", "source withdrawn during query", 409)
	}
	resp.ResultID = resultID
	resp.SummaryRef = EncodeRef(Ref{Origin: view.Origin, ResultID: resultID, Index: -1})
	if q.Trace {
		trace = append(trace, Step{Stage: "result", Count: len(items), DurationMS: time.Since(start).Milliseconds()})
		resp.Trace = trace
	}
	deps := []Ref{}
	for _, d := range view.Documents {
		deps = append(deps, Ref{Origin: view.Origin, KB: d.KB, Path: d.Path, Edition: d.Edition, FileHash: d.FileHash})
	}
	if view.Compact {
		for i := range items {
			if items[i].External == nil && (items[i].Kind == "chunk" || items[i].Kind == "wiki") {
				items[i].Text = ""
			}
		}
	}
	frozen := resultData{Response: resp, Items: items, Dependencies: deps, Request: q, Lazy: view.Compact, ViewEpoch: view.Epoch}
	if er = e.Repo.PutCache(ctx, resultID, Cache{Kind: "result", Owner: owner, Expires: time.Now().Add(10 * time.Minute).Unix(), Epoch: view.Epoch, Kbs: q.Kbs, Data: json.RawMessage(JSON(frozen))}); er != nil {
		return resp, er
	}
	if frozen.Lazy {
		return e.lazyPage(ctx, frozen, 0, q.Limit, q.MaxChars)
	}
	return page(frozen, 0, q.Limit, q.MaxChars), nil
}
func consistency(mode string) string {
	if mode == "local" {
		return "filesystem_live"
	}
	if mode == "direct" {
		return "direct_live"
	}
	return "file_view"
}
func subsetView(v View, ch map[string]Chunk) View {
	out := v
	out.Documents = nil
	for _, d := range v.Documents {
		dd := d
		dd.Chunks = nil
		for _, c := range d.Chunks {
			if _, ok := ch[c.ID]; ok {
				dd.Chunks = append(dd.Chunks, c)
			}
		}
		if len(dd.Chunks) > 0 {
			out.Documents = append(out.Documents, dd)
		}
	}
	return out
}
func sortItems(items []Item) {
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if a.KB != b.KB {
			return a.KB < b.KB
		}
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		if a.LineStart != b.LineStart {
			return a.LineStart < b.LineStart
		}
		return a.ChunkID < b.ChunkID
	})
}
func itemIDs(items []Item, n int) []string {
	out := []string{}
	for _, it := range items {
		out = append(out, it.ChunkID)
		if len(out) >= n {
			break
		}
	}
	return out
}
func Fuse(routes [][]Item) []Item {
	m := map[string]Item{}
	for _, r := range routes {
		for i, it := range r {
			key := itemKey(it)
			old, ok := m[key]
			if !ok {
				old = it
				old.Score = 0
			}
			old.Score += 1 / (60 + float64(i+1))
			old.Ranking = "rrf"
			m[key] = old
		}
	}
	out := []Item{}
	for _, it := range m {
		out = append(out, it)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score == out[j].Score {
			return itemKey(out[i]) < itemKey(out[j])
		}
		return out[i].Score > out[j].Score
	})
	return out
}

type cursorData struct {
	R      string `json:"r"`
	Offset int    `json:"o"`
}

func page(d resultData, offset, limit, maxChars int) Response {
	r := d.Response
	r.Items = []Item{}
	chars := 0
	end := offset
	for end < len(d.Items) && len(r.Items) < limit {
		it := d.Items[end]
		n := utf8.RuneCountInString(it.Text)
		remaining := maxChars - chars
		if remaining <= 0 {
			break
		}
		if n > remaining {
			it.Text = string([]rune(it.Text)[:remaining])
			it.More = true
			n = remaining
		}
		r.Items = append(r.Items, it)
		chars += n
		end++
	}
	r.Returned = len(r.Items)
	r.Coverage.DisplayTruncated = end < len(d.Items)
	for _, it := range r.Items {
		if it.More {
			r.Coverage.DisplayTruncated = true
		}
	}
	if end < len(d.Items) {
		r.NextCursor = base64.RawURLEncoding.EncodeToString([]byte(JSON(cursorData{r.ResultID, end})))
	} else {
		r.NextCursor = ""
	}
	return r
}
func (e *Engine) Page(ctx context.Context, q Request, owner string) (Response, error) {
	var zero Response
	cur := cursorData{R: q.ResultID}
	if q.Cursor != "" {
		b, er := base64.RawURLEncoding.DecodeString(q.Cursor)
		if er != nil || json.Unmarshal(b, &cur) != nil {
			return zero, Err("INVALID_ARGUMENT", "invalid cursor", 400)
		}
		if q.ResultID != "" && cur.R != q.ResultID {
			return zero, Err("HANDLE_SCOPE_MISMATCH", "cursor belongs to another result", 409)
		}
	}
	c, er := e.load(ctx, cur.R, "result", owner, q.Kbs)
	if er != nil {
		return zero, er
	}
	var d resultData
	if er = json.Unmarshal(c.Data, &d); er != nil {
		return zero, er
	}
	if cur.Offset < 0 || cur.Offset > len(d.Items) {
		return zero, Err("INVALID_ARGUMENT", "invalid cursor offset", 400)
	}
	if q.Limit == 0 {
		q.Limit = 10
	}
	if q.MaxChars == 0 {
		q.MaxChars = 8000
	}
	if d.Lazy {
		d.ViewEpoch = c.Epoch
		return e.lazyPage(ctx, d, cur.Offset, q.Limit, q.MaxChars)
	}
	return page(d, cur.Offset, q.Limit, q.MaxChars), nil
}
func (e *Engine) read(ctx context.Context, q Request, owner string, resp Response) (Response, error) {
	var ref Ref
	var err error
	if q.Ref != "" {
		if q.Path != "" {
			return resp, Err("INVALID_ARGUMENT", "provide ref or path, not both", 400)
		}
		ref, err = DecodeRef(q.Ref)
		if err != nil {
			return resp, err
		}
		if ref.ResultID != "" {
			if ar, ok := e.Repo.(EvidenceReader); ok {
				data, found, er := ar.ReadEvidence(ctx, q.Ref, owner, q.Kbs)
				if er != nil {
					return resp, er
				}
				if found {
					resp.Data = data
					resp.Total = Count(1, "evidence", "archived result")
					return resp, nil
				}
			}
			origin := ""
			if ir, ok := e.Repo.(IndexedReader); ok {
				origin = ir.OriginID()
			} else {
				v, ve := e.Repo.Snapshot(ctx, q.Kbs)
				if ve != nil {
					return resp, ve
				}
				origin = v.Origin
			}
			if ref.Origin != origin {
				return resp, Err("REF_ORIGIN_MISMATCH", "reference from a different data origin", 409)
			}
			c, er := e.load(ctx, ref.ResultID, "result", owner, q.Kbs)
			if er != nil {
				return resp, er
			}
			var d resultData
			if er = json.Unmarshal(c.Data, &d); er != nil {
				return resp, er
			}
			if ref.Index == -1 {
				resp.Data = map[string]any{"kind": "aggregate", "metric": d.Response.Total, "coverage": d.Response.Coverage, "result_id": ref.ResultID, "query_id": d.Response.QueryID}
				resp.Total = Count(1, "metrics", "saved result")
				return resp, nil
			}
			if ref.Index < 0 || ref.Index >= len(d.Items) {
				return resp, Err("REF_GONE", "result member not found", 410)
			}
			it := d.Items[ref.Index]
			if it.External != nil {
				if e.External == nil {
					return resp, Err("CAPABILITY_UNAVAILABLE", "original external provider is unavailable", 422)
				}
				data, er := e.External.ReadExternal(ctx, *it.External)
				if er != nil {
					return resp, er
				}
				resp.Data = data
			} else {
				resp.Data = it
			}
			resp.Total = Count(1, "results", "saved result")
			return resp, nil
		}
	} else {
		if len(q.Kbs) != 1 || q.Path == "" {
			return resp, Err("INVALID_ARGUMENT", "path read requires one kb", 400)
		}
		if ir, ok := e.Repo.(IndexedReader); ok {
			ref, err = ir.CurrentReference(ctx, q.Kbs[0], q.Path)
			if err != nil {
				return resp, err
			}
		} else {
			v, er := e.Repo.Snapshot(ctx, q.Kbs)
			if er != nil {
				return resp, er
			}
			for _, d := range v.Documents {
				if d.Path == q.Path {
					ref = Ref{Origin: v.Origin, KB: d.KB, Path: d.Path, Edition: d.Edition, FileHash: d.FileHash}
					break
				}
			}
		}
		if ref.Edition == "" {
			return resp, Err("REF_GONE", "file is not published", 410)
		}
	}
	if !Subset([]string{ref.KB}, q.Kbs) {
		return resp, Err("PERMISSION_DENIED", "reference outside selected knowledge bases", 403)
	}
	d, ch, fresh, er := e.Repo.Read(ctx, ref)
	if er != nil {
		return resp, er
	}
	text := d.Body
	kind := "document"
	if ch != nil {
		text = ch.Text
		kind = "chunk"
	}
	if ch != nil && ch.TextMissing {
		return resp, Err("TEXT_UNAVAILABLE", "graph provenance exists but no text backend is configured for it", 422)
	}
	if ref.RangeStart != nil && ref.RangeEnd != nil {
		rr := []rune(text)
		lo, hi := *ref.RangeStart, *ref.RangeEnd
		if lo < 0 || hi < lo || hi > len(rr) {
			return resp, Err("REF_GONE", "invalid saved content range", 410)
		}
		text = string(rr[lo:hi])
		kind = "document_range"
	}
	rs := []rune(text)
	if q.Length == 0 {
		q.Length = 8000
	}
	if q.Offset > len(rs) || q.Length > 100000 {
		return resp, Err("INVALID_ARGUMENT", "invalid read range", 400)
	}
	end := q.Offset + q.Length
	if end > len(rs) {
		end = len(rs)
	}
	var next *int
	if end < len(rs) {
		n := end
		next = &n
	}
	resp.Data = ReadData{Ref: EncodeRef(ref), KB: ref.KB, Path: ref.Path, Edition: ref.Edition, Text: string(rs[q.Offset:end]), Offset: q.Offset, Length: end - q.Offset, TotalChars: len(rs), NextOffset: next, Freshness: fresh, Availability: "available", FileHash: ref.FileHash, Kind: kind}
	resp.Total = Count(1, "files", "reference")
	resp.Coverage = Coverage{DisplayTruncated: next != nil, Consistency: consistency(e.Mode), ScopeBasis: "enumerated"}
	return resp, nil
}
func (e *Engine) supports(ctx context.Context, v View, ch map[string]Chunk) ([]Support, error) {
	ss := []Support{}
	if e.Graph != nil {
		var er error
		ss, er = e.Graph.Supports(ctx, subsetView(v, ch))
		if er != nil {
			return nil, er
		}
	} else {
		for _, d := range v.Documents {
			ss = append(ss, d.Supports...)
		}
	}
	out := []Support{}
	seen := map[string]bool{}
	for _, s := range ss {
		if _, ok := ch[s.ChunkID]; ok && !seen[s.ID] {
			out = append(out, s)
			if len(out) > 20000 {
				return nil, Err("GRAPH_BUDGET_EXCEEDED", "raw support set exceeds 20000; narrow the query scope", 422)
			}
			seen[s.ID] = true
		}
	}
	return out, nil
}
func Cosine(a, b []float64) (float64, error) {
	if len(a) != len(b) || len(a) == 0 {
		return 0, Err("MODEL_MISMATCH", "vector dimensions differ", 409)
	}
	dot, aa, bb := 0., 0., 0.
	for i := range a {
		if math.IsNaN(a[i]) || math.IsInf(a[i], 0) || math.IsNaN(b[i]) || math.IsInf(b[i], 0) {
			return 0, Err("MODEL_MISMATCH", "non-finite vector", 409)
		}
		dot += a[i] * b[i]
		aa += a[i] * a[i]
		bb += b[i] * b[i]
	}
	if aa == 0 || bb == 0 {
		return 0, Err("MODEL_MISMATCH", "zero vector", 409)
	}
	return dot / math.Sqrt(aa*bb), nil
}
func Debug(v any) string { return fmt.Sprint(v) }

func MatchesFilters(c Chunk, filters []Filter) (bool, error) {
	for _, f := range filters {
		ok, e := MatchFilter(c, f)
		if e != nil {
			return false, e
		}
		if !ok {
			return false, nil
		}
	}
	return true, nil
}
func itemKey(it Item) string {
	if it.External != nil {
		return "external:" + JSON(it.External)
	}
	if it.ChunkID != "" {
		return it.KB + ":" + it.Edition + ":" + it.ChunkID
	}
	return it.Ref + ":" + it.Kind + ":" + it.ID
}

// allowedSpans permits cross-chunk matches only across contiguous selected original ranges.
func allowedSpans(d Document, selected map[string]Chunk) [][2]int {
	rs := [][2]int{}
	full := len(d.Chunks) > 0
	for _, c := range d.Chunks {
		if _, ok := selected[c.ID]; !ok {
			full = false
			continue
		}
		if c.End <= len([]rune(d.Body)) {
			rs = append(rs, [2]int{c.Start, c.End})
		}
	}
	if full {
		return [][2]int{{0, len([]rune(d.Body))}}
	}
	sort.Slice(rs, func(i, j int) bool { return rs[i][0] < rs[j][0] })
	out := [][2]int{}
	for _, r := range rs {
		if len(out) > 0 && r[0] <= out[len(out)-1][1] {
			if r[1] > out[len(out)-1][1] {
				out[len(out)-1][1] = r[1]
			}
		} else {
			out = append(out, r)
		}
	}
	return out
}
