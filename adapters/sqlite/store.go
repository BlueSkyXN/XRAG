package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/BlueSkyXN/XRAG/core/document"
	"github.com/BlueSkyXN/XRAG/core/types"
	"github.com/BlueSkyXN/XRAG/migrations"
)

type Store struct {
	DB *sql.DB
}

type ChunkRow struct {
	ID, Edition, Key, Title, Text, Path, Hash, KB   string
	Ordinal, CharStart, CharEnd, LineStart, LineEnd int
}

type ActiveFile struct {
	KB, Path, Source, Edition, Hash, ContentPath, VectorState string
}

type EditionHead struct {
	ID, Hash, Settings, VectorState string
	LexicalReady                    bool
}

func Open(path string) (*Store, error) {
	dsn := "file:" + path + "?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(8)
	s := &Store{DB: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.DB.Close() }

func (s *Store) migrate() error {
	var existing int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='kb'`).Scan(&existing); err != nil {
		return err
	}
	if existing > 0 {
		return nil
	}
	for _, stmt := range splitSQL(migrations.CoreSQL) {
		if _, err := s.DB.Exec(stmt); err != nil {
			return fmt.Errorf("migrate: %w\n%s", err, stmt)
		}
	}
	return nil
}

func splitSQL(script string) []string {
	var out []string
	var b strings.Builder
	inString := false
	for i := 0; i < len(script); {
		if !inString && strings.HasPrefix(script[i:], "--") {
			next := strings.IndexByte(script[i:], '\n')
			if next < 0 {
				break
			}
			i += next + 1
			continue
		}
		ch := script[i]
		if ch == '\'' {
			inString = !inString
		}
		if ch == ';' && !inString {
			if stmt := strings.TrimSpace(b.String()); stmt != "" {
				out = append(out, stmt)
			}
			b.Reset()
			i++
			continue
		}
		b.WriteByte(ch)
		i++
	}
	if stmt := strings.TrimSpace(b.String()); stmt != "" {
		out = append(out, stmt)
	}
	return out
}

func stripComments(s string) string {
	var lines []string
	for _, line := range strings.Split(s, "\n") {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "--") {
			continue
		}
		lines = append(lines, line)
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func (s *Store) CreateKB(ctx context.Context, id, name, origin string) error {
	cfg, _ := json.Marshal(map[string]string{"origin_id": origin})
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO kb(id,name,config_json) VALUES(?,?,?)`, id, name, string(cfg)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO kb_publication_policy(kb_id,mode,auto_kinds_json,revision) VALUES(?,'manual','[]',1)`, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Origin(ctx context.Context, kb string) (string, error) {
	var raw string
	if err := s.DB.QueryRowContext(ctx, `SELECT config_json FROM kb WHERE id=?`, kb).Scan(&raw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", types.Fail("NOT_FOUND", "knowledge base not found", 404)
		}
		return "", err
	}
	var cfg struct {
		Origin string `json:"origin_id"`
	}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return "", err
	}
	return cfg.Origin, nil
}

func (s *Store) AddPathSource(ctx context.Context, kb, id, mount, root string) error {
	cfg, _ := json.Marshal(map[string]string{"root": root})
	_, err := s.DB.ExecContext(ctx, `INSERT INTO source(id,kb_id,kind,mount,config_json) VALUES(?,?,'path',?,?)`, id, kb, mount, string(cfg))
	return err
}

type Source struct {
	ID, KB, Mount, Root string
}

func (s *Store) Sources(ctx context.Context, kb string) ([]Source, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id,kb_id,mount,config_json FROM source WHERE kb_id=? AND kind='path' ORDER BY id`, kb)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Source
	for rows.Next() {
		var src Source
		var raw string
		if err := rows.Scan(&src.ID, &src.KB, &src.Mount, &raw); err != nil {
			return nil, err
		}
		var cfg struct {
			Root string `json:"root"`
		}
		if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
			return nil, err
		}
		src.Root = cfg.Root
		out = append(out, src)
	}
	return out, rows.Err()
}

