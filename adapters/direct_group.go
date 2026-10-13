package adapters

import (
	"context"
	"time"
	"github.com/BlueSkyXN/XRAG/core"
)

// All KBs share one XRAG origin; independent origins require separate profiles.
type DirectGroup struct {
	Readers  map[string]*Direct
	CacheDir string
}

func (g *DirectGroup) Snapshot(ctx context.Context, kbs []string) (core.View, error) {
	v := core.View{Mode: "direct", At: time.Now(), Documents: []core.Document{}}
	for _, kb := range kbs {
		d, ok := g.Readers[kb]
		if !ok {
			return v, core.Err("PERMISSION_DENIED", "KB not configured in direct profile", 403)
		}
		part, e := d.Snapshot(ctx, []string{kb})
		if e != nil {
			return v, e
		}
		if v.Origin != "" && v.Origin != part.Origin {
			return v, core.Err("REF_ORIGIN_MISMATCH", "use separate profiles for different authorities", 409)
		}
		v.Origin = part.Origin
		v.Documents = append(v.Documents, part.Documents...)
	}
	return v, nil
}
func (g *DirectGroup) Epoch(ctx context.Context) (int64, error) { return 0, ctx.Err() }
func (g *DirectGroup) Read(ctx context.Context, r core.Ref) (core.Document, *core.Chunk, string, error) {
	d, ok := g.Readers[r.KB]
	if !ok {
		return core.Document{}, nil, "", core.Err("PERMISSION_DENIED", "reference outside direct bindings", 403)
	}
	return d.Read(ctx, r)
}
func directParts(v core.View) map[string]core.View {
	out := map[string]core.View{}
	for _, d := range v.Documents {
		p := out[d.KB]
		p.Compact = v.Compact
		p.Mode = v.Mode
		p.Origin = v.Origin
		p.At = v.At
		p.Documents = append(p.Documents, d)
		out[d.KB] = p
	}
	return out
}
func (g *DirectGroup) Lexical(ctx context.Context, v core.View, q string) ([]core.Ranked, error) {
	out := []core.Ranked{}
	for kb, p := range directParts(v) {
		d := g.Readers[kb]
		if d == nil || d.Q == nil {
			return nil, core.Err("CAPABILITY_UNAVAILABLE", "text search not configured for "+kb, 422)
		}
		r, e := d.Lexical(ctx, p, q)
		if e != nil {
			return nil, e
		}
		out = append(out, r...)
	}
	return out, nil
}
func (g *DirectGroup) Search(ctx context.Context, v core.View, q string) ([]core.Ranked, error) {
	out := []core.Ranked{}
	for kb, p := range directParts(v) {
		d := g.Readers[kb]
		if d == nil {
			return nil, core.Err("CAPABILITY_UNAVAILABLE", "binding missing", 422)
		}
		r, e := d.Search(ctx, p, q)
		if e != nil {
			return nil, e
		}
		for i, x := range r {
			x.Score = 1 / (60 + float64(i+1))
			out = append(out, x)
		}
	}
	SortRanks(out)
	return out, nil
}
func (g *DirectGroup) Label() string { return "direct_per_kb_rank_fusion" }
func (g *DirectGroup) Supports(ctx context.Context, v core.View) ([]core.Support, error) {
	out := []core.Support{}
	for kb, p := range directParts(v) {
		d := g.Readers[kb]
		if d == nil || d.Neo == nil {
			return nil, core.Err("CAPABILITY_UNAVAILABLE", "graph missing for "+kb, 422)
		}
		ss, e := d.Supports(ctx, p)
		if e != nil {
			return nil, e
		}
		out = append(out, ss...)
	}
	return out, nil
}
func (g *DirectGroup) PutCache(ctx context.Context, id string, c core.Cache) error {
	return (&Local{CacheDir: g.CacheDir}).PutCache(ctx, id, c)
}
func (g *DirectGroup) GetCache(ctx context.Context, id string) (core.Cache, error) {
	return (&Local{CacheDir: g.CacheDir}).GetCache(ctx, id)
}
func (g *DirectGroup) ValidateView(ctx context.Context, v core.View) error {
	for kb, p := range directParts(v) {
		d := g.Readers[kb]
		if d == nil {
			return core.Err("REF_ORIGIN_MISMATCH", "binding removed", 409)
		}
		if e := d.ValidateView(ctx, p); e != nil {
			return e
		}
	}
	return nil
}
