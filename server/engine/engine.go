package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/BlueSkyXN/XRAG/adapters/sources/path"
	"github.com/BlueSkyXN/XRAG/adapters/sqlite"
	"github.com/BlueSkyXN/XRAG/core/document"
	"github.com/BlueSkyXN/XRAG/core/ids"
	"github.com/BlueSkyXN/XRAG/core/types"
)

type VectorMode int

const (
	VectorOff VectorMode = iota
	VectorOptional
	VectorRequired
)

type Embedder interface {
	Profile() types.VectorProfile
	Embed(ctx context.Context, text string) ([]float32, error)
}

type Options struct {
	Vector          VectorMode
	Embedder        Embedder
	HoldPublication bool
}

type FileResult struct {
	Path   string `json:"path"`
	Action string `json:"action"`
	State  string `json:"state"`
	Before string `json:"before_edition,omitempty"`
	After  string `json:"after_edition,omitempty"`
}

type Summary struct {
	BuildID string       `json:"build_id"`
	Files   []FileResult `json:"files"`
}

type Engine struct {
	store  *sqlite.Store
	root   string
	origin string
}

func Open(dir string) (*Engine, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	origin, err := loadOrigin(dir)
	if err != nil {
		return nil, err
	}
	store, err := sqlite.Open(filepath.Join(dir, "xrag.db"))
	if err != nil {
		return nil, err
	}
	return &Engine{store: store, root: dir, origin: origin}, nil
}

func (e *Engine) Close() error { return e.store.Close() }

func (e *Engine) Root() string { return e.root }

func loadOrigin(dir string) (string, error) {
	p := filepath.Join(dir, "origin.json")
	if raw, err := os.ReadFile(p); err == nil {
		var doc struct {
			ID string `json:"origin_id"`
		}
		if json.Unmarshal(raw, &doc) == nil && doc.ID != "" {
			return doc.ID, nil
		}
	}
	id, err := ids.Prefixed("ori_")
	if err != nil {
		return "", err
	}
	raw, _ := json.Marshal(map[string]string{"origin_id": id})
	return id, os.WriteFile(p, append(raw, '\n'), 0o644)
}

func (e *Engine) CreateKB(ctx context.Context, id, name string) error {
	if !slug(id) {
		return types.Fail("INVALID_ARGUMENT", "kb id must be a lowercase slug", 400)
	}
	return e.store.CreateKB(ctx, id, name, e.origin)
}

