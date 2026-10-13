package tests

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"github.com/BlueSkyXN/XRAG/core"
	"github.com/BlueSkyXN/XRAG/internal/mcpauth"
)

func oauthClaims9(resource, issuer string) map[string]any {
	return map[string]any{"active": true, "sub": "alice", "iss": issuer, "aud": resource, "exp": time.Now().Unix() + 300, "scope": "xrag:read xrag:write xrag:research", "token_type": "Bearer"}
}
func oauthVerifier9(t *testing.T, mutate func(map[string]any)) (*mcpauth.Verifier, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	var issuer string
	t.Setenv("XRAG_TEST_INTROSPECTION_SECRET", "fixture-secret")
	h := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		user, pass, ok := r.BasicAuth()
		if !ok || user != "fixture-client" || pass != "fixture-secret" {
			t.Error("bad introspection credentials")
			w.WriteHeader(401)
			return
		}
		if r.Method != "POST" || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
			t.Error("wrong introspection request")
		}
		r.ParseForm()
		if r.Form.Get("token") != "opaque-token" || r.Form.Get("token_type_hint") != "access_token" {
			t.Error("unexpected token")
		}
		c := oauthClaims9("https://xrag.example/mcp", issuer)
		if mutate != nil {
			mutate(c)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(c)
	}))
	t.Cleanup(h.Close)
	issuer = h.URL
	v, e := mcpauth.New(mcpauth.Config{Resource: "https://xrag.example/mcp", Issuer: issuer, IntrospectionEndpoint: h.URL + "/introspect", ClientID: "fixture-client", ClientSecretRef: "env:XRAG_TEST_INTROSPECTION_SECRET", AllowLoopbackHTTP: true, Subjects: map[string]mcpauth.Subject{"alice": {Role: "reader", KBs: []string{"demo"}}}})
	must(t, e)
	return v, &calls
}
func TestDemo9OAuthIntrospectionBindsScopeIdentityAndAudience(t *testing.T) {
	v, calls := oauthVerifier9(t, nil)
	id, e := v.Verify(ctx, "opaque-token")
	must(t, e)
	if id.Subject.Role != "reader" || !id.Scopes["xrag:research"] || calls.Load() != 1 || !strings.HasPrefix(id.ID, "oauth_") {
		t.Fatal(id, calls.Load())
	}
	again, e := v.Verify(ctx, "opaque-token")
	must(t, e)
	if again.ID != id.ID || calls.Load() != 2 {
		t.Fatal("not stable or unexpected token cache")
	}
}
func TestDemo9OAuthRejectsClaimFailures(t *testing.T) {
	cases := map[string]func(map[string]any){
		"inactive": func(c map[string]any) { c["active"] = false }, "expired": func(c map[string]any) { c["exp"] = time.Now().Unix() - 1 },
		"issuer": func(c map[string]any) { c["iss"] = "https://other.invalid" }, "audience": func(c map[string]any) { c["aud"] = "https://other.invalid/mcp" },
		"missing_expiry": func(c map[string]any) { delete(c, "exp") }, "future": func(c map[string]any) { c["nbf"] = time.Now().Unix() + 90 },
		"unmapped_subject": func(c map[string]any) { c["sub"] = "mallory" }, "missing_aud": func(c map[string]any) { delete(c, "aud") },
		"not_access_token": func(c map[string]any) { c["token_type"] = "DPoP" },
	}
	for name, fn := range cases {
		t.Run(name, func(t *testing.T) {
			v, _ := oauthVerifier9(t, fn)
			_, e := v.Verify(ctx, "opaque-token")
			code(t, e, "UNAUTHENTICATED")
		})
	}
}
func TestDemo9OAuthAudienceArrayAcceptedOnlyForTarget(t *testing.T) {
	v, _ := oauthVerifier9(t, func(c map[string]any) { c["aud"] = []string{"another", "https://xrag.example/mcp"} })
	_, e := v.Verify(ctx, "opaque-token")
	must(t, e)
}
func TestDemo9OAuthRequiresReadScope(t *testing.T) {
	v, _ := oauthVerifier9(t, func(c map[string]any) { c["scope"] = "xrag:write" })
	_, e := v.Verify(ctx, "opaque-token")
	code(t, e, "INSUFFICIENT_SCOPE")
}
func TestDemo9OAuthConfigurationDoesNotFetchOrReadCredentials(t *testing.T) {
	v, calls := oauthVerifier9(t, nil)
	v.Config.ClientSecretRef = "env:XRAG_MISSING_TEST_SECRET"
	t.Setenv("XRAG_MISSING_TEST_SECRET", "")
	_, e := mcpauth.New(v.Config)
	must(t, e)
	if calls.Load() != 0 {
		t.Fatal(calls.Load())
	}
	_, e = v.Verify(ctx, "opaque-token")
	code(t, e, "AUTH_PROVIDER_UNAVAILABLE")
	if calls.Load() != 0 {
		t.Fatal("sent unauthenticated introspection")
	}
}
func TestDemo9OAuthRejectsHTTPNonLoopbackAndGlobalAdmin(t *testing.T) {
	v, _ := oauthVerifier9(t, nil)
	for _, raw := range []string{"http://auth.example/introspect", "http://localhost/introspect", "https://u:p@auth.example/x", "https://auth.example/x?secret=y"} {
		c := v.Config
		c.IntrospectionEndpoint = raw
		if _, e := mcpauth.New(c); e == nil {
			t.Fatal(raw)
		}
	}
	c := v.Config
	c.Subjects = map[string]mcpauth.Subject{"alice": {Role: "admin", KBs: []string{"demo"}}}
	if _, e := mcpauth.New(c); e == nil {
		t.Fatal("global admin granted")
	}
}
func TestDemo9OAuthUnavailableIsNotInvalidToken(t *testing.T) {
	v, _ := oauthVerifier9(t, nil)
	h := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	defer h.Close()
	v.Config.IntrospectionEndpoint = h.URL
	_, e := v.Verify(ctx, "opaque-token")
	code(t, e, "AUTH_PROVIDER_UNAVAILABLE")
}
func TestDemo9OAuthDoesNotForwardTokenOnRedirect(t *testing.T) {
	var leaked atomic.Int32
	dest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked.Add(1) }))
	defer dest.Close()
	h := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, dest.URL, 307) }))
	defer h.Close()
	v, _ := oauthVerifier9(t, nil)
	v.Config.IntrospectionEndpoint = h.URL
	_, e := v.Verify(ctx, "opaque-token")
	code(t, e, "AUTH_PROVIDER_UNAVAILABLE")
	if leaked.Load() != 0 {
		t.Fatal("token followed redirect")
	}
}
func TestDemo9OAuthFileReferenceAndStrictConfig(t *testing.T) {
	v, _ := oauthVerifier9(t, nil)
	dir := t.TempDir()
	secret := filepath.Join(dir, "secret")
	must(t, os.WriteFile(secret, []byte("fixture-secret\n"), 0600))
	v.Config.ClientSecretRef = "file:" + secret
	config := filepath.Join(dir, "auth.json")
	must(t, os.WriteFile(config, []byte(core.JSON(v.Config)), 0600))
	p, e := mcpauth.Read(config)
	must(t, e)
	_, e = p.Verify(ctx, "opaque-token")
	must(t, e)
	must(t, os.WriteFile(config, []byte(core.JSON(v.Config)+" {}"), 0600))
	if _, e = mcpauth.Read(config); e == nil {
		t.Fatal("multiple configs accepted")
	}
}
func TestDemo9OAuthMetadataChallengeAndLocalKeyStillWork(t *testing.T) {
	_, app, h, key := mcpApp9(t)
	v, _ := oauthVerifier9(t, nil)
	v.Config.Resource = h.URL + "/mcp"
	app.MCPAuth = v
	r, e := http.Get(h.URL + "/.well-known/oauth-protected-resource/mcp")
	must(t, e)
	defer r.Body.Close()
	var got map[string]any
	must(t, json.NewDecoder(r.Body).Decode(&got))
	if r.StatusCode != 200 || got["resource"] != h.URL+"/mcp" {
		t.Fatal(got)
	}
	status, head, _ := postMCP9(t, h.URL+"/mcp", "", rpc9("tools/list", map[string]any{}), "application/json")
	if status != 401 || !strings.Contains(head.Get("WWW-Authenticate"), "resource_metadata=") {
		t.Fatal(status, head)
	}
	status, _, _ = postMCP9(t, h.URL+"/mcp", key, rpc9("tools/list", map[string]any{}), "application/json")
	if status != 200 {
		t.Fatal(status)
	}
}
func TestDemo9OAuthScopeAndRoleIntersection(t *testing.T) {
	s, app, h, _ := mcpApp9(t)
	pub(t, s, "a.md", "# log\nE203")
	t.Setenv("XRAG_OAUTH_FIXTURE_SECRET", "fixture-secret")
	var issuer string
	auth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		c := oauthClaims9(h.URL+"/mcp", issuer)
		if r.Form.Get("token") == "read-only" {
			c["scope"] = "xrag:read"
		}
		json.NewEncoder(w).Encode(c)
	}))
	defer auth.Close()
	issuer = auth.URL
	v, e := mcpauth.New(mcpauth.Config{Resource: h.URL + "/mcp", Issuer: auth.URL, IntrospectionEndpoint: auth.URL, ClientID: "c", ClientSecretRef: "env:XRAG_OAUTH_FIXTURE_SECRET", AllowLoopbackHTTP: true, Subjects: map[string]mcpauth.Subject{"alice": {Role: "reader", KBs: []string{"demo"}}}})
	must(t, e)
	app.MCPAuth = v
	status, _, b := postMCP9(t, h.URL+"/mcp?toolsets=core,write,research", "full-token", rpc9("tools/list", map[string]any{}), "application/json")
	if status != 200 || strings.Contains(string(b), "knowledge_submit") {
		t.Fatal(status, string(b))
	}
	status, headers, _ := postMCP9(t, h.URL+"/mcp?toolsets=write", "read-only", rpc9("tools/call", map[string]any{"name": "knowledge_submit", "arguments": map[string]any{}}), "application/json")
	if status != 403 || !strings.Contains(headers.Get("WWW-Authenticate"), "xrag:write") {
		t.Fatal(status, headers)
	}
	status, _, b = postMCP9(t, h.URL+"/mcp", "full-token", rpc9("tools/call", map[string]any{"name": "search_keyword", "arguments": map[string]any{"kbs": []string{"demo"}, "q": "E203"}}), "application/json")
	if status != 200 || !strings.Contains(string(b), "E203") {
		t.Fatal(status, string(b))
	}
}
func TestDemo9OAuthCancellationStopsIntrospection(t *testing.T) {
	v, _ := oauthVerifier9(t, nil)
	c, cancel := context.WithCancel(ctx)
	cancel()
	_, e := v.Verify(c, "opaque-token")
	if e == nil {
		t.Fatal("cancel ignored")
	}
}
