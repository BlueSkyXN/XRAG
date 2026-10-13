package tests

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/BlueSkyXN/XRAG/adapters"
	"github.com/BlueSkyXN/XRAG/core"
	"github.com/BlueSkyXN/XRAG/internal/build"
	"github.com/BlueSkyXN/XRAG/internal/cli"
	"github.com/BlueSkyXN/XRAG/internal/maintenance"
	"github.com/BlueSkyXN/XRAG/internal/server"
	db "github.com/BlueSkyXN/XRAG/internal/sqlite"
	"github.com/BlueSkyXN/XRAG/internal/store"
)

type productTest struct {
	s        *store.Store
	app      *server.App
	url, key string
}

func productSetup(t *testing.T) productTest {
	t.Helper()
	s, _ := setup(t)
	key, e := s.CreateKey(ctx, "initial admin", "admin", nil)
	must(t, e)
	a := server.New(s, &build.Builder{Store: s}, nil)
	ts := httptest.NewServer(a)
	t.Cleanup(ts.Close)
	return productTest{s, a, ts.URL, key}
}
func (h productTest) request(t *testing.T, method, path string, v any, token string) (int, []byte) {
	t.Helper()
	var body io.Reader
	if v != nil {
		body = strings.NewReader(core.JSON(v))
	}
	req, e := http.NewRequest(method, h.url+path, body)
	must(t, e)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, e := http.DefaultClient.Do(req)
	must(t, e)
	defer res.Body.Close()
	b, e := io.ReadAll(res.Body)
	must(t, e)
	return res.StatusCode, b
}
func object(t *testing.T, b []byte) map[string]any {
	t.Helper()
	v := map[string]any{}
	must(t, json.Unmarshal(b, &v))
	return v
}
func TestProductKeyListingDoesNotExposeTokensOrHashes(t *testing.T) {
	h := productSetup(t)
	k, token, e := h.s.CreateManagedKey(ctx, "Agent", "reader", []string{"demo"}, time.Now().Add(time.Hour).Unix())
	must(t, e)
	n, b := h.request(t, "GET", "/v1/keys", nil, h.key)
	if n != 200 || strings.Contains(string(b), token) || strings.Contains(string(b), `"hash"`) {
		t.Fatal(n, string(b))
	}
	if !strings.Contains(string(b), k.ID) {
		t.Fatal(string(b))
	}
	n, _ = h.request(t, "GET", "/v1/keys", nil, token)
	if n != 403 {
		t.Fatal(n)
	}
}
func TestProductExpiredKeyRejected(t *testing.T) {
	h := productSetup(t)
	k, token, e := h.s.CreateManagedKey(ctx, "Temp", "reader", []string{"demo"}, time.Now().Add(time.Hour).Unix())
	must(t, e)
	must(t, h.s.DB.Do(ctx, func(c *db.Conn) error {
		return c.Exec("UPDATE key_options SET expires_at=? WHERE key_id=?", time.Now().Unix()-1, k.ID)
	}))
	_, e = h.s.Authenticate(ctx, token)
	code(t, e, "UNAUTHENTICATED")
	xs, e := h.s.Keys(ctx)
	must(t, e)
	for _, x := range xs {
		if x.ID == k.ID && x.State != "expired" {
			t.Fatal(x)
		}
	}
}
func TestProductKeysValidateScopeAndLifetime(t *testing.T) {
	h := productSetup(t)
	_, _, e := h.s.CreateManagedKey(ctx, "bad", "reader", []string{"missing"}, 0)
	code(t, e, "INVALID_ARGUMENT")
	_, _, e = h.s.CreateManagedKey(ctx, "bad", "reader", nil, 0)
	code(t, e, "INVALID_ARGUMENT")
	_, _, e = h.s.CreateManagedKey(ctx, "bad", "admin", nil, time.Now().Unix()-1)
	code(t, e, "INVALID_ARGUMENT")
}
func TestProductLastPermanentAdminPreserved(t *testing.T) {
	h := productSetup(t)
	p, e := h.s.Authenticate(ctx, h.key)
	must(t, e)
	code(t, h.s.DisableKey(ctx, p.ID), "LAST_ADMIN")
	_, _, e = h.s.CreateManagedKey(ctx, "expires", "admin", nil, time.Now().Add(time.Hour).Unix())
	must(t, e)
	code(t, h.s.DisableKey(ctx, p.ID), "LAST_ADMIN")
	_, _, e = h.s.CreateManagedKey(ctx, "second permanent", "admin", nil, 0)
	must(t, e)
	must(t, h.s.DisableKey(ctx, p.ID))
	_, e = h.s.Authenticate(ctx, h.key)
	code(t, e, "UNAUTHENTICATED")
}
func TestProductRevokedKeyInvalidatesExistingSession(t *testing.T) {
	h := productSetup(t)
	k, token, e := h.s.CreateManagedKey(ctx, "Reader", "reader", []string{"demo"}, 0)
	must(t, e)
	req, _ := http.NewRequest("POST", h.url+"/v1/session", strings.NewReader(core.JSON(map[string]any{"key": token})))
	req.Header.Set("Content-Type", "application/json")
	r, e := http.DefaultClient.Do(req)
	must(t, e)
	cookie := r.Cookies()[0]
	r.Body.Close()
	must(t, h.s.DisableKey(ctx, k.ID))
	req, _ = http.NewRequest("GET", h.url+"/v1/system", nil)
	req.AddCookie(cookie)
	r, e = http.DefaultClient.Do(req)
	must(t, e)
	r.Body.Close()
	if r.StatusCode != 401 {
		t.Fatal(r.StatusCode)
	}
}
func TestProductLogoutRemovesSession(t *testing.T) {
	h := productSetup(t)
	req, _ := http.NewRequest("POST", h.url+"/v1/session", strings.NewReader(core.JSON(map[string]any{"key": h.key})))
	req.Header.Set("Content-Type", "application/json")
	r, e := http.DefaultClient.Do(req)
	must(t, e)
	cookie := r.Cookies()[0]
	r.Body.Close()
	req, _ = http.NewRequest("DELETE", h.url+"/v1/session", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	r, e = http.DefaultClient.Do(req)
	must(t, e)
	r.Body.Close()
	if r.StatusCode != 200 {
		t.Fatal(r.StatusCode)
	}
	req, _ = http.NewRequest("GET", h.url+"/v1/system", nil)
	req.AddCookie(cookie)
	r, e = http.DefaultClient.Do(req)
	must(t, e)
	r.Body.Close()
	if r.StatusCode != 401 {
		t.Fatal(r.StatusCode)
	}
}
func TestProductConfigSaveIsDeferredAndCAS(t *testing.T) {
	h := productSetup(t)
	config := map[string]any{"chat": map[string]any{"url": "http://127.0.0.1:1/v1", "model": "fixture-not-contacted", "key": "env:UNSET_FIXTURE_KEY"}}
	n, b := h.request(t, "PUT", "/v1/settings/providers", map[string]any{"expected_revision": 0, "config": config}, h.key)
	if n != 200 {
		t.Fatal(n, string(b))
	}
	if h.app.Builder.Config.Chat != nil || object(t, b)["model_calls"] != float64(0) {
		t.Fatal("saving performed a live change")
	}
	n, b = h.request(t, "GET", "/v1/settings/providers", nil, h.key)
	if n != 200 || object(t, b)["restart_required"] != true {
		t.Fatal(n, string(b))
	}
	n, _ = h.request(t, "PUT", "/v1/settings/providers", map[string]any{"expected_revision": 0, "config": map[string]any{}}, h.key)
	if n != 409 {
		t.Fatal(n)
	}
	st, e := h.s.ProviderSetting(ctx)
	must(t, e)
	cfg, e := adapters.ParseConfig(st.Data)
	must(t, e)
	if cfg.Chat == nil || cfg.Chat.Model != "fixture-not-contacted" {
		t.Fatal(cfg)
	}
}
func TestProductConfigRejectsPlaintextAndInvalidJSON(t *testing.T) {
	h := productSetup(t)
	for _, cfg := range []any{map[string]any{"chat": map[string]any{"url": "http://localhost/v1", "model": "x", "key": "actual-secret"}}, map[string]any{"qdrant": map[string]any{"url": "http://localhost:6333"}}, map[string]any{"unknown": true}, nil} {
		n, b := h.request(t, "PUT", "/v1/settings/providers", map[string]any{"expected_revision": 0, "config": cfg}, h.key)
		if n < 400 {
			t.Fatal(n, string(b))
		}
	}
	_, e := adapters.ParseConfig([]byte(`{} {}`))
	code(t, e, "INVALID_ARGUMENT")
}
func TestProductExternalConfigRemainsExplicit(t *testing.T) {
	h := productSetup(t)
	h.app.ConfigSource = "external_file"
	h.app.Builder.Config.Chat = &adapters.Endpoint{URL: "http://localhost/v1", Model: "m", Key: "DO_NOT_LEAK"}
	n, b := h.request(t, "GET", "/v1/settings/providers", nil, h.key)
	if n != 200 || strings.Contains(string(b), "DO_NOT_LEAK") || object(t, b)["editable"] != false {
		t.Fatal(n, string(b))
	}
	n, _ = h.request(t, "PUT", "/v1/settings/providers", map[string]any{"expected_revision": 0, "config": map[string]any{}}, h.key)
	if n != 409 {
		t.Fatal(n)
	}
}
func TestProductFeedbackPaginationAndOwnerScope(t *testing.T) {
	h := productSetup(t)
	d := pub(t, h.s, "a.md", "原文反馈")
	ref := core.ChunkItem(d.Chunks[0], h.s.Origin).Ref
	_, token, e := h.s.CreateManagedKey(ctx, "Contributor", "contributor", []string{"demo"}, 0)
	must(t, e)
	p, e := h.s.Authenticate(ctx, token)
	must(t, e)
	for i := 0; i < 3; i++ {
		must(t, h.s.Feedback(ctx, "demo", ref, "wrong", "说明", p.ID))
	}
	must(t, h.s.Feedback(ctx, "demo", ref, "useful", "另一个提交者", "other"))
	page, e := h.s.Feedbacks(ctx, p, []string{"demo"}, "", 2, 0)
	must(t, e)
	if page.Total != 3 || len(page.Items) != 2 || page.NextOffset == nil || *page.NextOffset != 2 {
		t.Fatal(page)
	}
	next, e := h.s.Feedbacks(ctx, p, []string{"demo"}, "", 2, 2)
	must(t, e)
	if len(next.Items) != 1 || next.NextOffset != nil {
		t.Fatal(next)
	}
	n, b := h.request(t, "GET", "/v1/feedback?kb=demo&limit=2", nil, h.key)
	if n != 200 || object(t, b)["total"] != float64(4) {
		t.Fatal(n, string(b))
	}
}
func TestProductFeedbackResolutionRevisionAndRole(t *testing.T) {
	h := productSetup(t)
	d := pub(t, h.s, "a.md", "反馈")
	must(t, h.s.Feedback(ctx, "demo", core.ChunkItem(d.Chunks[0], h.s.Origin).Ref, "wrong", "问题", "owner"))
	p, e := h.s.Authenticate(ctx, h.key)
	must(t, e)
	page, e := h.s.Feedbacks(ctx, p, nil, "", 10, 0)
	must(t, e)
	id := page.Items[0].ID
	must(t, h.s.ResolveFeedback(ctx, p, id, "resolved", "补好了", 0))
	code(t, h.s.ResolveFeedback(ctx, p, id, "open", "旧修改", 0), "VERSION_CONFLICT")
	code(t, h.s.ResolveFeedback(ctx, store.Principal{ID: "reader", Role: "reader", KBs: []string{"demo"}}, id, "open", "", 1), "PERMISSION_DENIED")
	page, e = h.s.Feedbacks(ctx, p, nil, "resolved", 10, 0)
	must(t, e)
	if page.Items[0].Resolution != "补好了" {
		t.Fatal(page)
	}
}
func TestProductFeedbackWithdrawalDoesNotExposeSavedNote(t *testing.T) {
	h := productSetup(t)
	d := pub(t, h.s, "a.md", "原文")
	must(t, h.s.Feedback(ctx, "demo", core.ChunkItem(d.Chunks[0], h.s.Origin).Ref, "wrong", "withheld-feedback-body", "owner"))
	must(t, h.s.Withdraw(ctx, "demo", "a.md"))
	n, b := h.request(t, "GET", "/v1/feedback?kb=demo", nil, h.key)
	if n != 200 || strings.Contains(string(b), "withheld-feedback-body") || strings.Contains(string(b), `"ref"`) {
		t.Fatal(n, string(b))
	}
}
func TestProductPublishedExportExcludesDraftsAndWithdrawn(t *testing.T) {
	h := productSetup(t)
	d := pub(t, h.s, "docs/a.md", "公开原文")
	pub(t, h.s, "removed.md", "SECRET_WITHDRAWN")
	_, e := h.s.SaveKnowledge(ctx, store.Knowledge{KB: "demo", Title: "草稿", Kind: "case", Body: "SECRET_DRAFT", Refs: []string{core.ChunkItem(d.Chunks[0], h.s.Origin).Ref}})
	must(t, e)
	must(t, h.s.Withdraw(ctx, "demo", "removed.md"))
	n, b := h.request(t, "GET", "/v1/kbs/demo/export", nil, h.key)
	if n != 200 {
		t.Fatal(n, string(b))
	}
	z, e := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	must(t, e)
	names := []string{}
	for _, f := range z.File {
		names = append(names, f.Name)
		rc, e := f.Open()
		must(t, e)
		v, e := io.ReadAll(rc)
		rc.Close()
		must(t, e)
		if strings.Contains(string(v), "SECRET_") {
			t.Fatal(string(v))
		}
	}
	if len(names) != 2 || names[0] != "documents/docs/a.md" || names[1] != "manifest.json" {
		t.Fatal(names)
	}
}
func TestProductExportAndBackupRequireManagementRole(t *testing.T) {
	h := productSetup(t)
	_, key, e := h.s.CreateManagedKey(ctx, "reader", "reader", []string{"demo"}, 0)
	must(t, e)
	for _, path := range []string{"/v1/kbs/demo/export", "/v1/backups", "/v1/settings/providers", "/v1/maintenance/cache"} {
		n, _ := h.request(t, "GET", path, nil, key)
		if n != 403 {
			t.Fatal(path, n)
		}
	}
}
func TestProductBackupDownloadAndRemove(t *testing.T) {
	h := productSetup(t)
	pub(t, h.s, "a.md", "用于备份")
	n, _ := h.request(t, "POST", "/v1/backups", map[string]any{}, h.key)
	if n != 400 {
		t.Fatal(n)
	}
	n, b := h.request(t, "POST", "/v1/backups", map[string]any{"confirm": true}, h.key)
	if n != 201 {
		t.Fatal(n, string(b))
	}
	id := object(t, b)["id"].(string)
	n, b = h.request(t, "GET", "/v1/backups/"+id+"/download", nil, h.key)
	if n != 200 || !bytes.HasPrefix(b, []byte("SQLite format 3\x00")) {
		t.Fatal(n)
	}
	n, _ = h.request(t, "DELETE", "/v1/backups/"+id, map[string]any{"confirm": true}, h.key)
	if n != 200 {
		t.Fatal(n)
	}
	n, _ = h.request(t, "GET", "/v1/backups/"+id+"/download", nil, h.key)
	if n != 404 {
		t.Fatal(n)
	}
}
func TestProductRestoreKeepsRefsButDisablesOldOperation(t *testing.T) {
	h := productSetup(t)
	d := pub(t, h.s, "a.md", "恢复后的原文")
	ref := core.ChunkItem(d.Chunks[0], h.s.Origin).Ref
	source := t.TempDir()
	must(t, h.s.AddSource(ctx, store.Source{KB: "demo", ID: "notes", Kind: "path", Mount: "notes", Root: source}))
	must(t, h.s.SetPolicy(ctx, "demo", "notes", store.SourcePolicy{EverySeconds: 60}))
	_, e := h.s.SaveProviderSetting(ctx, 0, json.RawMessage(`{"chat":{"url":"http://localhost:1/v1","model":"not-called"}}`))
	must(t, e)
	job, e := h.s.Enqueue(ctx, "demo", "build", "owner", "restore-test", map[string]any{"mode": "incremental"})
	must(t, e)
	must(t, h.s.DB.Do(ctx, func(c *db.Conn) error {
		return c.Exec("INSERT INTO call_usage(id,job_id,kind,state,usage,created) VALUES('interrupted',?,'model','sent','{}',?)", job.ID, time.Now().Unix())
	}))
	backup, e := h.s.CreateBackup(ctx)
	must(t, e)
	_, path, e := h.s.BackupPath(ctx, backup.ID)
	must(t, e)
	dest := filepath.Join(t.TempDir(), "restored")
	result, e := maintenance.Restore(ctx, path, dest)
	must(t, e)
	if result["old_keys_disabled"] != true {
		t.Fatal(result)
	}
	restored, e := store.Open(dest)
	must(t, e)
	defer restored.Close()
	if restored.Origin != h.s.Origin {
		t.Fatal("origin changed")
	}
	r, e := core.DecodeRef(ref)
	must(t, e)
	got, _, _, e := restored.Read(ctx, r)
	must(t, e)
	if got.Body != d.Body {
		t.Fatal("body changed")
	}
	_, e = restored.Authenticate(ctx, h.key)
	code(t, e, "UNAUTHENTICATED")
	newkey, e := os.ReadFile(filepath.Join(dest, "admin.key"))
	must(t, e)
	_, e = restored.Authenticate(ctx, strings.TrimSpace(string(newkey)))
	must(t, e)
	pol, e := restored.Policy(ctx, "demo", "notes")
	must(t, e)
	if pol.EverySeconds != 0 {
		t.Fatal(pol)
	}
	cfg, e := restored.ProviderSetting(ctx)
	must(t, e)
	if string(cfg.Data) != "{}" {
		t.Fatal(cfg)
	}
	j, e := restored.Job(ctx, job.ID)
	must(t, e)
	if j.State != "failed" {
		t.Fatal(j)
	}
	must(t, restored.DB.Do(ctx, func(c *db.Conn) error {
		rows, e := c.Query("SELECT state FROM call_usage WHERE id='interrupted'")
		if e == nil && db.String(rows[0], "state") != "outcome_unknown" {
			t.Fatal(rows)
		}
		return e
	}))
}
func TestProductRestoreNeverOverwritesAndRejectsNonDatabase(t *testing.T) {
	h := productSetup(t)
	backup, e := h.s.CreateBackup(ctx)
	must(t, e)
	_, path, e := h.s.BackupPath(ctx, backup.ID)
	must(t, e)
	existing := t.TempDir()
	must(t, os.WriteFile(filepath.Join(existing, "keep"), []byte("keep"), 0600))
	_, e = maintenance.Restore(ctx, path, existing)
	code(t, e, "VERSION_CONFLICT")
	b, e := os.ReadFile(filepath.Join(existing, "keep"))
	must(t, e)
	if string(b) != "keep" {
		t.Fatal("changed")
	}
	invalid := filepath.Join(t.TempDir(), "not.sqlite")
	must(t, os.WriteFile(invalid, []byte("not a database"), 0600))
	_, e = maintenance.Restore(ctx, invalid, filepath.Join(t.TempDir(), "new"))
	code(t, e, "INVALID_BACKUP")
}
func TestProductCacheCleanupIsNarrow(t *testing.T) {
	h := productSetup(t)
	pub(t, h.s, "a.md", "保留历史")
	must(t, h.s.DB.Do(ctx, func(c *db.Conn) error {
		if e := c.Exec("INSERT INTO cache(id,kind,owner,expires,data) VALUES('expired','result','x',1,'{}')"); e != nil {
			return e
		}
		return c.Exec("INSERT INTO cache(id,kind,owner,expires,data) VALUES('live','result','x',?,'{}')", time.Now().Unix()+1000)
	}))
	counts, e := h.s.CleanupTransient(ctx, false)
	must(t, e)
	if counts["cache"] != 1 {
		t.Fatal(counts)
	}
	counts, e = h.s.CleanupTransient(ctx, true)
	must(t, e)
	if counts["cache"] != 1 {
		t.Fatal(counts)
	}
	must(t, h.s.DB.Do(ctx, func(c *db.Conn) error {
		r, e := c.Query("SELECT COUNT(*) n FROM edition")
		if e == nil && db.Int(r[0], "n") != 1 {
			t.Fatal(r)
		}
		return e
	}))
}
func TestProductOverviewUsesCurrentScope(t *testing.T) {
	h := productSetup(t)
	pub(t, h.s, "a.md", "内容")
	must(t, h.s.CreateKB(ctx, "other", "Other"))
	_, key, e := h.s.CreateManagedKey(ctx, "r", "reader", []string{"demo"}, 0)
	must(t, e)
	n, b := h.request(t, "GET", "/v1/overview", nil, key)
	if n != 200 || strings.Contains(string(b), "database_bytes") || strings.Contains(string(b), `"other"`) {
		t.Fatal(n, string(b))
	}
	if object(t, b)["counts"].(map[string]any)["files"] != float64(1) {
		t.Fatal(string(b))
	}
}
func TestProductProfileRenameKeepsIdentityAndRemoveIsExplicit(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "client.json")
	var out, errout bytes.Buffer
	must(t, cli.Main([]string{"profile", "add", "team", "--config", cfg, "--server", "http://localhost:8765"}, &out, &errout))
	old, e := cli.Load(cfg)
	must(t, e)
	id := old.Profiles["team"].ID
	must(t, cli.Main([]string{"profile", "rename", "team", "work", "--config", cfg}, &out, &errout))
	now, e := cli.Load(cfg)
	must(t, e)
	if now.Default != "work" || now.Profiles["work"].ID != id {
		t.Fatal(now)
	}
	code(t, cli.Main([]string{"profile", "remove", "work", "--config", cfg}, &out, &errout), "CONFIRM_REQUIRED")
	must(t, cli.Main([]string{"profile", "remove", "work", "--confirm", "--config", cfg}, &out, &errout))
}
func TestProductExplicitUnknownProfileDoesNotFallBack(t *testing.T) {
	var out, errout bytes.Buffer
	code(t, cli.Main([]string{"status", "--profile", "missing", "--config", filepath.Join(t.TempDir(), "none.json")}, &out, &errout), "NOT_FOUND")
}
func TestProductDownloadDoesNotOverwrite(t *testing.T) {
	h := productSetup(t)
	pub(t, h.s, "a.md", "发布")
	t.Setenv("TEST_KEY", h.key)
	p := cli.Profile{URL: h.url, Key: "env:TEST_KEY"}
	dest := filepath.Join(t.TempDir(), "out.zip")
	must(t, cli.Download(ctx, p, "/v1/kbs/demo/export", dest))
	before, e := os.ReadFile(dest)
	must(t, e)
	code(t, cli.Download(ctx, p, "/v1/kbs/demo/export", dest), "VERSION_CONFLICT")
	after, e := os.ReadFile(dest)
	must(t, e)
	if !bytes.Equal(before, after) {
		t.Fatal("overwritten")
	}
}

func TestProductCLIKeyJSONScopeDoesNotFallBackToProfile(t *testing.T) {
	h := productSetup(t)
	t.Setenv("XRAG_API_KEY", h.key)
	var out, errout bytes.Buffer
	// An explicit empty Reader scope must be rejected, not widened to default demo.
	e := cli.Main([]string{"keys", "create", "--server", h.url, "--config", filepath.Join(t.TempDir(), "client.json"), "--data", `{"name":"empty","role":"reader","kbs":[]}`}, &out, &errout)
	if e == nil {
		t.Fatal("explicit empty scope widened to default profile")
	}
}