func (e *Engine) AddPathSource(ctx context.Context, kb, id, mount, root string) error {
	if !slug(id) {
		return types.Fail("INVALID_ARGUMENT", "source id must be a lowercase slug", 400)
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	if _, err := os.Stat(abs); err != nil {
		return err
	}
	if mount == "" {
		mount = id
	}
	return e.store.AddPathSource(ctx, kb, id, mount, abs)
}

func slug(s string) bool {
	if s == "" || len(s) > 63 {
		return false
	}
	for i, r := range s {
		ok := r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-'
		if !ok || (i == 0 && r == '-') {
			return false
		}
	}
	return true
}

func (e *Engine) Build(ctx context.Context, kb string, opt Options) (Summary, error) {
	if opt.Vector != VectorOff && opt.Embedder == nil {
		return Summary{}, types.Fail("INVALID_ARGUMENT", "vector mode requires an embedder", 400)
	}
	buildID, err := ids.Prefixed("bld_")
	if err != nil {
		return Summary{}, err
	}
	req, _ := ids.Hash("full", opt.Vector, profileID(opt))
	if err := e.store.StartBuild(ctx, buildID, kb, req); err != nil {
		return Summary{}, err
	}
	state := "succeeded"
	defer func() { _ = e.store.FinishBuild(context.Background(), buildID, state) }()
	sources, err := e.store.Sources(ctx, kb)
	if err != nil {
		state = "failed"
		return Summary{}, err
	}
	if len(sources) == 0 {
		state = "failed"
		return Summary{}, types.Fail("NOT_FOUND", "no path source on this knowledge base", 404)
	}
	var summary Summary
	summary.BuildID = buildID
	failed := 0
	for _, src := range sources {
		files, err := path.Inventory(src.Root)
		if err != nil {
			state = "failed"
			return summary, err
		}
		seen := map[string]bool{}
		for _, file := range files {
			full, err := path.Mount(src.Mount, file.Rel)
			if err != nil {
				state = "failed"
				return summary, err
			}
			seen[full] = true
			result, err := e.prepareFile(ctx, kb, src.ID, full, file.Abs, buildID, opt)
			if err != nil {
				state = "failed"
				return summary, err
			}
			if result.State == "failed" {
				failed++
			}
			summary.Files = append(summary.Files, result)
		}
		existing, err := e.store.Paths(ctx, kb, src.ID)
		if err != nil {
			state = "failed"
			return summary, err
		}
		for _, old := range existing {
			if seen[old] {
				continue
			}
			if err := e.store.RetirePath(ctx, kb, old); err != nil {
				state = "failed"
				return summary, err
			}
			if err := e.store.RecordFile(ctx, buildID, old, "removed", "retired", "", "", "", ""); err != nil {
				state = "failed"
				return summary, err
			}
			summary.Files = append(summary.Files, FileResult{Path: old, Action: "removed", State: "retired"})
		}
	}
	if failed > 0 {
		state = "partial"
	}
	return summary, nil
}

func profileID(opt Options) string {
	if opt.Embedder == nil {
		return ""
	}
	return opt.Embedder.Profile().ID
}

func (e *Engine) prepareFile(ctx context.Context, kb, source, full, abs, buildID string, opt Options) (FileResult, error) {
	raw, err := path.Read(abs)
	if err != nil {
		_ = e.store.RecordFile(ctx, buildID, full, "fetch", "failed", "", "", "", "")
		return FileResult{Path: full, Action: "fetch", State: "failed"}, nil
	}
	parsed, err := document.Parse(raw)
	if err != nil {
		_ = e.store.RecordFile(ctx, buildID, full, "parse", "failed", "", "", "", "")
		return FileResult{Path: full, Action: "parse", State: "failed"}, nil
	}
	hash := sha256.Sum256([]byte(parsed.Content))
	fileHash := hex.EncodeToString(hash[:])
	settings, err := ids.Hash("chunk-v1", profileID(opt), opt.Vector)
	if err != nil {
		return FileResult{}, err
	}
	if err := e.store.RecordScan(ctx, buildID, source, full, fileHash); err != nil {
		return FileResult{}, err
	}
	head, ok, err := e.store.ActiveHead(ctx, kb, full)
	if err != nil {
		return FileResult{}, err
	}
	if ok && head.Hash == fileHash && head.Settings == settings && head.LexicalReady && vectorHealthy(opt.Vector, head.VectorState) {
		if err := e.store.RecordFile(ctx, buildID, full, "unchanged", "kept", head.ID, head.ID, fileHash, fileHash); err != nil {
			return FileResult{}, err
		}
		return FileResult{Path: full, Action: "unchanged", State: "kept", Before: head.ID, After: head.ID}, nil
	}
	editionID, err := ids.EditionID()
	if err != nil {
		return FileResult{}, err
	}
	rel := filepath.ToSlash(filepath.Join("content", kb, fileHash+".md"))
	if err := os.MkdirAll(filepath.Join(e.root, "content", kb), 0o755); err != nil {
		return FileResult{}, err
	}
	if err := os.WriteFile(filepath.Join(e.root, filepath.FromSlash(rel)), []byte(parsed.Content), 0o644); err != nil {
		return FileResult{}, err
	}
	var idsChunk []string
	var titles []string
	for i := range parsed.Chunks {
		ch := &parsed.Chunks[i]
		key, err := ids.ChunkKey(kb, full, ch.Heading, ch.Text, ch.Dup)
		if err != nil {
			return FileResult{}, err
		}
		ch.Key = key
		cid, err := ids.ChunkID(editionID, key)
		if err != nil {
			return FileResult{}, err
		}
		idsChunk = append(idsChunk, cid)
		title := full
		if len(ch.Heading) > 0 {
			title = ch.Heading[len(ch.Heading)-1]
		}
		titles = append(titles, title)
	}
	vectorState := "disabled"
	if opt.Embedder != nil {
		var embedErr error
		vectorState, embedErr = e.embed(ctx, kb, opt, parsed.Chunks)
		if embedErr != nil && opt.Vector == VectorRequired {
			ed := sqlite.NewEdition{ID: editionID, KB: kb, Path: full, Source: source, Hash: fileHash, Settings: settings, ContentPath: rel, VectorState: "failed", Chunks: parsed.Chunks, ChunkIDs: idsChunk, Titles: titles, Fields: parsed.Fields}
			if err := e.store.StageEdition(ctx, ed); err != nil {
				return FileResult{}, err
			}
			if err := e.store.AbortEdition(ctx, editionID); err != nil {
				return FileResult{}, err
			}
			before := ""
			if ok {
				before = head.ID
			}
			if err := e.store.RecordFile(ctx, buildID, full, "vector", "failed", before, editionID, head.Hash, fileHash); err != nil {
				return FileResult{}, err
			}
			return FileResult{Path: full, Action: "vector", State: "failed", Before: before, After: editionID}, nil
		}
	}
	ed := sqlite.NewEdition{ID: editionID, KB: kb, Path: full, Source: source, Hash: fileHash, Settings: settings, ContentPath: rel, VectorState: vectorState, Chunks: parsed.Chunks, ChunkIDs: idsChunk, Titles: titles, Fields: parsed.Fields}
	if err := e.store.StageEdition(ctx, ed); err != nil {
		return FileResult{}, err
	}
	before := ""
	beforeHash := ""
	if ok {
		before = head.ID
		beforeHash = head.Hash
	}
	if opt.HoldPublication {
		if err := e.store.RecordFile(ctx, buildID, full, "staged", "staging", before, editionID, beforeHash, fileHash); err != nil {
			return FileResult{}, err
		}
		return FileResult{Path: full, Action: "staged", State: "staging", Before: before, After: editionID}, nil
	}
	tokens := make([][2]string, len(parsed.Chunks))
	for i, ch := range parsed.Chunks {
		tokens[i] = [2]string{document.IndexText(titles[i]), document.IndexText(ch.Text)}
	}
	if err := e.store.Activate(ctx, ed, tokens); err != nil {
		return FileResult{}, err
	}
	if err := e.store.RecordFile(ctx, buildID, full, "published", "active", before, editionID, beforeHash, fileHash); err != nil {
		return FileResult{}, err
	}
	return FileResult{Path: full, Action: "published", State: "active", Before: before, After: editionID}, nil
}

func vectorHealthy(mode VectorMode, state string) bool {
	switch mode {
	case VectorOff:
		return state == "disabled" || state == "ready"
	case VectorRequired:
		return state == "ready"
	default:
		return state == "ready" || state == "partial"
	}
}

func (e *Engine) embed(ctx context.Context, kb string, opt Options, chunks []document.Chunk) (string, error) {
	profile := opt.Embedder.Profile()
	desc, _ := json.Marshal(profile)
	hash, _ := ids.Hash(profile)
	_ = e.store.SaveDescriptor(ctx, kb, "g1", "xrag_"+kb+"_g1", hash, string(desc))
	failed := 0
	for _, ch := range chunks {
		key, err := ids.Hash("embedding", profile.ID, profile.Revision, ch.Text)
		if err != nil {
			return "failed", err
		}
		if _, ok, err := e.store.CacheGet(ctx, key); err != nil {
			return "failed", err
		} else if ok {
			continue
		}
		vec, err := opt.Embedder.Embed(ctx, ch.Text)
		if err != nil {
			failed++
			if opt.Vector == VectorRequired {
				return "failed", err
			}
			continue
		}
		raw, _ := json.Marshal(map[string]int{"dimension": len(vec)})
		input, _ := ids.Hash(ch.Text)
		if err := e.store.CachePut(ctx, key, input, profile.ID, string(raw)); err != nil {
			return "failed", err
		}
	}
	if failed > 0 {
		return "partial", nil
	}
	return "ready", nil
}

type Page struct {
	Limit  int
	Offset int
}

func (e *Engine) Keyword(ctx context.Context, kb, query string, page Page) (types.Response, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return types.Response{}, types.Fail("INVALID_ARGUMENT", "q is required", 400)
	}
	match := document.MatchQuery(query)
	if match == "" {
		return types.Response{}, types.Fail("INVALID_ARGUMENT", "query has no searchable token", 400)
	}
	hits, err := e.store.Search(ctx, kb, match)
	if err != nil {
		return types.Response{}, err
	}
	origin, err := e.store.Origin(ctx, kb)
	if err != nil {
		return types.Response{}, err
	}
	var items []types.Item
	for _, hit := range hits {
		if !document.Accept(hit.Text, query) && !document.Accept(hit.Title, query) {
			continue
		}
		ref, err := ids.EncodeRef(ids.Ref{Origin: origin, KB: kb, Path: hit.Path, Edition: hit.Edition, Chunk: hit.ID})
		if err != nil {
			return types.Response{}, err
		}
		items = append(items, types.Item{Kind: "chunk", KB: kb, Path: hit.Path, Edition: hit.Edition, ChunkID: hit.ID, ChunkKey: hit.Key, FileHash: hit.Hash, Title: hit.Title, Text: hit.Text, LineStart: hit.LineStart, LineEnd: hit.LineEnd, Ref: ref})
	}
	return e.freeze(ctx, kb, "search.keyword", "chunks", items, page)
}

