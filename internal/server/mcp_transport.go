package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"github.com/BlueSkyXN/XRAG/core"
	"github.com/BlueSkyXN/XRAG/internal/mcpauth"
	"github.com/BlueSkyXN/XRAG/internal/oplock"
	"github.com/BlueSkyXN/XRAG/internal/store"
	"github.com/BlueSkyXN/XRAG/protocols/mcp"
)

func bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if len(h) >= 7 && strings.EqualFold(h[:7], "Bearer ") {
		return strings.TrimSpace(h[7:])
	}
	return ""
}
func (a *App) mcpPrincipal(r *http.Request) (store.Principal, *mcpauth.Identity, error) {
	// MCP uses a header identity, not a browser session cookie.
	if p, ok, e := a.edgePrincipal(r); ok || e != nil {
		return p, nil, e
	}
	token := bearer(r)
	if token != "" {
		p, e := a.Store.Authenticate(r.Context(), token)
		if e == nil {
			return p, nil, nil
		}
		if core.AsError(e).Code != "UNAUTHENTICATED" {
			return p, nil, e
		}
	}
	if a.MCPAuth == nil || token == "" {
		return store.Principal{}, nil, core.Err("UNAUTHENTICATED", "provide a Bearer API key or a configured OAuth access token", 401)
	}
	id, e := a.MCPAuth.Verify(r.Context(), token)
	if e != nil {
		return store.Principal{}, nil, e
	}
	p := store.Principal{ID: id.ID, Name: id.Subject.Name, Role: id.Subject.Role, KBs: id.Subject.KBs}
	return p, &id, nil
}
func (a *App) mcpMetadata(w http.ResponseWriter, r *http.Request) bool {
	if a.MCPAuth == nil || a.DisableMCP {
		return false
	}
	if r.URL.Path != a.MCPAuth.MetadataPath() && r.URL.Path != "/.well-known/oauth-protected-resource" {
		return false
	}
	if r.Method != "GET" {
		w.Header().Set("Allow", "GET")
		w.WriteHeader(405)
		return true
	}
	write(w, 200, a.MCPAuth.Metadata())
	return true
}
func (a *App) mcpDenied(w http.ResponseWriter, e error, scope string) {
	ce := core.AsError(e)
	if a.MCPAuth != nil && (ce.Status == 401 || ce.Status == 403) {
		code := "invalid_token"
		if ce.Status == 403 {
			code = "insufficient_scope"
		}
		w.Header().Set("WWW-Authenticate", a.MCPAuth.Challenge(scope, code))
	} else if ce.Status == 401 {
		w.Header().Set("WWW-Authenticate", `Bearer realm="XRAG"`)
	}
	a.fail(w, e)
}
func mcpRequiredScope(action string) string {
	if action == "knowledge.submit" || action == "feedback.submit" {
		return "xrag:write"
	}
	if action == "research.submit" || action == "recipe.run" || action == "task.resume" || action == "task.status" || action == "task.cancel" {
		return "xrag:research"
	}
	return "xrag:read"
}
func (a *App) serveMCP(w http.ResponseWriter, r *http.Request, p store.Principal, id *mcpauth.Identity) {
	visible, e := a.Store.VisibleKBs(r.Context(), p)
	if e != nil {
		a.fail(w, e)
		return
	}
	semantic := false
	for _, kb := range visible {
		c, er := a.Builder.Capabilities(r.Context(), kb)
		semantic = semantic || (er == nil && c["embedding"] == true)
	}
	groups := r.URL.Query().Get("toolsets")
	if groups == "" {
		groups = "core,graph,files,utility,external"
	}
	writeOK := store.RoleAtLeast(p, "contributor")
	tasksOK := true
	if id != nil {
		writeOK = writeOK && id.Scopes["xrag:write"]
		tasksOK = id.Scopes["xrag:research"]
	}
	s := mcp.Server{Assets: true, AllowTasks: tasksOK, AllowWrite: writeOK, Semantic: semantic, Graph: true, Toolsets: groups,
		Execute: func(ctx context.Context, action string, b []byte) (any, error) {
			if id != nil && !id.Scopes[mcpRequiredScope(action)] {
				return nil, core.Err("INSUFFICIENT_SCOPE", mcpRequiredScope(action)+" scope required", 403)
			}
			// The complete operation owns its lease, independent of the streaming HTTP
			// connection. Closing an SSE subscriber cannot prematurely release the lease.
			c, release, e := oplock.Acquire(ctx, a.Store.Dir, false)
			if e != nil {
				return nil, e
			}
			defer release()
			return a.Execute(c, p, action, b)
		}}
	a.MCPTransport.Serve(w, r, s, p.ID)
}

// Introspection scopes and local role/KB binding are intersected. Input names are
// parsed only to issue the standard HTTP scope challenge; normal validation is
// still performed by the protocol and operation handlers.
func (a *App) checkMCPScope(raw []byte, id *mcpauth.Identity) string {
	if id == nil {
		return ""
	}
	var req struct {
		Method string `json:"method"`
		Params struct {
			Name string `json:"name"`
		} `json:"params"`
	}
	if json.Unmarshal(raw, &req) != nil || req.Method != "tools/call" {
		return ""
	}
	name := req.Params.Name
	scope := "xrag:read"
	switch name {
	case "knowledge_submit", "feedback_submit":
		scope = "xrag:write"
	case "research_submit", "recipe_run", "research_resume", "research_status", "research_cancel":
		scope = "xrag:research"
	}
	if !id.Scopes[scope] {
		return scope
	}
	return ""
}
