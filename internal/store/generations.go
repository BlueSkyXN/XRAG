package store

import (
	"context"
	"encoding/json"
	"strings"
	"time"
	"github.com/BlueSkyXN/XRAG/adapters"
	"github.com/BlueSkyXN/XRAG/core"
	db "github.com/BlueSkyXN/XRAG/internal/sqlite"
)

var generationRegistrySchema = []string{
	`CREATE TABLE IF NOT EXISTS vector_registry(id TEXT PRIMARY KEY NOT NULL,kb TEXT NOT NULL,physical TEXT NOT NULL,data TEXT NOT NULL,state TEXT NOT NULL,created INTEGER NOT NULL,retired_at INTEGER NOT NULL DEFAULT 0,updated INTEGER NOT NULL)`,
	`CREATE TABLE IF NOT EXISTS projection_generation(edition TEXT NOT NULL,registry_id TEXT NOT NULL,PRIMARY KEY(edition,registry_id))`,
	`CREATE INDEX IF NOT EXISTS projection_generation_registry ON projection_generation(registry_id)`,
	`CREATE TRIGGER IF NOT EXISTS projection_generation_retire AFTER DELETE ON projection_gc BEGIN DELETE FROM projection_generation WHERE edition=old.edition; UPDATE vector_registry SET state='retired',retired_at=CAST(strftime('%s','now') AS INTEGER),updated=CAST(strftime('%s','now') AS INTEGER) WHERE state='present' AND NOT EXISTS(SELECT 1 FROM projection_generation p WHERE p.registry_id=vector_registry.id); END`,
}

type VectorGeneration struct {
	ID        string          `json:"id"`
	KB        string          `json:"kb"`
	Physical  string          `json:"physical_collection"`
	State     string          `json:"state"`
	Created   int64           `json:"created_at"`
	RetiredAt int64           `json:"retired_at"`
	Holds     int             `json:"edition_holds"`
	Config    adapters.Config `json:"-"`
}

func VectorRegistryID(origin, kb string, cfg adapters.Config) string {
	if cfg.Qdrant == nil || cfg.Embedding == nil {
		return ""
	}
	q := adapters.Qdrant{Origin: origin, Endpoint: *cfg.Qdrant, Embed: &adapters.Embedder{Config: *cfg.Embedding}}
	// Credentials are locators, not physical identity. Rotation of a key reference
	// must not create two competing owners of the same physical collection.
	return "vg_" + core.Hash([]string{origin, kb, strings.TrimRight(cfg.Qdrant.URL, "/"), q.Collection(kb)})[:32]
}
func registerProjectionGenerations(c *db.Conn, origin string, d core.Document, b ProjectionBinding) error {
	if e := c.Exec("DELETE FROM projection_generation WHERE edition=?", d.Edition); e != nil {
		return e
	}
	for _, cfg := range b.Vectors {
		if cfg.Qdrant == nil || cfg.Embedding == nil {
			continue
		}
		id := VectorRegistryID(origin, d.KB, cfg)
		q := adapters.Qdrant{Origin: origin, Endpoint: *cfg.Qdrant, Embed: &adapters.Embedder{Config: *cfg.Embedding}}
		// Store only the two bindings necessary for cleanup, never unrelated model
		// endpoints or executable configuration from the runtime.
		clean := adapters.Config{Qdrant: cfg.Qdrant, Embedding: cfg.Embedding}
		now := time.Now().Unix()
		if e := c.Exec(`INSERT INTO vector_registry(id,kb,physical,data,state,created,updated) VALUES(?,?,?,?,'present',?,?) ON CONFLICT(id) DO UPDATE SET data=excluded.data,state='present',retired_at=0,updated=excluded.updated`, id, d.KB, q.Collection(d.KB), core.JSON(clean), now, now); e != nil {
			return e
		}
		if e := c.Exec("INSERT OR IGNORE INTO projection_generation VALUES(?,?)", d.Edition, id); e != nil {
			return e
		}
	}
	// A repair can replace a failed staging binding before publication. Retire
	// formerly held generations only after all new links have been registered.
	return c.Exec(`UPDATE vector_registry SET state='retired',retired_at=?,updated=? WHERE state='present' AND NOT EXISTS(SELECT 1 FROM projection_generation p WHERE p.registry_id=vector_registry.id)`, time.Now().Unix(), time.Now().Unix())
}
func (s *Store) VectorGenerations(ctx context.Context) ([]VectorGeneration, error) {
	out := []VectorGeneration{}
	err := s.DB.Tx(ctx, false, func(c *db.Conn) error {
		rows, e := c.Query(`SELECT g.*, (SELECT COUNT(*) FROM projection_generation p WHERE p.registry_id=g.id) holds FROM vector_registry g WHERE g.state<>'deleted' ORDER BY g.kb,g.created,g.id LIMIT 1001`)
		if e != nil {
			return e
		}
		if len(rows) > 1000 {
			return core.Err("MAINTENANCE_LIMIT", "over 1000 vector generations; no collection deletion performed", 422)
		}
		for _, r := range rows {
			g := VectorGeneration{ID: db.String(r, "id"), KB: db.String(r, "kb"), Physical: db.String(r, "physical"), State: db.String(r, "state"), Created: db.Int(r, "created"), RetiredAt: db.Int(r, "retired_at"), Holds: int(db.Int(r, "holds"))}
			if e = json.Unmarshal([]byte(db.String(r, "data")), &g.Config); e != nil {
				return e
			}
			out = append(out, g)
		}
		return nil
	})
	return out, err
}

