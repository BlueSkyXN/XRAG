package store

import (
	"context"
	"strings"
	"github.com/BlueSkyXN/XRAG/core"
)

// Diagnose uses only stored execution metadata and the explicitly selected
// current document. It does not rerun a query or call a model.
func (s *Store) Diagnose(ctx context.Context, p Principal, id, kb, path, anchor string) (map[string]any, error) {
	rs, e := s.Queries(ctx, p, id)
	if e != nil {
		return nil, e
	}
	if len(rs) != 1 {
		return nil, core.Err("RESULT_EXPIRED", "query record unavailable", 410)
	}
	r := rs[0]
	if !core.Subset([]string{kb}, r.KBs) {
		return nil, core.Err("PERMISSION_DENIED", "target KB was not in the recorded query", 403)
	}
	if _, e = core.CleanPath(path); e != nil {
		return nil, e
	}
	if len(anchor) > 2000 {
		return nil, core.Err("INVALID_ARGUMENT", "anchor too long", 400)
	}
	out := map[string]any{"query_id": id, "kb": kb, "path": path, "reran_query": false, "model_calls": 0, "historical_execution": true, "current_document_check": true, "notes": []string{"Missing stage IDs do not prove exclusion when the saved trace is partial. Current document status is not retroactive proof of its state at query time."}}
	ref, er := s.CurrentReference(ctx, kb, path)
	if er != nil {
		if core.AsError(er).Code == "REF_GONE" {
			out["publication"] = "not_currently_published"
			return out, nil
		}
		return out, er
	}
	doc, _, _, er := s.Read(ctx, ref)
	if er != nil {
		return nil, er
	}
	out["publication"] = "published"
	out["current_edition"] = doc.Edition
	out["anchor_present"] = anchor == "" || strings.Contains(doc.Body, anchor)
	matches, er := core.MatchesFilters(core.Chunk{KB: kb, Path: path, Title: doc.Title, Fields: doc.Fields}, r.Request.Filters)
	if er != nil {
		return nil, er
	}
	out["current_filter_match"] = matches
	out["saved_scope"] = "not_used"
	if r.Request.Within != "" {
		out["saved_scope"] = "requires_saved_handle; not inferred from current document"
	}
	visible := false
	historical := false
	ids := map[string]bool{}
	for _, c := range doc.Chunks {
		ids[c.ID] = true
	}
	for _, it := range r.Response.Items {
		if it.KB == kb && it.Path == path {
			visible = true
			if it.Edition != doc.Edition {
				historical = true
			}
			ids[it.ChunkID] = true
		}
	}
	out["returned_on_recorded_page"] = visible
	out["returned_version_differs_from_current"] = historical
	stages := []map[string]any{}
	for _, st := range r.Response.Trace {
		hit := false
		for _, id := range st.IDs {
			hit = hit || ids[id]
		}
		status := "unknown"
		if hit {
			status = "observed"
		} else if visible && len(st.IDs) == st.Count && st.Count > 0 && !historical {
			status = "not_observed_in_recorded_stage"
		}
		stages = append(stages, map[string]any{"stage": st.Stage, "status": status, "stage_count": st.Count, "recorded_ids": len(st.IDs), "note": st.Note})
	}
	out["stages"] = stages
	next := "Compare this query with a human-maintained expected case; unrecorded stages remain unknown."
	if !matches {
		next = "The current file does not match the request filters. Check whether those filters match the intended task."
	} else if anchor != "" && !strings.Contains(doc.Body, anchor) {
		next = "Expected text is absent from the current published version; inspect the source and latest build."
	} else if visible {
		next = "The file was returned on the recorded page; inspect the cited range and any output truncation."
	}
	out["next_step"] = next
	return out, nil
}
