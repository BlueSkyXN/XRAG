package tests

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/BlueSkyXN/XRAG/core"
)

func TestAskTagsRoutesAndFusesThem(t *testing.T) {
	s, e := setup(t)
	pub(t, s, "rel.md", "# 关系\n[[设备A]] --使用--> [[组件B]]\n")
	pub(t, s, "plain.md", "# 说明\n设备A 运行正常。\n")
	graph := true
	r := run(t, e, "+ask", core.Request{Q: "设备A", Graph: &graph})
	routes := map[string][]string{}
	for _, it := range r.Items {
		routes[it.Path] = it.Routes
	}
	if !slices.Equal(routes["rel.md"], []string{"keyword", "graph"}) {
		t.Fatalf("relation chunk should be found by keyword and graph: %v", routes)
	}
	if !slices.Equal(routes["plain.md"], []string{"keyword"}) {
		t.Fatalf("plain chunk has no relation support: %v", routes)
	}
	plain := run(t, e, "search.keyword", core.Request{Q: "设备A"})
	for _, it := range plain.Items {
		if it.Routes != nil {
			t.Fatalf("single-route actions do not tag routes: %v", it.Routes)
		}
	}
}

func TestReadWindowComesFromPinnedEdition(t *testing.T) {
	s, e := setup(t)
	body := "---\ntitle: 窗口\n---\n# 窗口\n\n## 甲\n甲的正文\n\n## 乙\n乙的旧正文\n\n## 丙\n丙的正文\n"
	old := pub(t, s, "w.md", body)
	var chunk core.Chunk
	for _, c := range old.Chunks {
		if strings.Contains(c.Text, "乙的旧正文") {
			chunk = c
		}
	}
	if chunk.LineStart == 0 {
		t.Fatalf("parser did not record lines: %+v", old.Chunks)
	}
	ref := core.ChunkItem(chunk, s.Origin).Ref
	pub(t, s, "w.md", strings.Replace(body, "乙的旧正文", "乙的新正文", 1))

	r := run(t, e, "files.read", core.Request{Ref: ref, ContextLines: 2})
	w := r.Data.(core.ReadData).Window
	if w == nil {
		t.Fatal("window missing")
	}
	lines := strings.Split(body, "\n")
	if w.CiteStart != chunk.LineStart || w.CiteEnd != chunk.LineEnd || w.LineStart != chunk.LineStart-2 || w.TotalLines != len(lines)-1 {
		t.Fatalf("window bounds %+v for chunk %d-%d", w, chunk.LineStart, chunk.LineEnd)
	}
	if !strings.Contains(w.Text, "乙的旧正文") || strings.Contains(w.Text, "乙的新正文") {
		t.Fatalf("window must come from the cited edition: %q", w.Text)
	}
	if got := strings.Split(w.Text, "\n")[chunk.LineStart-w.LineStart]; got != lines[chunk.LineStart-1] {
		t.Fatalf("line numbers drifted: %q vs %q", got, lines[chunk.LineStart-1])
	}

	match := run(t, e, "files.grep", core.Request{Pattern: "丙的正文"}).Items[0]
	mw := run(t, e, "files.read", core.Request{Ref: match.Ref, ContextLines: 1}).Data.(core.ReadData).Window
	if mw == nil || mw.CiteStart != match.LineStart || mw.CiteEnd != match.LineEnd || !strings.Contains(mw.Text, "## 丙") {
		t.Fatalf("grep match window %+v for line %d", mw, match.LineStart)
	}

	plain := run(t, e, "files.read", core.Request{Ref: ref})
	if plain.Data.(core.ReadData).Window != nil {
		t.Fatal("window is opt-in")
	}
	_, x := e.Run(ctx, "files.read", core.Request{Kbs: []string{"demo"}, Ref: ref, ContextLines: 501}, "tester")
	code(t, x, "INVALID_ARGUMENT")
}