func (e *Engine) Tree(ctx context.Context, kb string) (types.Response, error) {
	files, err := e.store.Published(ctx, kb)
	if err != nil {
		return types.Response{}, err
	}
	origin, err := e.store.Origin(ctx, kb)
	if err != nil {
		return types.Response{}, err
	}
	items := make([]types.Item, 0, len(files))
	for _, f := range files {
		ref, err := ids.EncodeRef(ids.Ref{Origin: origin, KB: f.KB, Path: f.Path, Edition: f.Edition})
		if err != nil {
			return types.Response{}, err
		}
		items = append(items, types.Item{Kind: "file", KB: f.KB, Path: f.Path, Edition: f.Edition, FileHash: f.Hash, Ref: ref})
	}
	return e.freeze(ctx, kb, "files.tree", "files", items, Page{Limit: len(items)})
}

func (e *Engine) Grep(ctx context.Context, kb, pattern string) (types.Response, error) {
	re, err := regexp.Compile(pattern)
	if err != nil || pattern == "" {
		return types.Response{}, types.Fail("INVALID_ARGUMENT", "a valid RE2 pattern is required", 400)
	}
	files, err := e.store.Published(ctx, kb)
	if err != nil {
		return types.Response{}, err
	}
	origin, err := e.store.Origin(ctx, kb)
	if err != nil {
		return types.Response{}, err
	}
	var items []types.Item
	for _, f := range files {
		raw, err := os.ReadFile(filepath.Join(e.root, filepath.FromSlash(f.ContentPath)))
		if err != nil {
			return types.Response{}, err
		}
		lines := strings.Split(string(raw), "\n")
		for i, line := range lines {
			if !re.MatchString(line) {
				continue
			}
			ref, err := ids.EncodeRef(ids.Ref{Origin: origin, KB: f.KB, Path: f.Path, Edition: f.Edition})
			if err != nil {
				return types.Response{}, err
			}
			items = append(items, types.Item{Kind: "match", KB: f.KB, Path: f.Path, Edition: f.Edition, FileHash: f.Hash, Text: line, LineStart: i + 1, LineEnd: i + 1, Ref: ref})
		}
	}
	return e.freeze(ctx, kb, "files.grep", "matched_lines", items, Page{Limit: 100})
}

