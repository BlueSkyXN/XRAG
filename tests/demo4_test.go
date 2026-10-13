package tests

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"github.com/BlueSkyXN/XRAG/adapters"
	"github.com/BlueSkyXN/XRAG/core"
	"github.com/BlueSkyXN/XRAG/internal/build"
	"github.com/BlueSkyXN/XRAG/internal/source"
	"github.com/BlueSkyXN/XRAG/internal/store"
)

func publishedKnowledge(t *testing.T, s *store.Store, b *build.Builder, kind string) store.Knowledge {
	t.Helper()
	d := pub(t, s, "original.md", "# 日志\n日志保留规则与错误码 E203。\n")
	k, e := s.SaveKnowledge(ctx, store.Knowledge{KB: "demo", Kind: kind, Title: "日志综述", Body: "旧版已经发布的内容 [1]。", Refs: []string{core.ChunkItem(d.Chunks[0], s.Origin).Ref}})
	must(t, e)
	j := claim(t, s, "publish", map[string]any{"id": k.ID, "revision": k.Draft})
	b.Execute(ctx, j)
	done, e := s.Job(ctx, j.ID)
	must(t, e)
	if done.State != "succeeded" {
		t.Fatal(done.Error)
	}
	k, e = s.Knowledge(ctx, k.ID, "")
	must(t, e)
	return k
}
func runSync(t *testing.T, s *store.Store, b *build.Builder, k store.Knowledge, target string) store.Job {
	t.Helper()
	j := claim(t, s, "sync", build.SyncRequest{ID: k.ID, Revision: k.Published, Target: target})
	b.Execute(ctx, j)
	j, e := s.Job(ctx, j.ID)
	must(t, e)
	return j
}
func TestWikiRegenerateRetainsPublicationAndUsesOriginalSources(t *testing.T) {
	s, engine := setup(t)
	b := &build.Builder{Store: s}
	k := publishedKnowledge(t, s, b, "wiki")
	var calls atomic.Int32
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if strings.Contains(string(raw), "旧版已经发布的内容") {
			t.Error("old Wiki fed into its regeneration")
		}
		calls.Add(1)
		io.WriteString(w, `{"choices":[{"message":{"content":"新草稿生成内容 [1]。"}}],"usage":{"fixture":true,"total_tokens":10}}`)
	}))
	defer model.Close()
	b.Config.Chat = &adapters.Endpoint{URL: model.URL, Model: "fixture"}
	j := claim(t, s, "wiki", build.WikiRequest{ID: k.ID, Topic: "日志", Expected: k.Draft})
	b.Execute(ctx, j)
	job, e := s.Job(ctx, j.ID)
	must(t, e)
	if job.State != "succeeded" {
		t.Fatal(job.Error)
	}
	latest, e := s.Knowledge(ctx, k.ID, "")
	must(t, e)
	if latest.Published != k.Published || latest.Draft == k.Draft || calls.Load() != 2 {
		t.Fatal(latest, calls.Load())
	}
	r := run(t, engine, "search.keyword", core.Request{Q: "新草稿生成内容"})
	if r.Returned != 0 {
		t.Fatal("draft leaked")
	}
	old := run(t, engine, "search.keyword", core.Request{Q: "旧版已经发布的内容"})
	if old.Returned == 0 {
		t.Fatal("old publication unavailable")
	}
	revs, e := s.KnowledgeRevisions(ctx, k.ID)
	must(t, e)
	if len(revs) != 2 {
		t.Fatal(revs)
	}
	j = claim(t, s, "publish", map[string]any{"id": latest.ID, "revision": latest.Draft})
	b.Execute(ctx, j)
	r = run(t, engine, "search.keyword", core.Request{Q: "新草稿生成内容"})
	if r.Returned == 0 {
		t.Fatal("new publication missing")
	}
}
func TestWikiRegenerateBadCitationPreservesDraft(t *testing.T) {
	s, _ := setup(t)
	b := &build.Builder{Store: s}
	k := publishedKnowledge(t, s, b, "wiki")
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"choices":[{"message":{"content":"不正确编号 [99]。"}}]}`)
	}))
	defer model.Close()
	b.Config.Chat = &adapters.Endpoint{URL: model.URL, Model: "fixture"}
	j := claim(t, s, "wiki", build.WikiRequest{ID: k.ID, Topic: "日志", Expected: k.Draft})
	b.Execute(ctx, j)
	job, _ := s.Job(ctx, j.ID)
	if job.State != "failed" {
		t.Fatal(job)
	}
	latest, e := s.Knowledge(ctx, k.ID, "")
	must(t, e)
	if latest.Draft != k.Draft || latest.Published != k.Published {
		t.Fatal("failed generation changed content")
	}
}
func TestWikiRegenerationRejectsStaleDraftBeforeModel(t *testing.T) {
	s, _ := setup(t)
	b := &build.Builder{Store: s}
	k := publishedKnowledge(t, s, b, "wiki")
	var n atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { n.Add(1) }))
	defer ts.Close()
	b.Config.Chat = &adapters.Endpoint{URL: ts.URL, Model: "fixture"}
	j := claim(t, s, "wiki", build.WikiRequest{ID: k.ID, Topic: "日志", Expected: "old"})
	_, e := b.RunWiki(ctx, j)
	code(t, e, "VERSION_CONFLICT")
	if n.Load() != 0 {
		t.Fatal("stale generation made model call")
	}
}
func TestWikiScopeWithoutEvidenceDoesNotCallModel(t *testing.T) {
	s, _ := setup(t)
	pub(t, s, "docs/a.md", "# 日志\n日志内容")
	b := &build.Builder{Store: s, Config: adapters.Config{Chat: &adapters.Endpoint{URL: "http://127.0.0.1:1", Model: "fixture"}}}
	j := claim(t, s, "wiki", build.WikiRequest{Topic: "日志", Path: "other/"})
	_, e := b.RunWiki(ctx, j)
	code(t, e, "NO_EVIDENCE")
}
func TestWritebackPathIsExplicitAndIdempotent(t *testing.T) {
	s, _ := setup(t)
	b := &build.Builder{Store: s}
	k := publishedKnowledge(t, s, b, "case")
	root := t.TempDir()
	target, e := s.AddSyncTarget(ctx, store.SyncTarget{ID: "out", KB: "demo", Kind: "path", Root: root})
	must(t, e)
	if len(mustEntries(t, root)) != 0 {
		t.Fatal("registering destination wrote content")
	}
	j := runSync(t, s, b, k, target.ID)
	if j.State != "succeeded" {
		t.Fatal(j.Error)
	}
	receipt, ok, e := s.SyncReceipt(ctx, target.ID, k.ID)
	must(t, e)
	if !ok {
		t.Fatal("no receipt")
	}
	file := filepath.Join(root, receipt.Path)
	raw, e := os.ReadFile(file)
	must(t, e)
	if !strings.Contains(string(raw), k.Body) {
		t.Fatal("wrong body")
	}
	j = runSync(t, s, b, k, target.ID)
	if j.State != "succeeded" {
		t.Fatal(j.Error)
	}
	again, e := os.ReadFile(file)
	must(t, e)
	if string(raw) != string(again) {
		t.Fatal("duplicate changed export")
	}
}
func mustEntries(t *testing.T, p string) []os.DirEntry {
	t.Helper()
	r, e := os.ReadDir(p)
	must(t, e)
	return r
}
func TestWritebackConflictNeverOverwritesUserEdit(t *testing.T) {
	s, _ := setup(t)
	b := &build.Builder{Store: s}
	k := publishedKnowledge(t, s, b, "case")
	target, e := s.AddSyncTarget(ctx, store.SyncTarget{ID: "out", KB: "demo", Kind: "path", Root: t.TempDir()})
	must(t, e)
	j := runSync(t, s, b, k, target.ID)
	if j.State != "succeeded" {
		t.Fatal(j.Error)
	}
	r, _, _ := s.SyncReceipt(ctx, target.ID, k.ID)
	dest := filepath.Join(target.Root, r.Path)
	must(t, os.WriteFile(dest, []byte("user edit"), 0600))
	j = runSync(t, s, b, k, target.ID)
	if j.State != "failed" || !strings.Contains(j.Error, "SYNC_CONFLICT") {
		t.Fatal(j)
	}
	raw, e := os.ReadFile(dest)
	must(t, e)
	if string(raw) != "user edit" {
		t.Fatal("overwritten")
	}
}
func TestSyncCopyNeedsReceiptAndDigest(t *testing.T) {
	s, _ := setup(t)
	b := &build.Builder{Store: s}
	k := publishedKnowledge(t, s, b, "case")
	root := t.TempDir()
	target, e := s.AddSyncTarget(ctx, store.SyncTarget{ID: "out", KB: "demo", Kind: "path", Root: root})
	must(t, e)
	j := runSync(t, s, b, k, target.ID)
	if j.State != "succeeded" {
		t.Fatal(j.Error)
	}
	receipt, _, _ := s.SyncReceipt(ctx, target.ID, k.ID)
	raw, e := os.ReadFile(filepath.Join(root, receipt.Path))
	must(t, e)
	src := store.Source{Kind: "path", KB: "demo", Root: root}
	ok, e := s.IsSyncCopy(ctx, src, receipt.Path, string(raw))
	must(t, e)
	if !ok {
		t.Fatal("actual copy not recognized")
	}
	ok, e = s.IsSyncCopy(ctx, src, receipt.Path, string(raw)+"edit")
	must(t, e)
	if ok {
		t.Fatal("marker bypassed digest")
	}
}
func TestWritebackGitKeepsWorkingTreeAndMain(t *testing.T) {
	s, _ := setup(t)
	b := &build.Builder{Store: s}
	k := publishedKnowledge(t, s, b, "case")
	repo := gitrepo(t)
	must(t, os.WriteFile(filepath.Join(repo, "initial.md"), []byte("original"), 0600))
	commit(t, repo)
	old := gitcmd(t, repo, "rev-parse", "HEAD")
	must(t, os.WriteFile(filepath.Join(repo, "untracked.md"), []byte("not committed"), 0600))
	before := gitcmd(t, repo, "status", "--porcelain")
	target, e := s.AddSyncTarget(ctx, store.SyncTarget{ID: "gitout", KB: "demo", Kind: "git", Root: repo})
	must(t, e)
	j := runSync(t, s, b, k, target.ID)
	if j.State != "succeeded" {
		t.Fatal(j.Error)
	}
	r, _, _ := s.SyncReceipt(ctx, target.ID, k.ID)
	body := gitcmd(t, repo, "show", target.Branch+":"+r.Path)
	if !strings.Contains(body, k.Body) {
		t.Fatal(body)
	}
	if gitcmd(t, repo, "rev-parse", "HEAD") != old || gitcmd(t, repo, "status", "--porcelain") != before {
		t.Fatal("working tree or main changed")
	}
	first := gitcmd(t, repo, "rev-parse", target.Branch)
	j = runSync(t, s, b, k, target.ID)
	if j.State != "succeeded" || gitcmd(t, repo, "rev-parse", target.Branch) != first {
		t.Fatal("duplicate sync generated extra commit", j.Error)
	}
}
func TestWritebackRefusesSymlinkAndDefaultBranch(t *testing.T) {
	s, _ := setup(t)
	_, e := s.AddSyncTarget(ctx, store.SyncTarget{ID: "bad", KB: "demo", Kind: "git", Root: t.TempDir(), Branch: "main"})
	code(t, e, "INVALID_ARGUMENT")
	root := t.TempDir()
	outside := t.TempDir()
	must(t, os.Symlink(outside, filepath.Join(root, "sub")))
	_, e = source.WriteBack(ctx, store.SyncTarget{Kind: "path", Root: root}, "sub/x.md", "test", "")
	code(t, e, "SOURCE_CHANGED")
	if len(mustEntries(t, outside)) != 0 {
		t.Fatal("symlink target written")
	}
}
func TestArchiveKeepsHistoricalRefButLeavesSearch(t *testing.T) {
	s, engine := setup(t)
	b := &build.Builder{Store: s}
	k := publishedKnowledge(t, s, b, "case")
	r := run(t, engine, "search.keyword", core.Request{Q: "旧版已经发布的内容"})
	ref, e := core.DecodeRef(r.Items[0].Ref)
	must(t, e)
	must(t, s.ArchiveKnowledge(ctx, k.ID, k.Published))
	r = run(t, engine, "search.keyword", core.Request{Q: "旧版已经发布的内容"})
	if r.Returned != 0 {
		t.Fatal("archived still searched")
	}
	_, _, fresh, e := s.Read(ctx, ref)
	must(t, e)
	if fresh != "historical" {
		t.Fatal(fresh)
	}
}
func TestSyncTargetAndLifecycleHTTPRoles(t *testing.T) {
	h := productSetup(t)
	_, token, e := h.s.CreateManagedKey(ctx, "reader", "reader", []string{"demo"}, 0)
	must(t, e)
	n, _ := h.request(t, "GET", "/v1/kbs/demo/sync-targets", nil, token)
	if n != 403 {
		t.Fatal(n)
	}
	q := map[string]any{"target": store.SyncTarget{ID: "out", Kind: "path", Root: t.TempDir()}}
	n, _ = h.request(t, "POST", "/v1/kbs/demo/sync-targets", q, h.key)
	if n != 400 {
		t.Fatal(n)
	}
	q["confirm"] = true
	n, raw := h.request(t, "POST", "/v1/kbs/demo/sync-targets", q, h.key)
	if n != 201 {
		t.Fatal(n, string(raw))
	}
	n, _ = h.request(t, "GET", "/v1/kbs/demo/sync-targets", nil, h.key)
	if n != 200 {
		t.Fatal(n)
	}
}
func TestSyncRejectedForDraftOrWithdrawnEvidence(t *testing.T) {
	s, _ := setup(t)
	b := &build.Builder{Store: s}
	k := publishedKnowledge(t, s, b, "case")
	target, e := s.AddSyncTarget(ctx, store.SyncTarget{ID: "out", KB: "demo", Kind: "path", Root: t.TempDir()})
	must(t, e)
	must(t, s.Withdraw(ctx, "demo", "original.md"))
	j := runSync(t, s, b, k, target.ID)
	if j.State != "failed" {
		t.Fatal(j)
	}
	if len(mustEntries(t, target.Root)) != 0 {
		t.Fatal("withdrawn material exported")
	}
}
func TestDirectSemanticUsesActualOriginCollection(t *testing.T) {
	ep := adapters.Endpoint{Model: "fixture", Revision: "r1", Dimension: 2}
	origin := "origin_unit"
	d := parsed(t, "a.md", "日志")
	d.Chunks[0].Vector = []float64{1, 0}
	d.Chunks[0].Generation = ep.SemanticID()
	var queries atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/embeddings":
			io.WriteString(w, `{"data":[{"index":0,"embedding":[1,0]}]}`)
		case strings.HasSuffix(r.URL.Path, "/points/query"):
			queries.Add(1)
			json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"points": []any{map[string]any{"id": d.Chunks[0].ID, "score": 1}}}})
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer ts.Close()
	ep.URL = ts.URL
	// SemanticID excludes URL, but set the actual ID explicitly to mirror construction.
	d.Chunks[0].Generation = ep.SemanticID()
	q := &adapters.Qdrant{Endpoint: adapters.Endpoint{URL: ts.URL}, Embed: &adapters.Embedder{Config: ep}, Origin: origin}
	direct := &adapters.Direct{Q: q, Collection: q.Collection("demo"), KB: "demo", Origin: origin}
	rs, e := direct.Search(ctx, core.View{Origin: origin, Documents: []core.Document{d}}, "日志")
	must(t, e)
	if len(rs) != 1 || queries.Load() != 1 {
		t.Fatal(rs, queries.Load())
	}
}
