package server

import (
	"context"
	"crypto/subtle"
	"embed"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"github.com/BlueSkyXN/XRAG/adapters"
	"github.com/BlueSkyXN/XRAG/contracts"
	"github.com/BlueSkyXN/XRAG/core"
	"github.com/BlueSkyXN/XRAG/internal/build"
	"github.com/BlueSkyXN/XRAG/internal/mcpauth"
	"github.com/BlueSkyXN/XRAG/internal/oplock"
	"github.com/BlueSkyXN/XRAG/internal/store"
	"github.com/BlueSkyXN/XRAG/protocols/mcp"
)

//go:embed web/*
var static embed.FS

type session struct {
	Token   string
	Expires time.Time
	Edge    bool
}
type App struct {
	Store         *store.Store
	Builder       *build.Builder
	Logger        *log.Logger
	Workers       bool
	DisableWeb    bool
	DisableMCP    bool
	ConfigSource  string
	EdgeUser      string
	EdgePass      string
	maintenanceMu sync.Mutex
	mu            sync.Mutex
	sessions      map[string]session
	MCPAuth       *mcpauth.Verifier
	MCPTransport  mcp.HTTPTransport
}

func New(s *store.Store, b *build.Builder, logger *log.Logger) *App {
	return &App{Store: s, Builder: b, Logger: logger, sessions: map[string]session{}}
}
func write(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func (a *App) fail(w http.ResponseWriter, e error) {
	ce := core.AsError(e)
	if ce.Code == "INTERNAL_ERROR" && a.Logger != nil {
		a.Logger.Printf("request error: %v", e)
	}
	write(w, ce.Status, map[string]any{"error": ce})
}
func body(r *http.Request, max int64) ([]byte, error) {
	b, e := io.ReadAll(io.LimitReader(r.Body, max+1))
	if e != nil {
		return nil, e
	}
	if int64(len(b)) > max {
		return nil, core.Err("INVALID_ARGUMENT", "request body too large", 413)
	}
	return b, nil
}
func decode(b []byte, out any) error {
	d := json.NewDecoder(strings.NewReader(string(b)))
	d.DisallowUnknownFields()
	if e := d.Decode(out); e != nil {
		return core.Err("INVALID_ARGUMENT", "invalid request: "+e.Error(), 400)
	}
	if e := d.Decode(new(any)); e != io.EOF {
		return core.Err("INVALID_ARGUMENT", "only one JSON value is allowed", 400)
	}
	return nil
}
// serveApp serves the bundled workbench. Page paths such as /search fall back to
// index.html so the client router can reload them; missing files stay 404.
func serveApp(w http.ResponseWriter, r *http.Request) {
	assets, _ := fs.Sub(static, "web")
	name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if name != "" && name != "." {
		if st, err := fs.Stat(assets, name); err == nil && !st.IsDir() {
			if strings.HasPrefix(name, "assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			http.FileServer(http.FS(assets)).ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(name, "assets/") || strings.Contains(path.Base(name), ".") {
			http.NotFound(w, r)
			return
		}
	}
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeFileFS(w, r, assets, "index.html")
}
func (a *App) passwordMatches(user, pass string) bool {
	if a.EdgeUser == "" || a.EdgePass == "" || user == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(user), []byte(a.EdgeUser)) == 1 && subtle.ConstantTimeCompare([]byte(pass), []byte(a.EdgePass)) == 1
}
func (a *App) principal(r *http.Request) (store.Principal, error) {
	token := bearer(r)
	if token == "" {
		if c, e := r.Cookie("xrag_session"); e == nil {
			a.mu.Lock()
			ss, ok := a.sessions[c.Value]
			if ok && time.Now().Before(ss.Expires) && ss.Edge {
				a.mu.Unlock()
				return a.Store.PermanentAdmin(r.Context())
			}
			if ok && time.Now().Before(ss.Expires) {
				token = ss.Token
			} else {
				delete(a.sessions, c.Value)
			}
			a.mu.Unlock()
		}
	}
	return a.Store.Authenticate(r.Context(), token)
}
func (a *App) Execute(ctx context.Context, p store.Principal, action string, b []byte) (any, error) {
	if v, handled, err := a.ExecuteTask(ctx, p, action, b, ""); handled {
		return v, err
	}
	if v, handled, err := a.ExecuteWrite(ctx, p, action, b); handled {
		return v, err
	}
	ctx = adapters.WithRequestID(ctx, core.ID("query_"))
	ctx = store.WithActor(ctx, p.ID)
	spec, ok := contracts.Find(action)
	if !ok {
		return nil, core.Err("MODE_UNSUPPORTED", "action not implemented", 422)
	}
	req, e := contracts.Decode(spec, b)
	if e != nil {
		return nil, e
	}
	req.Kbs, e = a.Store.Scope(ctx, p, req.Kbs)
	if e != nil {
		return nil, e
	}
	before, e := a.Store.Epoch(ctx)
	if e != nil {
		return nil, e
	}
	wantTrace := req.Trace
	req.Trace = true
	eng, e := a.Builder.EngineFor(ctx, req.Kbs)
	if e != nil {
		return nil, e
	}
	result, e := eng.Run(ctx, action, req, p.ID)
	if e != nil {
		result.Failure = core.AsError(e)
		result.Coverage.ExecutionPartial = true
		result.Action = action
		result.Kbs = req.Kbs
		result.Mode = "server"
		if result.QueryID == "" {
			result.QueryID = core.ID("q_")
		}
		result.Trace = append(result.Trace, core.Step{Stage: "error", Note: result.Failure.Code})
	}
	if action == "kb.discover" && e == nil {
		if data, ok := result.Data.(map[string]any); ok {
			data["service_operations"] = map[string]any{"research": true, "requires_configured_chat": true, "recipe": true, "checkpoint_resume": "explicit"}
		}
	}
	if err := a.Store.RecordQuery(ctx, p, req, result, before); err != nil && a.Logger != nil {
		a.Logger.Printf("query history: %v", err)
	}
	if !wantTrace {
		result.Trace = nil
	}
	return result, e
}
func (a *App) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if a.mcpMetadata(w, r) {
		return
	}
	if strings.HasPrefix(r.URL.Path, "/v1/") && r.URL.Path != "/v1/tasks/events" {
		leaseCtx, release, err := oplock.Acquire(r.Context(), a.Store.Dir, false)
		if err != nil {
			a.fail(w, err)
			return
		}
		defer release()
		r = r.WithContext(leaseCtx)
	}

	if (a.DisableMCP && r.URL.Path == "/mcp") || (a.DisableWeb && !strings.HasPrefix(r.URL.Path, "/v1/") && r.URL.Path != "/mcp" && r.URL.Path != "/healthz") {
		http.NotFound(w, r)
		return
	}
	defer func() {
		if v := recover(); v != nil {
			if a.Logger != nil {
				a.Logger.Printf("panic: %v", v)
			}
			a.fail(w, errors.New("internal panic"))
		}
	}()
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Frame-Options", "DENY")
	if r.Method == "POST" || r.Method == "PATCH" || r.Method == "DELETE" || r.Method == "PUT" || r.URL.Path == "/mcp" {
		if origin := r.Header.Get("Origin"); origin != "" {
			u, e := url.Parse(origin)
			if e != nil || u.Host != r.Host {
				a.fail(w, core.Err("PERMISSION_DENIED", "cross-origin write is not allowed", 403))
				return
			}
		}
	}
	if (r.Method == "POST" || r.Method == "PATCH" || r.Method == "PUT" || r.Method == "DELETE") && !(r.URL.Path == "/mcp" && r.Method == "DELETE") {
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			a.fail(w, core.Err("INVALID_ARGUMENT", "application/json is required", 415))
			return
		}
	}
	if strings.HasPrefix(r.URL.Path, "/v1/hooks/") {
		a.hook(w, r)
		return
	}
	if r.URL.Path == "/healthz" {
		write(w, 200, map[string]any{"status": "ok", "version": core.Version})
		return
	}
	if r.URL.Path == "/v1/login" && r.Method == "GET" {
		mode := "key"
		if a.EdgeUser != "" {
			mode = "password"
		}
		write(w, 200, map[string]any{"mode": mode})
		return
	}
	if r.URL.Path == "/v1/session" && r.Method == "POST" {
		b, e := body(r, 8192)
		if e != nil {
			a.fail(w, e)
			return
		}
		var input struct {
			Key      string `json:"key"`
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if e = decode(b, &input); e != nil {
			a.fail(w, e)
			return
		}
		var p store.Principal
		edge := false
		if input.Username != "" || input.Password != "" {
			if !a.passwordMatches(input.Username, input.Password) {
				a.fail(w, core.Err("UNAUTHENTICATED", "username or password is wrong", 401))
				return
			}
			p, e = a.Store.PermanentAdmin(r.Context())
			edge = true
		} else {
			p, e = a.Store.Authenticate(r.Context(), input.Key)
		}
		if e != nil {
			a.fail(w, e)
			return
		}
		sid := core.ID("session_")
		a.mu.Lock()
		if len(a.sessions) > 1000 {
			a.sessions = map[string]session{}
		}
		a.sessions[sid] = session{Token: input.Key, Expires: time.Now().Add(8 * time.Hour), Edge: edge}
		a.mu.Unlock()
		http.SetCookie(w, &http.Cookie{Name: "xrag_session", Value: sid, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: r.TLS != nil, MaxAge: 28800})
		write(w, 200, p)
		return
	}
	if r.URL.Path == "/v1/session" && r.Method == "DELETE" {
		if c, e := r.Cookie("xrag_session"); e == nil {
			a.mu.Lock()
			delete(a.sessions, c.Value)
			a.mu.Unlock()
		}
		http.SetCookie(w, &http.Cookie{Name: "xrag_session", Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: r.TLS != nil})
		write(w, 200, map[string]any{"logged_out": true})
		return
	}
	if !strings.HasPrefix(r.URL.Path, "/v1/") && r.URL.Path != "/mcp" {
		if r.Method != "GET" && r.Method != "HEAD" {
			http.NotFound(w, r)
			return
		}
		serveApp(w, r)
		return
	}
	if r.URL.Path == "/mcp" {
		p, id, e := a.mcpPrincipal(r)
		if e != nil {
			a.mcpDenied(w, e, "xrag:read")
			return
		}
		if id != nil && r.Method == "POST" {
			raw, e := body(r, 2<<20)
			if e != nil {
				a.fail(w, e)
				return
			}
			if scope := a.checkMCPScope(raw, id); scope != "" {
				a.mcpDenied(w, core.Err("INSUFFICIENT_SCOPE", scope+" scope required", 403), scope)
				return
			}
			r.Body = io.NopCloser(strings.NewReader(string(raw)))
		}
		a.serveMCP(w, r, p, id)
		return
	}
	p, e := a.principal(r)
	if e != nil {
		a.fail(w, e)
		return
	}
	if a.taskEvents(w, r, p) {
		return
	}
	if a.taskRoutes(w, r, p) {
		return
	}
	if a.writeToolRoute(w, r, p) {
		return
	}
	if a.iteration6Routes(w, r, p) {
		return
	}
	if a.completeRoutes(w, r, p) {
		return
	}
	if a.lifecycleRoutes(w, r, p) {
		return
	}
	if a.productRoutes(w, r, p) {
		return
	}
	if a.diagnosticRoutes(w, r, p) {
		return
	}
	if r.URL.Path == "/v1/system" && r.Method == "GET" {
		ids, err := a.Store.VisibleKBs(r.Context(), p)
		if err != nil {
			a.fail(w, err)
			return
		}
		byKB := map[string]any{}
		for _, id := range ids {
			c, err := a.Builder.Capabilities(r.Context(), id)
			if err != nil {
				c = map[string]any{"configuration_error": err.Error(), "embedding": false, "wiki": false, "qdrant": false, "rerank": false, "graph_backend": "configuration incomplete"}
			}
			byKB[id] = c
		}
		plugins := map[string]any{}
		if store.RoleAtLeast(p, "maintainer") {
			for id, spec := range a.Builder.Config.Plugins {
				plugins[id] = map[string]any{"kind": spec.Kind, "version": spec.Version, "protocol": spec.Protocol}
			}
		}
		write(w, 200, map[string]any{"plugins": plugins, "version": core.Version, "origin_id": a.Store.Origin, "identity": p, "edge_login": a.EdgeUser != "", "workers": a.Workers, "storage": "SQLite / same-machine Demo", "keyword": "FTS5 BM25 + character/bigram fallback", "graph_backend": func() string {
			if a.Builder.Config.Neo4j != nil {
				return "neo4j-query-api"
			}
			return "SQLite explicit relation Demo"
		}(), "embedding": a.Builder.Config.Embedding != nil, "qdrant": a.Builder.Config.Qdrant != nil, "wiki": a.Builder.Config.Chat != nil, "rerank": a.Builder.Config.Rerank != nil, "agent": true, "investigation": map[string]any{"runtime": true, "planner_requires_chat": true, "recipe": true, "resume": "explicit_checkpoint", "events": "current_snapshot_sse"}, "mcp": map[string]any{"enabled": !a.DisableMCP, "versions": mcp.Versions, "http_post": []string{"json", "sse"}, "resources": true, "prompts": true, "oauth_resource_server": a.MCPAuth != nil, "server_initiated_stream": false}, "git_source": true, "source_polling": true, "signed_hook": "xrag-hmac-v1", "evaluation": true, "model_policy": "No mock model is enabled in the application.", "by_kb": byKB})
		return
	}
	if r.URL.Path == "/v1/schema" && r.Method == "GET" {
		write(w, 200, map[string]any{"actions": contracts.All, "output_schema": contracts.Output(), "status": "implemented demo subset"})
		return
	}
	if spec, ok := contracts.Find(r.URL.Path); ok {
		var b []byte
		if r.Method == "GET" && spec.Action == "kb.discover" {
			q := map[string]any{}
			if x := r.URL.Query().Get("kbs"); x != "" {
				q["kbs"] = strings.Split(x, ",")
			}
			b = []byte(core.JSON(q))
		} else if r.Method == "POST" {
			b, e = body(r, 1<<20)
			if e != nil {
				a.fail(w, e)
				return
			}
		} else {
			write(w, 405, map[string]any{"error": "method not allowed"})
			return
		}
		res, e := a.Execute(r.Context(), p, spec.Action, b)
		if e != nil {
			a.fail(w, e)
			return
		}
		write(w, 200, res)
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) >= 3 && parts[1] == "handles" && r.Method == "GET" {
		res, e := a.Execute(r.Context(), p, "handles.describe", []byte(core.JSON(map[string]any{"id": parts[2]})))
		if e != nil {
			a.fail(w, e)
			return
		}
		write(w, 200, res)
		return
	}
	if r.URL.Path == "/v1/kbs" {
		if r.Method == "GET" {
			rs, e := a.Store.KBs(r.Context())
			if e != nil {
				a.fail(w, e)
				return
			}
			out := []map[string]any{}
			for _, v := range rs {
				if p.Role == "admin" || core.Subset([]string{v["id"].(string)}, p.KBs) {
					out = append(out, v)
				}
			}
			write(w, 200, map[string]any{"items": out})
			return
		}
		if r.Method == "POST" {
			if !store.RoleAtLeast(p, "admin") {
				a.fail(w, core.Err("PERMISSION_DENIED", "admin role required", 403))
				return
			}
			var q struct{ ID, Name string }
			b, e := body(r, 8192)
			if e == nil {
				e = decode(b, &q)
			}
			if e == nil {
				e = a.Store.CreateKB(r.Context(), q.ID, q.Name)
			}
			if e != nil {
				a.fail(w, e)
				return
			}
			write(w, 201, map[string]any{"id": q.ID})
			return
		}
	}
	if r.URL.Path == "/v1/jobs" && r.Method == "GET" {
		kbs, e := a.Store.Scope(r.Context(), p, nil)
		if e != nil {
			a.fail(w, e)
			return
		}
		rs, e := a.Store.Jobs(r.Context(), kbs)
		if e != nil {
			a.fail(w, e)
			return
		}
		out := []store.Job{}
		kbFilter, kindFilter := r.URL.Query().Get("kb"), r.URL.Query().Get("kind")
		for _, j := range rs {
			if (kbFilter != "" && j.KB != kbFilter) || (kindFilter != "" && !slices.Contains(strings.Split(kindFilter, ","), j.Kind)) {
				continue
			}
			if j.Owner == p.ID || store.RoleAtLeast(p, "maintainer") {
				out = append(out, a.jobVisible(j))
			}
		}
		write(w, 200, map[string]any{"items": out})
		return
	}
	if len(parts) >= 3 && (parts[1] == "jobs" || parts[1] == "builds") {
		j, e := a.Store.Job(r.Context(), parts[2])
		if e != nil {
			a.fail(w, e)
			return
		}
		if _, e = a.Store.Scope(r.Context(), p, []string{j.KB}); e != nil || j.Owner != p.ID && !store.RoleAtLeast(p, "maintainer") {
			a.fail(w, core.Err("PERMISSION_DENIED", "job not in current scope", 403))
			return
		}
		if len(parts) == 3 && r.Method == "GET" {
			write(w, 200, a.jobVisible(j))
			return
		}
		if len(parts) == 4 && parts[3] == "details" && r.Method == "GET" {
			v, e := a.Store.JobDetails(r.Context(), j.ID)
			if e != nil {
				a.fail(w, e)
				return
			}
			write(w, 200, v)
			return
		}
		if len(parts) == 4 && parts[3] == "cancel" && r.Method == "POST" {
			e = a.Store.Cancel(r.Context(), j.ID)
			if e != nil {
				a.fail(w, e)
				return
			}
			write(w, 200, map[string]any{"id": j.ID, "cancel_requested": true})
			return
		}
	}
	if len(parts) >= 4 && parts[1] == "kbs" {
		kb := parts[2]
		if _, e = a.Store.Scope(r.Context(), p, []string{kb}); e != nil {
			a.fail(w, e)
			return
		}
		op := parts[3]
		if op == "knowledge" && r.Method == "GET" {
			if !store.RoleAtLeast(p, "contributor") {
				a.fail(w, core.Err("PERMISSION_DENIED", "knowledge management requires contributor", 403))
				return
			}
			rs, e := a.Store.KnowledgeList(r.Context(), []string{kb})
			if e != nil {
				a.fail(w, e)
				return
			}
			write(w, 200, map[string]any{"items": rs})
			return
		}
		if !store.RoleAtLeast(p, "maintainer") && !(op == "knowledge" && r.Method == "POST" && store.RoleAtLeast(p, "contributor")) {
			a.fail(w, core.Err("PERMISSION_DENIED", "maintainer role required", 403))
			return
		}
		if op == "sources" {
			if len(parts) == 6 && parts[5] == "policy" {
				if _, e := a.Store.Source(r.Context(), kb, parts[4]); e != nil {
					a.fail(w, e)
					return
				}
				if r.Method == "GET" {
					v, e := a.Store.Policy(r.Context(), kb, parts[4])
					if e != nil {
						a.fail(w, e)
						return
					}
					write(w, 200, v)
					return
				}
				if r.Method == "PUT" {
					var q store.SourcePolicy
					b, e := body(r, 8192)
					if e == nil {
						e = decode(b, &q)
					}
					if e == nil {
						e = a.Store.SetPolicy(r.Context(), kb, parts[4], q)
					}
					if e != nil {
						a.fail(w, e)
						return
					}
					v, _ := a.Store.Policy(r.Context(), kb, parts[4])
					write(w, 200, v)
					return
				}
			}
			if len(parts) != 4 {
				a.fail(w, core.Err("NOT_FOUND", "source route not found", 404))
				return
			}
			if r.Method == "GET" {
				rs, e := a.Store.Sources(r.Context(), kb)
				if e != nil {
					a.fail(w, e)
					return
				}
				out := []map[string]any{}
				for _, src := range rs {
					pol, _ := a.Store.Policy(r.Context(), kb, src.ID)
					st, _ := a.Store.SourceState(r.Context(), kb, src.ID)
					out = append(out, map[string]any{"source": src, "policy": pol, "status": st})
				}
				write(w, 200, map[string]any{"items": rs, "details": out})
				return
			}
			if r.Method == "POST" {
				var src store.Source
				b, e := body(r, 8192)
				if e == nil {
					e = decode(b, &src)
				}
				if e == nil {
					src.KB = kb
					e = a.Store.AddSource(r.Context(), src)
				}
				if e != nil {
					a.fail(w, e)
					return
				}
				write(w, 201, src)
				return
			}
		}
		if op == "builds" && r.Method == "POST" {
			var q struct {
				Mode string `json:"mode"`
			}
			b, e := body(r, 8192)
			if e == nil {
				e = decode(b, &q)
			}
			if e != nil {
				a.fail(w, e)
				return
			}
			if q.Mode == "" {
				q.Mode = "incremental"
			}
			if !core.Subset([]string{q.Mode}, []string{"incremental", "full", "vectors_only", "graph_only"}) {
				a.fail(w, core.Err("MODE_UNSUPPORTED", "supported build modes: incremental, full, vectors_only, graph_only", 422))
				return
			}
			j, e := a.Store.Enqueue(r.Context(), kb, "build", p.ID, r.Header.Get("Idempotency-Key"), q)
			if e != nil {
				a.fail(w, e)
				return
			}
			write(w, 202, j)
			return
		}
		if op == "build-estimates" && r.Method == "POST" {
			res, e := a.Builder.Estimate(r.Context(), kb)
			if e != nil {
				a.fail(w, e)
				return
			}
			write(w, 200, res)
			return
		}
		if op == "upload" && r.Method == "POST" {
			var q struct {
				Path string `json:"path"`
				Body string `json:"body"`
			}
			b, e := body(r, 6<<20)
			if e == nil {
				e = decode(b, &q)
			}
			if e == nil {
				e = a.upload(r.Context(), kb, q.Path, q.Body)
			}
			if e != nil {
				a.fail(w, e)
				return
			}
			j, e := a.Store.Enqueue(r.Context(), kb, "build", p.ID, r.Header.Get("Idempotency-Key"), map[string]any{"mode": "incremental"})
			if e != nil {
				a.fail(w, e)
				return
			}
			write(w, 202, j)
			return
		}
		if op == "withdraw" && r.Method == "POST" {
			var q struct {
				Path    string `json:"path"`
				Confirm bool   `json:"confirm"`
			}
			b, e := body(r, 8192)
			if e == nil {
				e = decode(b, &q)
			}
			if e == nil && !q.Confirm {
				e = core.Err("INVALID_ARGUMENT", "explicit confirm=true is required", 400)
			}
			if e == nil {
				e = a.Store.Withdraw(r.Context(), kb, q.Path)
			}
			if e != nil {
				a.fail(w, e)
				return
			}
			write(w, 200, map[string]any{"withdrawn": q.Path})
			return
		}
		if op == "knowledge" && r.Method == "POST" {
			var k store.Knowledge
			b, e := body(r, 400000)
			if e == nil {
				e = decode(b, &k)
			}
			if e == nil {
				k.KB = kb
				k, e = a.Store.SaveKnowledge(store.WithActor(r.Context(), p.ID), k)
			}
			if e != nil {
				a.fail(w, e)
				return
			}
			write(w, 201, k)
			return
		}
		if op == "wiki" && r.Method == "POST" {
			var q build.WikiRequest
			b, e := body(r, 8192)
			if e == nil {
				e = decode(b, &q)
			}
			if e != nil {
				a.fail(w, e)
				return
			}
			scoped, resolveErr := a.Builder.ForKB(r.Context(), kb)
			if resolveErr != nil {
				a.fail(w, resolveErr)
				return
			}
			if scoped.Config.Chat == nil {
				a.fail(w, core.Err("CAPABILITY_UNAVAILABLE", "configure chat to generate a Wiki draft", 422))
				return
			}
			j, e := a.Store.Enqueue(r.Context(), kb, "wiki", p.ID, r.Header.Get("Idempotency-Key"), q)
			if e != nil {
				a.fail(w, e)
				return
			}
			write(w, 202, j)
			return
		}
	}
	if len(parts) >= 3 && parts[1] == "knowledge" {
		k, e := a.Store.Knowledge(r.Context(), parts[2], "")
		if e != nil {
			a.fail(w, e)
			return
		}
		if _, e = a.Store.Scope(r.Context(), p, []string{k.KB}); e != nil {
			a.fail(w, e)
			return
		}
		if !store.RoleAtLeast(p, "contributor") {
			a.fail(w, core.Err("PERMISSION_DENIED", "drafts require contributor role", 403))
			return
		}
		if len(parts) == 3 && r.Method == "GET" {
			write(w, 200, k)
			return
		}
		if len(parts) == 4 && parts[3] == "publish" && r.Method == "POST" {
			if !store.RoleAtLeast(p, "maintainer") {
				a.fail(w, core.Err("PERMISSION_DENIED", "maintainer role required", 403))
				return
			}
			var q struct {
				Revision string `json:"revision"`
			}
			b, e := body(r, 8192)
			if e == nil {
				e = decode(b, &q)
			}
			if e != nil {
				a.fail(w, e)
				return
			}
			if q.Revision != k.Draft {
				a.fail(w, core.Err("VERSION_CONFLICT", "publish an explicitly selected current draft revision", 409))
				return
			}
			j, e := a.Store.Enqueue(r.Context(), k.KB, "publish", p.ID, r.Header.Get("Idempotency-Key"), map[string]any{"id": k.ID, "revision": q.Revision})
			if e != nil {
				a.fail(w, e)
				return
			}
			write(w, 202, j)
			return
		}
	}
	if r.URL.Path == "/v1/feedback" && r.Method == "POST" {
		if !store.RoleAtLeast(p, "contributor") {
			a.fail(w, core.Err("PERMISSION_DENIED", "contributor role required", 403))
			return
		}
		var q struct{ KB, Ref, Label, Note string }
		b, e := body(r, 12000)
		if e == nil {
			e = decode(b, &q)
		}
		if e == nil {
			_, e = a.Store.Scope(r.Context(), p, []string{q.KB})
		}
		if e == nil {
			e = a.Store.Feedback(r.Context(), q.KB, q.Ref, q.Label, q.Note, p.ID)
		}
		if e != nil {
			a.fail(w, e)
			return
		}
		write(w, 201, map[string]any{"saved": true})
		return
	}

	a.fail(w, core.Err("NOT_FOUND", "route not implemented in Demo", 404))
}
func (a *App) upload(ctx context.Context, kb, p, text string) error {
	if _, e := core.CleanPath(p); e != nil {
		return e
	}
	if !strings.HasSuffix(strings.ToLower(p), ".md") && !strings.HasSuffix(strings.ToLower(p), ".txt") {
		return core.Err("MODE_UNSUPPORTED", "only MD/TXT upload is supported", 422)
	}
	if len(text) > 5<<20 {
		return core.Err("SOURCE_LIMIT", "upload too large", 422)
	}
	root := filepath.Join(a.Store.Dir, "uploads", kb)
	if e := os.MkdirAll(root, 0700); e != nil {
		return e
	}
	dst := filepath.Join(root, filepath.FromSlash(p))
	parts := strings.Split(filepath.ToSlash(p), "/")
	walk := root
	for _, x := range parts {
		walk = filepath.Join(walk, x)
		if st, e := os.Lstat(walk); e == nil && st.Mode()&os.ModeSymlink != 0 {
			return core.Err("INVALID_ARGUMENT", "symlink upload path is not allowed", 400)
		}
	}
	if e := os.MkdirAll(filepath.Dir(dst), 0700); e != nil {
		return e
	}
	sources, e := a.Store.Sources(ctx, kb)
	if e != nil {
		return e
	}
	found := false
	for _, s := range sources {
		if s.ID == "uploads" {
			found = true
			if s.Root != root {
				return core.Err("VERSION_CONFLICT", "uploads source is configured at another path", 409)
			}
		}
	}
	if !found {
		if e = a.Store.AddSource(ctx, store.Source{KB: kb, ID: "uploads", Root: root, Mount: "uploads", Kind: "builtin"}); e != nil {
			return e
		}
	}
	tmp := dst + "." + core.ID("") + ".tmp"
	if e = os.WriteFile(tmp, []byte(text), 0600); e != nil {
		return e
	}
	return os.Rename(tmp, dst)
}
func (a *App) ShutdownSessions() { a.mu.Lock(); defer a.mu.Unlock(); a.sessions = map[string]session{} }

var _ = time.Second
var _ = adapters.Config{}
