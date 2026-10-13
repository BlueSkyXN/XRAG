package tests

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
	"github.com/BlueSkyXN/XRAG/core"
	"github.com/BlueSkyXN/XRAG/evaluation"
	"github.com/BlueSkyXN/XRAG/internal/automation"
	"github.com/BlueSkyXN/XRAG/internal/build"
	"github.com/BlueSkyXN/XRAG/internal/server"
	"github.com/BlueSkyXN/XRAG/internal/source"
	"github.com/BlueSkyXN/XRAG/internal/store"
)

func gitcmd(t *testing.T, root string, args ...string) string {
	t.Helper()
	a := []string{"-C", root, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid"}
	c := exec.Command("git", append(a, args...)...)
	c.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
	b, e := c.CombinedOutput()
	if e != nil {
		t.Fatalf("git %v: %v %s", args, e, b)
	}
	return strings.TrimSpace(string(b))
}
func gitrepo(t *testing.T) string {
	t.Helper()
	r := t.TempDir()
	gitcmd(t, r, "init", "-b", "main")
	return r
}
func commit(t *testing.T, r string) string {
	t.Helper()
	gitcmd(t, r, "add", "--all")
	gitcmd(t, r, "commit", "-m", "fixture")
	return gitcmd(t, r, "rev-parse", "HEAD")
}
func buildNow(t *testing.T, s *store.Store, b *build.Builder) store.Job {
	t.Helper()
	j := claim(t, s, "build", map[string]any{"mode": "incremental"})
	out, e := b.RunBuild(ctx, j)
	must(t, e)
	must(t, s.Finish(ctx, j, out, nil))
	j, e = s.Job(ctx, j.ID)
	must(t, e)
	return j
}
func TestGitPinnedObjectsIgnoreUncommittedWorkingTree(t *testing.T) {
	r := gitrepo(t)
	must(t, os.WriteFile(filepath.Join(r, "a.md"), []byte("# committed\n正确提交正文"), 0600))
	rev := commit(t, r)
	must(t, os.WriteFile(filepath.Join(r, "a.md"), []byte("dirty not committed"), 0600))
	must(t, os.WriteFile(filepath.Join(r, "new.md"), []byte("untracked"), 0600))
	scan, e := source.Open(ctx, t.TempDir(), store.Source{KB: "demo", ID: "repo", Kind: "git", Root: r, Ref: "main"})
	must(t, e)
	if scan.Revision != rev || len(scan.Entries) != 1 || !strings.Contains(scan.Entries[0].Body, "正确提交正文") {
		t.Fatal(scan)
	}
	if gitcmd(t, r, "symbolic-ref", "--short", "HEAD") != "main" {
		t.Fatal("checkout changed")
	}
}
func TestGitChangeDeleteRenameKeepsUnchangedFilesAndOldRefs(t *testing.T) {
	s, eng := setup(t)
	r := gitrepo(t)
	for _, n := range []string{"a", "b", "c"} {
		must(t, os.WriteFile(filepath.Join(r, n+".md"), []byte("# 文档\n日志"+n), 0600))
	}
	commit(t, r)
	must(t, s.AddSource(ctx, store.Source{KB: "demo", ID: "repo", Root: r, Mount: "git", Kind: "git", Ref: "main"}))
	builder := &build.Builder{Store: s}
	buildNow(t, s, builder)
	old := run(t, eng, "search.keyword", core.Request{Q: "日志b"}).Items[0].Ref
	must(t, os.WriteFile(filepath.Join(r, "a.md"), []byte("# 更新\n日志a新版"), 0600))
	commit(t, r)
	buildNow(t, s, builder)
	if run(t, eng, "files.tree", core.Request{}).Returned != 3 {
		t.Fatal("delta deleted unchanged files")
	}
	must(t, os.Remove(filepath.Join(r, "b.md")))
	must(t, os.Rename(filepath.Join(r, "c.md"), filepath.Join(r, "renamed.md")))
	rev := commit(t, r)
	j := buildNow(t, s, builder)
	tree := run(t, eng, "files.tree", core.Request{})
	if tree.Returned != 2 {
		t.Fatal(tree)
	}
	if !strings.Contains(core.JSON(run(t, eng, "files.read", core.Request{Ref: old})), "historical") {
		t.Fatal("old source ref lost")
	}
	st, e := s.SourceState(ctx, "demo", "repo")
	must(t, e)
	if st.Revision != rev || st.Indexed == "" {
		t.Fatal(st)
	}
	details, e := s.JobDetails(ctx, j.ID)
	must(t, e)
	if !strings.Contains(core.JSON(details), "removed") {
		t.Fatal(details)
	}
}
func TestGitSymlinkAndSubmoduleNotReadAsDocuments(t *testing.T) {
	r := gitrepo(t)
	must(t, os.WriteFile(filepath.Join(r, "real.md"), []byte("hello"), 0600))
	must(t, os.Symlink("/etc/passwd", filepath.Join(r, "escape.md")))
	rev := commit(t, r)
	// A gitlink is a submodule tree entry; no clone or external network is needed.
	gitcmd(t, r, "update-index", "--add", "--cacheinfo", "160000,"+rev+",submodule.md")
	gitcmd(t, r, "commit", "-m", "gitlink fixture")
	scan, e := source.Open(ctx, t.TempDir(), store.Source{Kind: "git", Root: r, Ref: "HEAD"})
	must(t, e)
	if len(scan.Entries) != 1 || scan.Entries[0].Path != "real.md" {
		t.Fatal(scan)
	}
}
func TestGitInvalidRevisionPreservesPublishedFiles(t *testing.T) {
	s, eng := setup(t)
	r := gitrepo(t)
	must(t, os.WriteFile(filepath.Join(r, "a.md"), []byte("原文日志"), 0600))
	commit(t, r)
	must(t, s.AddSource(ctx, store.Source{KB: "demo", ID: "repo", Root: r, Kind: "git", Ref: "main"}))
	b := &build.Builder{Store: s}
	buildNow(t, s, b)
	gitcmd(t, r, "branch", "-m", "main", "other")
	j := claim(t, s, "build", map[string]any{})
	_, e := b.RunBuild(ctx, j)
	if e == nil {
		t.Fatal("missing branch accepted")
	}
	if run(t, eng, "files.tree", core.Request{}).Returned != 1 {
		t.Fatal("file removed on scan failure")
	}
}
func TestSourceRejectsUnsafeRefOrCredentialURL(t *testing.T) {
	s, _ := setup(t)
	for _, v := range []store.Source{{KB: "demo", ID: "repo", Kind: "git", Root: "https://user:pass@example.com/repo"}, {KB: "demo", ID: "repo", Kind: "git", Root: "https://example.com/repo", Ref: "--output=x"}, {KB: "demo", ID: "repo", Kind: "git", Root: "ext::sh malicious"}} {
		if e := s.AddSource(ctx, v); e == nil {
			t.Fatal(v)
		}
	}
}
func TestPathSameSizeSameMtimeStillDetectsContentChange(t *testing.T) {
	r := t.TempDir()
	p := filepath.Join(r, "a.md")
	must(t, os.WriteFile(p, []byte("aaa"), 0600))
	st, _ := os.Stat(p)
	a, e := source.Open(ctx, "", store.Source{Kind: "path", Root: r})
	must(t, e)
	must(t, os.WriteFile(p, []byte("bbb"), 0600))
	must(t, os.Chtimes(p, st.ModTime(), st.ModTime()))
	b, e := source.Open(ctx, "", store.Source{Kind: "path", Root: r})
	must(t, e)
	if a.Fingerprint == b.Fingerprint {
		t.Fatal("weak fingerprint")
	}
}
func TestWithdrawnFileDoesNotReappearOrMakeScansRetryForever(t *testing.T) {
	s, eng := setup(t)
	r := t.TempDir()
	must(t, os.WriteFile(filepath.Join(r, "a.md"), []byte("日志"), 0600))
	must(t, s.AddSource(ctx, store.Source{KB: "demo", ID: "src", Root: r, Kind: "path"}))
	b := &build.Builder{Store: s}
	buildNow(t, s, b)
	must(t, s.Withdraw(ctx, "demo", "a.md"))
	j := buildNow(t, s, b)
	if j.State != "succeeded" || run(t, eng, "files.tree", core.Request{}).Returned != 0 {
		t.Fatal(j)
	}
}
func TestFullBuildCoalescesWithoutDroppingStrongerIntent(t *testing.T) {
	s, _ := setup(t)
	a, e := s.Trigger(ctx, "demo", "src", "hook", "system", "", nil)
	must(t, e)
	b, e := s.Enqueue(ctx, "demo", "build", "tester", "full", map[string]any{"mode": "full"})
	must(t, e)
	c, e := s.Trigger(ctx, "demo", "src", "schedule", "system", "", nil)
	must(t, e)
	if a.ID != b.ID || a.ID != c.ID {
		t.Fatal("not coalesced")
	}
	j, e := s.Job(ctx, a.ID)
	must(t, e)
	if !strings.Contains(string(j.Payload), `"mode":"full"`) {
		t.Fatal(j)
	}
}
func TestScheduleOnlyQueuesChangedSources(t *testing.T) {
	s, _ := setup(t)
	r := t.TempDir()
	must(t, os.WriteFile(filepath.Join(r, "a.md"), []byte("日志"), 0600))
	must(t, s.AddSource(ctx, store.Source{KB: "demo", ID: "src", Root: r, Kind: "path"}))
	b := &build.Builder{Store: s}
	buildNow(t, s, b)
	must(t, s.SetPolicy(ctx, "demo", "src", store.SourcePolicy{EverySeconds: 10}))
	c := &automation.Controller{Builder: b}
	now := time.Now()
	must(t, c.Tick(ctx, now))
	js, e := s.Jobs(ctx, []string{"demo"})
	must(t, e)
	if len(js) != 1 {
		t.Fatal("unchanged source queued")
	}
	must(t, os.WriteFile(filepath.Join(r, "a.md"), []byte("新日志"), 0600))
	must(t, c.Tick(ctx, now.Add(11*time.Second)))
	must(t, c.Tick(ctx, now.Add(22*time.Second)))
	js, e = s.Jobs(ctx, []string{"demo"})
	must(t, e)
	if len(js) != 2 {
		t.Fatal(js)
	}
}
func TestWatchPollingDebouncesAndCanBeDisabled(t *testing.T) {
	s, _ := setup(t)
	r := t.TempDir()
	must(t, os.WriteFile(filepath.Join(r, "a.md"), []byte("日志"), 0600))
	must(t, s.AddSource(ctx, store.Source{KB: "demo", ID: "src", Root: r, Kind: "path"}))
	b := &build.Builder{Store: s}
	must(t, s.SetPolicy(ctx, "demo", "src", store.SourcePolicy{WatchSeconds: 2, DebounceSeconds: 5}))
	c := &automation.Controller{Builder: b}
	now := time.Now()
	must(t, c.Tick(ctx, now))
	must(t, c.Tick(ctx, now.Add(2*time.Second)))
	js, _ := s.Jobs(ctx, []string{"demo"})
	if len(js) != 0 {
		t.Fatal("debounce bypassed")
	}
	must(t, c.Tick(ctx, now.Add(6*time.Second)))
	js, _ = s.Jobs(ctx, []string{"demo"})
	if len(js) != 1 {
		t.Fatal(js)
	}
	must(t, s.SetPolicy(ctx, "demo", "src", store.SourcePolicy{}))
	must(t, os.WriteFile(filepath.Join(r, "a.md"), []byte("again"), 0600))
	must(t, c.Tick(ctx, now.Add(20*time.Second)))
	js, _ = s.Jobs(ctx, []string{"demo"})
	if len(js) != 1 {
		t.Fatal("disabled policy still enqueues")
	}
}
func TestAutomationUnavailableSourceDoesNotRemoveOrQueue(t *testing.T) {
	s, e := setup(t)
	r := t.TempDir()
	must(t, os.WriteFile(filepath.Join(r, "a.md"), []byte("原日志"), 0600))
	must(t, s.AddSource(ctx, store.Source{KB: "demo", ID: "src", Root: r, Kind: "path"}))
	b := &build.Builder{Store: s}
	buildNow(t, s, b)
	must(t, s.SetPolicy(ctx, "demo", "src", store.SourcePolicy{EverySeconds: 10}))
	must(t, os.Rename(r, r+"-gone"))
	defer os.Rename(r+"-gone", r)
	must(t, (&automation.Controller{Builder: b}).Tick(ctx, time.Now()))
	if run(t, e, "files.tree", core.Request{}).Returned != 1 {
		t.Fatal("lost content")
	}
	st, x := s.SourceState(ctx, "demo", "src")
	must(t, x)
	if st.Error == "" {
		t.Fatal("missing status error")
	}
}
func TestSourcePolicyAndRevisionSurviveReopen(t *testing.T) {
	dir := t.TempDir()
	r := t.TempDir()
	s, e := store.Open(dir)
	must(t, e)
	must(t, s.CreateKB(ctx, "demo", "d"))
	must(t, s.AddSource(ctx, store.Source{KB: "demo", ID: "src", Root: r, Kind: "git", Ref: "main"}))
	must(t, s.SetPolicy(ctx, "demo", "src", store.SourcePolicy{EverySeconds: 30, HookSecretRef: "env:DEMO_HOOK"}))
	must(t, s.Close())
	s, e = store.Open(dir)
	must(t, e)
	defer s.Close()
	v, e := s.Source(ctx, "demo", "src")
	must(t, e)
	p, e := s.Policy(ctx, "demo", "src")
	must(t, e)
	if v.Ref != "main" || p.EverySeconds != 30 {
		t.Fatal(v, p)
	}
}
func TestSignedHookChecksTimestampSignatureAndEventIdempotency(t *testing.T) {
	s, _ := setup(t)
	must(t, s.AddSource(ctx, store.Source{KB: "demo", ID: "src", Root: t.TempDir(), Kind: "path"}))
	secret := strings.Repeat("a", 40)
	t.Setenv("XRAG_TEST_HOOK", secret)
	must(t, s.SetPolicy(ctx, "demo", "src", store.SourcePolicy{HookSecretRef: "env:XRAG_TEST_HOOK"}))
	ts := httptest.NewServer(server.New(s, &build.Builder{Store: s}, nil))
	defer ts.Close()
	call := func(event, body string, stamp int64, valid bool) (int, map[string]any) {
		t.Helper()
		st := strconv.FormatInt(stamp, 10)
		m := hmac.New(sha256.New, []byte(secret))
		m.Write([]byte(st + "\n" + event + "\n" + body))
		sig := hex.EncodeToString(m.Sum(nil))
		if !valid {
			sig = strings.Repeat("0", 64)
		}
		r, _ := http.NewRequest("POST", ts.URL+"/v1/hooks/demo/src", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-XRAG-Timestamp", st)
		r.Header.Set("X-XRAG-Event", event)
		r.Header.Set("X-XRAG-Signature", "sha256="+sig)
		res, e := http.DefaultClient.Do(r)
		must(t, e)
		defer res.Body.Close()
		var v map[string]any
		must(t, json.NewDecoder(res.Body).Decode(&v))
		return res.StatusCode, v
	}
	now := time.Now().Unix()
	status, _ := call("e1", `{}`, now, false)
	if status != 401 {
		t.Fatal(status)
	}
	status, _ = call("e1", `{}`, now-500, true)
	if status != 401 {
		t.Fatal(status)
	}
	status, a := call("e1", `{}`, now, true)
	if status != 202 {
		t.Fatal(a)
	}
	_, b := call("e1", `{}`, now, true)
	if a["id"] != b["id"] {
		t.Fatal("duplicate event creates new job")
	}
	status, _ = call("e1", `{"reason":"changed"}`, now, true)
	if status != 409 {
		t.Fatal("event changed payload accepted", status)
	}
}
func tinySet() evaluation.Set {
	return evaluation.Set{KB: "demo", Name: "small-fixture", Cases: []evaluation.Case{{ID: "hit", Question: "日志", Expected: []evaluation.Expected{{Path: "a.md", Anchor: "日志"}}}, {ID: "empty", Question: "XYZZNOTEXIST", ExpectedEmpty: true}}}
}
func TestEvalScoringDoesNotDoubleCountMultipleChunks(t *testing.T) {
	c := evaluation.Case{Expected: []evaluation.Expected{{Path: "a.md"}, {Path: "b.md"}}}
	s := evaluation.ScoreCase(c, []core.Item{{Path: "a.md"}, {Path: "a.md"}}, 2)
	if s.Recall != .5 || s.Hit != 1 || s.ReciprocalRank != 1 {
		t.Fatal(s)
	}
}
func TestEvalRequiresExplicitExpectations(t *testing.T) {
	s := tinySet()
	s.Cases = append(s.Cases, evaluation.Case{ID: "bad", Question: "x"})
	if evaluation.ValidateSet(s) == nil {
		t.Fatal("missing expected silently negative")
	}
}
func TestEvalRunsFrozenPairedStrategiesAndSavesRealMetrics(t *testing.T) {
	s, _ := setup(t)
	pub(t, s, "a.md", "# 日志\n日志样本")
	set, e := s.SaveEvalSet(ctx, tinySet())
	must(t, e)
	j := claim(t, s, "eval", evaluation.Request{Set: set, Strategies: []string{"keyword", "ask"}, K: 5})
	b := &build.Builder{Store: s}
	out, e := b.RunEval(ctx, j)
	must(t, e)
	r := out.(evaluation.Report)
	if len(r.Rows) != 4 || r.ManifestHash == "" || len(r.Summaries) != 2 || r.Summaries[0].RecallAtK != 1 || r.Summaries[0].NegativeAccuracy == nil || *r.Summaries[0].NegativeAccuracy != 1 {
		t.Fatal(r)
	}
	for _, row := range r.Rows {
		if row.QueryID == "" || len(row.Trace) == 0 {
			t.Fatal(row)
		}
		for _, it := range row.Items {
			if it.Text != "" {
				t.Fatal("full text in report")
			}
		}
	}
	must(t, s.Finish(ctx, j, out, nil))
	saved, e := s.Job(ctx, j.ID)
	must(t, e)
	if saved.State != "succeeded" {
		t.Fatal(saved)
	}
}
func TestEvalStaleExpectedAnchorFailsInsteadOfSkipping(t *testing.T) {
	s, _ := setup(t)
	pub(t, s, "a.md", "different")
	set, e := s.SaveEvalSet(ctx, tinySet())
	must(t, e)
	j := claim(t, s, "eval", evaluation.Request{Set: set, Strategies: []string{"keyword"}, K: 5})
	_, e = (&build.Builder{Store: s}).RunEval(ctx, j)
	code(t, e, "EVAL_DATASET_INVALID")
}
func TestEvalErrorRowsCountAsZeroAndMarkPartial(t *testing.T) {
	s, _ := setup(t)
	pub(t, s, "a.md", "日志")
	set, e := s.SaveEvalSet(ctx, tinySet())
	must(t, e)
	j := claim(t, s, "eval", evaluation.Request{Set: set, Strategies: []string{"semantic"}, K: 5})
	out, e := (&build.Builder{Store: s}).RunEval(ctx, j)
	must(t, e)
	r := out.(evaluation.Report)
	if r.Summaries[0].Errors != 2 || r.Summaries[0].HitAtK != 0 {
		t.Fatal(r)
	}
	must(t, s.Finish(ctx, j, out, nil))
	saved, e := s.Job(ctx, j.ID)
	must(t, e)
	if saved.State != "partial" {
		t.Fatal(saved)
	}
}
func TestQueryHistoryHasTraceNotBodyAndHonorsWithdrawal(t *testing.T) {
	s, _ := setup(t)
	pub(t, s, "a.md", "日志秘密正文")
	key, e := s.CreateKey(ctx, "reader", "reader", []string{"demo"})
	must(t, e)
	p, e := s.Authenticate(ctx, key)
	must(t, e)
	app := server.New(s, &build.Builder{Store: s}, nil)
	result, e := app.Execute(ctx, p, "search.keyword", []byte(`{"kbs":["demo"],"q":"日志"}`))
	must(t, e)
	r := result.(core.Response)
	if len(r.Trace) != 0 {
		t.Fatal("unsolicited trace")
	}
	v, e := s.Queries(ctx, p, r.QueryID)
	must(t, e)
	if len(v) != 1 || len(v[0].Response.Trace) == 0 || strings.Contains(core.JSON(v), "秘密正文") {
		t.Fatal(v)
	}
	must(t, s.Withdraw(ctx, "demo", "a.md"))
	_, e = s.Queries(ctx, p, r.QueryID)
	code(t, e, "RESULT_EXPIRED")
}
func TestNewManagementEndpointsRespectRolesAndPersist(t *testing.T) {
	s, _ := setup(t)
	admin, e := s.CreateKey(ctx, "a", "admin", nil)
	must(t, e)
	reader, e := s.CreateKey(ctx, "r", "reader", []string{"demo"})
	must(t, e)
	ts := httptest.NewServer(server.New(s, &build.Builder{Store: s}, nil))
	defer ts.Close()
	call := func(method, path string, payload any, key string) (int, []byte) {
		r, _ := http.NewRequest(method, ts.URL+path, strings.NewReader(core.JSON(payload)))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer "+key)
		v, e := http.DefaultClient.Do(r)
		must(t, e)
		defer v.Body.Close()
		b, e := io.ReadAll(v.Body)
		must(t, e)
		return v.StatusCode, b
	}
	n, _ := call("POST", "/v1/eval/sets", tinySet(), reader)
	if n != 403 {
		t.Fatal(n)
	}
	n, b := call("POST", "/v1/eval/sets", tinySet(), admin)
	if n != 201 {
		t.Fatal(n, string(b))
	}
	var set evaluation.Set
	must(t, json.Unmarshal(b, &set))
	n, b = call("POST", "/v1/eval/runs", map[string]any{"set_id": set.ID, "strategies": []string{"semantic"}}, admin)
	if n != 422 {
		t.Fatal(n, string(b))
	}
	r := t.TempDir()
	n, b = call("POST", "/v1/kbs/demo/sources", map[string]any{"id": "src", "kind": "path", "root": r, "mount": "docs"}, admin)
	if n != 201 {
		t.Fatal(n, string(b))
	}
	n, b = call("PUT", "/v1/kbs/demo/sources/src/policy", store.SourcePolicy{EverySeconds: 20}, admin)
	if n != 200 {
		t.Fatal(n, string(b))
	}
	n, b = call("GET", "/v1/kbs/demo/sources", nil, admin)
	if n != 200 || !strings.Contains(string(b), "every_seconds") {
		t.Fatal(n, string(b))
	}
	n, _ = call("GET", "/v1/kbs/demo/sources/missing/policy", nil, admin)
	if n != 404 {
		t.Fatal("missing source policy", n)
	}
}

func TestBuildDetailsExposeTheirOutputCap(t *testing.T) {
	s, _ := setup(t)
	j := store.Job{ID: "job_details", KB: "demo"}
	for i := 0; i < 1001; i++ {
		must(t, s.BuildDetail(ctx, j, "src", "a.md", "unchanged", "e1", "e1", "r1", nil))
	}
	r, e := s.JobDetails(ctx, j.ID)
	must(t, e)
	if !r["truncated"].(map[string]bool)["files"] {
		t.Fatal(r["truncated"])
	}
	var out struct {
		Files []any `json:"files"`
	}
	must(t, json.Unmarshal([]byte(core.JSON(r)), &out))
	if len(out.Files) != 1000 {
		t.Fatal(len(out.Files))
	}
}
