package build

import (
	"context"
	"github.com/BlueSkyXN/XRAG/adapters"
	"github.com/BlueSkyXN/XRAG/core"
	"github.com/BlueSkyXN/XRAG/internal/store"
)

// Caller holds the exclusive offline workspace lock. No content is removed until its remote projections were deleted.
func (b *Builder) PruneExternal(ctx context.Context, days int, apply, dropModels bool) (store.PruneReport, error) {
	plan, e := b.Store.PruneHistory(ctx, days, false, dropModels)
	if e != nil || !apply {
		return plan, e
	}
	for _, id := range plan.EligibleEditions {
		binding, kb, state, err := b.Store.ProjectionBinding(ctx, id)
		if err != nil {
			if core.AsError(err).Code == "NOT_FOUND" {
				continue
			}
			return plan, err
		}
		if state == "remote_deleted" {
			continue
		}
		if err = b.Store.ProjectionState(ctx, id, "deleting"); err != nil {
			return plan, err
		}
		for _, cfg := range binding.Vectors {
			if cfg.Qdrant == nil || cfg.Embedding == nil {
				return plan, core.Err("MODEL_MISMATCH", "cleanup binding is incomplete", 409)
			}
			q := &adapters.Qdrant{Endpoint: *cfg.Qdrant, Embed: &adapters.Embedder{Config: *cfg.Embedding}, Origin: b.Store.Origin}
			if err = q.DeleteEdition(ctx, kb, id); err != nil {
				return plan, err
			}
		}
		if binding.Neo4j != nil {
			n := &adapters.Neo4j{Config: *binding.Neo4j, Origin: b.Store.Origin}
			if err = n.DeleteEdition(ctx, id); err != nil {
				return plan, err
			}
		}
		if err = b.Store.ProjectionState(ctx, id, "remote_deleted"); err != nil {
			return plan, err
		}
	}
	out, e := b.Store.PruneHistory(ctx, days, true, dropModels)
	out.ExternalBackendsCleaned = e == nil
	return out, e
}
