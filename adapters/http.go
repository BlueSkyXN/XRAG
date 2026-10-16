// Package adapters contains optional providers. Merely loading configuration performs no I/O.
package adapters

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
	"github.com/BlueSkyXN/XRAG/core"
)

type Endpoint struct {
	URL         string `json:"url"`
	Key         string `json:"key,omitempty"`
	Model       string `json:"model,omitempty"`
	Dimension   int    `json:"dimension,omitempty"`
	Revision    string `json:"revision,omitempty"`
	DocPrefix   string `json:"doc_prefix,omitempty"`
	QueryPrefix string `json:"query_prefix,omitempty"`
}
type NeoConfig struct {
	URL      string `json:"url"`
	Database string `json:"database"`
	User     string `json:"user"`
	Password string `json:"password"`
}
type Config struct {
	Plugins           map[string]PluginSpec `json:"plugins,omitempty"`
	ParserCommand     []string              `json:"parser_command,omitempty"`
	ParserRevision    string                `json:"parser_revision,omitempty"`
	Embedding         *Endpoint             `json:"embedding,omitempty"`
	Rerank            *Endpoint             `json:"rerank,omitempty"`
	Chat              *Endpoint             `json:"chat,omitempty"`
	Qdrant            *Endpoint             `json:"qdrant,omitempty"`
	Neo4j             *NeoConfig            `json:"neo4j,omitempty"`
	Extractor         []string              `json:"extractor,omitempty"`
	ExtractorRevision string                `json:"extractor_revision,omitempty"`
	Graph             GraphOptions          `json:"graph,omitempty"`
	External          []ExternalConfig      `json:"external,omitempty"`
	OptionalVectors   bool                  `json:"optional_vectors,omitempty"`
}

func ReadConfig(path string) (Config, error) {
	c := Config{}
	if path == "" {
		return c, nil
	}
	b, e := os.ReadFile(path)
	if e != nil {
		return c, e
	}
	return ParseConfig(b)
}

// ParseConfig validates configuration without resolving secrets or doing network I/O.
func ParseConfig(b []byte) (Config, error) {
	c := Config{}
	var e error
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if e = d.Decode(&c); e != nil {
		return c, e
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return c, core.Err("INVALID_ARGUMENT", "only one configuration object is allowed", 400)
	}
	if len(c.ParserCommand) > 0 && len(c.ParserRevision) > 200 {
		return c, core.Err("INVALID_ARGUMENT", "parser revision too long", 400)
	}
	if err := ValidatePlugins(c.Plugins); err != nil {
		return c, err
	}
	if c.Qdrant != nil && c.Embedding == nil {
		return c, core.Err("INVALID_ARGUMENT", "Server Qdrant projection requires embedding configuration", 400)
	}
	for _, ep := range []*Endpoint{c.Embedding, c.Rerank, c.Chat, c.Qdrant} {
		if ep != nil {
			if e = ValidURL(ep.URL); e != nil {
				return c, e
			}
		}
	}
	if c.Embedding != nil && (c.Embedding.Dimension < 1 || c.Embedding.Dimension > 8192 || c.Embedding.Model == "") {
		return c, core.Err("INVALID_ARGUMENT", "embedding model and dimension 1..8192 are required", 400)
	}
	if c.Neo4j != nil {
		if e = ValidURL(c.Neo4j.URL); e != nil {
			return c, e
		}
		if !core.ValidKB(c.Neo4j.Database) {
			return c, core.Err("INVALID_ARGUMENT", "Neo4j database must be a simple slug", 400)
		}
	}
	if e = ValidateGraphOptions(c.Graph); e != nil {
		return c, e
	}
	ids := map[string]bool{}
	for _, p := range c.External {
		if !core.ValidKB(p.ID) || ids[p.ID] || len(p.Kbs) == 0 {
			return c, core.Err("INVALID_ARGUMENT", "external providers require unique IDs and upstream kbs", 400)
		}
		ids[p.ID] = true
		if e = ValidURL(p.URL); e != nil {
			return c, e
		}
	}
	return c, nil
}
func ValidURL(raw string) error {
	u, e := url.Parse(raw)
	if e != nil || u.Host == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") || u.RawQuery != "" || u.Fragment != "" {
		return core.Err("INVALID_ARGUMENT", "endpoint must be an http(s) base URL without credentials or query", 400)
	}
	return nil
}
func Secret(ref string) (string, error) {
	if ref == "" {
		return "", nil
	}
	var s string
	if strings.HasPrefix(ref, "env:") {
		s = os.Getenv(strings.TrimPrefix(ref, "env:"))
	} else if strings.HasPrefix(ref, "file:") {
		p := strings.TrimPrefix(ref, "file:")
		if strings.HasPrefix(p, "~/") {
			h, e := os.UserHomeDir()
			if e != nil {
				return "", e
			}
			p = filepath.Join(h, p[2:])
		}
		b, e := os.ReadFile(p)
		if e != nil {
			return "", core.Err("CREDENTIAL_UNAVAILABLE", "credential file cannot be read", 422)
		}
		s = string(b)
	} else {
		return "", core.Err("INVALID_ARGUMENT", "credential must use env: or file: reference", 400)
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return "", core.Err("CREDENTIAL_UNAVAILABLE", "credential reference is empty", 422)
	}
	return s, nil
}

