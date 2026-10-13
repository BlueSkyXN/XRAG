package store

import (
	"context"
	"github.com/BlueSkyXN/XRAG/internal/oplock"
	db "github.com/BlueSkyXN/XRAG/internal/sqlite"
)

func (s *Store) BeginOperation(ctx context.Context) (context.Context, func(), error) {
	return oplock.Acquire(ctx, s.Dir, false)
}

func (s *Store) ContentCursor(ctx context.Context) (string, error) {
	var out string
	e := s.DB.Do(ctx, func(c *db.Conn) error {
		rs, e := c.Query("SELECT v FROM meta WHERE k='content_gc_cursor'")
		if e == nil && len(rs) > 0 {
			out = db.String(rs[0], "v")
		}
		return e
	})
	return out, e
}
func (s *Store) SetContentCursor(ctx context.Context, after string) error {
	return s.DB.Do(ctx, func(c *db.Conn) error {
		return c.Exec("INSERT INTO meta(k,v) VALUES('content_gc_cursor',?) ON CONFLICT(k) DO UPDATE SET v=excluded.v", after)
	})
}

func (s *Store) PreviewContent(ctx context.Context, days, batch int) (PruneReport, error) {
	after, e := s.ContentCursor(ctx)
	if e != nil {
		return PruneReport{}, e
	}
	return s.pruneHistory(ctx, days, false, false, batch, after)
}
