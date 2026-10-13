package store

import (
	"context"
	"encoding/json"
	"time"
	"github.com/BlueSkyXN/XRAG/core"
	db "github.com/BlueSkyXN/XRAG/internal/sqlite"
)

type actorKey struct{}

func WithActor(ctx context.Context, actor string) context.Context {
	return context.WithValue(ctx, actorKey{}, actor)
}
func Actor(ctx context.Context) string { s, _ := ctx.Value(actorKey{}).(string); return s }

type archivedEvidence struct {
	Ref, KB, Owner string
	Epoch          int64
	Data           any
	Paths          []string
}

func (s *Store) prepareEvidence(ctx context.Context, raw, kb string) (archivedEvidence, error) {
	a := archivedEvidence{Ref: raw, KB: kb, Owner: Actor(ctx)}
	r, e := core.DecodeRef(raw)
	if e != nil {
		return a, e
	}
	if r.Origin != s.Origin {
		return a, core.Err("REF_ORIGIN_MISMATCH", "evidence belongs to another origin", 409)
	}
	// A previously materialized archive may be cited again by another authorized KB maintainer.
	found := false
	e = s.DB.Do(ctx, func(c *db.Conn) error {
		rs, e := c.Query("SELECT * FROM evidence_archive WHERE ref=?", raw)
		if e != nil {
			return e
		}
		if len(rs) > 0 {
			found = true
			a.KB = db.String(rs[0], "kb")
			a.Epoch = db.Int(rs[0], "epoch")
			if e = json.Unmarshal([]byte(db.String(rs[0], "data")), &a.Data); e != nil {
				return e
			}
			return json.Unmarshal([]byte(db.String(rs[0], "paths")), &a.Paths)
		}
		return nil
	})
	if e != nil {
		return a, e
	}
	if found {
		if a.KB != kb {
			return a, core.Err("PERMISSION_DENIED", "evidence outside target KB", 403)
		}
		ep, e := s.Epoch(ctx)
		if e != nil {
			return a, e
		}
		if ep != a.Epoch {
			return a, core.Err("REF_WITHDRAWN", "source visibility changed after evidence was recorded", 410)
		}
		return a, nil
	}
	cache, e := s.GetCache(ctx, r.ResultID)
	if e != nil {
		return a, e
	}
	if cache.Kind != "result" || cache.Expires <= time.Now().Unix() {
		return a, core.Err("RESULT_EXPIRED", "result must be cited before it expires", 410)
	}
	if a.Owner == "" || cache.Owner != a.Owner {
		return a, core.Err("PERMISSION_DENIED", "cite a result produced by the current caller", 403)
	}
	if !core.Subset(cache.Kbs, []string{kb}) {
		return a, core.Err("PERMISSION_DENIED", "cross-KB evidence publishing is not enabled", 403)
	}
	var frozen struct {
		Response     core.Response `json:"response"`
		Items        []core.Item   `json:"items"`
		Dependencies []core.Ref    `json:"dependencies"`
		Request      core.Request  `json:"request"`
	}
	if e = json.Unmarshal(cache.Data, &frozen); e != nil {
		return a, e
	}
	if r.Index == -1 {
		if frozen.Response.Total.Value == nil || !frozen.Response.Total.CalculationComplete || frozen.Response.Total.Accuracy != "exact" || frozen.Response.Coverage.ExecutionPartial {
			return a, core.Err("INCOMPLETE_RESULT", "this result cannot support an exact aggregate claim", 422)
		}
		a.Data = map[string]any{"kind": "aggregate", "metric": frozen.Response.Total, "coverage": frozen.Response.Coverage, "request": frozen.Request, "query_id": frozen.Response.QueryID, "source_refs": frozen.Dependencies}
		for _, ref := range frozen.Dependencies {
			if ref.KB != kb {
				return a, core.Err("PERMISSION_DENIED", "cross-KB dependency", 403)
			}
			a.Paths = append(a.Paths, ref.Path)
		}
	} else {
		if r.Index < 0 || r.Index >= len(frozen.Items) {
			return a, core.Err("REF_GONE", "result member is missing", 410)
		}
		it := frozen.Items[r.Index]
		if it.Kind != "path" && it.Kind != "relation" && it.Kind != "entity" {
			return a, core.Err("INVALID_ARGUMENT", "search candidates are not final evidence; read the source first", 422)
		}
		a.Data = map[string]any{"kind": it.Kind, "item": it, "query_id": frozen.Response.QueryID, "coverage": frozen.Response.Coverage, "request": frozen.Request}
		for _, raw := range it.Refs {
			ref, e := core.DecodeRef(raw)
			if e != nil {
				return a, e
			}
			if ref.KB != kb {
				return a, core.Err("PERMISSION_DENIED", "cross-KB dependency", 403)
			}
			a.Paths = append(a.Paths, ref.Path)
		}
		if len(a.Paths) == 0 {
			for _, ref := range frozen.Dependencies {
				a.Paths = append(a.Paths, ref.Path)
			}
		}
	}
	a.Paths = core.Unique(a.Paths)
	a.Epoch = cache.Epoch
	return a, nil
}
func (s *Store) ReadEvidence(ctx context.Context, raw, owner string, kbs []string) (any, bool, error) {
	var data any
	found := false
	e := s.DB.Do(ctx, func(c *db.Conn) error {
		r, e := c.Query("SELECT * FROM evidence_archive WHERE ref=?", raw)
		if e != nil || len(r) == 0 {
			return e
		}
		found = true
		kb := db.String(r[0], "kb")
		if !core.Subset([]string{kb}, kbs) {
			return core.Err("PERMISSION_DENIED", "evidence exceeds current KB access", 403)
		}
		ep, e := epoch(c)
		if e != nil {
			return e
		}
		if ep != db.Int(r[0], "epoch") {
			return core.Err("REF_WITHDRAWN", "withdrawal invalidated this saved evidence", 410)
		}
		return json.Unmarshal([]byte(db.String(r[0], "data")), &data)
	})
	return data, found, e
}
func (s *Store) CheckEvidence(ctx context.Context, raw, kb string) error {
	r, e := core.DecodeRef(raw)
	if e != nil {
		return e
	}
	if r.ResultID != "" {
		_, found, e := s.ReadEvidence(ctx, raw, Actor(ctx), []string{kb})
		if e != nil {
			return e
		}
		if !found {
			return core.Err("REF_GONE", "saved evidence is missing", 410)
		}
		return nil
	}
	if r.KB != kb {
		return core.Err("PERMISSION_DENIED", "reference outside KB", 403)
	}
	_, _, _, e = s.Read(ctx, r)
	return e
}
