package build

import (
	"context"
	"encoding/json"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"github.com/BlueSkyXN/XRAG/adapters"
	"github.com/BlueSkyXN/XRAG/core"
	"github.com/BlueSkyXN/XRAG/internal/oplock"
	"github.com/BlueSkyXN/XRAG/internal/source"
	"github.com/BlueSkyXN/XRAG/internal/store"
)

type Builder struct {
	Store     *store.Store
	Config    adapters.Config
	Logger    *log.Logger
	Options   store.KBSettings
	BuildMode string
}

func (b *Builder) Engine() *core.Engine {
	e := &core.Engine{Repo: b.Store, Mode: "server", Graph: scopedGraph{b}}
	semantic, rerank, external := b.Config.Embedding != nil, b.Config.Rerank != nil, len(b.Config.External) > 0
	kbs, _ := b.Store.KBs(context.Background())
	for _, kb := range kbs {
		bb, err := b.ForKB(context.Background(), kb["id"].(string))
		if err == nil {
			semantic = semantic || bb.Config.Embedding != nil
			rerank = rerank || bb.Config.Rerank != nil
			external = external || len(bb.Config.External) > 0
		}
	}
	if semantic {
		e.Semantic = scopedSemantic{b}
	}
	if rerank {
		e.Rerank = scopedRerank{b}
	}
	if external {
		e.External = scopedExternal{b}
	}
	return e
}
func (b *Builder) Settings() string {
	ep := "none"
	if b.Config.Embedding != nil {
		ep = b.Config.Embedding.SemanticID()
	}
	q := "local"
	if b.Config.Qdrant != nil {
		q = b.Config.Qdrant.URL
	}
	n := "local"
	if b.Config.Neo4j != nil {
		n = b.Config.Neo4j.URL
	}
	return core.Hash([]any{core.ParserRevision, ep, q, n, b.Config.Extractor, b.Config.ExtractorRevision, b.Config.Graph, b.Options.ChunkMax, b.Options.Fields, b.Options.Dictionary, b.Options.Parser, b.Config.ParserCommand, b.Config.ParserRevision})
}

type ScanEntry struct {
	Rel  string
	Full string
	Size int64
	Mod  time.Time
}

