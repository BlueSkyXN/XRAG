package store

import (
	"context"
	"encoding/json"
	"sort"
	"time"
	"github.com/BlueSkyXN/XRAG/core"
	db "github.com/BlueSkyXN/XRAG/internal/sqlite"
)

var iteration7Schema = []string{
	`CREATE TABLE IF NOT EXISTS query_document(id TEXT PRIMARY KEY NOT NULL,kb TEXT NOT NULL,path TEXT NOT NULL,data TEXT NOT NULL)`,
	`CREATE INDEX IF NOT EXISTS query_document_kb ON query_document(kb,path)`,
	`CREATE TRIGGER IF NOT EXISTS query_document_delete AFTER DELETE ON edition BEGIN DELETE FROM query_document WHERE id=old.id; END`,
}

// Materialized at Stage in the same transaction as the edition and chunk index.
// This does not alter the canonical source, chunk keys or immutable references.
func queryDocument(d core.Document) core.Document {
	d.Body = ""
	d.Chunks = append([]core.Chunk(nil), d.Chunks...)
	for i := range d.Chunks {
		c := &d.Chunks[i]
		c.MentionNames = append([]string{}, core.Mentions(*c)...)
		c.Text = ""
		c.Vector = nil
	}
	return d
}
func sortedMapIDs(ids map[string]bool) []string {
	out := make([]string, 0, len(ids))
	for id := range ids {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func (s *Store) QueryView(ctx context.Context, kbs []string, refs []core.Ref, withWiki bool) (core.View, error) {
	v := core.View{Origin: s.Origin, Epoch: 0, Mode: "server", At: time.Now().UTC(), Documents: []core.Document{}, Compact: true}
	err := s.DB.Tx(ctx, false, func(c *db.Conn) error {
		var err error
		v.Epoch, err = epoch(c)
		if err != nil {
			return err
		}
		targets := map[string]core.Ref{}
		query := `SELECT q.data,CASE WHEN ? AND json_extract(q.data,'$.kind')='wiki' THEN json_extract(e.data,'$.body') ELSE '' END wiki_body FROM query_document q JOIN edition e ON e.id=q.id JOIN file f ON f.kb=q.kb AND f.path=q.path WHERE f.withdrawn=0 AND q.kb IN(SELECT value FROM json_each(?)) AND NOT EXISTS(SELECT 1 FROM kb_options o WHERE o.kb=q.kb AND o.deleted=1)`
		args := []any{withWiki, core.JSON(core.Unique(kbs))}
		if refs == nil {
			query += ` AND f.active=q.id`
		} else {
			for _, r := range refs {
				if r.Origin != s.Origin {
					return core.Err("REF_ORIGIN_MISMATCH", "saved scope belongs to another data origin", 409)
				}
				if !core.Subset([]string{r.KB}, kbs) {
					return core.Err("PERMISSION_DENIED", "saved reference outside selected knowledge bases", 403)
				}
				if prev, ok := targets[r.Edition]; ok && (prev.KB != r.KB || prev.Path != r.Path || prev.FileHash != r.FileHash) {
					return core.Err("REF_GONE", "inconsistent edition references", 410)
				}
				targets[r.Edition] = r
			}
			ids := []string{}
			for id := range targets {
				ids = append(ids, id)
			}
			query += ` AND q.id IN(SELECT value FROM json_each(?)) AND e.state IN('active','retired')`
			args = append(args, core.JSON(ids))
		}
		query += ` ORDER BY q.kb,q.path,q.id`
		seen := map[string]bool{}
		chunks := 0
		err = c.Each(query, args, func(row db.Row) error {
			var d core.Document
			if err := json.Unmarshal([]byte(db.String(row, "data")), &d); err != nil {
				return err
			}
			if refs != nil {
				r := targets[d.Edition]
				if r.KB != d.KB || r.Path != d.Path || r.FileHash != d.FileHash {
					return core.Err("REF_GONE", "saved edition identity mismatch", 410)
				}
			}
			d.Body = db.String(row, "wiki_body")
			chunks += len(d.Chunks)
			// Explicit capacity, independent of result-window capacity. No partial
			// metadata View is ever returned as a complete source range.
			if chunks > 200000 || len(v.Documents) >= 50000 {
				return core.Err("VIEW_TOO_LARGE", "query metadata exceeds 200000 chunks or 50000 files; select fewer knowledge bases", 422)
			}
			seen[d.Edition] = true
			v.Documents = append(v.Documents, d)
			return nil
		})
		if err != nil {
			return err
		}
		if refs != nil && len(seen) != len(targets) {
			return core.Err("REF_GONE", "saved scope edition unavailable or withdrawn", 410)
		}
		return nil
	})
	return v, err
}

// VectorTop performs exact cosine on one vector at a time. Model calls happen
// before this function, outside SQLite transactions. A query may be O(N) CPU,
// but no full corpus of text/vector arrays is retained by the Go process.
func (s *Store) VectorTop(ctx context.Context, v core.View, vector []float64, limit int) ([]core.Ranked, error) {
	ids := []string{}
	expected := map[string]string{}
	for _, d := range v.Documents {
		for _, ch := range d.Chunks {
			ids = append(ids, ch.ID)
			expected[ch.ID] = ch.Generation
		}
	}
	top := core.RankWindow{Limit: limit}
	seen := 0
	err := s.DB.Tx(ctx, false, func(c *db.Conn) error {
		ep, err := epoch(c)
		if err != nil {
			return err
		}
		if ep != v.Epoch {
			return core.Err("VIEW_INVALIDATED", "source withdrawn before vector read", 409)
		}
		return c.Each(`SELECT ch.id,json_extract(ch.data,'$.vector') vector,json_extract(ch.data,'$.generation') generation FROM chunk ch JOIN edition e ON e.id=ch.edition JOIN file f ON f.kb=ch.kb AND f.path=ch.path WHERE ch.id IN(SELECT value FROM json_each(?)) AND f.withdrawn=0 AND e.state IN('active','retired') ORDER BY ch.id`, []any{core.JSON(ids)}, func(row db.Row) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			id := db.String(row, "id")
			if db.String(row, "generation") != expected[id] {
				return core.Err("MODEL_MISMATCH", "stored vector generation changed", 409)
			}
			var value []float64
			if err := json.Unmarshal([]byte(db.String(row, "vector")), &value); err != nil {
				return core.Err("MODEL_MISMATCH", "vector projection missing or invalid", 409)
			}
			score, err := core.Cosine(vector, value)
			if err != nil {
				return err
			}
			top.Add(core.Ranked{ID: id, Score: score})
			seen++
			return nil
		})
	})
	if err == nil && seen != len(ids) {
		err = core.Err("REF_GONE", "vector scope lost one or more immutable chunks", 410)
	}
	return top.Sorted(), err
}