func (s *Store) StartBuild(ctx context.Context, id, kb, requestHash string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM job WHERE kb_id=? AND kind='build' AND state='running'`, kb).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return types.Fail("BUILD_IN_PROGRESS", "this knowledge base already has a running build", 409)
	}
	now := time.Now().Unix()
	_, err = tx.ExecContext(ctx, `INSERT INTO job(id,kb_id,kind,dedupe_key,request_hash,state,created_at,queue_expires_at,execution_started_at,execution_deadline_at,payload_json) VALUES(?,?,'build',?,?, 'running', ?, ?, ?, ?, '{}')`,
		id, kb, "build:"+kb, requestHash, now, now+24*3600, now, now+3600)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return types.Fail("BUILD_IN_PROGRESS", "this knowledge base already has a running build", 409)
		}
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO build(id,kb_id,mode,state,started_at) VALUES(?,?,'full','running',?)`, id, kb, now)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) FinishBuild(ctx context.Context, id, state string) error {
	now := time.Now().Unix()
	_, err := s.DB.ExecContext(ctx, `UPDATE job SET state=? WHERE id=?`, state, id)
	if err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx, `UPDATE build SET state=?, ended_at=? WHERE id=?`, state, now, id)
	return err
}

func (s *Store) ActiveHead(ctx context.Context, kb, path string) (EditionHead, bool, error) {
	var h EditionHead
	err := s.DB.QueryRowContext(ctx, `SELECT e.id,e.file_hash,e.settings_hash,e.vector_state FROM file f JOIN file_edition e ON e.id=f.active_edition WHERE f.kb_id=? AND f.path=? AND e.state='active'`, kb, path).Scan(&h.ID, &h.Hash, &h.Settings, &h.VectorState)
	if errors.Is(err, sql.ErrNoRows) {
		return EditionHead{}, false, nil
	}
	if err != nil {
		return EditionHead{}, false, err
	}
	var state string
	var ready, total int
	err = s.DB.QueryRowContext(ctx, `SELECT state,ready,total FROM projection_status WHERE edition_id=? AND projection='lexical'`, h.ID).Scan(&state, &ready, &total)
	if errors.Is(err, sql.ErrNoRows) {
		return h, true, nil
	}
	if err != nil {
		return EditionHead{}, false, err
	}
	h.LexicalReady = state == "complete" && ready == total
	return h, true, nil
}

type NewEdition struct {
	ID, KB, Path, Source, Hash, Settings, ContentPath, VectorState string
	Chunks                                                         []document.Chunk
	ChunkIDs                                                       []string
	Titles                                                         []string
	Fields                                                         []document.Field
}