func Inventory(root string) ([]ScanEntry, error) {
	entries := []ScanEntry{}
	e := filepath.WalkDir(root, func(p string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if d.IsDir() {
			if p != root && (strings.HasPrefix(d.Name(), ".") || d.Name() == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(p))
		if ext != ".md" && ext != ".markdown" && ext != ".txt" {
			return nil
		}
		info, e := d.Info()
		if e != nil {
			return e
		}
		if info.Size() > 5<<20 {
			return core.Err("SOURCE_LIMIT", "a source file exceeds 5 MiB", 422)
		}
		rel, e := filepath.Rel(root, p)
		if e != nil {
			return e
		}
		rel = filepath.ToSlash(rel)
		if _, e = core.CleanPath(rel); e != nil {
			return e
		}
		entries = append(entries, ScanEntry{rel, p, info.Size(), info.ModTime()})
		if len(entries) > 5000 {
			return core.Err("SOURCE_LIMIT", "source limit: 5000 files per directory source", 422)
		}
		return nil
	})
	return entries, e
}
func Fetch(entry ScanEntry) (string, error) {
	before, e := os.Lstat(entry.Full)
	if e != nil {
		return "", e
	}
	if before.Mode()&os.ModeSymlink != 0 || !before.Mode().IsRegular() {
		return "", core.Err("SOURCE_CHANGED", "source file changed type", 409)
	}
	b, e := os.ReadFile(entry.Full)
	if e != nil {
		return "", e
	}
	after, e := os.Lstat(entry.Full)
	if e != nil {
		return "", e
	}
	if len(b) > 5<<20 || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) || after.Mode()&os.ModeSymlink != 0 {
		return "", core.Err("SOURCE_CHANGED", "file changed during read", 409)
	}
	return core.Normalize(b)
}
func (b *Builder) Prepare(ctx context.Context, d *core.Document, j store.Job) error {
	d.Settings = b.Settings()
	ctx = b.MeterContext(ctx, d.KB, j.ID, j.Owner)
	graph := b.Config.Graph
	graphOn := core.Included(d.Path, graph.Include, graph.Exclude)
	if b.BuildMode != "vectors_only" {
		d.Supports = core.ExplicitSupports(*d)
		if !graphOn {
			d.Supports = nil
		}
	}
	if d.Kind == "wiki" {
		d.Supports = b.WikiLinks(ctx, *d)
	}
	if err := b.ApplyFields(d); err != nil {
		return err
	}
	if b.Config.Embedding != nil {
		if err := b.Store.SaveGeneration(ctx, d.KB, b.Config); err != nil {
			return err
		}
	}
	for i := range d.Chunks {
		c := &d.Chunks[i]
		if b.Config.Embedding != nil && b.BuildMode != "graph_only" {
			embed := &adapters.Embedder{Config: *b.Config.Embedding}
			input := c.Title + "\n" + strings.Join(c.Heading, " / ") + "\n" + c.Text
			key := core.Hash([]string{embed.Config.SemanticID(), input})
			saved, ok, err := b.Store.ModelGet(ctx, key)
			if err != nil {
				return err
			}
			if ok {
				err = json.Unmarshal(saved, &c.Vector)
			} else {
				call, ee := b.Store.UsageStart(ctx, j, "embedding")
				if ee != nil {
					return ee
				}
				vec, usage, ee := embed.Embed(ctx, []string{input}, false)
				_ = b.Store.UsageEnd(context.Background(), call, usage, ee == nil)
				err = ee
				if err == nil {
					c.Vector = vec[0]
					err = b.Store.ModelPut(ctx, key, c.Vector)
				}
			}
			if err != nil {
				if !b.Config.OptionalVectors || b.BuildMode == "vectors_only" {
					return err
				}
				c.Vector = nil
				c.VectorState = "failed"
				c.Generation = ""
			} else {
				c.Generation = embed.Config.SemanticID()
				c.VectorState = "ready"
			}
		}
		if b.BuildMode == "vectors_only" || d.Kind == "wiki" || !graphOn {
			continue
		}
		mode := graph.Mode
		if mode == "" && len(b.Config.Extractor) > 0 {
			mode = "plugin"
		}
		if mode != "llm" && mode != "plugin" {
			c.GraphState = "ready"
			continue
		}
		key := core.Hash([]any{graph, b.Config.ExtractorRevision, b.Config.Extractor, c.Title, c.Heading, c.Text})
		saved, ok, err := b.Store.ModelGet(ctx, key)
		if err != nil {
			return err
		}
		ss := []core.Support{}
		if ok {
			err = json.Unmarshal(saved, &ss)
		} else {
			if mode == "llm" {
				if graph.Model == nil {
					return core.Err("CAPABILITY_UNAVAILABLE", "graph LLM mode requires a configured model", 422)
				}
				var usage any
				ss, usage, err = adapters.ExtractLLM(ctx, *graph.Model, graph, *c)
				_ = usage
			} else {
				ss, err = adapters.Extract(ctx, b.Config.Extractor, b.Config.ExtractorRevision, *c)
			}
			if err == nil {
				err = b.Store.ModelPut(ctx, key, ss)
			}
		}
		if err != nil {
			if !graph.Optional {
				return err
			}
			c.GraphState = "failed"
			continue
		}
		for _, old := range ss {
			old.KB = c.KB
			old.Path = c.Path
			old.Edition = c.Edition
			old.ChunkID = c.ID
			old.ID = "sp_" + core.Hash([]any{c.ID, key, old.From, old.Relation, old.To, old.Origin})[:32]
			d.Supports = append(d.Supports, old)
		}
		c.GraphState = "ready"
	}
	return nil
}
func (b *Builder) Materialize(ctx context.Context, d core.Document, j store.Job, item, rev string) error {
	if e := b.Store.CheckJob(ctx, j); e != nil {
		return e
	}
	if e := b.Prepare(ctx, &d, j); e != nil {
		return e
	}
	if e := b.Store.Stage(ctx, d); e != nil {
		return e
	}
	ok := false
	defer func() {
		if !ok {
			_ = b.Store.Abort(context.Background(), d.Edition)
		}
	}()
	if e := b.WriteProjections(ctx, d); e != nil {
		return e
	}
	if e := b.Store.Publish(ctx, d, j, item, rev); e != nil {
		return e
	}
	ok = true
	return nil
}
func (b *Builder) RunBuild(ctx context.Context, j store.Job) (any, error) {
	scoped, resolveErr := b.ForKB(ctx, j.KB)
	if resolveErr != nil {
		return nil, resolveErr
	}
	b = scoped
	var mode struct {
		Mode string `json:"mode"`
	}
	_ = json.Unmarshal(j.Payload, &mode)
	b.BuildMode = mode.Mode
	if mode.Mode == "vectors_only" || mode.Mode == "graph_only" {
		return b.RunProjectionBuild(ctx, j, mode.Mode)
	}

	sources, err := b.Store.Sources(ctx, j.KB)
	if err != nil {
		return nil, err
	}
	var payload struct {
		Mode string `json:"mode"`
	}
	_ = json.Unmarshal(j.Payload, &payload)
	changed, unchanged, failed := 0, 0, 0
	errs := []string{}
	revisions := map[string]string{}
	for _, src := range sources {
		if src.Options.Disabled {
			continue
		}
		scan, e := source.Open(ctx, b.Store.Dir, src)
		if e != nil {
			failed++
			errs = append(errs, src.ID+": "+e.Error())
			_ = b.Store.ObserveSource(ctx, src, "", "", e)
			_ = b.Store.BuildDetail(ctx, j, src.ID, "", "scan_failed", "", "", "", e)
			continue // no removal on incomplete enumeration/read; other sources can still succeed
		}
		revisions[src.ID] = scan.Revision
		fp := core.Hash([]string{scan.Fingerprint, b.Settings()})
		if e = b.Store.ObserveSource(ctx, src, scan.Revision, fp, nil); e != nil {
			return nil, e
		}
		seen := map[string]bool{}
		sourceFailed := false
		for _, entry := range scan.Entries {
			if !core.Included(entry.Path, src.Options.Include, src.Options.Exclude) {
				continue
			}
			if e = b.Store.CheckJob(ctx, j); e != nil {
				return nil, e
			}
			path := entry.Path
			if src.Mount != "" {
				path = src.Mount + "/" + path
			}
			copy, ce := b.Store.IsSyncCopy(ctx, src, entry.Path, entry.Body)
			if ce != nil {
				return nil, ce
			}
			if copy {
				_ = b.Store.BuildDetail(ctx, j, src.ID, path, "synced_copy_skipped", "", "", scan.Revision, nil)
				continue
			}
			seen[path] = true
			old, exists, e := b.Store.Current(ctx, j.KB, path)
			// Withdrawal is intentional: a subsequent source scan never restores it.
			if e != nil && core.AsError(e).Code == "REF_WITHDRAWN" {
				_ = b.Store.BuildDetail(ctx, j, src.ID, path, "withdrawn", "", "", scan.Revision, nil)
				continue
			}
			if e != nil {
				failed++
				sourceFailed = true
				errs = append(errs, path+": "+e.Error())
				continue
			}
			if payload.Mode != "full" && exists && old.SourceHash == core.TextHash(entry.Body) && old.Settings == b.Settings() && Healthy(old) {
				unchanged++
				_ = b.Store.BuildDetail(ctx, j, src.ID, path, "unchanged", old.Edition, old.Edition, scan.Revision, nil)
				continue
			}
			d, e := b.ParseDocument(ctx, j.KB, path, entry.Body, core.ID("ed_"), src.ID)
			if e == nil {
				d.SourceHash = core.TextHash(entry.Body)
				d.SourceRevision = scan.Revision
				d.SourceConsistency = scan.Consistency
				e = b.Materialize(ctx, d, j, "", "")
			}
			state := "published"
			if e != nil {
				state = "failed"
				failed++
				sourceFailed = true
				errs = append(errs, path+": "+e.Error())
			} else {
				changed++
				current, ok, ce := b.Store.Current(ctx, j.KB, path)
				if ce == nil && ok && !Healthy(current) {
					failed++
					sourceFailed = true
					state = "partial"
					errs = append(errs, path+": optional projection incomplete")
				}
			}
			if x := b.Store.BuildDetail(ctx, j, src.ID, path, state, old.Edition, d.Edition, scan.Revision, e); x != nil {
				return nil, x
			}
		}
		// The inventory and bytes are complete even if an optional file's processing failed.
		// Preserve missing members conservatively until the entire source succeeds.
		if !sourceFailed {
			before, _ := b.Store.Snapshot(ctx, []string{j.KB})
			if e = b.Store.RemoveMissing(ctx, src, seen, j); e != nil {
				return nil, e
			}
			for _, d := range before.Documents {
				if d.Source == src.ID && !seen[d.Path] {
					_ = b.Store.BuildDetail(ctx, j, src.ID, d.Path, "removed", d.Edition, "", scan.Revision, nil)
				}
			}
			if e = b.Store.SourceIndexed(ctx, src, scan.Revision, fp, j); e != nil {
				return nil, e
			}
		} else {
			_ = b.Store.ObserveSource(ctx, src, scan.Revision, fp, core.Err("BUILD_PARTIAL", "one or more files failed; indexed fingerprint not advanced", 422))
		}
	}
	result := map[string]any{"changed": changed, "unchanged": unchanged, "failed": failed, "errors": errs, "source_revisions": revisions, "consistency": "file_publication"}
	if len(revisions) == 0 && failed > 0 {
		return result, core.Err("SOURCE_UNAVAILABLE", strings.Join(errs, "; "), 503)
	}
	return result, nil
}
func (b *Builder) RunPublish(ctx context.Context, j store.Job) (any, error) {
	scoped, er := b.ForKB(ctx, j.KB)
	if er != nil {
		return nil, er
	}
	b = scoped

	var p struct {
		ID       string `json:"id"`
		Revision string `json:"revision"`
	}
	if e := json.Unmarshal(j.Payload, &p); e != nil {
		return nil, e
	}
	k, e := b.Store.Knowledge(ctx, p.ID, p.Revision)
	if e != nil {
		return nil, e
	}
	if k.State == "blocked" {
		return nil, core.Err("REF_WITHDRAWN", "knowledge dependency was withdrawn", 410)
	}
	for _, raw := range k.Refs {
		if e = b.Store.CheckEvidence(ctx, raw, k.KB); e != nil {
			return nil, e
		}
	}
	body := "# " + k.Title + "\n\n" + k.Body
	d, e := b.ParseDocument(ctx, k.KB, "_knowledge/"+k.ID+".md", body, core.ID("ed_"), "managed")
	if e != nil {
		return nil, e
	}
	d.Kind = k.Kind
	d.Fields["knowledge_kind"] = k.Kind
	e = b.Materialize(ctx, d, j, k.ID, p.Revision)
	return map[string]any{"item_id": k.ID, "revision": p.Revision, "path": d.Path, "edition_id": d.Edition}, e
}
func (b *Builder) Execute(ctx context.Context, j store.Job) {
	ctx, cancel := context.WithDeadline(ctx, time.Unix(j.Deadline, 0))
	defer cancel()
	stop := make(chan struct{})
	done := make(chan struct{})
	go func(renewCtx context.Context) {
		defer close(done)
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-renewCtx.Done():
				return
			case <-ticker.C:
				if e := b.Store.Renew(renewCtx, j); e != nil {
					cancel()
					return
				}
			}
		}
	}(ctx)
	var result any
	var err error
	leaseCtx, release, lockErr := oplock.Acquire(ctx, b.Store.Dir, false)
	if lockErr == nil {
		ctx = leaseCtx
		defer release()
	} else {
		err = lockErr
	}
	if err == nil {
		switch j.Kind {
		case "build":
			result, err = b.RunBuild(ctx, j)
		case "publish":
			result, err = b.RunPublish(ctx, j)
		case "wiki":
			result, err = b.RunWiki(ctx, j)
		case "eval":
			result, err = b.RunEval(ctx, j)
		case "sync":
			result, err = b.RunSync(ctx, j)
		case "research", "recipe":
			result, err = b.RunInvestigation(ctx, j)
		default:
			err = core.Err("MODE_UNSUPPORTED", "unknown job kind", 422)
		}
	}
	close(stop)
	<-done
	if ctx.Err() != nil && err == nil {
		err = ctx.Err()
	}
	finishErr := b.Store.Finish(context.Background(), j, result, err)
	if finishErr != nil && b.Logger != nil {
		b.Logger.Printf("job finish %s: %v", j.ID, finishErr)
	}
	if err == nil && finishErr == nil && j.Kind == "publish" {
		if e := b.Store.DispatchAutoSync(context.Background()); e != nil && b.Logger != nil {
			b.Logger.Printf("auto-sync dispatch: %v", e)
		}
	}

	if err != nil && b.Logger != nil {
		b.Logger.Printf("job %s: %v", j.ID, err)
	}
}
func (b *Builder) Loop(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()
	for {
		if ctx.Err() != nil {
			return
		}
		if er := b.Store.DispatchAutoSync(ctx); er != nil && b.Logger != nil {
			b.Logger.Printf("auto-sync dispatch: %v", er)
		}
		leaseCtx, release, lockErr := oplock.Acquire(ctx, b.Store.Dir, false)
		if lockErr != nil {
			return
		}
		j, ok, e := b.Store.Claim(leaseCtx)
		if e != nil && ctx.Err() == nil && b.Logger != nil {
			b.Logger.Printf("claim: %v", e)
		}
		if ok {
			b.Execute(leaseCtx, j)
			release()
			continue
		}
		release()
		select {
		case <-ctx.Done():
			return
		case <-time.After(300 * time.Millisecond):
		}
	}
}
func (b *Builder) Estimate(ctx context.Context, kb string) (any, error) {
	scoped, err := b.ForKB(ctx, kb)
	if err != nil {
		return nil, err
	}
	b = scoped
	sources, e := b.Store.Sources(ctx, kb)
	if e != nil {
		return nil, e
	}
	files, chars, chunks := 0, 0, 0
	revisions := map[string]string{}
	for _, src := range sources {
		if src.Options.Disabled {
			continue
		}
		scan, e := source.Open(ctx, b.Store.Dir, src)
		if e != nil {
			return nil, e
		}
		revisions[src.ID] = scan.Revision
		for _, entry := range scan.Entries {
			if !core.Included(entry.Path, src.Options.Include, src.Options.Exclude) {
				continue
			}
			path := entry.Path
			if src.Mount != "" {
				path = src.Mount + "/" + path
			}
			d, e := b.ParseDocument(ctx, kb, path, entry.Body, "estimate", src.ID)
			if e != nil {
				return nil, e
			}
			files++
			chars += len([]rune(entry.Body))
			chunks += len(d.Chunks)
		}
	}
	return map[string]any{"files": files, "characters": chars, "estimated_chunks": chunks, "source_revisions": revisions, "models_called": 0, "indexes_written": false, "note": "Git estimates may fetch the registered repository cache; no knowledge index is changed."}, nil
}

