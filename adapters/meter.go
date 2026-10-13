package adapters

import (
	"context"
	"encoding/json"
	"strings"
	"github.com/BlueSkyXN/XRAG/core"
)

type CallMeter interface {
	Reserve(context.Context, string, int, int) (string, error)
	Complete(context.Context, string, map[string]any, bool) error
}
type meterKey struct{}

func WithMeter(ctx context.Context, m CallMeter) context.Context {
	return context.WithValue(ctx, meterKey{}, m)
}
func meterStart(ctx context.Context, uri string, body any) (CallMeter, string, error) {
	m, _ := ctx.Value(meterKey{}).(CallMeter)
	if m == nil {
		return nil, "", nil
	}
	op := ""
	if strings.HasSuffix(uri, "/embeddings") {
		op = "embedding"
	}
	if strings.HasSuffix(uri, "/chat/completions") {
		op = "chat"
	}
	if strings.HasSuffix(uri, "/rerank") {
		op = "rerank"
	}
	if op == "" {
		return nil, "", nil
	}
	b, _ := json.Marshal(body)
	out := 0
	if op == "chat" {
		var obj map[string]any
		_ = json.Unmarshal(b, &obj)
		if n, ok := obj["max_tokens"].(float64); ok {
			out = int(n)
		} else {
			out = 1800
		}
	}
	id, e := m.Reserve(ctx, op, len(b), out)
	return m, id, e
}
func HasMeter(ctx context.Context) bool { _, ok := ctx.Value(meterKey{}).(CallMeter); return ok }

type requestIDKey struct{}

func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, id)
}
func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	if id == "" {
		id = core.ID("query_")
	}
	return id
}