func (s *Store) StageEdition(ctx context.Context, ed NewEdition) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO file(kb_id,path,source_id) VALUES(?,?,?) ON CONFLICT(kb_id,path) DO NOTHING`, ed.KB, ed.Path, ed.Source); err != nil {
		return err
	}
	now := time.Now().Unix()
	if _, err := tx.ExecContext(ctx, `INSERT INTO file_edition(id,kb_id,path,file_hash,settings_hash,state,content_path,created_at,vector_state,graph_state) VALUES(?,?,?,?,?,'staging',?,?,?,'disabled')`,
		ed.ID, ed.KB, ed.Path, ed.Hash, ed.Settings, ed.ContentPath, now, ed.VectorState); err != nil {
		return err
	}
	for i, ch := range ed.Chunks {
		heading, _ := json.Marshal(ch.Heading)
		title := ed.Titles[i]
		if _, err := tx.ExecContext(ctx, `INSERT INTO chunk(id,edition_id,chunk_key,ordinal,title,heading_json,text,char_start,char_end,line_start,line_end) VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
			ed.ChunkIDs[i], ed.ID, ch.Key, i, title, string(heading), ch.Text, ch.CharStart, ch.CharEnd, ch.LineStart, ch.LineEnd); err != nil {
			return err
		}
	}
	for _, f := range ed.Fields {
		if _, err := tx.ExecContext(ctx, `INSERT INTO file_field(edition_id,name,value_type,value_key,value_text,value_num) VALUES(?,?,?,?,?,?)`,
			ed.ID, f.Name, f.Type, f.Key, nullString(f.Text, f.Type == "text"), f.Number); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func nullString(v string, ok bool) any {
	if !ok {
		return nil
	}
	return v
}

func (s *Store) AbortEdition(ctx context.Context, id string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE file_edition SET state='aborted', vector_state='failed' WHERE id=? AND state='staging'`, id)
	return err
}

func (s *Store) Activate(ctx context.Context, ed NewEdition, tokens [][2]string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT id FROM file_edition WHERE kb_id=? AND path=? AND state='active'`, ed.KB, ed.Path)
	if err != nil {
		return err
	}
	var old []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		old = append(old, id)
	}
	rows.Close()
	for _, id := range old {
		if _, err := tx.ExecContext(ctx, `DELETE FROM chunk_fts WHERE chunk_id IN (SELECT id FROM chunk WHERE edition_id=?)`, id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE file_edition SET state='retired' WHERE id=?`, id); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE file_edition SET state='active', vector_state=? WHERE id=?`, ed.VectorState, ed.ID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE file SET active_edition=? WHERE kb_id=? AND path=?`, ed.ID, ed.KB, ed.Path); err != nil {
		return err
	}
	for i, pair := range tokens {
		if _, err := tx.ExecContext(ctx, `INSERT INTO chunk_fts(chunk_id,title_tokens,body_tokens) VALUES(?,?,?)`, ed.ChunkIDs[i], pair[0], pair[1]); err != nil {
			return err
		}
	}
	now := time.Now().Unix()
	total := len(ed.Chunks)
	if _, err := tx.ExecContext(ctx, `INSERT INTO projection_status(edition_id,projection,state,total,ready,failed,pending,excluded,observed_at) VALUES(?,'lexical','complete',?,?,0,0,0,?)`,
		ed.ID, total, total, now); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO materialization_journal(edition_id,projection,phase,plan_json,updated_at) VALUES(?,'lexical','verified','{}',?)`, ed.ID, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) RetirePath(ctx context.Context, kb, path string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var id sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT active_edition FROM file WHERE kb_id=? AND path=?`, kb, path).Scan(&id); err != nil {
		return err
	}
	if id.Valid {
		if _, err := tx.ExecContext(ctx, `DELETE FROM chunk_fts WHERE chunk_id IN (SELECT id FROM chunk WHERE edition_id=?)`, id.String); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE file_edition SET state='retired' WHERE id=?`, id.String); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE file SET active_edition=NULL WHERE kb_id=? AND path=?`, kb, path); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Paths(ctx context.Context, kb, source string) ([]string, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT path FROM file WHERE kb_id=? AND source_id=? ORDER BY path`, kb, source)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) RecordFile(ctx context.Context, buildID, path, action, state, before, after, beforeHash, afterHash string) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO build_file(build_id,path,action,state,before_edition,after_edition,before_hash,after_hash) VALUES(?,?,?,?,?,?,?,?)`,
		buildID, path, action, state, nullIfEmpty(before), nullIfEmpty(after), nullIfEmpty(beforeHash), nullIfEmpty(afterHash))
	return err
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (s *Store) RecordScan(ctx context.Context, scanID, source, path, hash string) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO scan_entry(scan_id,source_id,path,fetched_hash) VALUES(?,?,?,?)`, scanID, source, path, hash)
	return err
}

func (s *Store) CacheGet(ctx context.Context, key string) (string, bool, error) {
	var raw string
	err := s.DB.QueryRowContext(ctx, `SELECT result_json FROM processing_cache WHERE kind='embedding' AND cache_key=?`, key).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return raw, err == nil, err
}

func (s *Store) CachePut(ctx context.Context, key, inputHash, profileHash, result string) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO processing_cache(kind,cache_key,input_hash,profile_hash,result_json) VALUES('embedding',?,?,?,?)`, key, inputHash, profileHash, result)
	return err
}

func (s *Store) SaveDescriptor(ctx context.Context, kb, generation, collection, hash, raw string) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO vector_descriptor(kb_id,generation,physical_collection,descriptor_hash,descriptor_json,ready) VALUES(?,?,?,?,?,0) ON CONFLICT(kb_id,generation) DO NOTHING`,
		kb, generation, collection, hash, raw)
	return err
}