// VectorCollection reads the physical binding saved for this edition, including
// collections created before the current naming schema.
func (s *Store) VectorCollection(ctx context.Context, edition string, cfg adapters.Config) (string, error) {
	physical := ""
	if cfg.Qdrant == nil || cfg.Embedding == nil {
		return physical, nil
	}
	err := s.DB.Do(ctx, func(c *db.Conn) error {
		rows, err := c.Query(`SELECT g.physical,g.data FROM projection_generation p JOIN vector_registry g ON g.id=p.registry_id WHERE p.edition=? AND g.state<>'deleted'`, edition)
		if err != nil {
			return err
		}
		for _, row := range rows {
			var bound adapters.Config
			if err := json.Unmarshal([]byte(db.String(row, "data")), &bound); err != nil {
				return err
			}
			if bound.Qdrant == nil || bound.Embedding == nil || bound.Embedding.SemanticID() != cfg.Embedding.SemanticID() || strings.TrimRight(bound.Qdrant.URL, "/") != strings.TrimRight(cfg.Qdrant.URL, "/") {
				continue
			}
			name := db.String(row, "physical")
			if physical != "" && physical != name {
				return core.Err("MODEL_MISMATCH", "edition has conflicting vector collection bindings", 409)
			}
			physical = name
		}
		return nil
	})
	return physical, err
}

// Called only under the exclusive offline workspace lock. It also rejects
// pending jobs because they can reference retired configuration after restart.
func (s *Store) RequireNoPendingJobs(ctx context.Context) error {
	return s.DB.Do(ctx, func(c *db.Conn) error {
		rs, e := c.Query("SELECT id FROM job WHERE state IN ('queued','running') AND cancel=0 LIMIT 1")
		if e != nil {
			return e
		}
		if len(rs) > 0 {
			return core.Err("WORKSPACE_BUSY", "finish or cancel pending jobs before generation collection", 409)
		}
		return nil
	})
}
func (s *Store) SetGenerationGCState(ctx context.Context, id, state string) error {
	if state != "deleting" && state != "deleted" {
		return core.Err("INVALID_ARGUMENT", "invalid collection cleanup state", 400)
	}
	return s.DB.Tx(ctx, true, func(c *db.Conn) error {
		r, e := c.Query("SELECT g.id FROM vector_registry g WHERE id=? AND NOT EXISTS(SELECT 1 FROM projection_generation p WHERE p.registry_id=g.id)", id)
		if e != nil {
			return e
		}
		if len(r) != 1 {
			return core.Err("VERSION_CONFLICT", "generation gained edition holders", 409)
		}
		return c.Exec("UPDATE vector_registry SET state=?,updated=? WHERE id=?", state, time.Now().Unix(), id)
	})
}
