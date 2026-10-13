package source

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"github.com/BlueSkyXN/XRAG/adapters"
	"github.com/BlueSkyXN/XRAG/core"
	"github.com/BlueSkyXN/XRAG/internal/store"
)

// The supported contract is the paginated knowledge/chunks API; this exports
// text chunks as Markdown. It is not an original-file downloader or a live RAG proxy.
type wkDocument struct {
	ID      string `json:"id"`
	KB      string `json:"knowledge_base_id"`
	Title   string `json:"title"`
	Updated string `json:"updated_at"`
	Status  string `json:"parse_status"`
	Enabled string `json:"enable_status"`
}
type wkChunk struct {
	ID          string `json:"id"`
	KnowledgeID string `json:"knowledge_id"`
	Content     string `json:"content"`
	Index       int    `json:"chunk_index"`
	Revision    int    `json:"content_revision"`
	Type        string `json:"chunk_type"`
	Enabled     *bool  `json:"is_enabled"`
}
type wkReader struct {
	s   store.Source
	key string
}

func (c wkReader) get(ctx context.Context, path string, q url.Values, v any) error {
	u := c.s.Root + "/api/v1" + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	r, e := http.NewRequestWithContext(ctx, "GET", u, nil)
	if e != nil {
		return e
	}
	key := c.key
	if c.s.Remote.AuthHeader == "Authorization" {
		key = "Bearer " + key
	}
	r.Header.Set(c.s.Remote.AuthHeader, key)
	b, _, e := readResponse(r, 8<<20)
	if e != nil {
		return e
	}
	if e = json.Unmarshal(b, v); e != nil {
		return core.Err("SOURCE_INVALID", "invalid WeKnora response", 422)
	}
	return nil
}
func (c wkReader) list(ctx context.Context) ([]wkDocument, error) {
	out := []wkDocument{}
	seen := map[string]bool{}
	expected := -1
	for page := 1; page <= 100; page++ {
		var v struct {
			Success  bool         `json:"success"`
			Data     []wkDocument `json:"data"`
			Total    *int         `json:"total"`
			Page     int          `json:"page"`
			PageSize int          `json:"page_size"`
		}
		e := c.get(ctx, "/knowledge-bases/"+url.PathEscape(c.s.Remote.KnowledgeBase)+"/knowledge", url.Values{"page": {strconv.Itoa(page)}, "page_size": {"100"}, "sort_by": {"created_at"}, "sort_order": {"asc"}}, &v)
		if e != nil {
			return nil, e
		}
		if !v.Success || v.Total == nil || *v.Total < 0 || *v.Total > MaxFiles || v.Page != page || v.PageSize < 1 || v.PageSize > 100 {
			return nil, core.Err("SOURCE_INVALID", "WeKnora listing contract/limit mismatch", 422)
		}
		if expected >= 0 && expected != *v.Total {
			return nil, core.Err("SOURCE_CHANGED", "WeKnora document count changed", 409)
		}
		expected = *v.Total
		for _, d := range v.Data {
			if !core.ValidKB(d.ID) || d.KB != c.s.Remote.KnowledgeBase || seen[d.ID] {
				return nil, core.Err("SOURCE_CHANGED", "invalid, duplicate or cross-KB WeKnora document", 409)
			}
			seen[d.ID] = true
			// Preserve the previous source if any document is still parsing; never turn
			// a failed/pending parse into an empty export and delete its old material.
			if d.Status != "completed" {
				return nil, core.Err("SOURCE_NOT_READY", "a WeKnora document is not completed; previous XRAG publication retained", 409)
			}
			out = append(out, d)
		}
		if len(out) == expected {
			sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
			return out, nil
		}
		if len(v.Data) == 0 || len(out) > expected {
			return nil, core.Err("SOURCE_CHANGED", "WeKnora pagination incomplete", 409)
		}
	}
	return nil, core.Err("SOURCE_LIMIT", "too many WeKnora listing pages", 422)
}
func (c wkReader) chunks(ctx context.Context, id string) ([]wkChunk, error) {
	out := []wkChunk{}
	seen := map[string]bool{}
	expected := -1
	totalBytes := 0
	for page := 1; page <= 100; page++ {
		var v struct {
			Success  bool      `json:"success"`
			Data     []wkChunk `json:"data"`
			Total    *int      `json:"total"`
			Page     int       `json:"page"`
			PageSize int       `json:"page_size"`
		}
		e := c.get(ctx, "/chunks/"+url.PathEscape(id), url.Values{"page": {strconv.Itoa(page)}, "page_size": {"100"}, "chunk_type": {"text"}}, &v)
		if e != nil {
			return nil, e
		}
		if !v.Success || v.Total == nil || *v.Total < 0 || *v.Total > 10000 || v.Page != page || v.PageSize < 1 || v.PageSize > 100 {
			return nil, core.Err("SOURCE_INVALID", "WeKnora chunks contract/limit mismatch", 422)
		}
		if expected >= 0 && expected != *v.Total {
			return nil, core.Err("SOURCE_CHANGED", "WeKnora chunk count changed", 409)
		}
		expected = *v.Total
		for _, ch := range v.Data {
			if !core.ValidKB(ch.ID) || ch.KnowledgeID != id || seen[ch.ID] || (ch.Type != "" && ch.Type != "text") || ch.Enabled == nil {
				return nil, core.Err("SOURCE_INVALID", "unexpected WeKnora chunk fields", 422)
			}
			seen[ch.ID] = true
			totalBytes += len(ch.Content)
			if totalBytes > MaxFileBytes {
				return nil, core.Err("SOURCE_LIMIT", "WeKnora exported document exceeds 5MiB", 422)
			}
			out = append(out, ch)
		}
		if len(out) == expected {
			sort.Slice(out, func(i, j int) bool {
				if out[i].Index == out[j].Index {
					return out[i].ID < out[j].ID
				}
				return out[i].Index < out[j].Index
			})
			return out, nil
		}
		if len(v.Data) == 0 || len(out) > expected {
			return nil, core.Err("SOURCE_CHANGED", "WeKnora chunk pagination incomplete", 409)
		}
	}
	return nil, core.Err("SOURCE_LIMIT", "too many WeKnora chunk pages", 422)
}
func scanWeKnora(ctx context.Context, s store.Source) (Scan, error) {
	out := Scan{Consistency: "upstream_live_export", Entries: []Entry{}}
	if s.Remote == nil {
		return out, core.Err("INVALID_ARGUMENT", "WeKnora config missing", 400)
	}
	key, e := adapters.Secret(s.Remote.APIKey)
	if e != nil {
		return out, e
	}
	c := wkReader{s: s, key: key}
	docs, e := c.list(ctx)
	if e != nil {
		return out, e
	}
	total := 0
	for _, d := range docs {
		if d.Enabled != "" && d.Enabled != "enabled" { // Disabled export is explicit absence, not a parse failure.
			if d.Enabled == "disabled" {
				continue
			}
			return out, core.Err("SOURCE_INVALID", "unknown WeKnora enable_status", 422)
		}
		chunks, e := c.chunks(ctx, d.ID)
		if e != nil {
			return out, e
		}
		var b strings.Builder
		b.WriteString("# " + strings.ReplaceAll(strings.ReplaceAll(d.Title, "\n", " "), "\r", " ") + "\n\n")
		b.WriteString(fmt.Sprintf("> WeKnora parsed-text export · knowledge=%s\n> Original KB=%s; updated=%s. Chunk boundaries and overlap are retained; this is not the original file.\n\n", d.ID, d.KB, d.Updated))
		for _, ch := range chunks {
			if !*ch.Enabled {
				continue
			}
			b.WriteString(fmt.Sprintf("<!-- weknora-chunk:%s revision:%d -->\n%s\n\n", ch.ID, ch.Revision, ch.Content))
		}
		text, e := core.Normalize([]byte(b.String()))
		if e != nil {
			return out, e
		}
		total += len(text)
		if len(text) > MaxFileBytes || total > MaxScanBytes {
			return out, core.Err("SOURCE_LIMIT", "WeKnora export exceeds source size limits", 422)
		}
		out.Entries = append(out.Entries, Entry{Path: d.ID + ".md", Body: text, Blob: core.Hash(chunks)})
	}
	after, e := c.list(ctx)
	if e != nil {
		return out, e
	}
	if core.Hash(after) != core.Hash(docs) {
		return out, core.Err("SOURCE_CHANGED", "WeKnora inventory changed during export", 409)
	}
	out.Revision = "observed:" + core.Hash(docs)
	return out, nil
}