func (e *Engine) Read(ctx context.Context, ref string) (types.Response, error) {
	decoded, err := ids.DecodeRef(ref)
	if err != nil {
		return types.Response{}, types.Fail("INVALID_ARGUMENT", "ref cannot be decoded", 400)
	}
	if decoded.Chunk == "" {
		return types.Response{}, types.Fail("INVALID_ARGUMENT", "file refs are not a content read; use a chunk ref", 400)
	}
	ch, kb, withdrawn, err := e.store.Chunk(ctx, decoded.Chunk)
	if err != nil {
		return types.Response{}, types.Fail("NOT_FOUND", "chunk not found", 404)
	}
	origin, err := e.store.Origin(ctx, kb)
	if err != nil {
		return types.Response{}, err
	}
	if decoded.Origin != "" && decoded.Origin != origin {
		return types.Response{}, types.Fail("ORIGIN_MISMATCH", "ref origin does not match this server", 409)
	}
	if withdrawn != 0 {
		return types.Response{}, types.Fail("REF_WITHDRAWN", "withdrawn content is not returned", 410)
	}
	if decoded.Edition != ch.Edition || decoded.Path != ch.Path {
		return types.Response{}, types.Fail("REF_MISMATCH", "ref does not match the stored chunk", 409)
	}
	item := types.Item{Kind: "chunk", KB: ch.KB, Path: ch.Path, Edition: ch.Edition, ChunkID: ch.ID, ChunkKey: ch.Key, FileHash: ch.Hash, Title: ch.Title, Text: ch.Text, LineStart: ch.LineStart, LineEnd: ch.LineEnd, Ref: ref}
	resp := e.baseCoverage(ctx, kb, []string{ch.Edition}, false)
	resp.Action = "files.read"
	resp.Items = []types.Item{item}
	resp.Total = types.Exact(1, "chunks", "ref")
	return resp, nil
}

