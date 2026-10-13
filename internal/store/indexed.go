package store

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"time"
	"github.com/BlueSkyXN/XRAG/core"
	db "github.com/BlueSkyXN/XRAG/internal/sqlite"
)

// Catalogs keep large bodies/vectors out of the metadata scan. They are updated
// in the same SQLite transaction as their immutable edition/chunk records.
var iteration6Schema = []string{
	`CREATE TABLE IF NOT EXISTS edition_catalog(id TEXT PRIMARY KEY,kb TEXT NOT NULL,path TEXT NOT NULL,hash TEXT NOT NULL,title TEXT NOT NULL,fields TEXT NOT NULL,dictionary TEXT NOT NULL,kind TEXT NOT NULL)`,
	`CREATE INDEX IF NOT EXISTS edition_catalog_kb ON edition_catalog(kb,path)`,
	`CREATE TABLE IF NOT EXISTS chunk_catalog(id TEXT PRIMARY KEY,edition TEXT NOT NULL,kb TEXT NOT NULL,path TEXT NOT NULL,start INTEGER NOT NULL,end INTEGER NOT NULL,line_start INTEGER NOT NULL,line_end INTEGER NOT NULL)`,
	`CREATE INDEX IF NOT EXISTS chunk_catalog_edition ON chunk_catalog(edition,id)`,
	`CREATE INDEX IF NOT EXISTS chunk_edition_lookup ON chunk(edition,id)`,
	`CREATE INDEX IF NOT EXISTS edition_kb_lookup ON edition(kb,path,state)`,
	`CREATE TRIGGER IF NOT EXISTS edition_catalog_insert AFTER INSERT ON edition BEGIN INSERT OR REPLACE INTO edition_catalog VALUES(new.id,new.kb,new.path,new.hash,COALESCE(json_extract(new.data,'$.title'),''),COALESCE(json_extract(new.data,'$.fields'),'{}'),COALESCE(json_extract(new.data,'$.dictionary'),'[]'),COALESCE(json_extract(new.data,'$.kind'),'')); END`,
	`CREATE TRIGGER IF NOT EXISTS edition_catalog_update AFTER UPDATE OF data ON edition BEGIN INSERT OR REPLACE INTO edition_catalog VALUES(new.id,new.kb,new.path,new.hash,COALESCE(json_extract(new.data,'$.title'),''),COALESCE(json_extract(new.data,'$.fields'),'{}'),COALESCE(json_extract(new.data,'$.dictionary'),'[]'),COALESCE(json_extract(new.data,'$.kind'),'')); END`,
	`CREATE TRIGGER IF NOT EXISTS edition_catalog_delete AFTER DELETE ON edition BEGIN DELETE FROM edition_catalog WHERE id=old.id; END`,
	`CREATE TRIGGER IF NOT EXISTS chunk_catalog_insert AFTER INSERT ON chunk BEGIN INSERT OR REPLACE INTO chunk_catalog VALUES(new.id,new.edition,new.kb,new.path,COALESCE(json_extract(new.data,'$.char_start'),0),COALESCE(json_extract(new.data,'$.char_end'),0),COALESCE(json_extract(new.data,'$.line_start'),0),COALESCE(json_extract(new.data,'$.line_end'),0)); END`,
	`CREATE TRIGGER IF NOT EXISTS chunk_catalog_delete AFTER DELETE ON chunk BEGIN DELETE FROM chunk_catalog WHERE id=old.id; END`,
}

