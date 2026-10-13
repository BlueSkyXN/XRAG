package tests

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"github.com/BlueSkyXN/XRAG/adapters"
	"github.com/BlueSkyXN/XRAG/core"
	"github.com/BlueSkyXN/XRAG/internal/build"
	db "github.com/BlueSkyXN/XRAG/internal/sqlite"
	"github.com/BlueSkyXN/XRAG/internal/store"
)

var ctx = context.Background()

func must(t *testing.T, e error) {
	t.Helper()
	if e != nil {
		t.Fatal(e)
	}
}
func code(t *testing.T, e error, want string) {
	t.Helper()
	if e == nil || core.AsError(e).Code != want {
		t.Fatalf("want %s, got %v", want, e)
	}
}
func setup(t *testing.T) (*store.Store, *core.Engine) {
	t.Helper()
	s, e := store.Open(t.TempDir())
	must(t, e)
	t.Cleanup(func() { s.Close() })
	must(t, s.CreateKB(ctx, "demo", "Demo"))
	return s, &core.Engine{Repo: s, Mode: "server"}
}
func parsed(t *testing.T, p, body string) core.Document {
	t.Helper()
	d, e := core.Parse("demo", p, body, core.ID("ed_"), "manual")
	must(t, e)
	d.Supports = core.ExplicitSupports(d)
	return d
}
func pub(t *testing.T, s *store.Store, p, body string) core.Document {
	t.Helper()
	d := parsed(t, p, body)
	must(t, s.Stage(ctx, d))
	must(t, s.Publish(ctx, d, store.Job{}, "", ""))
	return d
}
func run(t *testing.T, e *core.Engine, a string, q core.Request) core.Response {
	t.Helper()
	if q.Kbs == nil {
		q.Kbs = []string{"demo"}
	}
	r, x := e.Run(ctx, a, q, "tester")
	must(t, x)
	return r
}
func TestNormalizeAndUTF8(t *testing.T) {
	s, e := core.Normalize([]byte("\ufeffa\r\n中\rb"))
	must(t, e)
	if s != "a\n中\nb" {
		t.Fatal(s)
	}
	_, e = core.Normalize([]byte{255})
	code(t, e, "INVALID_ENCODING")
}
func TestParserRuneLocations(t *testing.T) {
	body := "---\nproduct: A\n---\n# 开始\n中文🙂原文\n## 后续\n日志文本\n"
	d := parsed(t, "doc.md", body)
	for _, c := range d.Chunks {
		if c.Text != string([]rune(body)[c.Start:c.End]) {
			t.Fatal("incorrect rune locator")
		}
	}
}
func TestContentReuseStillRefreshesEditionAndFields(t *testing.T) {
	a := parsed(t, "x.md", "---\nproduct: A\n---\n# 标题\n日志原文\n")
	b := parsed(t, "x.md", "---\nproduct: B\nextra: new\n---\n# 标题\n日志原文\n")
	if a.Chunks[0].Key != b.Chunks[0].Key || a.Chunks[0].ID == b.Chunks[0].ID || a.FileHash == b.FileHash || a.Chunks[0].LineStart == b.Chunks[0].LineStart {
		t.Fatal("identity separation failed")
	}
}
func TestPathBoundaryAndTraversal(t *testing.T) {
	if core.PathMatch("docs-old/a.md", "docs/") {
		t.Fatal("prefix widened")
	}
	for _, p := range []string{"../x", "/tmp/x", "a//b", "a/../b"} {
		if _, e := core.CleanPath(p); e == nil {
			t.Fatal(p)
		}
	}
}
func TestFTSChineseTwoCharactersAndIdentifierBoundary(t *testing.T) {
	s, e := setup(t)
	pub(t, s, "a.md", "# 日志\nE203 日志故障\n")
	pub(t, s, "b.md", "# 错误\nE2030 网络故障\n")
	r := run(t, e, "search.keyword", core.Request{Q: "E203"})
	if *r.Total.Value != 1 || r.Items[0].Path != "a.md" {
		t.Fatal(r)
	}
	r = run(t, e, "search.keyword", core.Request{Q: "日志"})
	if *r.Total.Value != 1 {
		t.Fatal(r)
	}
}
func TestNoMatchIsSuccessfulEmpty(t *testing.T) {
	s, e := setup(t)
	pub(t, s, "a.md", "# 苹果\n正常\n")
	r := run(t, e, "search.keyword", core.Request{Q: "不存在词"})
	if r.Total.Value == nil || *r.Total.Value != 0 || r.Coverage.ExecutionPartial {
		t.Fatal(r)
	}
}
func TestStagingNotVisibleToTextTreeGraphOrRead(t *testing.T) {
	s, e := setup(t)
	pub(t, s, "a.md", "# old\n旧原文\n")
	d := parsed(t, "a.md", "# 新版\n[[A]] --uses--> [[B]]\n新标记\n")
	must(t, s.Stage(ctx, d))
	r := run(t, e, "search.keyword", core.Request{Q: "新标记"})
	if r.Returned != 0 {
		t.Fatal(r)
	}
	r = run(t, e, "graph.entity", core.Request{Name: "A"})
	if r.Returned != 0 {
		t.Fatal(r)
	}
	_, _, _, x := s.Read(ctx, decode(t, core.ChunkItem(d.Chunks[0], s.Origin).Ref))
	code(t, x, "REF_GONE")
}
func decode(t *testing.T, r string) core.Ref { v, e := core.DecodeRef(r); must(t, e); return v }
func TestRetiredReferenceReadsOldVersion(t *testing.T) {
	s, e := setup(t)
	old := pub(t, s, "a.md", "# 版本\n旧正文\n")
	ref := core.ChunkItem(old.Chunks[0], s.Origin).Ref
	pub(t, s, "a.md", "# 版本\n新正文\n")
	r := run(t, e, "files.read", core.Request{Ref: ref})
	d := r.Data.(core.ReadData)
	if !strings.Contains(d.Text, "旧正文") || d.Freshness != "historical" {
		t.Fatal(d)
	}
}
func TestOrdinaryRemovalKeepsHistoricalReference(t *testing.T) {
	s, e := setup(t)
	d := pub(t, s, "a.md", "# 归档\n保留引用\n")
	must(t, s.RemoveMissing(ctx, store.Source{KB: "demo", ID: "manual"}, map[string]bool{}, store.Job{}))
	r := run(t, e, "files.tree", core.Request{})
	if r.Returned != 0 {
		t.Fatal(r)
	}
	r = run(t, e, "files.read", core.Request{Ref: core.ChunkItem(d.Chunks[0], s.Origin).Ref})
	if r.Data.(core.ReadData).Freshness != "historical" {
		t.Fatal(r)
	}
}
func TestWithdrawInvalidatesReferenceAndSavedPage(t *testing.T) {
	s, e := setup(t)
	d := pub(t, s, "a.md", "# 日志\n日志\n")
	r := run(t, e, "search.keyword", core.Request{Q: "日志"})
	must(t, s.Withdraw(ctx, "demo", "a.md"))
	_, x := e.Run(ctx, "files.read", core.Request{Kbs: []string{"demo"}, Ref: core.ChunkItem(d.Chunks[0], s.Origin).Ref}, "tester")
	code(t, x, "REF_WITHDRAWN")
	_, x = e.Run(ctx, "results.page", core.Request{Kbs: []string{"demo"}, ResultID: r.ResultID}, "tester")
	code(t, x, "VIEW_INVALIDATED")
}
func TestPagination37WithoutRerunning(t *testing.T) {
	s, e := setup(t)
	var b strings.Builder
	for i := 0; i < 37; i++ {
		fmt.Fprintf(&b, "## 第%d项\n分页样本记录 %d\n", i, i)
	}
	pub(t, s, "pages.md", b.String())
	r := run(t, e, "search.keyword", core.Request{Q: "分页样本记录", Limit: 10})
	id := r.QueryID
	seen := map[string]bool{}
	sizes := []int{}
	for {
		sizes = append(sizes, r.Returned)
		if r.Total.Value == nil || *r.Total.Value != 37 || r.QueryID != id {
			t.Fatal(r)
		}
		for _, it := range r.Items {
			if seen[it.ChunkID] {
				t.Fatal("duplicate")
			}
			seen[it.ChunkID] = true
		}
		if r.NextCursor == "" {
			break
		}
		r = run(t, e, "results.page", core.Request{ResultID: r.ResultID, Cursor: r.NextCursor, Limit: 10})
	}
	if fmt.Sprint(sizes) != "[10 10 10 7]" || len(seen) != 37 {
		t.Fatal(sizes)
	}
}
func TestDisplayBudgetDoesNotChangeExactCount(t *testing.T) {
	s, e := setup(t)
	pub(t, s, "a.md", "# 日志\n日志正文内容很多\n## 第二节\n日志正文二\n")
	r := run(t, e, "search.keyword", core.Request{Q: "日志", MaxChars: 5})
	if *r.Total.Value != 2 || !r.Coverage.DisplayTruncated || r.Coverage.ExecutionPartial {
		t.Fatal(r)
	}
}
func TestCursorDoesNotCrossResultSets(t *testing.T) {
	s, e := setup(t)
	pub(t, s, "a.md", "# 日志\n日志\n## 二\n日志\n")
	a := run(t, e, "search.keyword", core.Request{Q: "日志", Limit: 1})
	b := run(t, e, "search.keyword", core.Request{Q: "日志"})
	_, x := e.Run(ctx, "results.page", core.Request{Kbs: []string{"demo"}, ResultID: b.ResultID, Cursor: a.NextCursor}, "tester")
	code(t, x, "HANDLE_SCOPE_MISMATCH")
}
func TestWithinCannotUseOtherFileGraphSupport(t *testing.T) {
	s, e := setup(t)
	pub(t, s, "F1.md", "# 对象\n[[A]] 与 [[B]] 只被提及。\n")
	pub(t, s, "F2.md", "# 关系\n[[A]] --uses--> [[B]]\n")
	scope := run(t, e, "files.tree", core.Request{Filters: []core.Filter{{Field: "path", Op: "eq", Value: "F1.md"}}})
	all := run(t, e, "graph.neighbors", core.Request{Name: "A", Direction: "out"})
	if all.Returned != 1 {
		t.Fatal(all)
	}
	r := run(t, e, "graph.neighbors", core.Request{Name: "A", Direction: "out", Within: scope.Handle})
	if r.Returned != 0 {
		t.Fatal("scope leaked", r)
	}
}
func TestScopeExpandIsExplicit(t *testing.T) {
	s, e := setup(t)
	pub(t, s, "F1.md", "# 对象\n[[A]] 与 [[B]]。\n")
	pub(t, s, "F2.md", "# 关系\n[[A]] --uses--> [[B]]\n")
	a := run(t, e, "files.tree", core.Request{Filters: []core.Filter{{Field: "path", Op: "eq", Value: "F1.md"}}})
	b := run(t, e, "scope.expand", core.Request{Within: a.Handle})
	r := run(t, e, "graph.neighbors", core.Request{Name: "A", Direction: "out", Within: b.Handle})
	if r.Returned != 1 || b.Coverage.ScopeBasis != "expanded" {
		t.Fatal(r)
	}
}
func TestGraphPathPreservesDirectionAndCanReadResult(t *testing.T) {
	s, e := setup(t)
	pub(t, s, "g.md", "# 关系\n[[A]] --uses--> [[B]]\n[[B]] --records--> [[C]]\n")
	r := run(t, e, "graph.path", core.Request{FromID: "A", ToID: "C", Direction: "out", MaxHops: 3})
	if r.Returned != 1 || len(r.Items[0].Edges) != 2 || r.Items[0].Edges[0].From != "A" {
		t.Fatal(r)
	}
	read := run(t, e, "files.read", core.Request{Ref: r.Items[0].Ref})
	if read.Data == nil {
		t.Fatal(read)
	}
	rev := run(t, e, "graph.path", core.Request{FromID: "C", ToID: "A", Direction: "out", MaxHops: 3})
	if rev.Returned != 0 {
		t.Fatal("reversed")
	}
}
func TestExplicitSupportIdempotence(t *testing.T) {
	d := parsed(t, "g.md", "# 关系\n[[A]] --uses--> [[B]]\n[[A]] --uses--> [[B]]\n")
	if len(d.Supports) != 1 {
		t.Fatal(d.Supports)
	}
}
func TestOrdinaryProseIsNotFakeExtracted(t *testing.T) {
	d := parsed(t, "g.md", "# 说明\nA 不导致 B。A 使用了 B。\n")
	if len(d.Supports) != 0 {
		t.Fatal(d.Supports)
	}
}
func TestKnowledgeDraftHiddenUntilExplicitPublish(t *testing.T) {
	s, e := setup(t)
	base := pub(t, s, "base.md", "# 来源\n来源内容\n")
	ref := core.ChunkItem(base.Chunks[0], s.Origin).Ref
	k, x := s.SaveKnowledge(ctx, store.Knowledge{KB: "demo", Title: "经验", Body: "独特草稿标记", Refs: []string{ref}})
	must(t, x)
	r := run(t, e, "search.keyword", core.Request{Q: "独特草稿标记"})
	if r.Returned != 0 {
		t.Fatal(r)
	}
	_, x = e.Run(ctx, "files.read", core.Request{Kbs: []string{"demo"}, Path: "_knowledge/" + k.ID + ".md"}, "tester")
	code(t, x, "REF_GONE")
	b := build.Builder{Store: s}
	j := claim(t, s, "publish", map[string]any{"id": k.ID, "revision": k.Draft})
	res, x := b.RunPublish(ctx, j)
	must(t, x)
	must(t, s.Finish(ctx, j, res, nil))
	r = run(t, e, "search.keyword", core.Request{Q: "独特草稿标记"})
	if r.Returned != 1 {
		t.Fatal(r)
	}
}
func TestPublishedKnowledgeKeepsOldBodyWhileEditing(t *testing.T) {
	s, e := setup(t)
	base := pub(t, s, "base.md", "# source\n原始材料\n")
	k, x := s.SaveKnowledge(ctx, store.Knowledge{KB: "demo", Title: "经验", Body: "发布正文标记", Refs: []string{core.ChunkItem(base.Chunks[0], s.Origin).Ref}})
	must(t, x)
	b := build.Builder{Store: s}
	j := claim(t, s, "publish", map[string]any{"id": k.ID, "revision": k.Draft})
	res, x := b.RunPublish(ctx, j)
	must(t, x)
	must(t, s.Finish(ctx, j, res, nil))
	oldrev := k.Draft
	k.Expected = k.Draft
	k.Body = "未发布改稿标记"
	_, x = s.SaveKnowledge(ctx, k)
	must(t, x)
	r := run(t, e, "search.keyword", core.Request{Q: "发布正文标记"})
	if r.Returned != 1 {
		t.Fatal(r)
	}
	r = run(t, e, "search.keyword", core.Request{Q: "未发布改稿标记"})
	if r.Returned != 0 {
		t.Fatal(r)
	}
	j = claim(t, s, "publish", map[string]any{"id": k.ID, "revision": oldrev})
	_, x = b.RunPublish(ctx, j)
	code(t, x, "VERSION_CONFLICT")
}
func TestWithdrawBlocksPublishedKnowledge(t *testing.T) {
	s, e := setup(t)
	base := pub(t, s, "base.md", "# 来源\n内容\n")
	k, x := s.SaveKnowledge(ctx, store.Knowledge{KB: "demo", Title: "经验", Body: "派生条目标记", Refs: []string{core.ChunkItem(base.Chunks[0], s.Origin).Ref}})
	must(t, x)
	b := build.Builder{Store: s}
	j := claim(t, s, "publish", map[string]any{"id": k.ID, "revision": k.Draft})
	res, x := b.RunPublish(ctx, j)
	must(t, x)
	must(t, s.Finish(ctx, j, res, nil))
	must(t, s.Withdraw(ctx, "demo", "base.md"))
	r := run(t, e, "search.keyword", core.Request{Q: "派生条目标记"})
	if r.Returned != 0 {
		t.Fatal(r)
	}
	k, x = s.Knowledge(ctx, k.ID, "")
	must(t, x)
	if k.State != "blocked" {
		t.Fatal(k.State)
	}
}
func claim(t *testing.T, s *store.Store, kind string, payload any) store.Job {
	t.Helper()
	_, x := s.Enqueue(ctx, "demo", kind, "tester", core.ID("id_"), payload)
	must(t, x)
	j, ok, x := s.Claim(ctx)
	must(t, x)
	if !ok {
		t.Fatal("no claim")
	}
	return j
}
func TestJobsIdempotentAndFingerprintConflict(t *testing.T) {
	s, _ := setup(t)
	a, x := s.Enqueue(ctx, "demo", "build", "tester", "same", map[string]any{"mode": "incremental"})
	must(t, x)
	b, x := s.Enqueue(ctx, "demo", "build", "tester", "same", map[string]any{"mode": "incremental"})
	must(t, x)
	if a.ID != b.ID {
		t.Fatal("duplicate job")
	}
	_, x = s.Enqueue(ctx, "demo", "build", "tester", "same", map[string]any{"mode": "full"})
	code(t, x, "IDEMPOTENCY_CONFLICT")
}
func TestOneRunningPerKBAndQueueDoesNotConsumeExecution(t *testing.T) {
	s, _ := setup(t)
	j := claim(t, s, "build", map[string]any{})
	_, x := s.Enqueue(ctx, "demo", "wiki", "tester", "next", map[string]any{"topic": "x"})
	must(t, x)
	_, ok, x := s.Claim(ctx)
	must(t, x)
	if ok {
		t.Fatal("two running")
	}
	if j.Deadline-j.Started != 600 || j.Started == 0 {
		t.Fatal(j)
	}
	must(t, s.Finish(ctx, j, nil, nil))
	_, ok, x = s.Claim(ctx)
	must(t, x)
	if !ok {
		t.Fatal("queued not resumed")
	}
}
func TestCancelStopsPublication(t *testing.T) {
	s, _ := setup(t)
	j := claim(t, s, "build", map[string]any{})
	d := parsed(t, "x.md", "text")
	must(t, s.Stage(ctx, d))
	must(t, s.Cancel(ctx, j.ID))
	code(t, s.Publish(ctx, d, j, "", ""), "JOB_FENCE_LOST")
}
func TestExpiredLeaseMarksUnknownUsage(t *testing.T) {
	s, _ := setup(t)
	j := claim(t, s, "build", map[string]any{})
	id, x := s.UsageStart(ctx, j, "fixture-model")
	must(t, x)
	must(t, s.DB.Do(ctx, func(c *db.Conn) error { return c.Exec("UPDATE job SET lease=0 WHERE id=?", j.ID) }))
	_, _, x = s.Claim(ctx)
	must(t, x)
	must(t, s.DB.Do(ctx, func(c *db.Conn) error {
		r, x := c.Query("SELECT state FROM call_usage WHERE id=?", id)
		if x == nil && db.String(r[0], "state") != "outcome_unknown" {
			t.Fatal(r)
		}
		return x
	}))
	code(t, s.Renew(ctx, j), "JOB_FENCE_LOST")
}
func TestScopeAndOwnerNotInterchangeable(t *testing.T) {
	s, e := setup(t)
	pub(t, s, "x.md", "日志")
	r := run(t, e, "search.keyword", core.Request{Q: "日志"})
	_, x := e.Run(ctx, "results.page", core.Request{Kbs: []string{"demo"}, ResultID: r.ResultID}, "other")
	code(t, x, "PERMISSION_DENIED")
	key, x := s.CreateKey(ctx, "read", "reader", []string{"demo"})
	must(t, x)
	p, x := s.Authenticate(ctx, key)
	must(t, x)
	must(t, s.CreateKB(ctx, "private", "private"))
	_, x = s.Scope(ctx, p, []string{"private"})
	code(t, x, "PERMISSION_DENIED")
}
func TestUnknownFilterRejected(t *testing.T) {
	s, e := setup(t)
	pub(t, s, "x.md", "日志")
	_, x := e.Run(ctx, "search.keyword", core.Request{Kbs: []string{"demo"}, Q: "日志", Filters: []core.Filter{{Field: "fields.typo", Op: "eq", Value: "a"}}}, "tester")
	code(t, x, "FILTER_UNSUPPORTED")
}
func TestSemanticUnavailableIsNotFake(t *testing.T) {
	s, e := setup(t)
	pub(t, s, "x.md", "日志")
	_, x := e.Run(ctx, "search.semantic", core.Request{Kbs: []string{"demo"}, Q: "日志"}, "tester")
	code(t, x, "CAPABILITY_UNAVAILABLE")
	r := run(t, e, "+ask", core.Request{Q: "日志"})
	if r.Returned != 1 || r.Total.Value != nil {
		t.Fatal(r)
	}
}
func TestSourceInventoryNoPartialDeletion(t *testing.T) {
	s, e := setup(t)
	root := t.TempDir()
	must(t, os.WriteFile(filepath.Join(root, "a.md"), []byte("日志甲"), 0600))
	must(t, s.AddSource(ctx, store.Source{KB: "demo", ID: "src", Root: root, Mount: "docs", Kind: "path"}))
	b := build.Builder{Store: s}
	j := claim(t, s, "build", map[string]any{})
	res, x := b.RunBuild(ctx, j)
	must(t, x)
	must(t, s.Finish(ctx, j, res, nil))
	must(t, os.Rename(root, root+".away"))
	j = claim(t, s, "build", map[string]any{})
	_, x = b.RunBuild(ctx, j)
	if x == nil {
		t.Fatal("missing source succeeded")
	}
	r := run(t, e, "search.keyword", core.Request{Q: "日志甲"})
	if r.Returned != 1 {
		t.Fatal("lost published file")
	}
	must(t, os.Rename(root+".away", root))
}
func TestSourceScanOnlyChangedFileDoesNotDeleteOthers(t *testing.T) {
	s, e := setup(t)
	root := t.TempDir()
	for _, n := range []string{"a", "b", "c"} {
		must(t, os.WriteFile(filepath.Join(root, n+".md"), []byte("日志"+n), 0600))
	}
	must(t, s.AddSource(ctx, store.Source{KB: "demo", ID: "src", Root: root, Mount: "docs", Kind: "path"}))
	b := build.Builder{Store: s}
	for i := 0; i < 2; i++ {
		if i == 1 {
			must(t, os.WriteFile(filepath.Join(root, "a.md"), []byte("日志a 新内容"), 0600))
		}
		j := claim(t, s, "build", map[string]any{})
		res, x := b.RunBuild(ctx, j)
		must(t, x)
		must(t, s.Finish(ctx, j, res, nil))
	}
	r := run(t, e, "files.tree", core.Request{})
	if r.Returned != 3 {
		t.Fatal(r)
	}
}
func TestLocalReadChangedFileRejectsOldReference(t *testing.T) {
	root := t.TempDir()
	must(t, os.WriteFile(filepath.Join(root, "a.md"), []byte("# 资料\n日志原文\n"), 0600))
	l := &adapters.Local{Root: root, Origin: "local-test", KB: "demo", CacheDir: t.TempDir()}
	e := &core.Engine{Repo: l, Mode: "local"}
	r := run(t, e, "search.keyword", core.Request{Q: "日志"})
	must(t, os.WriteFile(filepath.Join(root, "a.md"), []byte("# 改变\n日志新版\n"), 0600))
	_, x := e.Run(ctx, "files.read", core.Request{Kbs: []string{"demo"}, Ref: r.Items[0].Ref}, "tester")
	code(t, x, "SCOPE_CHANGED")
	_, x = e.Run(ctx, "search.keyword", core.Request{Kbs: []string{"demo"}, Q: "日志", Within: r.Handle}, "tester")
	code(t, x, "SCOPE_CHANGED")
}
func TestLocalHasNoPersistentDatabase(t *testing.T) {
	root := t.TempDir()
	must(t, os.WriteFile(filepath.Join(root, "a.md"), []byte("日志"), 0600))
	cache := t.TempDir()
	e := &core.Engine{Repo: &adapters.Local{Root: root, Origin: "o", KB: "demo", CacheDir: cache}, Mode: "local"}
	run(t, e, "search.keyword", core.Request{Q: "日志"})
	for _, dir := range []string{root, cache} {
		must(t, filepath.WalkDir(dir, func(p string, d os.DirEntry, x error) error {
			if strings.HasSuffix(p, "sqlite") || strings.HasSuffix(p, ".db") {
				t.Fatal(p)
			}
			return x
		}))
	}
}
func TestBackupRestoresPublishedData(t *testing.T) {
	s, _ := setup(t)
	pub(t, s, "x.md", "日志备份")
	dir := t.TempDir()
	must(t, s.Backup(ctx, filepath.Join(dir, "xrag.sqlite")))
	r, x := store.Open(dir)
	must(t, x)
	defer r.Close()
	result := run(t, &core.Engine{Repo: r, Mode: "server"}, "search.keyword", core.Request{Q: "日志备份"})
	if result.Returned != 1 || r.Origin != s.Origin {
		t.Fatal(result)
	}
}
func TestExpiredHandleNeverFallsBackToCorpus(t *testing.T) {
	s, e := setup(t)
	pub(t, s, "x.md", "日志")
	r := run(t, e, "search.keyword", core.Request{Q: "日志"})
	must(t, s.DB.Do(ctx, func(c *db.Conn) error {
		return c.Exec("UPDATE cache SET expires=? WHERE id=?", time.Now().Unix()-1, r.Handle)
	}))
	_, x := e.Run(ctx, "search.keyword", core.Request{Kbs: []string{"demo"}, Q: "日志", Within: r.Handle}, "tester")
	if x == nil {
		t.Fatal("expired scope fell back")
	}
}

func TestConcurrentStoreClaimsHaveOneRunningPerKB(t *testing.T) {
	dir := t.TempDir()
	a, x := store.Open(dir)
	must(t, x)
	defer a.Close()
	b, x := store.Open(dir)
	must(t, x)
	defer b.Close()
	must(t, a.CreateKB(ctx, "demo", "Demo"))
	_, x = a.Enqueue(ctx, "demo", "build", "owner", "a", map[string]any{})
	must(t, x)
	_, x = a.Enqueue(ctx, "demo", "wiki", "owner", "b", map[string]any{"topic": "x"})
	must(t, x)
	type result struct {
		ok  bool
		err error
	}
	ch := make(chan result, 2)
	for _, s := range []*store.Store{a, b} {
		go func(s *store.Store) { _, ok, e := s.Claim(ctx); ch <- result{ok, e} }(s)
	}
	count := 0
	for i := 0; i < 2; i++ {
		r := <-ch
		must(t, r.err)
		if r.ok {
			count++
		}
	}
	if count != 1 {
		t.Fatal("multiple concurrent owners", count)
	}
}
