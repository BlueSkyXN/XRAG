package build

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
	"github.com/BlueSkyXN/XRAG/core"
	"github.com/BlueSkyXN/XRAG/internal/store"
)

func Healthy(d core.Document) bool {
	for _, c := range d.Chunks {
		if c.VectorState == "failed" || c.GraphState == "failed" {
			return false
		}
	}
	return true
}
func (b *Builder) ApplyFields(d *core.Document) error {
	d.Dictionary = append([]string{}, b.Options.Dictionary...)
	for key, typ := range b.Options.Fields {
		v, ok := d.Fields[key]
		if !ok {
			continue
		}
		values := []any{v}
		array := false
		if a, ok := v.([]any); ok {
			values = a
			array = true
		}
		converted := []any{}
		for _, x := range values {
			switch typ {
			case "string":
				x = fmt.Sprint(x)
			case "number":
				n, e := strconv.ParseFloat(fmt.Sprint(x), 64)
				if e != nil || math.IsNaN(n) || math.IsInf(n, 0) {
					return core.Err("INVALID_FIELD", key+" must be a finite number", 422)
				}
				x = n
			case "boolean":
				n, e := strconv.ParseBool(fmt.Sprint(x))
				if e != nil {
					return core.Err("INVALID_FIELD", key+" must be boolean", 422)
				}
				x = n
			case "date":
				if _, e := time.Parse("2006-01-02", fmt.Sprint(x)); e != nil {
					return core.Err("INVALID_FIELD", key+" must use YYYY-MM-DD", 422)
				}
				x = fmt.Sprint(x)
			}
			converted = append(converted, x)
		}
		if array {
			d.Fields[key] = converted
		} else {
			d.Fields[key] = converted[0]
		}
	}
	for i := range d.Chunks {
		d.Chunks[i].Fields = d.Fields
	}
	return nil
}
func (b *Builder) WikiLinks(ctx context.Context, d core.Document) []core.Support {
	out := []core.Support{}
	if len(d.Chunks) == 0 {
		return out
	}
	ks, e := b.Store.KnowledgeList(ctx, []string{d.KB})
	if e != nil {
		return out
	}
	titles := map[string][]store.Knowledge{}
	for _, k := range ks {
		if k.Published != "" && (k.State == "published" || k.State == "needs_review") && k.Kind == "wiki" {
			titles[k.Title] = append(titles[k.Title], k)
		}
	}
	rx := regexp.MustCompile(`\[\[([^\]\n]+)\]\]`)
	for _, c := range d.Chunks {
		for _, m := range rx.FindAllStringSubmatch(c.Text, -1) {
			name := strings.TrimSpace(m[1])
			if i := strings.Index(name, "|"); i >= 0 {
				name = name[:i]
			}
			targets := titles[name]
			if len(targets) != 1 || d.Title == name {
				continue
			}
			from, to := "Wiki:"+d.Title, "Wiki:"+name
			out = append(out, core.Support{ID: "sp_" + core.Hash([]any{c.ID, from, "links_to", to})[:32], KB: d.KB, Path: d.Path, Edition: d.Edition, ChunkID: c.ID, From: from, To: to, Relation: "links_to", Evidence: m[0], Origin: "wiki"})
		}
	}
	return out
}

// Projection rebuild does not re-read source directories or erase unpublished drafts.
// All selected files stage first. Metadata switches in one transaction so model migration
// never sends a query embedding to a partially migrated KB.
func (b *Builder) RunProjectionBuild(ctx context.Context, j store.Job, mode string) (any, error) {
	if mode == "vectors_only" && b.Config.Embedding == nil {
		return nil, core.Err("CAPABILITY_UNAVAILABLE", "vectors_only requires embedding configuration", 422)
	}
	v, e := b.Store.Snapshot(ctx, []string{j.KB})
	if e != nil {
		return nil, e
	}
	docs := []core.Document{}
	expected := map[string]string{}
	success := false
	defer func() {
		if !success {
			for _, d := range docs {
				_ = b.Store.Abort(context.Background(), d.Edition)
			}
		}
	}()
	for _, old := range v.Documents {
		if e = b.Store.CheckJob(ctx, j); e != nil {
			return nil, e
		}
		d := Rematerialize(old)
		expected[d.Path] = old.Edition
		docs = append(docs, d)
		if e = b.Prepare(ctx, &d, j); e != nil {
			return nil, e
		}
		docs[len(docs)-1] = d
		if e = b.Store.Stage(ctx, d); e != nil {
			return nil, e
		}
		if e = b.WriteProjections(ctx, d); e != nil {
			return nil, e
		}
	}
	if e = b.Store.PublishProjectionBatch(ctx, j, docs, expected, v.Epoch); e != nil {
		return nil, e
	}
	success = true
	for _, d := range docs {
		_ = b.Store.BuildDetail(ctx, j, d.Source, d.Path, mode, expected[d.Path], d.Edition, "published-input", nil)
	}
	notes := []string{}
	if mode == "vectors_only" && b.Config.Qdrant != nil {
		if e = b.PublishVectorAlias(ctx, j.KB); e != nil {
			notes = append(notes, "metadata published; compatibility alias update failed: "+core.AsError(e).Code)
		}
	}
	return map[string]any{"mode": mode, "changed": len(docs), "failed": 0, "reused_source_bytes": true, "notes": notes}, nil
}
