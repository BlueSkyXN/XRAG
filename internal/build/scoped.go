package build

import (
	"context"
	"encoding/json"
	"sort"
	"github.com/BlueSkyXN/XRAG/adapters"
	"github.com/BlueSkyXN/XRAG/core"
	"github.com/BlueSkyXN/XRAG/internal/store"
)

func (b *Builder) ForKB(ctx context.Context, kb string) (*Builder, error) {
	k, e := b.Store.KBSettings(ctx, kb)
	if e != nil {
		return nil, e
	}
	c := *b
	c.Options = k
	if k.Providers != nil {
		c.Config = *k.Providers
		c.Config.ParserCommand = b.Config.ParserCommand
		c.Config.ParserRevision = b.Config.ParserRevision
		c.Config.Extractor = b.Config.Extractor
		c.Config.ExtractorRevision = b.Config.ExtractorRevision
	}
	if core.JSON(k.Graph) != "{}" {
		c.Config.Graph = k.Graph
	}
	if c.Config.Graph.Mode == "llm" && c.Config.Graph.Model == nil {
		c.Config.Graph.Model = c.Config.Chat
	}
	if k.ParserPlugin != "" {
		p, ok := b.Config.Plugins[k.ParserPlugin]
		if !ok || p.Kind != "parser" {
			return nil, core.Err("CAPABILITY_UNAVAILABLE", "registered parser plugin not found", 422)
		}
		c.Options.Parser = "commonmark"
		c.Config.ParserCommand = p.Command
		c.Config.ParserRevision = p.Version
	}
	if c.Config.Graph.Plugin != "" {
		p, ok := b.Config.Plugins[c.Config.Graph.Plugin]
		if !ok || p.Kind != "extractor" {
			return nil, core.Err("CAPABILITY_UNAVAILABLE", "registered extractor plugin not found", 422)
		}
		c.Config.Extractor = p.Command
		c.Config.ExtractorRevision = p.Version
		c.Config.Graph.Mode = "plugin"
	}
	return &c, nil
}
func (b *Builder) MeterContext(ctx context.Context, kb, run, owner string) context.Context {
	return adapters.WithMeter(ctx, &store.Meter{Store: b.Store, KB: kb, Run: run, Owner: owner, Policy: b.Options.Budget})
}

type scopedSemantic struct{ Builder *Builder }

func (s scopedSemantic) CompactSemantic() {}

func (s scopedSemantic) Label() string { return "per_kb_model" }
func (s scopedSemantic) Search(ctx context.Context, v core.View, q string) ([]core.Ranked, error) {
	groups := map[string]core.View{}
	cfgs := map[string]adapters.Config{}
	for _, d := range v.Documents {
		byGen := map[string][]core.Chunk{}
		for _, c := range d.Chunks {
			if c.VectorState == "failed" || c.Generation == "" {
				continue
			}
			byGen[c.Generation] = append(byGen[c.Generation], c)
		}
		for g, ch := range byGen {
			key := d.KB + ":" + g
			cfg, e := s.Builder.Store.Generation(ctx, d.KB, g)
			if e != nil {
				b, er := s.Builder.ForKB(ctx, d.KB)
				if er != nil {
					return nil, er
				}
				cfg = b.Config
				if cfg.Embedding == nil || cfg.Embedding.SemanticID() != g {
					return nil, e
				}
			}
			cfgs[key] = cfg
			vv := groups[key]
			vv.Origin = v.Origin
			vv.Compact = v.Compact
			vv.Mode = v.Mode
			vv.At = v.At
			vv.Epoch = v.Epoch
			dd := d
			dd.Chunks = ch
			vv.Documents = append(vv.Documents, dd)
			groups[key] = vv
		}
	}
	if len(groups) == 0 {
		return nil, core.Err("CAPABILITY_UNAVAILABLE", "selected knowledge has no usable vector projection", 422)
	}
	keys := []string{}
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := []core.Ranked{}
	for _, key := range keys {
		vv := groups[key]
		cfg := cfgs[key]
		b, e := s.Builder.ForKB(ctx, vv.Documents[0].KB)
		if e != nil {
			return nil, e
		}
		// The run context, if already metered (build/eval), is kept across individual subqueries.
		callCtx := ctx
		if !adapters.HasMeter(ctx) {
			callCtx = b.MeterContext(ctx, vv.Documents[0].KB, adapters.RequestID(ctx), "query")
		}
		var sem core.Semantic = &adapters.ExactSemantic{Embed: &adapters.Embedder{Config: *cfg.Embedding}}
		if cfg.Qdrant != nil {
			sem = &adapters.Qdrant{Endpoint: *cfg.Qdrant, Embed: &adapters.Embedder{Config: *cfg.Embedding}, Origin: v.Origin}
		}
		var rs []core.Ranked
		if vv.Compact && cfg.Qdrant == nil {
			vec, _, er := (&adapters.Embedder{Config: *cfg.Embedding}).Embed(callCtx, []string{q}, true)
			if er != nil {
				return nil, er
			}
			rs, e = s.Builder.Store.VectorTop(ctx, vv, vec[0], 200)
		} else {
			rs, e = sem.Search(callCtx, vv, q)
		}
		if e != nil {
			return nil, e
		}
		// Different embedding models have incomparable scores. Use rank contributions, not raw cosine addition.
		for i, r := range rs {
			r.Score = 1 / (60 + float64(i+1))
			out = append(out, r)
		}
	}
	adapters.SortRanks(out)
	if len(out) > 200 {
		out = out[:200]
	}
	return out, nil
}

type scopedGraph struct{ Builder *Builder }

