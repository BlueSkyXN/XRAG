package build

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"
	"github.com/BlueSkyXN/XRAG/adapters"
	"github.com/BlueSkyXN/XRAG/core"
	"github.com/BlueSkyXN/XRAG/internal/oplock"
	"github.com/BlueSkyXN/XRAG/internal/store"
)

type RecoveryTarget struct {
	Qdrant *adapters.Endpoint  `json:"qdrant,omitempty"`
	Neo4j  *adapters.NeoConfig `json:"neo4j,omitempty"`
}
type RecoveryTargets struct {
	Bindings map[string]RecoveryTarget `json:"bindings"`
}
type RecoveryReport struct {
	ID           string   `json:"recovery_id"`
	DryRun       bool     `json:"dry_run"`
	Origin       string   `json:"origin_id"`
	Editions     int      `json:"editions"`
	VectorPoints int      `json:"vector_points"`
	Supports     int      `json:"supports"`
	Completed    int      `json:"completed_editions"`
	ModelCalls   int      `json:"model_calls"`
	State        string   `json:"state"`
	Collections  []string `json:"physical_collections"`
	Notes        []string `json:"notes"`
}

// RecoverProjections is an explicit OFFLINE operation on a restored copy. The
// caller holds RuntimeLock exclusively. Saved vectors and support records are
// replayed verbatim, with no embedding, graph extraction or source fetch.
// This recovers XRAG-managed projections, not arbitrary vendor databases.
func (b *Builder) RecoverProjections(ctx context.Context, targets RecoveryTargets, apply bool) (RecoveryReport, error) {
	out := RecoveryReport{DryRun: !apply, Origin: b.Store.Origin, State: "planned", Collections: []string{}, Notes: []string{"XRAG managed projections only; sources, provider credentials, arbitrary external records and vendor snapshots are not included.", "Keep this restored copy offline until completion. Dedicated target namespaces must have no other writers.", "Provider configurations remain disabled after restore: enable the original matching model endpoints explicitly before semantic queries."}}
	if _, err := os.Stat(filepath.Join(b.Store.Dir, "RESTORE_INFO.json")); err != nil {
		return out, core.Err("RESTORED_COPY_REQUIRED", "restore a same-version SQLite snapshot into a new directory first", 409)
	}
	locked, release, err := oplock.Acquire(ctx, b.Store.Dir, true)
	if err != nil {
		return out, err
	}
	defer release()
	ctx = locked
	if err = b.Store.RequireNoPendingJobs(ctx); err != nil {
		return out, err
	}
	if len(targets.Bindings) == 0 {
		return out, core.Err("INVALID_ARGUMENT", "at least one KB recovery binding required", 400)
	}
	for kb, t := range targets.Bindings {
		if !core.ValidKB(kb) || t.Qdrant == nil && t.Neo4j == nil {
			return out, core.Err("INVALID_ARGUMENT", "invalid recovery binding", 400)
		}
		if t.Qdrant != nil {
			if err = adapters.ValidURL(t.Qdrant.URL); err != nil {
				return out, err
			}
		}
		if t.Neo4j != nil {
			if err = adapters.ValidURL(t.Neo4j.URL); err != nil {
				return out, err
			}
			if !core.ValidKB(t.Neo4j.Database) {
				return out, core.Err("INVALID_ARGUMENT", "invalid Neo4j database", 400)
			}
		}
		if err = store.ValidateSecretReferences(adapters.Config{Qdrant: t.Qdrant, Neo4j: t.Neo4j}); err != nil {
			return out, err
		}
	}
	inv, err := b.Store.RecoveryEditions(ctx)
	if err != nil {
		return out, err
	}
	selected := []store.RecoveryEdition{}
	configs := map[string]adapters.Config{}
	bindings := map[string]store.ProjectionBinding{}
	manifest := []any{}
	for _, it := range inv {
		t, ok := targets.Bindings[it.KB]
		if !ok {
			continue
		}
		d, e := b.Store.RecoveryDocument(ctx, it.ID)
		if e != nil {
			return out, e
		}
		bind, _, _, be := b.Store.ProjectionBinding(ctx, it.ID)
		if be != nil && core.AsError(be).Code != "NOT_FOUND" {
			return out, be
		}
		if t.Neo4j != nil {
			bind.Neo4j = t.Neo4j
		}
		if t.Qdrant != nil {
			bind.Vectors = []adapters.Config{}
		}
		generations := map[string]bool{}
		for _, c := range d.Chunks {
			if len(c.Vector) == 0 {
				continue
			}
			if c.Generation == "" {
				return out, core.Err("MODEL_MISMATCH", "stored vector lacks generation", 409)
			}
			cfg, e := b.Store.Generation(ctx, d.KB, c.Generation)
			if e != nil {
				return out, e
			}
			if cfg.Embedding == nil || cfg.Embedding.SemanticID() != c.Generation || len(c.Vector) != cfg.Embedding.Dimension {
				return out, core.Err("MODEL_MISMATCH", "stored vector does not match its descriptor", 409)
			}
			if _, e = core.Cosine(c.Vector, c.Vector); e != nil {
				return out, e
			}
			if t.Qdrant != nil {
				cfg.Qdrant = t.Qdrant
				configs[d.KB+":"+c.Generation] = cfg
				out.VectorPoints++
				if !generations[c.Generation] {
					bind.Vectors = append(bind.Vectors, cfg)
					generations[c.Generation] = true
				}
			}
		}
		if t.Qdrant != nil && it.Vectors == 0 {
			out.Notes = append(out.Notes, "An edition has no saved vectors; its text is restored locally, but no fake vector is generated.")
		}
		if t.Neo4j != nil {
			out.Supports += len(d.Supports)
		}
		selected = append(selected, it)
		bindings[it.ID] = bind
		manifest = append(manifest, []any{it.ID, core.Hash(d)})
	}
	if len(selected) == 0 {
		return out, core.Err("INVALID_ARGUMENT", "no readable editions for selected KBs", 400)
	}
	out.Editions = len(selected)
	out.ID = "recover_" + core.Hash([]any{out.Origin, targets, manifest})[:32]
	keys := []string{}
	for key, cfg := range configs {
		keys = append(keys, key)
		kb := key[:len(key)-len(cfg.Embedding.SemanticID())-1]
		q := adapters.Qdrant{Endpoint: *cfg.Qdrant, Embed: &adapters.Embedder{Config: *cfg.Embedding}, Origin: out.Origin}
		out.Collections = append(out.Collections, q.Collection(kb))
	}
	sort.Strings(keys)
	sort.Strings(out.Collections)
	out.Notes = core.Unique(out.Notes)
	state, err := b.Store.RecoveryState(ctx, out.ID)
	if err != nil {
		return out, err
	}
	if v, ok := state["completed_editions"].(float64); ok {
		out.Completed = int(v)
	}
	if state["state"] == "succeeded" {
		out.State = "succeeded"
		return out, nil
	}
	if !apply {
		return out, nil
	}
	// Reject another incomplete recovery with different target/content via local
	// operation marker, rather than retargeting an interrupted operation.
	active, err := b.Store.RecoveryState(ctx, "active")
	if err != nil {
		return out, err
	}
	if id, ok := active["id"].(string); ok && id != out.ID {
		previous, err := b.Store.RecoveryState(ctx, id)
		if err != nil {
			return out, err
		}
		if previous["state"] != "succeeded" {
			return out, core.Err("VERSION_CONFLICT", "another recovery operation is unfinished", 409)
		}
		if err = b.Store.SetRecoveryState(ctx, "active", map[string]any{}); err != nil {
			return out, err
		}
	}
	resume := state["prepared"] == true
	if !resume {
		for _, key := range keys {
			cfg := configs[key]
			kb := key[:len(key)-len(cfg.Embedding.SemanticID())-1]
			q := adapters.Qdrant{Endpoint: *cfg.Qdrant, Embed: &adapters.Embedder{Config: *cfg.Embedding}, Origin: out.Origin}
			if err = q.VerifyRecoveryTarget(ctx, kb, false); err != nil {
				return out, err
			}
		}
		seen := map[string]bool{}
		for _, t := range targets.Bindings {
			if t.Neo4j == nil {
				continue
			}
			key := core.Hash(t.Neo4j)
			if seen[key] {
				continue
			}
			seen[key] = true
			n := adapters.Neo4j{Config: *t.Neo4j, Origin: out.Origin}
			if err = n.VerifyRecoveryTarget(ctx, false); err != nil {
				return out, err
			}
		}
		state = map[string]any{"prepared": true, "state": "running", "completed_editions": 0, "created_at": time.Now().Unix()}
		if err = b.Store.BeginRecovery(ctx, out.ID, state); err != nil {
			return out, err
		}
	}
	out.State = "running"
	for i, it := range selected {
		if i < out.Completed {
			continue
		}
		if err = ctx.Err(); err != nil {
			return out, err
		}
		d, e := b.Store.RecoveryDocument(ctx, it.ID)
		if e != nil {
			return out, e
		}
		bind := bindings[it.ID]
		for _, cfg := range bind.Vectors {
			if targets.Bindings[it.KB].Qdrant == nil {
				continue
			}
			q := adapters.Qdrant{Endpoint: *cfg.Qdrant, Embed: &adapters.Embedder{Config: *cfg.Embedding}, Origin: out.Origin}
			// Once a namespace is prepared, deterministic upserts can resume even if a
			// crash happened between collection creation and descriptor creation.
			part := d
			part.Chunks = nil
			for _, c := range d.Chunks {
				if c.Generation == cfg.Embedding.SemanticID() && len(c.Vector) > 0 {
					part.Chunks = append(part.Chunks, c)
				}
			}
			if e = q.Write(ctx, part); e != nil {
				state["state"] = "failed"
				state["error"] = core.AsError(e).Code
				recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
				_ = b.Store.SetRecoveryState(recordCtx, out.ID, state)
				cancel()
				out.State = "failed"
				return out, e
			}
		}
		if targets.Bindings[it.KB].Neo4j != nil {
			n := adapters.Neo4j{Config: *bind.Neo4j, Origin: out.Origin}
			if e = n.Write(ctx, d); e != nil {
				state["state"] = "failed"
				state["error"] = core.AsError(e).Code
				recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
				_ = b.Store.SetRecoveryState(recordCtx, out.ID, state)
				cancel()
				out.State = "failed"
				return out, e
			}
		}
		state["completed_editions"] = i + 1
		state["state"] = "running"
		if err = b.Store.SetRecoveryState(ctx, out.ID, state); err != nil {
			return out, err
		}
		out.Completed = i + 1
	}
	// Remote writes are complete. Rebind the restored copy only, never original
	// live metadata. Repeating this phase after a crash is idempotent.
	for _, it := range selected {
		d, e := b.Store.RecoveryDocument(ctx, it.ID)
		if e != nil {
			return out, e
		}
		if e = b.Store.SetRecoveredBinding(ctx, d, bindings[it.ID]); e != nil {
			return out, e
		}
	}
	state["state"] = "succeeded"
	delete(state, "error")
	if err = b.Store.SetRecoveryState(ctx, out.ID, state); err != nil {
		return out, err
	}
	if err = b.Store.SetRecoveryState(ctx, "active", map[string]any{}); err != nil {
		return out, err
	}
	out.State = "succeeded"
	return out, nil
}

func DecodeRecoveryTargets(raw []byte) (RecoveryTargets, error) {
	var t RecoveryTargets
	// A small versioned binding document, not executable provider configuration.
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	err := dec.Decode(&t)
	if err == nil && dec.Decode(new(any)) != io.EOF {
		err = core.Err("INVALID_ARGUMENT", "one recovery target object required", 400)
	}
	return t, err
}