func TestJobsFilterByKnowledgeBaseAndKind(t *testing.T) {
	h := productSetup(t)
	must(t, h.s.CreateKB(ctx, "other", "Other"))
	for _, kb := range []string{"demo", "other"} {
		if c, b := h.request(t, "POST", "/v1/kbs/"+kb+"/builds", map[string]any{"mode": "incremental"}, h.key); c >= 300 {
			t.Fatalf("build %s: %d %s", kb, c, b)
		}
	}
	count := func(query string) int {
		c, b := h.request(t, "GET", "/v1/jobs"+query, nil, h.key)
		if c != http.StatusOK {
			t.Fatalf("%s: %d %s", query, c, b)
		}
		return len(object(t, b)["items"].([]any))
	}
	if n := count(""); n != 2 {
		t.Fatalf("unfiltered jobs: %d", n)
	}
	if n := count("?kb=demo"); n != 1 {
		t.Fatalf("kb filter: %d", n)
	}
	if n := count("?kind=build,eval"); n != 2 {
		t.Fatalf("kind list filter: %d", n)
	}
	if n := count("?kb=demo&kind=wiki"); n != 0 {
		t.Fatalf("kind filter: %d", n)
	}
}

func TestHealthzReportsCommit(t *testing.T) {
	h := productSetup(t)
	c, b := h.request(t, "GET", "/healthz", nil, "")
	if c != http.StatusOK {
		t.Fatalf("healthz: %d", c)
	}
	v := object(t, b)
	if v["commit"] != core.Commit {
		t.Fatalf("healthz commit %v != core.Commit %v", v["commit"], core.Commit)
	}
	if v["version"] != core.Version {
		t.Fatalf("healthz version %v != core.Version", v["version"])
	}
}

func TestSessionCookieSecureBehindTrustedProxy(t *testing.T) {
	h := productSetup(t)
	h.app.EdgeUser = "xrag"
	h.app.EdgePass = "correct-password"

	login := func(forwarded string) []*http.Cookie {
		req, e := http.NewRequest("POST", h.url+"/v1/session", strings.NewReader(`{"username":"xrag","password":"correct-password"}`))
		must(t, e)
		req.Header.Set("Content-Type", "application/json")
		if forwarded != "" {
			req.Header.Set("X-Forwarded-Proto", forwarded)
		}
		res, e := http.DefaultClient.Do(req)
		must(t, e)
		res.Body.Close()
		if res.StatusCode != 200 || len(res.Cookies()) != 1 {
			t.Fatalf("login: %d %d cookies", res.StatusCode, len(res.Cookies()))
		}
		return res.Cookies()
	}

	// Off by default: a forwarded header must not mark the cookie Secure.
	h.app.TrustForwardedProto = false
	if c := login("https")[0]; c.Secure {
		t.Fatal("cookie must not be Secure when the proxy is not trusted")
	}
	h.app.TrustForwardedProto = true
	if c := login("https")[0]; !c.Secure {
		t.Fatal("cookie must be Secure when the trusted proxy says https")
	}
	if c := login("http")[0]; c.Secure {
		t.Fatal("cookie must not be Secure when the trusted proxy says http")
	}
	if c := login("")[0]; c.Secure {
		t.Fatal("cookie must not be Secure without a forwarded header")
	}
}

func TestWorkbenchPagesFallBackToIndex(t *testing.T) {
	h := productSetup(t)
	c, home := h.request(t, "GET", "/", nil, "")
	if c != http.StatusOK || !strings.Contains(string(home), `id="root"`) {
		t.Fatalf("home: %d %.200s", c, home)
	}
	for _, page := range []string{"/search", "/settings/keys", "/sources"} {
		c, b := h.request(t, "GET", page, nil, "")
		if c != http.StatusOK || string(b) != string(home) {
			t.Fatalf("%s should serve the app shell: %d", page, c)
		}
	}
	for _, missing := range []string{"/assets/missing.js", "/missing.png"} {
		if c, _ := h.request(t, "GET", missing, nil, ""); c != http.StatusNotFound {
			t.Fatalf("%s: %d", missing, c)
		}
	}
	if c, _ := h.request(t, "POST", "/search", map[string]any{}, ""); c != http.StatusNotFound {
		t.Fatalf("page paths accept only GET: %d", c)
	}
}