func (b *Builder) WriteProjections(ctx context.Context, d core.Document) error {
	groups := map[string][]core.Chunk{}
	binding := store.ProjectionBinding{Neo4j: b.Config.Neo4j}
	for _, c := range d.Chunks {
		if len(c.Vector) > 0 && c.Generation != "" {
			groups[c.Generation] = append(groups[c.Generation], c)
		}
	}
	for gen := range groups {
		cfg, err := b.Store.Generation(ctx, d.KB, gen)
		if err != nil {
			return err
		}
		if cfg.Qdrant != nil {
			binding.Vectors = append(binding.Vectors, cfg)
		}
	}
	if err := b.Store.BindProjections(ctx, d, binding); err != nil {
		return err
	}
	for gen, chunks := range groups {
		cfg, err := b.Store.Generation(ctx, d.KB, gen)
		if err != nil {
			return err
		}
		if cfg.Qdrant == nil {
			continue
		}
		if cfg.Embedding == nil {
			return core.Err("MODEL_MISMATCH", "generation lacks embedding descriptor", 409)
		}
		part := d
		part.Chunks = chunks
		q := &adapters.Qdrant{Endpoint: *cfg.Qdrant, Embed: &adapters.Embedder{Config: *cfg.Embedding}, Origin: b.Store.Origin}
		if err = q.Write(ctx, part); err != nil {
			return err
		}
	}
	if b.Config.Neo4j != nil {
		n := &adapters.Neo4j{Config: *b.Config.Neo4j, Origin: b.Store.Origin}
		if err := n.Write(ctx, d); err != nil {
			return err
		}
	}
	return nil
}
