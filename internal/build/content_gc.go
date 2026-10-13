package build

import (
	"context"
	"github.com/BlueSkyXN/XRAG/adapters"
	"github.com/BlueSkyXN/XRAG/core"
	"github.com/BlueSkyXN/XRAG/internal/oplock"
	"github.com/BlueSkyXN/XRAG/internal/store"
)

// CollectContent keeps processes alive. It excludes content operations only
// during this bounded batch. Direct external clients do not share this lease.
func (b *Builder) CollectContent(ctx context.Context, days, batch int, apply, external bool) (store.PruneReport, error) {
	locked, release, err := oplock.Acquire(ctx, b.Store.Dir, true)
	if err != nil {
		return store.PruneReport{}, err
	}
	defer release()
	ctx = locked
	after, err := b.Store.ContentCursor(ctx)
	if err != nil {
		return store.PruneReport{}, err
	}
	plan, err := b.Store.PruneContentBatch(ctx, days, batch, after, false)
	if err != nil || !apply {
		return plan, err
	}
	for _, id := range plan.EligibleEditions {
		binding, kb, state, er := b.Store.ProjectionBinding(ctx, id)
		if er != nil {
			if core.AsError(er).Code == "NOT_FOUND" {
				continue
			}
			return plan, er
		}
		if state == "remote_deleted" {
			continue
		}
		if (binding.Neo4j != nil || len(binding.Vectors) > 0) && !external {
			return plan, core.Err("EXTERNAL_CLEANUP_REQUIRED", "batch includes remote projections; enable content_external explicitly or use offline prune", 409)
		}
		if er = b.Store.ProjectionState(ctx, id, "deleting"); er != nil {
			return plan, er
		}
		for _, cfg := range binding.Vectors {
			if cfg.Qdrant == nil || cfg.Embedding == nil {
				return plan, core.Err("MODEL_MISMATCH", "incomplete projection binding", 409)
			}
			q := adapters.Qdrant{Endpoint: *cfg.Qdrant, Embed: &adapters.Embedder{Config: *cfg.Embedding}, Origin: b.Store.Origin}
			if er = q.DeleteEdition(ctx, kb, id); er != nil {
				return plan, er
			}
		}
		if binding.Neo4j != nil {
			n := adapters.Neo4j{Config: *binding.Neo4j, Origin: b.Store.Origin}
			if er = n.DeleteEdition(ctx, id); er != nil {
				return plan, er
			}
		}
		if er = b.Store.ProjectionState(ctx, id, "remote_deleted"); er != nil {
			return plan, er
		}
	}
	out, err := b.Store.PruneContentBatch(ctx, days, batch, after, true)
	if err != nil {
		return out, err
	}
	out.ExternalBackendsCleaned = external
	if err = b.Store.SetContentCursor(ctx, out.NextAfter); err != nil {
		return out, err
	}
	return out, nil
}
