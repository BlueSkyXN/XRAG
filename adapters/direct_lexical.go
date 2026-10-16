package adapters

import (
	"context"
	"sort"

	"github.com/BlueSkyXN/XRAG/core"
)

const legacyLexicalNote = "集合缺少稀疏词法索引，需要重建"

func sortDirectItems(items []core.Item) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].Score == items[j].Score {
			return items[i].Ref < items[j].Ref
		}
		return items[i].Score > items[j].Score
	})
}

func (d *Direct) selectLexical(ctx context.Context, q core.Request, capacity int, ids []string) (core.IndexSelection, error) {
	if _, err := d.checkIdentity(ctx); err != nil {
		return core.IndexSelection{}, err
	}
	if d.Q == nil {
		return core.IndexSelection{}, core.Err("CAPABILITY_UNAVAILABLE", "text search requires Qdrant", 422)
	}
	desc, err := d.Q.Descriptor(ctx, d.Collection)
	if err != nil {
		return core.IndexSelection{}, err
	}
	action := "search.keyword"
	if desc["lexical_schema"] == core.LexicalSchema {
		action = "search.lexical"
	}
	out, err := d.selectIndex(ctx, action, q, capacity, ids)
	if action == "search.keyword" {
		out.Notes = append(out.Notes, legacyLexicalNote)
	}
	return out, err
}

func (d *Direct) SelectLexical(ctx context.Context, v core.View, text string) (core.IndexSelection, error) {
	out := core.IndexSelection{Items: []core.Item{}}
	ids := []string{}
	for _, doc := range v.Documents {
		for _, c := range doc.Chunks {
			ids = append(ids, c.ID)
		}
	}
	ids = core.Unique(ids)
	sort.Strings(ids)
	for start := 0; start < len(ids); start += 500 {
		part, err := d.selectLexical(ctx, core.Request{Q: text}, 200, ids[start:min(start+500, len(ids))])
		if err != nil {
			return out, err
		}
		out.Items = append(out.Items, part.Items...)
		out.Notes = append(out.Notes, part.Notes...)
	}
	sortDirectItems(out.Items)
	if len(out.Items) > 200 {
		out.Items = out.Items[:200]
	}
	out.Notes = core.Unique(out.Notes)
	return out, nil
}

func (g *DirectGroup) SelectLexical(ctx context.Context, v core.View, text string) (core.IndexSelection, error) {
	out := core.IndexSelection{Items: []core.Item{}}
	for kb, part := range directParts(v) {
		d := g.Readers[kb]
		if d == nil {
			return out, core.Err("PERMISSION_DENIED", "direct KB not configured", 403)
		}
		selection, err := d.SelectLexical(ctx, part, text)
		if err != nil {
			return out, err
		}
		for i := range selection.Items {
			if selection.Items[i].Ranking != "none" {
				selection.Items[i].Score = 1 / (60 + float64(i+1))
				selection.Items[i].Ranking = "direct_model_rank_fusion"
			}
		}
		out.Items = append(out.Items, selection.Items...)
		out.Notes = append(out.Notes, selection.Notes...)
	}
	sortDirectItems(out.Items)
	if len(out.Items) > 200 {
		out.Items = out.Items[:200]
	}
	out.Notes = core.Unique(out.Notes)
	return out, nil
}
