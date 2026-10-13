package store

import (
	"context"
	"encoding/json"
	"time"
	"github.com/BlueSkyXN/XRAG/adapters"
	"github.com/BlueSkyXN/XRAG/core"
	db "github.com/BlueSkyXN/XRAG/internal/sqlite"
)

type RecoveryEdition struct {
	ID       string `json:"edition_id"`
	KB       string `json:"kb"`
	State    string `json:"state"`
	Hash     string `json:"file_hash"`
	Chunks   int    `json:"chunks"`
	Vectors  int    `json:"vectors"`
	Supports int    `json:"supports"`
}

// RecoveryEditions selects all readable active/history editions, not staged,
// aborted or explicitly withdrawn data. It contains no corpus text/vector copy.
func (s *Store) RecoveryEditions(ctx context.Context) ([]RecoveryEdition, error) {
	out := []RecoveryEdition{}
	err := s.DB.Do(ctx, func(c *db.Conn) error {
		rows, err := c.Query(`SELECT e.id,e.kb,e.state,e.hash,json_array_length(e.data,'$.chunks') chunks,json_array_length(e.data,'$.supports') supports,(SELECT COUNT(*) FROM json_each(e.data,'$.chunks') j WHERE json_array_length(j.value,'$.vector')>0) vectors FROM edition e JOIN file f ON f.kb=e.kb AND f.path=e.path WHERE f.withdrawn=0 AND e.state IN ('active','retired') AND NOT EXISTS(SELECT 1 FROM kb_options k WHERE k.kb=e.kb AND k.deleted=1) ORDER BY e.id LIMIT 100001`)
		if err != nil {
			return err
		}
		if len(rows) > 100000 {
			return core.Err("RECOVERY_LIMIT", "over 100000 editions; export a narrower workspace", 422)
		}
		for _, r := range rows {
			out = append(out, RecoveryEdition{db.String(r, "id"), db.String(r, "kb"), db.String(r, "state"), db.String(r, "hash"), int(db.Int(r, "chunks")), int(db.Int(r, "vectors")), int(db.Int(r, "supports"))})
		}
		return nil
	})
	return out, err
}
func (s *Store) RecoveryDocument(ctx context.Context, id string) (core.Document, error) {
	var d core.Document
	err := s.DB.Do(ctx, func(c *db.Conn) error {
		rs, err := c.Query(`SELECT e.data FROM edition e JOIN file f ON f.kb=e.kb AND f.path=e.path WHERE e.id=? AND f.withdrawn=0 AND e.state IN('active','retired')`, id)
		if err != nil {
			return err
		}
		if len(rs) != 1 {
			return core.Err("REF_GONE", "recovery edition unavailable", 410)
		}
		return json.Unmarshal([]byte(db.String(rs[0], "data")), &d)
	})
	return d, err
}

// RecoveryState is local durable progress. A failed response can be retried by
// rewriting the same deterministic identities in the same dedicated target.
func (s *Store) RecoveryState(ctx context.Context, id string) (map[string]any, error) {
	m := map[string]any{}
	err := s.DB.Do(ctx, func(c *db.Conn) error {
		rs, err := c.Query("SELECT v FROM meta WHERE k=?", "projection_recovery:"+id)
		if err != nil {
			return err
		}
		if len(rs) == 0 {
			return nil
		}
		return json.Unmarshal([]byte(db.String(rs[0], "v")), &m)
	})
	return m, err
}
func (s *Store) SetRecoveryState(ctx context.Context, id string, m map[string]any) error {
	return s.DB.Do(ctx, func(c *db.Conn) error {
		return c.Exec("INSERT INTO meta(k,v) VALUES(?,?) ON CONFLICT(k) DO UPDATE SET v=excluded.v", "projection_recovery:"+id, core.JSON(m))
	})
}
func (s *Store) SetRecoveredBinding(ctx context.Context, d core.Document, b ProjectionBinding) error {
	return s.DB.Tx(ctx, true, func(c *db.Conn) error {
		if err := registerProjectionGenerations(c, s.Origin, d, b); err != nil {
			return err
		}
		if err := c.Exec("INSERT INTO projection_gc(edition,kb,data,state,created) VALUES(?,?,?,'present',?) ON CONFLICT(edition) DO UPDATE SET data=excluded.data,state='present'", d.Edition, d.KB, core.JSON(b), time.Now().Unix()); err != nil {
			return err
		}
		for _, cfg := range b.Vectors {
			if cfg.Embedding == nil {
				continue
			}
			clean := adapters.Config{Embedding: cfg.Embedding, Qdrant: cfg.Qdrant}
			if err := c.Exec("UPDATE generation_config SET data=? WHERE kb=? AND generation=?", core.JSON(clean), d.KB, cfg.Embedding.SemanticID()); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Store) CheckRecoveryComplete(ctx context.Context) error {
	m, err := s.RecoveryState(ctx, "active")
	if err != nil {
		return err
	}
	if id, ok := m["id"].(string); ok && id != "" {
		state, err := s.RecoveryState(ctx, id)
		if err != nil {
			return err
		}
		if state["state"] != "succeeded" {
			return core.Err("RECOVERY_INCOMPLETE", "finish the offline projection recovery before serving this restored copy", 409)
		}
	}
	return nil
}

// BeginRecovery marks the copy unavailable before any remote side effect. Both
// progress and startup marker commit together.
func (s *Store) BeginRecovery(ctx context.Context, id string, state map[string]any) error {
	return s.DB.Tx(ctx, true, func(c *db.Conn) error {
		for k, v := range map[string]map[string]any{id: state, "active": {"id": id}} {
			if err := c.Exec("INSERT INTO meta(k,v) VALUES(?,?) ON CONFLICT(k) DO UPDATE SET v=excluded.v", "projection_recovery:"+k, core.JSON(v)); err != nil {
				return err
			}
		}
		return nil
	})
}