func (e *Engine) Page(ctx context.Context, resultID string, page Page) (types.Response, error) {
	frozen, err := e.store.LoadResult(ctx, resultID)
	if err != nil {
		return types.Response{}, err
	}
	paged := slice(frozen.Items, page)
	resp := types.Response{Action: "results.page", Items: paged, Total: frozen.Total, Handle: frozen.Handle, ResultID: resultID, Notes: []string{"page read uses the frozen result set"}}
	resp.Coverage = types.Coverage{ExecutionPartial: false, DisplayTruncated: page.Offset+len(paged) < frozen.Total.Value, Approximate: false, Consistency: "file_view", ScopeBasis: "enumerated", IndexCoverage: []types.ProjectionCoverage{{Projection: "lexical", State: "complete", Unit: "chunks", Total: intPtr(frozen.Total.Value), Ready: intPtr(frozen.Total.Value), Failed: intPtr(0), Pending: intPtr(0), Excluded: intPtr(0), ObservedAt: time.Now().UTC().Format(time.RFC3339), Scope: resultID, ReasonCodes: []string{}}}}
	return resp, nil
}

func (e *Engine) freeze(ctx context.Context, kb, action, unit string, items []types.Item, page Page) (types.Response, error) {
	files, err := e.store.Published(ctx, kb)
	if err != nil {
		return types.Response{}, err
	}
	origin, err := e.store.Origin(ctx, kb)
	if err != nil {
		return types.Response{}, err
	}
	idsList := make([]string, len(files))
	for i, f := range files {
		idsList[i] = f.Edition
	}
	viewID, _ := ids.Prefixed("qv_")
	resultID, _ := ids.Prefixed("rs_")
	handleID, _ := ids.Prefixed("h_")
	now := time.Now().UTC()
	handle := &types.Handle{ID: handleID, Kind: "files", Basis: "enumerated", Complete: true, Count: len(files), SourceRestricted: true, ExpiresAt: now.Add(24 * time.Hour).Format(time.RFC3339)}
	total := types.Exact(len(items), unit, "kb:"+kb)
	cov, err := e.store.Coverage(ctx, idsList, "kb:"+kb)
	if err != nil {
		return types.Response{}, err
	}
	frozen := sqlite.Frozen{Items: items, Total: total, Handle: handle}
	if err := e.store.Freeze(ctx, viewID, resultID, handleID, origin, action, files, frozen, now.Add(24*time.Hour).Unix(), now.Add(10*time.Minute).Unix()); err != nil {
		return types.Response{}, err
	}
	paged := slice(items, page)
	resp := types.Response{Action: action, Items: paged, Total: total, Handle: handle, ResultID: resultID}
	resp.Coverage = types.Coverage{ExecutionPartial: false, DisplayTruncated: page.Offset+len(paged) < len(items), Approximate: false, Consistency: "file_view", ScopeBasis: "enumerated", IndexCoverage: []types.ProjectionCoverage{cov}}
	return resp, nil
}

func (e *Engine) baseCoverage(ctx context.Context, kb string, editions []string, truncated bool) types.Response {
	cov, err := e.store.Coverage(ctx, editions, "kb:"+kb)
	if err != nil {
		cov = types.ProjectionCoverage{Projection: "lexical", State: "unknown", Unit: "chunks", ObservedAt: time.Now().UTC().Format(time.RFC3339), Scope: "kb:" + kb, ReasonCodes: []string{"COVERAGE_UNAVAILABLE"}}
	}
	return types.Response{Coverage: types.Coverage{ExecutionPartial: false, DisplayTruncated: truncated, Approximate: false, Consistency: "file_view", ScopeBasis: "enumerated", IndexCoverage: []types.ProjectionCoverage{cov}}}
}

func slice(items []types.Item, page Page) []types.Item {
	if page.Limit <= 0 {
		page.Limit = 10
	}
	if page.Offset < 0 {
		page.Offset = 0
	}
	if page.Offset >= len(items) {
		return []types.Item{}
	}
	end := page.Offset + page.Limit
	if end > len(items) {
		end = len(items)
	}
	return append([]types.Item{}, items[page.Offset:end]...)
}

func intPtr(n int) *int { return &n }

func IsNotFound(err error) bool {
	var f *types.Failure
	return errors.As(err, &f) && f.Code == "NOT_FOUND"
}
