package core

import (
	"context"
	"encoding/json"
	"time"
)

// IndexSelection contains references and metadata, never a corpus-sized copy of
// document bodies or vectors. Total counts the complete filtered query scope.
type IndexSelection struct {
	Metric        *Metric
	Basis         string
	Approximate   bool
	Notes         []string
	Execution     string
	Origin        string
	Epoch         int64
	Items         []Item
	Dependencies  []Ref
	Total         int
	SelectedTotal int
	Unit          string
}

// IndexedReader is an optional repository capability. Core still owns paging,
// result semantics, references, scope handling and the public response shape.
type IndexedReader interface {
	SelectIndex(context.Context, string, Request, int) (IndexSelection, error)
	HydrateItems(context.Context, []Item) ([]Item, error)
	ViewFromReferences(context.Context, []Ref) (View, error)
	CurrentReference(context.Context, string, string) (Ref, error)
	OriginID() string
}

func (e *Engine) indexed(ctx context.Context, action string, q Request, owner string, resp Response, ir IndexedReader) (Response, error) {
	start := time.Now()
	sel, err := ir.SelectIndex(ctx, action, q, e.capacity())
	if err != nil {
		return resp, err
	}
	basis := sel.Basis
	if basis == "" {
		basis = "corpus"
	}
	resp.Coverage = Coverage{Consistency: consistency(e.Mode), ScopeBasis: basis, Approximate: sel.Approximate}
	resp.Total = Count(sel.Total, sel.Unit, basis)
	if sel.Metric != nil {
		resp.Total = *sel.Metric
	}
	resp.Notes = append(resp.Notes, sel.Notes...)
	resp.CacheComplete = len(sel.Items) == sel.SelectedTotal
	resp.Strategy["execution"] = "indexed_metadata_then_page_hydration"
	if sel.Execution != "" {
		resp.Strategy["execution"] = sel.Execution
	}
	resp.Strategy["cached_members"] = len(sel.Items)
	resp.Strategy["selected_total"] = sel.SelectedTotal
	if !resp.CacheComplete {
		resp.Notes = append(resp.Notes, "The saved result window is bounded; total accuracy and scope are stated separately. Narrow the query for additional members.")
	}
	if q.PerFile > 0 && sel.SelectedTotal < sel.Total {
		resp.Notes = append(resp.Notes, "per_file limits selected results, not the complete keyword count")
	}
	// Handles contain only immutable refs. The selected documents are loaded only
	// when an operation actually consumes the handle.
	if q.SaveScope != "none" && len(sel.Items) > 0 {
		if !resp.CacheComplete {
			if q.SaveScope == "required" {
				return resp, Err("HANDLE_TOO_LARGE", "complete matching scope exceeds saved member capacity", 422)
			}
		} else {
			refs := []Ref{}
			ids := []string{}
			for _, it := range sel.Items {
				r, er := DecodeRef(it.Ref)
				if er != nil {
					return resp, er
				}
				refs = append(refs, r)
				if it.ChunkID != "" {
					ids = append(ids, it.ChunkID)
				}
			}
			basis := resp.Coverage.ScopeBasis
			if sel.SelectedTotal < sel.Total {
				basis = "candidate"
			}
			h := scopeData{Refs: refs, IDs: Unique(ids), Basis: basis, Files: action == "files.tree"}
			resp.Handle = ID("h_")
			if err = e.Repo.PutCache(ctx, resp.Handle, Cache{Kind: "handle", Owner: owner, Expires: time.Now().Add(24 * time.Hour).Unix(), Epoch: sel.Epoch, Kbs: q.Kbs, Data: json.RawMessage(JSON(h))}); err != nil {
				return resp, err
			}
		}
	}
	epoch, err := e.Repo.Epoch(ctx)
	if err != nil {
		return resp, err
	}
	if epoch != sel.Epoch {
		return resp, Err("VIEW_INVALIDATED", "source withdrawn during query", 409)
	}
	resp.ResultID = ID("rs_")
	resp.SummaryRef = EncodeRef(Ref{Origin: sel.Origin, ResultID: resp.ResultID, Index: -1})
	if q.Trace {
		resp.Trace = []Step{{Stage: "indexed_scope", Count: len(sel.Dependencies), Note: "metadata-only file scope; no corpus body or vectors loaded"}, {Stage: "index_match", Count: sel.Total, Note: "total fields determine exactness; a missing numeric total is not zero"}, {Stage: "result_window", Count: len(sel.Items), DurationMS: time.Since(start).Milliseconds()}}
	}
	d := resultData{Response: resp, Items: sel.Items, Dependencies: sel.Dependencies, Request: q, Lazy: true, ViewEpoch: sel.Epoch}
	if err = e.Repo.PutCache(ctx, resp.ResultID, Cache{Kind: "result", Owner: owner, Expires: time.Now().Add(10 * time.Minute).Unix(), Epoch: sel.Epoch, Kbs: q.Kbs, Data: json.RawMessage(JSON(d))}); err != nil {
		return resp, err
	}
	return e.lazyPage(ctx, d, 0, q.Limit, q.MaxChars)
}

func (e *Engine) lazyPage(ctx context.Context, d resultData, offset, limit, maxChars int) (Response, error) {
	ir, ok := e.Repo.(IndexedReader)
	if !ok {
		return Response{}, Err("CAPABILITY_UNAVAILABLE", "indexed reader is unavailable", 422)
	}
	end := offset + limit
	if end > len(d.Items) {
		end = len(d.Items)
	}
	items, err := ir.HydrateItems(ctx, d.Items[offset:end])
	if err != nil {
		return Response{}, err
	}
	// Keep the full reference-only window. Only the page being returned receives text.
	local := d
	local.Items = append([]Item(nil), d.Items...)
	copy(local.Items[offset:end], items)
	r := page(local, offset, limit, maxChars)
	r.Coverage.DisplayTruncated = r.Coverage.DisplayTruncated || !r.CacheComplete
	ep, err := e.Repo.Epoch(ctx)
	if err != nil {
		return r, err
	}
	// cache validation already checks withdrawal on continuation. Initial request
	// also checks after page hydration via immutable reads.
	if ep != d.ViewEpoch {
		return r, Err("VIEW_INVALIDATED", "source withdrawn while reading saved page", 409)
	}
	return r, nil
}

func scopeMembers(h scopeData) int {
	if h.Files {
		return len(h.Refs)
	}
	return len(h.IDs)
}
func scopeUnit(h scopeData) string {
	if h.Files {
		return "files"
	}
	return "chunks"
}

// NativeIndexSelector lets a repository select operations with faithful backend
// pushdown. Unsupported filter combinations keep the shared metadata path.
type NativeIndexSelector interface{ CanSelectIndex(string, Request) bool }
