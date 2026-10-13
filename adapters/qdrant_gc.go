package adapters

import (
	"context"
	"github.com/BlueSkyXN/XRAG/core"
)

// CollectionExists checks presence without interpreting an invalid or empty
// HTTP response as an absent collection.
func (q *Qdrant) CollectionExists(ctx context.Context, physical string) (bool, error) {
	var v struct {
		Result *struct {
			Exists *bool `json:"exists"`
		} `json:"result"`
	}
	if e := q.call(ctx, "GET", "/collections/"+physical+"/exists", nil, &v); e != nil {
		return false, e
	}
	if v.Result == nil || v.Result.Exists == nil {
		return false, core.Err("BACKEND_UNAVAILABLE", "missing collection existence response", 503)
	}
	return *v.Result.Exists, nil
}

// VerifyEmptyOwnedCollection never creates or writes a collection. It refuses
// foreign/mismatched descriptors, any alias, and any remaining point.
func (q *Qdrant) VerifyEmptyOwnedCollection(ctx context.Context, kb, physical string) error {
	if physical != q.Collection(kb) {
		return core.Err("MODEL_MISMATCH", "physical collection is not the recorded generation", 409)
	}
	desc, e := q.Descriptor(ctx, physical)
	if e != nil {
		return e
	}
	if desc["origin_id"] != q.Origin || desc["kb"] != kb || desc["model_identity"] != q.Embed.Config.SemanticID() {
		return core.Err("REF_ORIGIN_MISMATCH", "collection descriptor belongs to another model or authority", 409)
	}
	var aliases struct {
		Result *struct {
			Aliases []struct {
				Alias      string `json:"alias_name"`
				Collection string `json:"collection_name"`
			} `json:"aliases"`
		} `json:"result"`
	}
	if e = q.call(ctx, "GET", "/aliases", nil, &aliases); e != nil {
		return e
	}
	if aliases.Result == nil {
		return core.Err("BACKEND_UNAVAILABLE", "invalid alias inventory", 503)
	}
	for _, a := range aliases.Result.Aliases {
		if a.Collection == physical {
			return core.Err("COLLECTION_IN_USE", "collection is still named by an alias", 409)
		}
	}
	var count struct {
		Result *struct {
			Count *int64 `json:"count"`
		} `json:"result"`
	}
	if e = q.call(ctx, "POST", "/collections/"+physical+"/points/count", map[string]any{"exact": true}, &count); e != nil {
		return e
	}
	if count.Result == nil || count.Result.Count == nil || *count.Result.Count < 0 {
		return core.Err("BACKEND_UNAVAILABLE", "missing exact collection count", 503)
	}
	if *count.Result.Count != 0 {
		return core.Err("COLLECTION_NOT_EMPTY", "generation still contains points; prune eligible editions first", 409)
	}
	return nil
}
func (q *Qdrant) DropPhysicalCollection(ctx context.Context, physical string) error {
	var res struct {
		Result *bool `json:"result"`
	}
	if e := q.call(ctx, "DELETE", "/collections/"+physical, nil, &res); e != nil {
		return e
	}
	if res.Result == nil || !*res.Result {
		return core.Err("BACKEND_UNAVAILABLE", "collection deletion not confirmed", 503)
	}
	exists, e := q.CollectionExists(ctx, physical)
	if e != nil {
		return e
	}
	if exists {
		return core.Err("BACKEND_UNAVAILABLE", "collection remains after deletion", 503)
	}
	return nil
}
func (q *Qdrant) RemoveDescriptor(ctx context.Context, physical string) error {
	return q.call(ctx, "POST", "/collections/xrag_demo_meta/points/delete?wait=true", map[string]any{"points": []string{core.UUID(physical)}}, nil)
}

// VerifyRecoveryTarget permits only a dedicated absent collection or one
// resumed by the same local recovery operation. It never empties a target.
func (q *Qdrant) VerifyRecoveryTarget(ctx context.Context, kb string, resume bool) error {
	name := q.Collection(kb)
	exists, err := q.CollectionExists(ctx, name)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	if !resume {
		return core.Err("RECOVERY_TARGET_NOT_EMPTY", "use a dedicated absent Qdrant collection; existing data is not overwritten", 409)
	}
	d, err := q.Descriptor(ctx, name)
	if err != nil {
		return err
	}
	if d["origin_id"] != q.Origin || d["kb"] != kb || d["model_identity"] != q.Embed.Config.SemanticID() {
		return core.Err("REF_ORIGIN_MISMATCH", "recovery target descriptor changed", 409)
	}
	return nil
}
