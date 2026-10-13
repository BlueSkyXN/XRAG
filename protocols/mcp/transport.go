package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const MaxMessageBytes = 2 << 20

// These are deliberately explicit. We do not claim the 2026 wire revision,
// Tasks, subscriptions, sampling or an official SDK implementation.
var Versions = []string{"2025-11-25", "2025-06-18", "2025-03-26"}

func supported(v string) bool {
	for _, x := range Versions {
		if x == v {
			return true
		}
	}
	return false
}
func negotiate(v string) string {
	if supported(v) {
		return v
	}
	return Versions[0]
}
func rpcError(id any, code int, message string) map[string]any {
	return map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": message}}
}
func parseRequest(b []byte) (request, map[string]any) {
	var r request
	if len(b) > MaxMessageBytes {
		return r, rpcError(nil, -32600, "message exceeds 2 MiB")
	}
	if !utf8.Valid(b) || !json.Valid(b) {
		return r, rpcError(nil, -32700, "invalid JSON")
	}
	raw := bytes.TrimSpace(b)
	if len(raw) == 0 || raw[0] != '{' {
		return r, rpcError(nil, -32600, "one JSON-RPC object is required; batches are unsupported")
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || json.Unmarshal(raw, &r) != nil || r.JSONRPC != "2.0" {
		return r, rpcError(nil, -32600, "invalid JSON-RPC request")
	}
	if len(r.ID) > 0 {
		var id any
		d := json.NewDecoder(bytes.NewReader(r.ID))
		d.UseNumber()
		if d.Decode(&id) != nil {
			return r, rpcError(nil, -32600, "invalid request id")
		}
		switch v := id.(type) {
		case string:
			if len(v) > 128 {
				return r, rpcError(nil, -32600, "request id too long")
			}
		case json.Number:
			if _, e := strconv.ParseInt(string(v), 10, 64); e != nil {
				return r, rpcError(nil, -32600, "request id must be an integer or string")
			}
		default:
			return r, rpcError(nil, -32600, "request id must be an integer or string")
		}
	}
	if r.Method == "" {
		_, res := fields["result"]
		_, er := fields["error"]
		if len(r.ID) > 0 && res != er {
			return r, nil
		} // a client response is acknowledged, never answered
		return r, rpcError(nil, -32600, "method is required")
	}
	if len(r.Params) > 0 && (len(bytes.TrimSpace(r.Params)) == 0 || bytes.TrimSpace(r.Params)[0] != '{') {
		return r, rpcError(r.ID, -32602, "parameters must be an object")
	}
	return r, nil
}

// ID keys preserve the distinction between numeric and string IDs but canonicalize
// JSON string escapes, so cancellation of "a" also matches "\\u0061".
func idKey(raw json.RawMessage) string {
	var v string
	if json.Unmarshal(raw, &v) == nil {
		return "s:" + v
	}
	return "n:" + strings.TrimSpace(string(raw))
}
func safeHandle(s Server, ctx context.Context, raw []byte) (out any) {
	defer func() {
		if recover() != nil {
			var r request
			_ = json.Unmarshal(raw, &r)
			out = rpcError(r.ID, -32603, "internal operation error")
		}
	}()
	return s.Handle(ctx, raw)
}

// HTTPTransport is stateless between calls except the bounded in-flight
// cancellation registry. It does not create sessions or retain SSE replay logs.
type HTTPTransport struct {
	mu       sync.Mutex
	calls    map[string]context.CancelFunc
	Lifetime context.Context
}

func (t *HTTPTransport) register(owner string, id json.RawMessage, cancel context.CancelFunc) (func(), bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.calls == nil {
		t.calls = map[string]context.CancelFunc{}
	}
	key := owner + "\x00" + idKey(id)
	if _, ok := t.calls[key]; ok || len(t.calls) >= 128 {
		return nil, false
	}
	t.calls[key] = cancel
	return func() { t.mu.Lock(); delete(t.calls, key); t.mu.Unlock() }, true
}
func (t *HTTPTransport) cancel(owner string, params json.RawMessage) {
	var p struct {
		ID json.RawMessage `json:"requestId"`
	}
	if json.Unmarshal(params, &p) != nil {
		return
	}
	t.mu.Lock()
	f := t.calls[owner+"\x00"+idKey(p.ID)]
	t.mu.Unlock()
	if f != nil {
		f()
	}
}
func jsonHTTP(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func (t *HTTPTransport) Serve(w http.ResponseWriter, r *http.Request, s Server, owner string) {
	if v := r.Header.Get("MCP-Protocol-Version"); v != "" && !supported(v) {
		jsonHTTP(w, 400, rpcError(nil, -32600, "unsupported MCP-Protocol-Version"))
		return
	}
	if r.Method != "POST" {
		w.Header().Set("Allow", "POST")
		jsonHTTP(w, 405, rpcError(nil, -32600, "no standalone SSE stream or sessions; use POST"))
		return
	}
	media, _, e := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if e != nil || media != "application/json" {
		jsonHTTP(w, 415, rpcError(nil, -32600, "Content-Type application/json required"))
		return
	}
	accept := r.Header.Get("Accept")
	wantsSSE := accepts(accept, "text/event-stream")
	wantsJSON := accept == "" || accepts(accept, "application/json")
	if !wantsSSE && !wantsJSON {
		jsonHTTP(w, 406, rpcError(nil, -32600, "Accept must allow JSON or SSE"))
		return
	}
	b, e := io.ReadAll(io.LimitReader(r.Body, MaxMessageBytes+1))
	if e != nil {
		jsonHTTP(w, 400, rpcError(nil, -32700, "unable to read message"))
		return
	}
	if len(b) > MaxMessageBytes {
		jsonHTTP(w, 413, rpcError(nil, -32600, "message exceeds 2 MiB"))
		return
	}
	req, invalid := parseRequest(b)
	if invalid != nil {
		jsonHTTP(w, 400, invalid)
		return
	}
	if len(req.ID) == 0 || req.Method == "" {
		if req.Method == "notifications/cancelled" {
			t.cancel(owner, req.Params)
		}
		w.WriteHeader(http.StatusAccepted)
		return
	}
	// A disconnected POST is not a cancellation message. Finish bounded work and
	// discard its response. Durable Research continues as its own database job.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 10*time.Minute)
	unregister, ok := t.register(owner, req.ID, cancel)
	if !ok {
		cancel()
		jsonHTTP(w, 409, rpcError(req.ID, -32600, "duplicate in-flight id or server busy"))
		return
	}
	result := make(chan any, 1)
	go func() {
		defer unregister()
		defer cancel()
		if t.Lifetime != nil {
			stop := context.AfterFunc(t.Lifetime, cancel)
			defer stop()
		}
		result <- safeHandle(s, ctx, b)
	}()
	if !wantsSSE {
		select {
		case v := <-result:
			jsonHTTP(w, 200, v)
		case <-r.Context().Done():
			return
		}
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(200)
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Time{})
	if _, e = io.WriteString(w, ": xrag request stream\n\n"); e != nil {
		return
	}
	_ = rc.Flush()
	beat := time.NewTicker(15 * time.Second)
	defer beat.Stop()
	for {
		select {
		case v := <-result:
			raw, er := json.Marshal(v)
			if er != nil {
				raw, _ = json.Marshal(rpcError(req.ID, -32603, "cannot encode result"))
			}
			_, _ = fmt.Fprintf(w, "event: message\ndata: %s\n\n", raw)
			_ = rc.Flush()
			return
		case <-beat.C:
			if _, e = io.WriteString(w, ": keepalive\n\n"); e != nil {
				return
			}
			if rc.Flush() != nil {
				return
			}
		case <-r.Context().Done():
			return
		}
	}
}
func accepts(header, want string) bool {
	for _, part := range strings.Split(header, ",") {
		m, p, e := mime.ParseMediaType(strings.TrimSpace(part))
		if e != nil {
			continue
		}
		if q, ok := p["q"]; ok {
			n, err := strconv.ParseFloat(q, 64)
			if err != nil || n <= 0 || n > 1 {
				continue
			}
		}
		if m == want || m == "*/*" {
			return true
		}
	}
	return false
}

// Stdio serializes writes but permits cancellation while one tool is working.
// Finite input waits for accepted calls; the parent context can cancel all work.
func (s Server) Stdio(ctx context.Context, in io.Reader, out io.Writer) error {
	scan := bufio.NewScanner(in)
	scan.Buffer(make([]byte, 4096), MaxMessageBytes)
	var writes sync.Mutex
	var wg sync.WaitGroup
	var firstErr error
	emit := func(v any) {
		if v == nil {
			return
		}
		writes.Lock()
		defer writes.Unlock()
		if e := json.NewEncoder(out).Encode(v); e != nil && firstErr == nil {
			firstErr = e
		}
	}
	transport := HTTPTransport{}
	capacity := make(chan struct{}, 8)
	for scan.Scan() {
		b := append([]byte(nil), scan.Bytes()...)
		req, invalid := parseRequest(b)
		if invalid != nil {
			emit(invalid)
			continue
		}
		if len(req.ID) == 0 || req.Method == "" {
			if req.Method == "notifications/cancelled" {
				transport.cancel("stdio", req.Params)
			}
			continue
		}
		// Setup/listing are cheap and complete before later lines are processed.
		if req.Method != "tools/call" && req.Method != "resources/read" {
			emit(safeHandle(s, ctx, b))
			continue
		}
		select {
		case capacity <- struct{}{}:
		default:
			emit(rpcError(req.ID, -32000, "maximum concurrent calls reached"))
			continue
		}
		c, cancel := context.WithTimeout(ctx, 10*time.Minute)
		done, ok := transport.register("stdio", req.ID, cancel)
		if !ok {
			cancel()
			<-capacity
			emit(rpcError(req.ID, -32600, "duplicate in-flight request id"))
			continue
		}
		wg.Add(1)
		go func(b []byte) {
			defer wg.Done()
			defer done()
			defer cancel()
			defer func() { <-capacity }()
			emit(safeHandle(s, c, b))
		}(b)
	}
	wg.Wait()
	if scan.Err() != nil {
		return scan.Err()
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return firstErr
}