type SavedView struct {
	Editions []ActiveFile
}

func (s *Store) Published(ctx context.Context, kb string) ([]ActiveFile, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT f.kb_id,f.path,f.source_id,e.id,e.file_hash,e.content_path,e.vector_state FROM file f JOIN file_edition e ON e.id=f.active_edition WHERE f.kb_id=? AND f.withdrawn=0 AND e.state='active' ORDER BY f.path`, kb)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ActiveFile
	for rows.Next() {
		var f ActiveFile
		if err := rows.Scan(&f.KB, &f.Path, &f.Source, &f.Edition, &f.Hash, &f.ContentPath, &f.VectorState); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (s *Store) Search(ctx context.Context, kb, match string) ([]ChunkRow, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT v.id,v.edition_id,v.chunk_key,v.ordinal,v.title,v.text,v.char_start,v.char_end,v.line_start,v.line_end,v.path,v.file_hash,bm25(chunk_fts) FROM chunk_fts JOIN visible_chunk v ON v.id=chunk_fts.chunk_id WHERE v.kb_id=? AND chunk_fts MATCH ? ORDER BY bm25(chunk_fts), v.path, v.ordinal`, kb, match)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ChunkRow
	for rows.Next() {
		var c ChunkRow
		var rank float64
		if err := rows.Scan(&c.ID, &c.Edition, &c.Key, &c.Ordinal, &c.Title, &c.Text, &c.CharStart, &c.CharEnd, &c.LineStart, &c.LineEnd, &c.Path, &c.Hash, &rank); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) Chunk(ctx context.Context, id string) (ChunkRow, string, int, error) {
	var c ChunkRow
	var withdrawn int
	err := s.DB.QueryRowContext(ctx, `SELECT c.id,c.edition_id,c.chunk_key,c.ordinal,c.title,c.text,c.char_start,c.char_end,c.line_start,c.line_end,e.path,e.file_hash,e.kb_id,f.withdrawn FROM chunk c JOIN file_edition e ON e.id=c.edition_id JOIN file f ON f.kb_id=e.kb_id AND f.path=e.path WHERE c.id=?`, id).Scan(
		&c.ID, &c.Edition, &c.Key, &c.Ordinal, &c.Title, &c.Text, &c.CharStart, &c.CharEnd, &c.LineStart, &c.LineEnd, &c.Path, &c.Hash, &c.KB, &withdrawn)
	return c, c.KB, withdrawn, err
}

func (s *Store) ContentPath(ctx context.Context, edition string) (string, error) {
	var p string
	err := s.DB.QueryRowContext(ctx, `SELECT content_path FROM file_edition WHERE id=?`, edition).Scan(&p)
	return p, err
}

type Frozen struct {
	Items    []types.Item
	Total    types.Metric
	Coverage types.Coverage
	Handle   *types.Handle
}

