// Package core contains transport-independent XRAG operations and result semantics.
package core

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const Version = "0.10.3"

// Commit is the source revision the binary was built from. It stays "dev"
// unless the build injects it with -ldflags -X.
var Commit = "dev"

type Error struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Hint      string `json:"hint,omitempty"`
	Status    int    `json:"-"`
	Retryable bool   `json:"retryable"`
}

func (e *Error) Error() string               { return e.Code + ": " + e.Message }
func Err(code, msg string, status int) error { return &Error{Code: code, Message: msg, Status: status} }
func AsError(e error) *Error {
	if v, ok := e.(*Error); ok {
		return v
	}
	return &Error{Code: "INTERNAL_ERROR", Message: "operation failed; see server log", Status: 500}
}
func Hash(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func TextHash(text string) string { h := sha256.Sum256([]byte(text)); return hex.EncodeToString(h[:]) }
func ID(prefix string) string {
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return prefix + hex.EncodeToString(b)
}
func UUID(v any) string {
	h := Hash(v)
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}
func JSON(v any) string { b, _ := json.Marshal(v); return string(b) }

type Filter struct {
	Field string `json:"field"`
	Op    string `json:"op"`
	Value any    `json:"value"`
}
type Request struct {
	Kbs             []string `json:"kbs,omitempty"`
	Q               string   `json:"q,omitempty"`
	Name            string   `json:"name,omitempty"`
	ID              string   `json:"id,omitempty"`
	FromID          string   `json:"from_id,omitempty"`
	ToID            string   `json:"to_id,omitempty"`
	Direction       string   `json:"direction,omitempty"`
	MaxHops         int      `json:"max_hops,omitempty"`
	MaxExpansions   int      `json:"max_expansions,omitempty"`
	Depth           int      `json:"depth,omitempty"`
	Within          string   `json:"within,omitempty"`
	Limit           int      `json:"limit,omitempty"`
	MaxChars        int      `json:"max_chars,omitempty"`
	PerFile         int      `json:"per_file,omitempty"`
	Filters         []Filter `json:"filters,omitempty"`
	SaveScope       string   `json:"save_scope,omitempty"`
	Trace           bool     `json:"trace,omitempty"`
	Graph           *bool    `json:"graph,omitempty"`
	Rerank          *bool    `json:"rerank,omitempty"`
	External        *bool    `json:"external,omitempty"`
	RequireComplete bool     `json:"require_complete,omitempty"`
	Snippet         bool     `json:"snippet,omitempty"`
	Path            string   `json:"path,omitempty"`
	Ref             string   `json:"ref,omitempty"`
	Offset          int      `json:"offset,omitempty"`
	Length          int      `json:"length,omitempty"`
	ContextLines    int      `json:"context_lines,omitempty"`
	Pattern         string   `json:"pattern,omitempty"`
	ResultID        string   `json:"result_id,omitempty"`
	Cursor          string   `json:"cursor,omitempty"`
}
type Metric struct {
	Value               *int   `json:"value"`
	Accuracy            string `json:"accuracy"`
	Unit                string `json:"unit"`
	CalculationComplete bool   `json:"calculation_complete"`
	Scope               string `json:"scope"`
}

func Count(n int, unit, basis string) Metric { return Metric{&n, "exact", unit, true, basis} }
func Unknown(unit, basis string) Metric      { return Metric{nil, "unknown", unit, false, basis} }

type Coverage struct {
	ExecutionPartial bool   `json:"execution_partial"`
	DisplayTruncated bool   `json:"display_truncated"`
	Approximate      bool   `json:"approximate"`
	Consistency      string `json:"consistency"`
	ScopeBasis       string `json:"scope_basis"`
}
type Step struct {
	Stage      string   `json:"stage"`
	Count      int      `json:"count"`
	IDs        []string `json:"ids,omitempty"`
	DurationMS int64    `json:"duration_ms"`
	Note       string   `json:"note,omitempty"`
}
type Item struct {
	Kind          string             `json:"kind"`
	ID            string             `json:"id,omitempty"`
	Ref           string             `json:"ref,omitempty"`
	KB            string             `json:"kb,omitempty"`
	Path          string             `json:"path,omitempty"`
	Edition       string             `json:"edition_id,omitempty"`
	ChunkID       string             `json:"chunk_id,omitempty"`
	ChunkKey      string             `json:"chunk_key,omitempty"`
	FileHash      string             `json:"file_hash,omitempty"`
	Title         string             `json:"title,omitempty"`
	Text          string             `json:"text,omitempty"`
	Score         float64            `json:"score,omitempty"`
	Ranking       string             `json:"ranking,omitempty"`
	Routes        []string           `json:"routes,omitempty"`
	LineStart     int                `json:"line_start,omitempty"`
	LineEnd       int                `json:"line_end,omitempty"`
	Fields        map[string]any     `json:"fields,omitempty"`
	EntityID      string             `json:"entity_id,omitempty"`
	Name          string             `json:"name,omitempty"`
	Mentions      int                `json:"mentions,omitempty"`
	From          string             `json:"from,omitempty"`
	To            string             `json:"to,omitempty"`
	Relation      string             `json:"relation,omitempty"`
	Supports      int                `json:"support_count,omitempty"`
	Edges         []Support          `json:"edges,omitempty"`
	Nodes         []string           `json:"nodes,omitempty"`
	Refs          []string           `json:"support_refs,omitempty"`
	TextAvailable *bool              `json:"text_available,omitempty"`
	More          bool               `json:"content_truncated,omitempty"`
	CharStart     int                `json:"char_start,omitempty"`
	CharEnd       int                `json:"char_end,omitempty"`
	External      *ExternalReference `json:"external,omitempty"`
}
type Response struct {
	Failure       *Error         `json:"failure,omitempty"`
	SchemaVersion string         `json:"schema_version"`
	QueryID       string         `json:"query_id"`
	Action        string         `json:"action"`
	Mode          string         `json:"mode"`
	Kbs           []string       `json:"kbs"`
	Items         []Item         `json:"items"`
	Returned      int            `json:"returned"`
	Total         Metric         `json:"total"`
	Coverage      Coverage       `json:"coverage"`
	Handle        string         `json:"handle,omitempty"`
	ResultID      string         `json:"result_id,omitempty"`
	NextCursor    string         `json:"next_cursor,omitempty"`
	CacheComplete bool           `json:"cache_complete"`
	Strategy      map[string]any `json:"strategy"`
	Notes         []string       `json:"notes"`
	Trace         []Step         `json:"trace,omitempty"`
	Data          any            `json:"data,omitempty"`
	SummaryRef    string         `json:"summary_ref,omitempty"`
}
type Chunk struct {
	// MentionNames is populated only in the compact query projection. An empty
	// non-nil slice means the original was inspected and had no explicit mentions.
	MentionNames []string       `json:"query_mentions"`
	ID           string         `json:"chunk_id"`
	Key          string         `json:"chunk_key"`
	KB           string         `json:"kb"`
	Path         string         `json:"path"`
	Edition      string         `json:"edition_id"`
	FileHash     string         `json:"file_hash"`
	Title        string         `json:"title"`
	Heading      []string       `json:"heading_path"`
	Text         string         `json:"text"`
	Start        int            `json:"char_start"`
	End          int            `json:"char_end"`
	LineStart    int            `json:"line_start"`
	LineEnd      int            `json:"line_end"`
	Fields       map[string]any `json:"fields"`
	Vector       []float64      `json:"vector,omitempty"`
	Generation   string         `json:"generation,omitempty"`
	TextMissing  bool           `json:"text_missing,omitempty"`
	VectorState  string         `json:"vector_state,omitempty"`
	GraphState   string         `json:"graph_state,omitempty"`
}
type Document struct {
	SourceHash        string         `json:"source_hash,omitempty"`
	Dictionary        []string       `json:"dictionary,omitempty"`
	KB                string         `json:"kb"`
	Path              string         `json:"path"`
	Source            string         `json:"source"`
	SourceRevision    string         `json:"source_revision,omitempty"`
	SourceConsistency string         `json:"source_consistency,omitempty"`
	Edition           string         `json:"edition_id"`
	FileHash          string         `json:"file_hash"`
	Title             string         `json:"title"`
	Body              string         `json:"body"`
	Fields            map[string]any `json:"fields"`
	Chunks            []Chunk        `json:"chunks"`
	Supports          []Support      `json:"supports"`
	Settings          string         `json:"settings_hash"`
	Kind              string         `json:"kind"`
}
type Support struct {
	ID       string `json:"support_id"`
	KB       string `json:"kb"`
	Path     string `json:"path"`
	Edition  string `json:"edition_id"`
	ChunkID  string `json:"chunk_id"`
	From     string `json:"from"`
	To       string `json:"to"`
	Relation string `json:"relation"`
	Evidence string `json:"evidence,omitempty"`
	Origin   string `json:"origin"`
}
type View struct {
	Compact   bool       `json:"compact,omitempty"`
	Origin    string     `json:"origin"`
	Epoch     int64      `json:"withdrawal_epoch"`
	Documents []Document `json:"documents"`
	Mode      string     `json:"mode"`
	At        time.Time  `json:"at"`
}
type Ref struct {
	Origin     string `json:"o"`
	KB         string `json:"k"`
	Path       string `json:"p"`
	Edition    string `json:"e"`
	ChunkID    string `json:"c,omitempty"`
	RangeStart *int   `json:"s,omitempty"`
	RangeEnd   *int   `json:"t,omitempty"`
	FileHash   string `json:"h"`
	ResultID   string `json:"r,omitempty"`
	Index      int    `json:"i,omitempty"`
	Generation string `json:"g,omitempty"`
}

func EncodeRef(r Ref) string { return "xr1:" + base64.RawURLEncoding.EncodeToString([]byte(JSON(r))) }
func DecodeRef(s string) (Ref, error) {
	var r Ref
	if !strings.HasPrefix(s, "xr1:") || len(s) > 8192 {
		return r, Err("LEGACY_REF_UNVERSIONED", "expected an xr1 reference", 409)
	}
	b, e := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(s, "xr1:"))
	if e != nil {
		return r, Err("INVALID_ARGUMENT", "invalid reference", 400)
	}
	if e = json.Unmarshal(b, &r); e != nil || r.Origin == "" {
		return r, Err("INVALID_ARGUMENT", "invalid reference body", 400)
	}
	return r, nil
}
func ChunkItem(c Chunk, origin string) Item {
	available := !c.TextMissing
	return Item{TextAvailable: &available, Kind: "chunk", KB: c.KB, Path: c.Path, Edition: c.Edition, ChunkID: c.ID, ChunkKey: c.Key, FileHash: c.FileHash, Title: c.Title, Text: c.Text, Fields: c.Fields, LineStart: c.LineStart, LineEnd: c.LineEnd, Ref: EncodeRef(Ref{Origin: origin, KB: c.KB, Path: c.Path, Edition: c.Edition, ChunkID: c.ID, FileHash: c.FileHash, Generation: c.Generation})}
}

