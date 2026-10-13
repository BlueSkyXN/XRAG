// Package mcpauth is an optional OAuth protected-resource adapter. The external
// authorization server issues/refreshes tokens; XRAG never implements a login
// server, blindly trusts JWT payloads, or forwards these tokens to data providers.
package mcpauth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
	"github.com/BlueSkyXN/XRAG/adapters"
	"github.com/BlueSkyXN/XRAG/core"
)

type Subject struct {
	Name string   `json:"name"`
	Role string   `json:"role"`
	KBs  []string `json:"kbs"`
}
type Config struct {
	Resource              string             `json:"resource"`
	Issuer                string             `json:"issuer"`
	IntrospectionEndpoint string             `json:"introspection_endpoint"`
	ClientID              string             `json:"client_id"`
	ClientSecretRef       string             `json:"client_secret_ref"`
	AllowLoopbackHTTP     bool               `json:"allow_loopback_http,omitempty"`
	Subjects              map[string]Subject `json:"subjects"`
}
type Identity struct {
	ID      string
	Subject Subject
	Scopes  map[string]bool
	Expires int64
}
type Verifier struct {
	Config Config
	Client *http.Client
}

func Read(path string) (*Verifier, error) {
	if path == "" {
		return nil, nil
	}
	b, e := os.ReadFile(path)
	if e != nil {
		return nil, e
	}
	var cfg Config
	if len(b) > 65536 {
		return nil, fmt.Errorf("MCP auth configuration exceeds 64 KiB")
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if e = d.Decode(&cfg); e != nil {
		return nil, e
	}
	if d.Decode(new(any)) != io.EOF {
		return nil, fmt.Errorf("one auth configuration required")
	}
	return New(cfg)
}
func secureURL(raw string, allowLocal bool) error {
	u, e := url.Parse(raw)
	if e != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || strings.ContainsAny(raw, "\r\n\t\" ") {
		return fmt.Errorf("invalid explicit authorization URL")
	}
	if u.Scheme == "https" {
		return nil
	}
	ip := net.ParseIP(u.Hostname())
	if u.Scheme == "http" && allowLocal && ip != nil && ip.IsLoopback() {
		return nil
	}
	return fmt.Errorf("authorization URLs require HTTPS; tests may explicitly allow numeric loopback HTTP")
}
func New(c Config) (*Verifier, error) {
	for _, u := range []string{c.Resource, c.Issuer, c.IntrospectionEndpoint} {
		if e := secureURL(u, c.AllowLoopbackHTTP); e != nil {
			return nil, e
		}
	}
	r, _ := url.Parse(c.Resource)
	if !strings.HasSuffix(r.Path, "/mcp") || c.ClientID == "" || len(c.ClientID) > 300 {
		return nil, fmt.Errorf("resource must name the public /mcp endpoint; introspection client_id required")
	}
	if !(strings.HasPrefix(c.ClientSecretRef, "env:") || strings.HasPrefix(c.ClientSecretRef, "file:")) {
		return nil, fmt.Errorf("client_secret_ref must use env: or file:")
	}
	if len(c.Subjects) == 0 || len(c.Subjects) > 1000 {
		return nil, fmt.Errorf("explicit subject bindings required (1..1000)")
	}
	for sub, g := range c.Subjects {
		if sub == "" || len(sub) > 512 || len(g.KBs) == 0 {
			return nil, fmt.Errorf("each subject must have explicit knowledge bases")
		}
		if g.Role != "reader" && g.Role != "contributor" && g.Role != "maintainer" {
			return nil, fmt.Errorf("OAuth bindings allow reader/contributor/maintainer, not global admin")
		}
		for _, kb := range g.KBs {
			if !core.ValidKB(kb) {
				return nil, fmt.Errorf("invalid knowledge-base binding")
			}
		}
	}
	return &Verifier{Config: c, Client: &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
func (v *Verifier) MetadataPath() string {
	u, _ := url.Parse(v.Config.Resource)
	return "/.well-known/oauth-protected-resource" + u.EscapedPath()
}
func (v *Verifier) MetadataURL() string {
	u, _ := url.Parse(v.Config.Resource)
	return u.Scheme + "://" + u.Host + v.MetadataPath()
}
func (v *Verifier) Metadata() map[string]any {
	return map[string]any{"resource": v.Config.Resource, "authorization_servers": []string{v.Config.Issuer}, "scopes_supported": []string{"xrag:read", "xrag:write", "xrag:research"}, "bearer_methods_supported": []string{"header"}, "resource_name": "XRAG MCP"}
}
func (v *Verifier) Challenge(scope, code string) string {
	// Values are fixed by config validation and internal constants, not token text.
	result := fmt.Sprintf("Bearer resource_metadata=%q, scope=%q", v.MetadataURL(), scope)
	if code != "" {
		result += fmt.Sprintf(", error=%q", code)
	}
	return result
}
func (v *Verifier) Verify(ctx context.Context, token string) (Identity, error) {
	var out Identity
	invalid := func() (Identity, error) {
		return out, core.Err("UNAUTHENTICATED", "access token is inactive, expired, has the wrong issuer/resource, or lacks a subject binding", 401)
	}
	if token == "" || len(token) > 16384 || strings.ContainsAny(token, "\r\n\t ") {
		return invalid()
	}
	secret, e := adapters.Secret(v.Config.ClientSecretRef)
	if e != nil {
		return out, core.Err("AUTH_PROVIDER_UNAVAILABLE", "introspection credentials unavailable", 503)
	}
	form := url.Values{"token": {token}, "token_type_hint": {"access_token"}}
	req, e := http.NewRequestWithContext(ctx, "POST", v.Config.IntrospectionEndpoint, strings.NewReader(form.Encode()))
	if e != nil {
		return out, e
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.SetBasicAuth(v.Config.ClientID, secret)
	client := v.Client
	if client == nil {
		return out, core.Err("AUTH_PROVIDER_UNAVAILABLE", "introspection client unavailable", 503)
	}
	resp, e := client.Do(req)
	if e != nil {
		return out, core.Err("AUTH_PROVIDER_UNAVAILABLE", "authorization server could not be reached", 503)
	}
	defer resp.Body.Close()
	b, e := io.ReadAll(io.LimitReader(resp.Body, 65537))
	if e != nil || len(b) > 65536 || resp.StatusCode != 200 {
		return out, core.Err("AUTH_PROVIDER_UNAVAILABLE", "authorization server returned an unusable response", 503)
	}
	var claims struct {
		Active   bool            `json:"active"`
		Subject  string          `json:"sub"`
		Issuer   string          `json:"iss"`
		Audience json.RawMessage `json:"aud"`
		Exp      int64           `json:"exp"`
		NBF      int64           `json:"nbf"`
		Scope    string          `json:"scope"`
		Type     string          `json:"token_type"`
	}
	if json.Unmarshal(b, &claims) != nil {
		return out, core.Err("AUTH_PROVIDER_UNAVAILABLE", "invalid introspection response", 503)
	}
	now := time.Now().Unix()
	if !claims.Active || claims.Exp <= now || claims.NBF > now || claims.Issuer != v.Config.Issuer || claims.Subject == "" || (claims.Type != "" && !strings.EqualFold(claims.Type, "Bearer")) {
		return invalid()
	}
	audiences := []string{}
	var single string
	if json.Unmarshal(claims.Audience, &single) == nil {
		audiences = []string{single}
	} else if json.Unmarshal(claims.Audience, &audiences) != nil {
		return invalid()
	}
	good := false
	for _, a := range audiences {
		good = good || a == v.Config.Resource
	}
	if !good {
		return invalid()
	}
	subject, ok := v.Config.Subjects[claims.Subject]
	if !ok {
		return invalid()
	}
	scopes := map[string]bool{}
	for _, s := range strings.Fields(claims.Scope) {
		scopes[s] = true
	}
	if !scopes["xrag:read"] {
		return out, core.Err("INSUFFICIENT_SCOPE", "xrag:read scope required", 403)
	}
	out = Identity{ID: "oauth_" + core.Hash([]string{v.Config.Issuer, claims.Subject})[:32], Subject: subject, Scopes: scopes, Expires: claims.Exp}
	return out, nil
}