func (g scopedGraph) Supports(ctx context.Context, v core.View) ([]core.Support, error) {
	groups := map[string]core.View{}
	endpoints := map[string]*adapters.NeoConfig{}
	out := []core.Support{}
	for _, d := range v.Documents {
		binding, _, _, err := g.Builder.Store.ProjectionBinding(ctx, d.Edition)
		if err != nil && core.AsError(err).Code != "NOT_FOUND" {
			return nil, err
		}
		if err != nil || binding.Neo4j == nil {
			out = append(out, d.Supports...)
			continue
		}
		key := core.Hash(binding.Neo4j)
		vv := groups[key]
		vv.Origin = v.Origin
		vv.Documents = append(vv.Documents, d)
		groups[key] = vv
		endpoints[key] = binding.Neo4j
	}
	for key, vv := range groups {
		n := adapters.Neo4j{Config: *endpoints[key], Origin: v.Origin}
		ss, err := n.Supports(ctx, vv)
		if err != nil {
			return nil, err
		}
		out = append(out, ss...)
	}
	return out, nil
}

type scopedRerank struct{ Builder *Builder }

func (r scopedRerank) Rank(ctx context.Context, q string, items []core.Item) ([]core.Item, error) {
	groups := map[string][]core.Item{}
	for _, it := range items {
		kb := it.KB
		if it.External != nil && it.External.BindingKB != "" {
			kb = it.External.BindingKB
		}
		groups[kb] = append(groups[kb], it)
	}
	out := []core.Item{}
	for kb, ii := range groups {
		b, e := r.Builder.ForKB(ctx, kb)
		if e != nil {
			return nil, e
		}
		if b.Config.Rerank == nil {
			return nil, core.Err("CAPABILITY_UNAVAILABLE", "reranker is not configured for "+kb, 422)
		}
		cctx := ctx
		if !adapters.HasMeter(ctx) {
			cctx = b.MeterContext(ctx, kb, adapters.RequestID(ctx), "query")
		}
		rr := adapters.Reranker{Config: *b.Config.Rerank}
		ranked, e := rr.Rank(cctx, q, ii)
		if e != nil {
			return nil, e
		}
		for i, it := range ranked {
			it.Score = 1 / (60 + float64(i+1))
			it.Ranking = "rerank_rank_fusion"
			out = append(out, it)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score == out[j].Score {
			return out[i].Ref < out[j].Ref
		}
		return out[i].Score > out[j].Score
	})
	return out, nil
}

type scopedExternal struct{ Builder *Builder }

func (s scopedExternal) configs(ctx context.Context, kbs []string) ([]adapters.ExternalConfig, error) {
	out := []adapters.ExternalConfig{}
	seen := map[string]string{}
	for _, kb := range kbs {
		b, e := s.Builder.ForKB(ctx, kb)
		if e != nil {
			return nil, e
		}
		for _, p := range b.Config.External {
			if old, ok := seen[p.ID]; ok {
				if old != p.Revision() {
					return nil, core.Err("VERSION_CONFLICT", "external provider ID has different KB bindings", 409)
				}
				continue
			}
			seen[p.ID] = p.Revision()
			out = append(out, p)
		}
	}
	return out, nil
}
func (s scopedExternal) SearchExternal(ctx context.Context, q core.Request) ([]core.Item, error) {
	ps, e := s.configs(ctx, q.Kbs)
	if e != nil {
		return nil, e
	}
	if len(ps) == 0 {
		return nil, core.Err("CAPABILITY_UNAVAILABLE", "no external providers for selected KBs", 422)
	}
	items, e := (&adapters.External{Configs: ps}).SearchExternal(ctx, q)
	if e != nil {
		return nil, e
	}
	for i := range items {
		for _, kb := range q.Kbs {
			b, err := s.Builder.ForKB(ctx, kb)
			if err != nil {
				return nil, err
			}
			for _, p := range b.Config.External {
				if items[i].External != nil && p.ID == items[i].External.Provider && p.Revision() == items[i].External.Revision {
					items[i].External.BindingKB = kb
					break
				}
			}
			if items[i].External != nil && items[i].External.BindingKB != "" {
				break
			}
		}
	}
	return items, nil
}
func (s scopedExternal) ReadExternal(ctx context.Context, r core.ExternalReference) (any, error) {
	kbs, e := s.Builder.Store.KBs(ctx)
	if e != nil {
		return nil, e
	}
	ids := []string{}
	for _, k := range kbs {
		ids = append(ids, k["id"].(string))
	}
	ps, e := s.configs(ctx, ids)
	if e != nil {
		return nil, e
	}
	return (&adapters.External{Configs: ps}).ReadExternal(ctx, r)
}
func Rematerialize(d core.Document) core.Document {
	var n core.Document
	_ = json.Unmarshal([]byte(core.JSON(d)), &n)
	n.Edition = core.ID("ed_")
	ids := map[string]string{}
	for i := range n.Chunks {
		c := &n.Chunks[i]
		old := c.ID
		c.ID = core.UUID([]any{n.Edition, c.Key})
		c.Edition = n.Edition
		ids[old] = c.ID
	}
	for i := range n.Supports {
		s := &n.Supports[i]
		s.Edition = n.Edition
		s.ChunkID = ids[s.ChunkID]
		s.ID = "sp_" + core.Hash([]any{s.ChunkID, s.From, s.Relation, s.To, s.Origin})[:32]
	}
	return n
}

func (b *Builder) PublishVectorAlias(ctx context.Context, kb string) error {
	if b.Config.Embedding == nil || b.Config.Qdrant == nil {
		return nil
	}
	q := adapters.Qdrant{Endpoint: *b.Config.Qdrant, Embed: &adapters.Embedder{Config: *b.Config.Embedding}, Origin: b.Store.Origin}
	return q.PublishAlias(ctx, kb)
}