var HTTP = &http.Client{Timeout: 45 * time.Second, CheckRedirect: func(r *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}

func Call(ctx context.Context, method, uri string, headers map[string]string, body, out any) error {
	var reader io.Reader
	if body != nil {
		b, e := json.Marshal(body)
		if e != nil {
			return e
		}
		reader = bytes.NewReader(b)
	}
	req, e := http.NewRequestWithContext(ctx, method, uri, reader)
	if e != nil {
		return e
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	meter, callID, me := meterStart(ctx, uri, body)
	if me != nil {
		return me
	}
	usage := map[string]any{}
	callOK := false
	if meter != nil {
		defer func() { _ = meter.Complete(context.Background(), callID, usage, callOK) }()
	}
	res, e := HTTP.Do(req)
	if e != nil {
		return core.Err("BACKEND_UNAVAILABLE", "backend request failed", 503)
	}
	defer res.Body.Close()
	b, e := io.ReadAll(io.LimitReader(res.Body, 16<<20+1))
	if e != nil {
		return e
	}
	if len(b) > 16<<20 {
		return core.Err("BACKEND_UNAVAILABLE", "backend response exceeds 16 MiB", 503)
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return core.Err("BACKEND_UNAVAILABLE", fmt.Sprintf("backend returned HTTP %d", res.StatusCode), 503)
	}
	if meter != nil {
		var envelope map[string]any
		if json.Unmarshal(b, &envelope) == nil {
			if u, ok := envelope["usage"].(map[string]any); ok {
				usage = u
			}
		}
	}
	callOK = true
	if out != nil && len(b) > 0 {
		if e = json.Unmarshal(b, out); e != nil {
			return core.Err("BACKEND_UNAVAILABLE", "backend returned invalid JSON", 503)
		}
	}
	return nil
}
func Auth(key string) (map[string]string, error) {
	s, e := Secret(key)
	if e != nil {
		return nil, e
	}
	h := map[string]string{}
	if s != "" {
		h["Authorization"] = "Bearer " + s
	}
	return h, nil
}
func (e Endpoint) SemanticID() string {
	return core.Hash([]any{e.Model, e.Revision, e.Dimension, e.DocPrefix, e.QueryPrefix, "demo-input-title-body-v1"})
}

type Embedder struct{ Config Endpoint }

func (e *Embedder) Embed(ctx context.Context, texts []string, query bool) ([][]float64, any, error) {
	in := []string{}
	prefix := e.Config.DocPrefix
	if query {
		prefix = e.Config.QueryPrefix
	}
	for _, s := range texts {
		in = append(in, prefix+s)
	}
	h, err := Auth(e.Config.Key)
	if err != nil {
		return nil, nil, err
	}
	var result struct {
		Data []struct {
			Index     int       `json:"index"`
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
		Usage map[string]any `json:"usage"`
	}
	err = Call(ctx, "POST", strings.TrimRight(e.Config.URL, "/")+"/embeddings", h, map[string]any{"model": e.Config.Model, "input": in}, &result)
	if err != nil {
		return nil, nil, err
	}
	if len(result.Data) != len(in) {
		return nil, result.Usage, core.Err("MODEL_MISMATCH", "embedding count mismatch", 409)
	}
	out := make([][]float64, len(in))
	seen := map[int]bool{}
	for _, d := range result.Data {
		if d.Index < 0 || d.Index >= len(in) || seen[d.Index] || len(d.Embedding) != e.Config.Dimension {
			return nil, result.Usage, core.Err("MODEL_MISMATCH", "embedding index or dimension mismatch", 409)
		}
		if _, err = core.Cosine(d.Embedding, d.Embedding); err != nil {
			return nil, result.Usage, err
		}
		seen[d.Index] = true
		out[d.Index] = d.Embedding
	}
	return out, result.Usage, nil
}

type ExactSemantic struct{ Embed *Embedder }

func (e *ExactSemantic) Label() string { return "cosine_exact" }
func (e *ExactSemantic) Search(ctx context.Context, v core.View, q string) ([]core.Ranked, error) {
	vec, _, er := e.Embed.Embed(ctx, []string{q}, true)
	if er != nil {
		return nil, er
	}
	rs := []core.Ranked{}
	for _, d := range v.Documents {
		for _, c := range d.Chunks {
			if len(c.Vector) == 0 {
				return nil, core.Err("MODEL_MISMATCH", "active file has no vector; build it with the configured model", 409)
			}
			if c.Generation != e.Embed.Config.SemanticID() {
				return nil, core.Err("MODEL_MISMATCH", "rebuild knowledge after changing embedding configuration", 409)
			}
			score, er := core.Cosine(vec[0], c.Vector)
			if er != nil {
				return nil, er
			}
			rs = append(rs, core.Ranked{ID: c.ID, Score: score})
		}
	}
	SortRanks(rs)
	if len(rs) > 200 {
		rs = rs[:200]
	}
	return rs, nil
}

type Reranker struct{ Config Endpoint }

func (r *Reranker) Rank(ctx context.Context, q string, items []core.Item) ([]core.Item, error) {
	if len(items) == 0 {
		return items, nil
	}
	docs := []string{}
	for _, it := range items {
		docs = append(docs, it.Text)
	}
	h, e := Auth(r.Config.Key)
	if e != nil {
		return nil, e
	}
	var res struct {
		Results []struct {
			Index int     `json:"index"`
			Score float64 `json:"relevance_score"`
		} `json:"results"`
	}
	e = Call(ctx, "POST", strings.TrimRight(r.Config.URL, "/")+"/rerank", h, map[string]any{"model": r.Config.Model, "query": q, "documents": docs, "top_n": len(docs)}, &res)
	if e != nil {
		return nil, e
	}
	if len(res.Results) != len(items) {
		return nil, core.Err("MODEL_MISMATCH", "rerank returned incomplete results", 409)
	}
	seen := map[int]bool{}
	out := []core.Item{}
	for _, x := range res.Results {
		if x.Index < 0 || x.Index >= len(items) || seen[x.Index] {
			return nil, core.Err("MODEL_MISMATCH", "invalid rerank index", 409)
		}
		seen[x.Index] = true
		it := items[x.Index]
		it.Score = x.Score
		it.Ranking = "reranker"
		out = append(out, it)
	}
	return out, nil
}
func Chat(ctx context.Context, ep Endpoint, system, prompt string) (string, any, error) {
	return ChatImage(ctx, ep, system, prompt, nil)
}

// ImagePart is one image attachment for a vision chat call. Data holds the raw
// image bytes; Mime must describe them. Only data URIs are sent upstream, so no
// caller-supplied URL reaches the model backend.
type ImagePart struct {
	Data []byte
	Mime string
}

func ChatImage(ctx context.Context, ep Endpoint, system, prompt string, images []ImagePart) (string, any, error) {
	h, e := Auth(ep.Key)
	if e != nil {
		return "", nil, e
	}
	var res struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage map[string]any `json:"usage"`
	}
	body := map[string]any{"model": ep.Model, "max_tokens": 1800, "temperature": 0}
	if len(images) == 0 {
		body["messages"] = []map[string]string{{"role": "system", "content": system}, {"role": "user", "content": prompt}}
	} else {
		parts := []map[string]any{{"type": "text", "text": prompt}}
		for _, img := range images {
			parts = append(parts, map[string]any{"type": "image_url", "image_url": map[string]any{
				"url":    "data:" + img.Mime + ";base64," + base64.StdEncoding.EncodeToString(img.Data),
				"detail": "auto",
			}})
		}
		body["messages"] = []map[string]any{{"role": "system", "content": system}, {"role": "user", "content": parts}}
	}
	e = Call(ctx, "POST", strings.TrimRight(ep.URL, "/")+"/chat/completions", h, body, &res)
	if e != nil {
		return "", res.Usage, e
	}
	if len(res.Choices) != 1 || strings.TrimSpace(res.Choices[0].Message.Content) == "" {
		return "", res.Usage, core.Err("MODEL_RESPONSE_INVALID", "chat returned no content", 502)
	}
	return res.Choices[0].Message.Content, res.Usage, nil
}
