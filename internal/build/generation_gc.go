package build

import (
	"context"
	"time"
	"github.com/BlueSkyXN/XRAG/adapters"
	"github.com/BlueSkyXN/XRAG/core"
	"github.com/BlueSkyXN/XRAG/internal/store"
)

type GenerationGCEntry struct {
	ID        string `json:"id"`
	KB        string `json:"kb"`
	Physical  string `json:"physical_collection"`
	RetiredAt int64  `json:"retired_at"`
	Holds     int    `json:"edition_holds"`
	Decision  string `json:"decision"`
	Error     string `json:"error,omitempty"`
}
type GenerationGCReport struct {
	DryRun        bool                `json:"dry_run"`
	KeepDays      int                 `json:"keep_days"`
	Entries       []GenerationGCEntry `json:"entries"`
	Removed       int                 `json:"removed"`
	RemoteChecked bool                `json:"remote_checked"`
}

// CollectVectorGenerations requires the caller's exclusive offline RuntimeLock.
// Preview is local-only. Apply never touches a collection not recorded by this
// workspace and never removes an alias or non-empty collection.
func (b *Builder) CollectVectorGenerations(ctx context.Context, days int, apply bool) (GenerationGCReport, error) {
	out := GenerationGCReport{DryRun: !apply, KeepDays: days, Entries: []GenerationGCEntry{}}
	if days < 1 || days > 36500 {
		return out, core.Err("INVALID_ARGUMENT", "keep-days must be 1..36500", 400)
	}
	if err := b.Store.RequireNoPendingJobs(ctx); err != nil {
		return out, err
	}
	desired := map[string]bool{}
	kbs, err := b.Store.KBs(ctx)
	if err != nil {
		return out, err
	}
	for _, kb := range kbs {
		bb, e := b.ForKB(ctx, kb["id"].(string))
		if e != nil {
			return out, e
		}
		desired[store.VectorRegistryID(b.Store.Origin, kb["id"].(string), bb.Config)] = true
	}
	generations, err := b.Store.VectorGenerations(ctx)
	if err != nil {
		return out, err
	}
	cutoff := time.Now().Add(-time.Duration(days) * 24 * time.Hour).Unix()
	for _, g := range generations {
		entry := GenerationGCEntry{ID: g.ID, KB: g.KB, Physical: g.Physical, RetiredAt: g.RetiredAt, Holds: g.Holds}
		switch {
		case g.Holds > 0:
			entry.Decision = "held_by_editions"
		case desired[g.ID]:
			entry.Decision = "selected_by_configuration"
		case g.RetiredAt == 0:
			entry.Decision = "not_retired"
		case g.RetiredAt > cutoff:
			entry.Decision = "retention_window"
		default:
			entry.Decision = "eligible_locally"
		}
		if apply && entry.Decision == "eligible_locally" {
			out.RemoteChecked = true
			err = b.collectOneGeneration(ctx, g)
			if err != nil {
				entry.Decision = "blocked"
				entry.Error = core.AsError(err).Code
				out.Entries = append(out.Entries, entry)
				return out, err
			}
			entry.Decision = "deleted"
			out.Removed++
		}
		out.Entries = append(out.Entries, entry)
	}
	return out, nil
}
func (b *Builder) collectOneGeneration(ctx context.Context, g store.VectorGeneration) error {
	if g.Config.Embedding == nil || g.Config.Qdrant == nil {
		return core.Err("MODEL_MISMATCH", "incomplete generation cleanup binding", 409)
	}
	q := adapters.Qdrant{Origin: b.Store.Origin, Endpoint: *g.Config.Qdrant, Embed: &adapters.Embedder{Config: *g.Config.Embedding}}
	if q.Collection(g.KB) != g.Physical || store.VectorRegistryID(b.Store.Origin, g.KB, g.Config) != g.ID {
		return core.Err("MODEL_MISMATCH", "recorded generation binding identity mismatch", 409)
	}
	exists, err := q.CollectionExists(ctx, g.Physical)
	if err != nil {
		return err
	}
	if exists {
		if err = q.VerifyEmptyOwnedCollection(ctx, g.KB, g.Physical); err != nil {
			return err
		}
		if err = b.Store.SetGenerationGCState(ctx, g.ID, "deleting"); err != nil {
			return err
		}
		if err = q.DropPhysicalCollection(ctx, g.Physical); err != nil {
			return err
		}
	} else {
		// Absence is accepted only for this locally retired recorded collection.
		// A previous successful delete may have lost its response.
		if err = b.Store.SetGenerationGCState(ctx, g.ID, "deleting"); err != nil {
			return err
		}
	}
	if err = q.RemoveDescriptor(ctx, g.Physical); err != nil {
		return err
	}
	return b.Store.SetGenerationGCState(ctx, g.ID, "deleted")
}