func (s *Store) Freeze(ctx context.Context, viewID, resultID, handleID, origin, action string, files []ActiveFile, frozen Frozen, handleExpiry, viewExpiry int64) error {
	sel := make([]map[string]string, 0, len(files))
	for _, f := range files {
		sel = append(sel, map[string]string{"path": f.Path, "edition_id": f.Edition})
	}
	selection, _ := json.Marshal(sel)
	items, _ := json.Marshal(frozen.Items)
	total, _ := json.Marshal(frozen.Total)
	members, _ := json.Marshal(sel)
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	epochs := map[string]int{}
	if len(files) > 0 {
		epochs[files[0].KB] = 0
	}
	ep, _ := json.Marshal(epochs)
	if _, err := tx.ExecContext(ctx, `INSERT INTO query_view(id,origin_id,selection_json,descriptors_json,revoke_epochs_json,created_at,expires_at) VALUES(?,?,?,'[]',?,?,?)`,
		viewID, origin, string(selection), string(ep), time.Now().Unix(), viewExpiry); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO result_set(id,view_id,action,items_json,total_json,cache_complete,expires_at) VALUES(?,?,?,?,?,1,?)`,
		resultID, viewID, action, string(items), string(total), viewExpiry); err != nil {
		return err
	}
	complete := 0
	if frozen.Handle != nil && frozen.Handle.Complete {
		complete = 1
	}
	basis := "enumerated"
	if frozen.Handle != nil && frozen.Handle.Basis != "" {
		basis = frozen.Handle.Basis
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO handle(id,view_id,kind,basis,members_json,origin_scope_json,complete,created_at,expires_at) VALUES(?,?,?,?,?,'{}',?,?,?)`,
		handleID, viewID, "files", basis, string(members), complete, time.Now().Unix(), handleExpiry); err != nil {
		return err
	}
	for _, f := range files {
		if _, err := tx.ExecContext(ctx, `INSERT INTO edition_pin(owner_kind,owner_id,edition_id,expires_at) VALUES('view',?,?,?)`, viewID, f.Edition, handleExpiry); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) LoadResult(ctx context.Context, id string) (Frozen, error) {
	var itemsRaw, totalRaw string
	var viewID string
	err := s.DB.QueryRowContext(ctx, `SELECT view_id,items_json,total_json FROM result_set WHERE id=?`, id).Scan(&viewID, &itemsRaw, &totalRaw)
	if errors.Is(err, sql.ErrNoRows) {
		return Frozen{}, types.Fail("NOT_FOUND", "result set not found", 404)
	}
	if err != nil {
		return Frozen{}, err
	}
	var frozen Frozen
	if err := json.Unmarshal([]byte(itemsRaw), &frozen.Items); err != nil {
		return Frozen{}, err
	}
	if err := json.Unmarshal([]byte(totalRaw), &frozen.Total); err != nil {
		return Frozen{}, err
	}
	var handleID, basis string
	var complete int
	var exp int64
	err = s.DB.QueryRowContext(ctx, `SELECT id,basis,complete,expires_at FROM handle WHERE view_id=?`, viewID).Scan(&handleID, &basis, &complete, &exp)
	if err == nil {
		frozen.Handle = &types.Handle{ID: handleID, Kind: "files", Basis: basis, Complete: complete == 1, Count: frozen.Total.Value, SourceRestricted: true, ExpiresAt: time.Unix(exp, 0).UTC().Format(time.RFC3339)}
	}
	return frozen, nil
}

func (s *Store) Coverage(ctx context.Context, editionIDs []string, scope string) (types.ProjectionCoverage, error) {
	cov := types.ProjectionCoverage{Projection: "lexical", State: "complete", Unit: "chunks", Scope: scope, ReasonCodes: []string{}, ObservedAt: time.Now().UTC().Format(time.RFC3339)}
	total, ready, failed, pending, excluded := 0, 0, 0, 0, 0
	if len(editionIDs) == 0 {
		cov.Total, cov.Ready, cov.Failed, cov.Pending, cov.Excluded = &total, &ready, &failed, &pending, &excluded
		return cov, nil
	}
	q := `SELECT state,total,ready,failed,pending,excluded FROM projection_status WHERE projection='lexical' AND edition_id IN (` + placeholders(len(editionIDs)) + `)`
	args := make([]any, len(editionIDs))
	for i, id := range editionIDs {
		args[i] = id
	}
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return cov, err
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var state string
		var t, r, f, p, e int
		if err := rows.Scan(&state, &t, &r, &f, &p, &e); err != nil {
			return cov, err
		}
		n++
		total += t
		ready += r
		failed += f
		pending += p
		excluded += e
		if state != "complete" {
			cov.State = "partial"
		}
	}
	if n != len(editionIDs) {
		cov.State = "partial"
	}
	cov.Total, cov.Ready, cov.Failed, cov.Pending, cov.Excluded = &total, &ready, &failed, &pending, &excluded
	return cov, rows.Err()
}

func placeholders(n int) string {
	parts := make([]string, n)
	for i := range parts {
		parts[i] = "?"
	}
	return strings.Join(parts, ",")
}
