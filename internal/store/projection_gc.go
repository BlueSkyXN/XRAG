package store

import (
	"context"
	"encoding/json"
	"time"
	"github.com/BlueSkyXN/XRAG/adapters"
	"github.com/BlueSkyXN/XRAG/core"
	db "github.com/BlueSkyXN/XRAG/internal/sqlite"
)

type ProjectionBinding struct {
	Neo4j   *adapters.NeoConfig `json:"neo4j,omitempty"`
	Vectors []adapters.Config   `json:"vectors,omitempty"`
}

func (s *Store) BindProjections(ctx context.Context, d core.Document, b ProjectionBinding) error {
	return s.DB.Tx(ctx, true, func(c *db.Conn) error {
		if err := registerProjectionGenerations(c, s.Origin, d, b); err != nil {
			return err
		}
		return c.Exec("INSERT INTO projection_gc(edition,kb,data,state,created) VALUES(?,?,?,'present',?) ON CONFLICT(edition) DO UPDATE SET data=excluded.data,state='present'", d.Edition, d.KB, core.JSON(b), time.Now().Unix())
	})
}
func (s *Store) ProjectionBinding(ctx context.Context, id string) (ProjectionBinding, string, string, error) {
	var b ProjectionBinding
	var kb, state string
	e := s.DB.Do(ctx, func(c *db.Conn) error {
		r, e := c.Query("SELECT * FROM projection_gc WHERE edition=?", id)
		if e != nil {
			return e
		}
		if len(r) != 1 {
			return core.Err("NOT_FOUND", "projection binding is not recorded", 404)
		}
		kb = db.String(r[0], "kb")
		state = db.String(r[0], "state")
		return json.Unmarshal([]byte(db.String(r[0], "data")), &b)
	})
	return b, kb, state, e
}
func (s *Store) ProjectionState(ctx context.Context, id, state string) error {
	return s.DB.Do(ctx, func(c *db.Conn) error { return c.Exec("UPDATE projection_gc SET state=? WHERE edition=?", state, id) })
}