type Ranked struct {
	ID    string
	Score float64
}
type ReadData struct {
	Ref          string `json:"ref"`
	KB           string `json:"kb"`
	Path         string `json:"path"`
	Edition      string `json:"edition_id"`
	Text         string `json:"text"`
	Offset       int    `json:"offset"`
	Length       int    `json:"length"`
	TotalChars   int    `json:"total_chars"`
	NextOffset   *int   `json:"next_offset"`
	Freshness    string `json:"freshness"`
	Availability string `json:"availability"`
	FileHash     string `json:"file_hash"`
	Kind         string `json:"kind"`
	// Window is set only when context_lines is requested for a chunk reference.
	// It comes from the same pinned edition as the chunk, never from the current file.
	Window *ReadWindow `json:"window,omitempty"`
}

// ReadWindow holds whole source lines around a cited chunk. Line numbers are 1-based
// and refer to the edition's original file, including front matter.
type ReadWindow struct {
	LineStart  int    `json:"line_start"`
	LineEnd    int    `json:"line_end"`
	CiteStart  int    `json:"cite_start"`
	CiteEnd    int    `json:"cite_end"`
	TotalLines int    `json:"total_lines"`
	Text       string `json:"text"`
}
type Cache struct {
	Kind    string          `json:"kind"`
	Owner   string          `json:"owner"`
	Expires int64           `json:"expires"`
	Epoch   int64           `json:"epoch"`
	Kbs     []string        `json:"kbs"`
	Data    json.RawMessage `json:"data"`
}
type Repository interface {
	Snapshot(context.Context, []string) (View, error)
	Read(context.Context, Ref) (Document, *Chunk, string, error)
	Lexical(context.Context, View, string) ([]Ranked, error)
	PutCache(context.Context, string, Cache) error
	GetCache(context.Context, string) (Cache, error)
	Epoch(context.Context) (int64, error)
}
type Semantic interface {
	Search(context.Context, View, string) ([]Ranked, error)
	Label() string
}
type GraphReader interface {
	Supports(context.Context, View) ([]Support, error)
}
type Reranker interface {
	Rank(context.Context, string, []Item) ([]Item, error)
}

func Bool(v *bool) bool { return v != nil && *v }
func Unique(xs []string) []string {
	m := map[string]bool{}
	out := []string{}
	for _, s := range xs {
		if !m[s] {
			out = append(out, s)
			m[s] = true
		}
	}
	return out
}
func ValueString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	default:
		return fmt.Sprint(x)
	}
}

// ExternalReference pins the configured provider revision, never an arbitrary URL supplied by a caller.
type ExternalReference struct {
	BindingKB   string `json:"binding_kb,omitempty"`
	Provider    string `json:"provider"`
	Revision    string `json:"revision"`
	UpstreamRef string `json:"upstream_ref"`
	Origin      string `json:"origin,omitempty"`
}
type ExternalSearch interface {
	SearchExternal(context.Context, Request) ([]Item, error)
	ReadExternal(context.Context, ExternalReference) (any, error)
}
type MetadataReader interface {
	Discover(context.Context, []string) (map[string]any, error)
}
type EvidenceReader interface {
	ReadEvidence(context.Context, string, string, []string) (any, bool, error)
}

// OperationLeaser optionally holds a repository read lease across all query
// reads, cache publication and page hydration. It never starts a server.
type OperationLeaser interface {
	BeginOperation(context.Context) (context.Context, func(), error)
}