func (s *Store) OriginID() string { return s.Origin }
func (s *Store) CurrentReference(ctx context.Context, kb, path string) (core.Ref, error) {
	r := core.Ref{Origin: s.Origin, KB: kb, Path: path}
	err := s.DB.Do(ctx, func(c *db.Conn) error {
		rs, e := c.Query(`SELECT e.id,e.hash FROM file f JOIN edition_catalog e ON e.id=f.active WHERE f.kb=? AND f.path=? AND f.withdrawn=0`, kb, path)
		if e != nil {
			return e
		}
		if len(rs) != 1 {
			return core.Err("REF_GONE", "file is not published", 410)
		}
		r.Edition = db.String(rs[0], "id")
		r.FileHash = db.String(rs[0], "hash")
		return nil
	})
	return r, err
}
func (s *Store) SelectIndex(ctx context.Context, action string, q core.Request, limit int) (core.IndexSelection, error) {
	out := core.IndexSelection{Origin: s.Origin, Items: []core.Item{}, Dependencies: []core.Ref{}, Unit: "chunks"}
	if action == "files.tree" {
		out.Unit = "files"
	}
	if action == "search.keyword" {
		if _, e := core.FTSQuery(q.Q); e != nil {
			return out, e
		}
	}
	if q.PerFile < 0 {
		return out, core.Err("INVALID_ARGUMENT", "per_file cannot be negative", 400)
	}
	err := s.DB.Tx(ctx, false, func(c *db.Conn) error {
		var e error
		out.Epoch, e = epoch(c)
		if e != nil {
			return e
		}
		groups := map[string][]string{}
		words := map[string][]string{}
		docs := map[string]core.Document{}
		knownFields := map[string]bool{}
		// Only catalog records are scanned here, not edition.data.
		e = c.Each(`SELECT m.* FROM file f JOIN edition_catalog m ON m.id=f.active WHERE f.withdrawn=0 AND f.kb IN(SELECT value FROM json_each(?)) AND NOT EXISTS(SELECT 1 FROM kb_options o WHERE o.kb=f.kb AND o.deleted=1) ORDER BY f.kb,f.path`, []any{core.JSON(core.Unique(q.Kbs))}, func(row db.Row) error {
			d := core.Document{KB: db.String(row, "kb"), Path: db.String(row, "path"), Edition: db.String(row, "id"), FileHash: db.String(row, "hash"), Title: db.String(row, "title"), Kind: db.String(row, "kind")}
			if er := json.Unmarshal([]byte(db.String(row, "fields")), &d.Fields); er != nil {
				return er
			}
			if er := json.Unmarshal([]byte(db.String(row, "dictionary")), &d.Dictionary); er != nil {
				return er
			}
			for name := range d.Fields {
				knownFields[name] = true
			}
			ok, er := core.MatchesFilters(core.Chunk{KB: d.KB, Path: d.Path, Title: d.Title, Fields: d.Fields}, q.Filters)
			if er != nil {
				return er
			}
			if !ok {
				return nil
			}
			docs[d.Edition] = d
			key := core.Hash(d.Dictionary)
			groups[key] = append(groups[key], d.Edition)
			words[key] = d.Dictionary
			out.Dependencies = append(out.Dependencies, core.Ref{Origin: s.Origin, KB: d.KB, Path: d.Path, Edition: d.Edition, FileHash: d.FileHash})
			return nil
		})
		if e != nil {
			return e
		}
		for _, f := range q.Filters {
			if len(f.Field) > 7 && f.Field[:7] == "fields." && !knownFields[f.Field[7:]] {
				return core.Err("FILTER_UNSUPPORTED", "unknown field: "+f.Field, 422)
			}
		}
		if action == "files.tree" {
			out.Total = len(out.Dependencies)
			out.SelectedTotal = out.Total
			for _, r := range out.Dependencies {
				if len(out.Items) >= limit {
					break
				}
				d := docs[r.Edition]
				out.Items = append(out.Items, core.Item{Kind: "file", KB: d.KB, Path: d.Path, Title: d.Title, Edition: d.Edition, FileHash: d.FileHash, Ref: core.EncodeRef(r), Fields: d.Fields})
			}
			return nil
		}
		for k, editions := range groups {
			expression, er := core.DictionaryQuery(q.Q, words[k])
			if er != nil {
				return er
			}
			join := ` FROM fts JOIN chunk_catalog ck ON ck.id=fts.chunk_id WHERE fts MATCH ? AND ck.edition IN(SELECT value FROM json_each(?))`
			args := []any{expression, core.JSON(editions)}
			rs, er := c.Query(`SELECT COUNT(*) n`+join, args...)
			if er != nil {
				return er
			}
			out.Total += int(db.Int(rs[0], "n"))
			if q.PerFile > 0 {
				rs, er = c.Query(`SELECT COALESCE(SUM(MIN(n,?)),0) n FROM(SELECT COUNT(*) n`+join+` GROUP BY ck.edition)`, append([]any{q.PerFile}, args...)...)
			}
			if er != nil {
				return er
			}
			out.SelectedTotal += int(db.Int(rs[0], "n"))
			counts := map[string]int{}
			groupItems := []core.Item{}
			stop := errors.New("selected enough")
			er = c.Each(`SELECT ck.*,bm25(fts) score`+join+` ORDER BY score,ck.id`, args, func(row db.Row) error {
				ed := db.String(row, "edition")
				if q.PerFile > 0 && counts[ed] >= q.PerFile {
					return nil
				}
				counts[ed]++
				d := docs[ed]
				r := core.Ref{Origin: s.Origin, KB: d.KB, Path: d.Path, Edition: ed, FileHash: d.FileHash, ChunkID: db.String(row, "id")}
				it := core.Item{Kind: "chunk", ID: r.ChunkID, ChunkID: r.ChunkID, KB: d.KB, Path: d.Path, Edition: ed, FileHash: d.FileHash, Title: d.Title, Fields: d.Fields, Ref: core.EncodeRef(r), Score: -db.Float(row, "score"), Ranking: "bm25", LineStart: int(db.Int(row, "line_start")), LineEnd: int(db.Int(row, "line_end"))}
				groupItems = append(groupItems, it)
				if len(groupItems) >= limit {
					return stop
				}
				return nil
			})
			if er != nil && !errors.Is(er, stop) {
				return er
			}
			out.Items = append(out.Items, groupItems...)
		}
		sort.Slice(out.Items, func(i, j int) bool {
			if out.Items[i].Score == out.Items[j].Score {
				return out.Items[i].ChunkID < out.Items[j].ChunkID
			}
			return out.Items[i].Score > out.Items[j].Score
		})
		if len(out.Items) > limit {
			out.Items = out.Items[:limit]
		}
		return nil
	})
	return out, err
}
func (s *Store) HydrateItems(ctx context.Context, items []core.Item) ([]core.Item, error) {
	out := append([]core.Item(nil), items...)
	err := s.DB.Tx(ctx, false, func(c *db.Conn) error {
		for i, it := range out {
			if it.External != nil || (it.Kind != "chunk" && it.Kind != "file" && it.Kind != "wiki") {
				continue
			}
			r, e := core.DecodeRef(it.Ref)
			if e != nil {
				return e
			}
			if r.Origin != s.Origin {
				return core.Err("REF_ORIGIN_MISMATCH", "different origin", 409)
			}
			rows, e := c.Query("SELECT withdrawn FROM file WHERE kb=? AND path=?", r.KB, r.Path)
			if e != nil {
				return e
			}
			if len(rows) != 1 || db.Int(rows[0], "withdrawn") != 0 {
				return core.Err("REF_WITHDRAWN", "source no longer available", 410)
			}
			if it.Kind == "file" {
				continue
			}
			rows, e = c.Query(`SELECT json_remove(c.data,'$.vector') data FROM chunk c JOIN edition ed ON ed.id=c.edition WHERE c.id=? AND c.edition=? AND c.kb=? AND c.path=? AND ed.hash=? AND ed.state NOT IN('staging','aborted')`, r.ChunkID, r.Edition, r.KB, r.Path, r.FileHash)
			if e != nil {
				return e
			}
			if len(rows) != 1 {
				return core.Err("REF_GONE", "saved chunk version unavailable", 410)
			}
			var ch core.Chunk
			if e = json.Unmarshal([]byte(db.String(rows[0], "data")), &ch); e != nil {
				return e
			}
			got := core.ChunkItem(ch, s.Origin)
			got.Kind = it.Kind
			got.Score = it.Score
			got.Ranking = it.Ranking
			out[i] = got
		}
		return nil
	})
	return out, err
}
func (s *Store) ViewFromReferences(ctx context.Context, refs []core.Ref) (core.View, error) {
	v := core.View{Origin: s.Origin, Mode: "server", At: time.Now().UTC(), Documents: []core.Document{}}
	var e error
	v.Epoch, e = s.Epoch(ctx)
	if e != nil {
		return v, e
	}
	seen := map[string]bool{}
	for _, r := range refs {
		if seen[r.Edition] {
			continue
		}
		seen[r.Edition] = true
		r.ChunkID = ""
		d, _, _, er := s.Read(ctx, r)
		if er != nil {
			return v, er
		}
		v.Documents = append(v.Documents, d)
	}
	after, e := s.Epoch(ctx)
	if e != nil {
		return v, e
	}
	if after != v.Epoch {
		return v, core.Err("VIEW_INVALIDATED", "source withdrawn while loading saved scope", 409)
	}
	return v, nil
}
