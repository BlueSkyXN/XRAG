package tests

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"github.com/BlueSkyXN/XRAG/adapters"
	"github.com/BlueSkyXN/XRAG/core"
	"github.com/BlueSkyXN/XRAG/internal/automation"
	"github.com/BlueSkyXN/XRAG/internal/build"
	"github.com/BlueSkyXN/XRAG/internal/source"
	"github.com/BlueSkyXN/XRAG/internal/store"
	"github.com/BlueSkyXN/XRAG/internal/watch"
)

func TestV5CommonMarkRealParserAndYAML(t *testing.T) {
	p, _ := filepath.Abs("../plugins/commonmark/parse.py")
	body := "---\ntitle: 中文标题\nproduct:\n  - A\n  - B\nsummary: |\n  第一行\n  第二行\n---\n# 标题\n\n```go\n# 不是章节\n```\n\n| A | B |\n|---|---|\n| x | y |\n\nCafe\u0301\n"
	d, e := adapters.ParseDocument(ctx, []string{"python3", p}, "demo", "x.md", body, "ed_demo", "manual", 1200)
	must(t, e)
	if d.Title != "中文标题" || !strings.Contains(d.Body, "Café") || len(d.Chunks) < 1 {
		t.Fatal(core.JSON(d))
	}
	if a, ok := d.Fields["product"].([]any); !ok || len(a) != 2 {
		t.Fatal(d.Fields)
	}
	for _, c := range d.Chunks {
		if c.Text != string([]rune(d.Body)[c.Start:c.End]) {
			t.Fatal("locator changed")
		}
		for _, h := range c.Heading {
			if strings.Contains(h, "不是章节") {
				t.Fatal("code fence parsed as heading")
			}
		}
	}
}
func TestV5ParserRejectsDuplicateOrAliasYAML(t *testing.T) {
	p, _ := filepath.Abs("../plugins/commonmark/parse.py")
	for _, body := range []string{"---\na: 1\na: 2\n---\nhi", "---\na: &x 1\nb: *x\n---\nhi"} {
		_, e := adapters.ParseDocument(ctx, []string{"python3", p}, "demo", "a.md", body, "ed_a", "manual", 1200)
		code(t, e, "PARSER_FAILED")
	}
}
func TestV5ParserSourceHashDoesNotRebuildEveryScan(t *testing.T) {
	s, _ := setup(t)
	p, _ := filepath.Abs("../plugins/commonmark/parse.py")
	root := t.TempDir()
	must(t, os.WriteFile(filepath.Join(root, "a.md"), []byte("# Café\nCafe\u0301 original"), 0600))
	must(t, s.AddSource(ctx, store.Source{KB: "demo", ID: "dir", Kind: "path", Root: root, Mount: "docs"}))
	b := &build.Builder{Store: s, Config: adapters.Config{ParserCommand: []string{"python3", p}, ParserRevision: "test"}}
	for i := 0; i < 2; i++ {
		j := claim(t, s, "build", map[string]any{"mode": "incremental"})
		b.Execute(ctx, j)
		job, e := s.Job(ctx, j.ID)
		must(t, e)
		if job.State != "succeeded" {
			t.Fatal(job.Error)
		}
		if i == 1 {
			var result map[string]any
			must(t, json.Unmarshal(job.Result, &result))
			if result["changed"] != float64(0) || result["unchanged"] != float64(1) {
				t.Fatal(result)
			}
		}
	}
}
func TestV5DictionaryPersistsWithEdition(t *testing.T) {
	s, _ := setup(t)
	b := &build.Builder{Store: s}
	setopts(t, s, store.KBSettings{Dictionary: []string{"知识工程"}})
	root := t.TempDir()
	must(t, os.WriteFile(filepath.Join(root, "a.md"), []byte("# 知识工程\n领域术语知识工程用于资料建设"), 0600))
	must(t, s.AddSource(ctx, store.Source{KB: "demo", ID: "dir", Kind: "path", Root: root}))
	j := claim(t, s, "build", map[string]any{})
	b.Execute(ctx, j)
	r := run(t, b.Engine(), "search.keyword", core.Request{Q: "知识工程"})
	if r.Returned == 0 {
		t.Fatal(core.JSON(r))
	}
	d, _, e := s.Current(ctx, "demo", "a.md")
	must(t, e)
	if len(d.Dictionary) != 1 {
		t.Fatal(d.Dictionary)
	}
}
func TestV5CronFieldsAndTimezone(t *testing.T) {
	c, e := core.ParseCron("*/15 9-10 * * 1-5", "Asia/Shanghai")
	must(t, e)
	for _, tt := range []struct {
		iso  string
		want bool
	}{{"2026-10-12T01:15:00Z", true}, {"2026-10-12T01:16:00Z", false}, {"2026-10-11T01:15:00Z", false}} {
		tm, e := time.Parse(time.RFC3339, tt.iso)
		must(t, e)
		if c.Matches(tm) != tt.want {
			t.Fatal(tt)
		}
	}
	_, e = core.ParseCron("* * * * * *", "UTC")
	code(t, e, "INVALID_ARGUMENT")
}
func TestV5LinuxNativeWatchActuallyDetectsWrite(t *testing.T) {
	if !watch.Available() {
		t.Skip("native events unavailable on this OS")
	}
	root := t.TempDir()
	m, e := watch.New(root)
	must(t, e)
	defer m.Close()
	must(t, os.WriteFile(filepath.Join(root, "a.md"), []byte("event"), 0600))
	dirty, e := m.Changed()
	must(t, e)
	if !dirty {
		t.Fatal("no filesystem event")
	}
	dirty, e = m.Changed()
	must(t, e)
	if dirty {
		t.Fatal("events not drained")
	}
}
func TestV5CronSchedulesIntoSameQueue(t *testing.T) {
	s, _ := setup(t)
	root := t.TempDir()
	must(t, os.WriteFile(filepath.Join(root, "a.md"), []byte("# cron\ntext"), 0600))
	src := store.Source{KB: "demo", ID: "clock", Kind: "path", Root: root}
	must(t, s.AddSource(ctx, src))
	must(t, s.SetPolicy(ctx, "demo", "clock", store.SourcePolicy{Schedule: "15 9 * * *", Timezone: "Asia/Shanghai"}))
	c := automation.Controller{Builder: &build.Builder{Store: s}}
	tm, _ := time.Parse(time.RFC3339, "2026-10-12T01:14:00Z")
	must(t, c.Tick(ctx, tm))
	_, ok, e := s.Claim(ctx)
	must(t, e)
	if ok {
		t.Fatal("unexpected early job")
	}
	must(t, c.Tick(ctx, tm.Add(time.Minute)))
	_, ok, e = s.Claim(ctx)
	must(t, e)
	if !ok {
		t.Fatal("cron did not queue")
	}
}
func TestV5NativeWatchRequiresFallbackSchedule(t *testing.T) {
	s, _ := setup(t)
	must(t, s.AddSource(ctx, store.Source{KB: "demo", ID: "watch", Kind: "path", Root: t.TempDir()}))
	must(t, s.SetPolicy(ctx, "demo", "watch", store.SourcePolicy{NativeWatch: true}))
	p, e := s.Policy(ctx, "demo", "watch")
	must(t, e)
	if p.EverySeconds == 0 {
		t.Fatal("no rescan fallback")
	}
}
func TestV5S3ConditionalWritesAndConflict(t *testing.T) {
	body := ""
	etag := "\"v1\""
	writes := 0
	reject := false
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			t.Error("unsigned S3 request")
		}
		switch r.Method {
		case "GET":
			if body == "" {
				w.WriteHeader(404)
				return
			}
			w.Header().Set("ETag", etag)
			io.WriteString(w, body)
		case "PUT":
			if body == "" && r.Header.Get("If-None-Match") != "*" {
				t.Error("missing create precondition")
			}
			if body != "" && r.Header.Get("If-Match") != etag {
				t.Error("missing etag precondition")
			}
			if reject {
				w.WriteHeader(412)
				return
			}
			raw, _ := io.ReadAll(r.Body)
			body = string(raw)
			writes++
			etag = "\"v2\""
			w.Header().Set("ETag", etag)
		default:
			w.WriteHeader(405)
		}
	}))
	defer ts.Close()
	t.Setenv("S3_TEST_A", "example")
	t.Setenv("S3_TEST_S", "secret")
	dest := store.SyncTarget{Kind: "s3", Root: ts.URL, Remote: &store.RemoteSource{Bucket: "bucket", Region: "us-east-1", AccessKey: "env:S3_TEST_A", SecretKey: "env:S3_TEST_S"}}
	_, e := source.WriteBack(ctx, dest, "prefix/a.md", "# A\n正文", "")
	must(t, e)
	_, e = source.WriteBack(ctx, dest, "prefix/a.md", "# A\n正文", "")
	must(t, e)
	if writes != 1 {
		t.Fatal("not idempotent")
	}
	old := body
	_, e = source.WriteBack(ctx, dest, "prefix/a.md", "# B\n更新", core.TextHash(old))
	must(t, e)
	reject = true
	_, e = source.WriteBack(ctx, dest, "prefix/a.md", "# C", core.TextHash(body))
	code(t, e, "SYNC_CONFLICT")
}
func TestV5GitPushToActualBareRemote(t *testing.T) {
	root := t.TempDir()
	remote := filepath.Join(t.TempDir(), "remote.git")
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		b, e := cmd.CombinedOutput()
		if e != nil {
			t.Fatal(string(b), e)
		}
		return strings.TrimSpace(string(b))
	}
	git("init", "-b", "main")
	git("config", "user.email", "fixture@example.invalid")
	git("config", "user.name", "XRAG test")
	must(t, os.WriteFile(filepath.Join(root, "a.md"), []byte("original"), 0600))
	git("add", "a.md")
	git("commit", "-m", "initial")
	c := exec.Command("git", "init", "--bare", remote)
	raw, e := c.CombinedOutput()
	if e != nil {
		t.Fatal(string(raw), e)
	}
	dest := store.SyncTarget{Kind: "git", Root: root, Branch: "xrag/knowledge", Push: true, RemoteURL: remote}
	commit, e := source.WriteBack(ctx, dest, "knowledge/a.md", "# synced", "")
	must(t, e)
	must(t, source.PushGit(ctx, dest, commit, ""))
	must(t, source.PushGit(ctx, dest, commit, ""))
	if git("symbolic-ref", "--short", "HEAD") != "main" {
		t.Fatal("changed branch")
	}
	rr, e := exec.Command("git", "--git-dir", remote, "rev-parse", "refs/heads/xrag/knowledge").Output()
	must(t, e)
	if strings.TrimSpace(string(rr)) != commit {
		t.Fatal("remote not updated")
	}
	commit2, e := source.WriteBack(ctx, dest, "knowledge/a.md", "# second", core.TextHash("# synced"))
	must(t, e)
	code(t, source.PushGit(ctx, dest, commit2, ""), "SYNC_CONFLICT")
	must(t, source.PushGit(ctx, dest, commit2, commit))
}
func TestV5AutoSyncOnlyAfterPublishedRevision(t *testing.T) {
	s, _ := setup(t)
	b := &build.Builder{Store: s}
	root := t.TempDir()
	target, e := s.AddSyncTarget(ctx, store.SyncTarget{ID: "out", KB: "demo", Kind: "path", Root: root, AutoSync: true})
	must(t, e)
	d := pub(t, s, "original.md", "# original\n正文")
	k, e := s.SaveKnowledge(ctx, store.Knowledge{KB: "demo", Title: "case", Body: "body", Refs: []string{core.ChunkItem(d.Chunks[0], s.Origin).Ref}})
	must(t, e)
	j, ok, e := s.Claim(ctx)
	must(t, e)
	if ok {
		t.Fatal("draft triggered writeback", j)
	}
	j = claim(t, s, "publish", map[string]any{"id": k.ID, "revision": k.Draft})
	b.Execute(ctx, j)
	next, ok, e := s.Claim(ctx)
	must(t, e)
	if !ok || next.Kind != "sync" {
		t.Fatal("publication did not enqueue sync", next)
	}
	b.Execute(ctx, next)
	raw, e := os.ReadFile(filepath.Join(root, target.Prefix, "demo", k.ID+".md"))
	must(t, e)
	if !strings.Contains(string(raw), "body") {
		t.Fatal(string(raw))
	}
	must(t, s.DispatchAutoSync(ctx))
	_, ok, e = s.Claim(ctx)
	must(t, e)
	if ok {
		t.Fatal("duplicate sync")
	}
}
func TestV5RemotePruneFailureKeepsLocalEdition(t *testing.T) {
	s, _ := setup(t)
	d := pub(t, s, "a.md", "# old\nold")
	pub(t, s, "a.md", "# new\nnew")
	ageRetired(t, s)
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	defer remote.Close()
	binding := store.ProjectionBinding{Neo4j: &adapters.NeoConfig{URL: remote.URL, Database: "neo4j"}}
	must(t, s.BindProjections(ctx, d, binding))
	b := &build.Builder{Store: s}
	_, e := b.PruneExternal(ctx, 30, true, false)
	if e == nil {
		t.Fatal("missing remote failure")
	}
	r, _ := core.DecodeRef(core.ChunkItem(d.Chunks[0], s.Origin).Ref)
	_, _, _, e = s.Read(ctx, r)
	must(t, e)
}
func TestV5RemotePruneDeletesOnlyRecordedEdition(t *testing.T) {
	s, _ := setup(t)
	d := pub(t, s, "a.md", "# old\nold")
	pub(t, s, "a.md", "# new\nnew")
	ageRetired(t, s)
	calls := 0
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(raw), d.Edition) && !strings.Contains(string(raw), "NOT (e)--()") {
			t.Error("cleanup not scoped by edition")
		}
		calls++
		io.WriteString(w, `{"data":{"fields":[],"values":[]},"bookmarks":[]}`)
	}))
	defer remote.Close()
	must(t, s.BindProjections(ctx, d, store.ProjectionBinding{Neo4j: &adapters.NeoConfig{URL: remote.URL, Database: "neo4j"}}))
	b := &build.Builder{Store: s}
	p, e := b.PruneExternal(ctx, 30, true, false)
	must(t, e)
	if p.RemovedEditions != 1 || calls < 1 {
		t.Fatal(p, calls)
	}
}
func TestV5WikiUsesSemanticEvidenceWithoutLexicalHit(t *testing.T) {
	s, _ := setup(t)
	model, n := embed5(t)
	chat := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(raw), "原始材料不包含查询词") {
			t.Error("missing semantic evidence")
		}
		io.WriteString(w, `{"choices":[{"message":{"content":"TEST_FIXTURE 主题资料 [1]。"}}],"usage":{"prompt_tokens":5,"completion_tokens":5}}`)
	}))
	defer chat.Close()
	b := &build.Builder{Store: s, Config: adapters.Config{Embedding: &adapters.Endpoint{URL: model.URL, Model: "m1", Dimension: 2}, Chat: &adapters.Endpoint{URL: chat.URL, Model: "fixture"}}}
	root := t.TempDir()
	must(t, os.WriteFile(filepath.Join(root, "a.md"), []byte("# 原始材料\n原始材料不包含查询词"), 0600))
	must(t, s.AddSource(ctx, store.Source{KB: "demo", ID: "src", Kind: "path", Root: root}))
	j := claim(t, s, "build", map[string]any{})
	b.Execute(ctx, j)
	j = claim(t, s, "wiki", build.WikiRequest{Topic: "zzunmatchedzz"})
	b.Execute(ctx, j)
	job, e := s.Job(ctx, j.ID)
	must(t, e)
	if job.State != "succeeded" {
		t.Fatal(job.Error)
	}
	if n.Load() < 2 {
		t.Fatal("no query embedding")
	}
}
