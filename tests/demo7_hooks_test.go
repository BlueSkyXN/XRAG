package tests

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"github.com/BlueSkyXN/XRAG/core"
	"github.com/BlueSkyXN/XRAG/internal/build"
	"github.com/BlueSkyXN/XRAG/internal/server"
	db "github.com/BlueSkyXN/XRAG/internal/sqlite"
	"github.com/BlueSkyXN/XRAG/internal/store"
)

const nativeSecret7 = "synthetic-hook-secret-at-least-32-characters"

func nativeApp7(t *testing.T, provider string) (*store.Store, http.Handler) {
	t.Helper()
	s, _ := setup(t)
	root := gitrepo(t)
	must(t, s.AddSource(ctx, store.Source{KB: "demo", ID: "git", Kind: "git", Root: root, Ref: "main"}))
	t.Setenv("NATIVE_HOOK_SECRET", nativeSecret7)
	must(t, s.SetPolicy(ctx, "demo", "git", store.SourcePolicy{HookProvider: provider, HookRepository: "group/project", HookRef: "refs/heads/main", HookSecretRef: "env:NATIVE_HOOK_SECRET"}))
	return s, server.New(s, &build.Builder{Store: s}, nil)
}
func nativeRequest7(a http.Handler, provider, event, delivery, raw string, mutate func(*http.Request)) (int, map[string]any) {
	req := httptest.NewRequest("POST", "/v1/hooks/demo/git", strings.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	mac := hmac.New(sha256.New, []byte(nativeSecret7))
	mac.Write([]byte(raw))
	sig := hex.EncodeToString(mac.Sum(nil))
	switch provider {
	case "github":
		req.Header.Set("X-GitHub-Event", event)
		req.Header.Set("X-GitHub-Delivery", delivery)
		req.Header.Set("X-Hub-Signature-256", "sha256="+sig)
	case "gitea":
		req.Header.Set("X-Gitea-Event", event)
		req.Header.Set("X-Gitea-Delivery", delivery)
		req.Header.Set("X-Gitea-Signature", sig)
	case "gitlab":
		req.Header.Set("X-Gitlab-Event", event)
		req.Header.Set("Idempotency-Key", delivery)
		req.Header.Set("X-Gitlab-Token", nativeSecret7)
	}
	if mutate != nil {
		mutate(req)
	}
	w := httptest.NewRecorder()
	a.ServeHTTP(w, req)
	out := map[string]any{}
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}
func nativeJobs7(t *testing.T, s *store.Store) int {
	n := 0
	must(t, s.DB.Do(ctx, func(c *db.Conn) error {
		r, e := c.Query("SELECT COUNT(*) n FROM job")
		if e == nil {
			n = int(db.Int(r[0], "n"))
		}
		return e
	}))
	return n
}

const nativeBody7 = `{"ref":"refs/heads/main","repository":{"full_name":"group/project","clone_url":"https://ignored.example/evil"},"commits":[{"removed":["unchanged.md"],"message":"not executed"}]}`

func TestV7GitHubRawSignatureQueuesOnlyRegisteredSource(t *testing.T) {
	s, a := nativeApp7(t, "github")
	status, r := nativeRequest7(a, "github", "push", "delivery-1", nativeBody7, nil)
	if status != 202 || r["state"] != "queued" || nativeJobs7(t, s) != 1 {
		t.Fatal(status, r)
	}
	j, e := s.Job(ctx, r["id"].(string))
	must(t, e)
	if strings.Contains(string(j.Payload), "clone_url") || strings.Contains(string(j.Payload), "removed") || !strings.Contains(string(j.Payload), `"source":"git"`) {
		t.Fatal(string(j.Payload))
	}
}
func TestV7NativeDeliveryIdempotencyAndChangedBody(t *testing.T) {
	s, a := nativeApp7(t, "github")
	_, r := nativeRequest7(a, "github", "push", "same", nativeBody7, nil)
	st, r2 := nativeRequest7(a, "github", "push", "same", nativeBody7, nil)
	if st != 202 || r["id"] != r2["id"] || nativeJobs7(t, s) != 1 {
		t.Fatal(st, r2)
	}
	st, r2 = nativeRequest7(a, "github", "push", "same", nativeBody7+" ", nil)
	if st != 409 {
		t.Fatal(st, r2)
	}
}
func TestV7NativeBadSignatureCannotEnqueue(t *testing.T) {
	s, a := nativeApp7(t, "github")
	st, r := nativeRequest7(a, "github", "push", "bad", nativeBody7, func(r *http.Request) { r.Header.Set("X-Hub-Signature-256", "sha256="+strings.Repeat("0", 64)) })
	if st != 401 || nativeJobs7(t, s) != 0 {
		t.Fatal(st, r)
	}
}
func TestV7NativeNoAuthenticationDowngrade(t *testing.T) {
	s, a := nativeApp7(t, "github")
	st, r := nativeRequest7(a, "gitlab", "Push Hook", "bad", nativeBody7, nil)
	if st != 401 || nativeJobs7(t, s) != 0 {
		t.Fatal(st, r)
	}
}
func TestV7NativeRepositoryRestriction(t *testing.T) {
	s, a := nativeApp7(t, "github")
	st, r := nativeRequest7(a, "github", "push", "wrong", strings.ReplaceAll(nativeBody7, "group/project", "other/project"), nil)
	if st != 403 || nativeJobs7(t, s) != 0 {
		t.Fatal(st, r)
	}
}
func TestV7NativeBranchRestriction(t *testing.T) {
	s, a := nativeApp7(t, "github")
	st, r := nativeRequest7(a, "github", "push", "other", strings.ReplaceAll(nativeBody7, "refs/heads/main", "refs/heads/other"), nil)
	if st != 202 || r["ignored"] != true || nativeJobs7(t, s) != 0 {
		t.Fatal(st, r)
	}
}
func TestV7NativePingAndOtherEventsDoNotBuild(t *testing.T) {
	s, a := nativeApp7(t, "github")
	for _, event := range []string{"ping", "issues"} {
		st, r := nativeRequest7(a, "github", event, event, nativeBody7, nil)
		if st >= 300 || r["effect"] != "none" {
			t.Fatal(st, r)
		}
	}
	if nativeJobs7(t, s) != 0 {
		t.Fatal("event unexpectedly built")
	}
}
func TestV7NativeRequiresDeliveryAndJSON(t *testing.T) {
	s, a := nativeApp7(t, "github")
	for _, arg := range [][2]string{{"", nativeBody7}, {"bad-json", "{"}} {
		st, r := nativeRequest7(a, "github", "push", arg[0], arg[1], nil)
		if st != 400 {
			t.Fatal(st, r)
		}
	}
	if nativeJobs7(t, s) != 0 {
		t.Fatal("invalid request queued")
	}
}
func TestV7GiteaUsesUnprefixedSignature(t *testing.T) {
	s, a := nativeApp7(t, "gitea")
	st, r := nativeRequest7(a, "gitea", "push", "valid", nativeBody7, nil)
	if st != 202 || nativeJobs7(t, s) != 1 {
		t.Fatal(st, r)
	}
	st, _ = nativeRequest7(a, "gitea", "push", "invalid", nativeBody7, func(r *http.Request) { r.Header.Set("X-Gitea-Signature", "sha256="+r.Header.Get("X-Gitea-Signature")) })
	if st != 401 {
		t.Fatal(st)
	}
}
func TestV7GitLabTokenEventAndFallbackDelivery(t *testing.T) {
	s, a := nativeApp7(t, "gitlab")
	raw := `{"object_kind":"push","ref":"refs/heads/main","project":{"path_with_namespace":"group/project"}}`
	st, r := nativeRequest7(a, "gitlab", "Push Hook", "one", raw, func(r *http.Request) { r.Header.Del("Idempotency-Key"); r.Header.Set("X-Gitlab-Webhook-UUID", "one") })
	if st != 202 || nativeJobs7(t, s) != 1 {
		t.Fatal(st, r)
	}
	st, r = nativeRequest7(a, "gitlab", "Push Hook", "two", strings.Replace(raw, `"push"`, `"tag_push"`, 1), nil)
	if st != 400 {
		t.Fatal(st, r)
	}
}
func TestV7GitLabInvalidToken(t *testing.T) {
	s, a := nativeApp7(t, "gitlab")
	st, r := nativeRequest7(a, "gitlab", "Push Hook", "one", nativeBody7, func(r *http.Request) { r.Header.Set("X-Gitlab-Token", "wrong") })
	if st != 401 || nativeJobs7(t, s) != 0 {
		t.Fatal(st, r)
	}
}
func TestV7NativePolicyRejectsUnscopedAndNonGit(t *testing.T) {
	s, _ := setup(t)
	must(t, s.AddSource(ctx, store.Source{KB: "demo", ID: "path", Root: t.TempDir(), Kind: "path", Mount: "path"}))
	e := s.SetPolicy(ctx, "demo", "path", store.SourcePolicy{HookProvider: "github", HookSecretRef: "env:X", HookRepository: "group/repo", HookRef: "refs/heads/main"})
	code(t, e, "MODE_UNSUPPORTED")
	must(t, s.AddSource(ctx, store.Source{KB: "demo", ID: "git", Root: gitrepo(t), Kind: "git", Ref: "main", Mount: "git"}))
	for _, p := range []store.SourcePolicy{{HookProvider: "github"}, {HookProvider: "github", HookRepository: "../repo", HookRef: "main", HookSecretRef: "env:X"}, {HookProvider: "custom", HookRepository: "group/repo"}, {HookProvider: "guess"}} {
		if e := s.SetPolicy(ctx, "demo", "git", p); e == nil {
			t.Fatal(core.JSON(p))
		}
	}
}
