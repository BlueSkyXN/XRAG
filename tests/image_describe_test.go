package tests

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/BlueSkyXN/XRAG/adapters"
	"github.com/BlueSkyXN/XRAG/core"
	"github.com/BlueSkyXN/XRAG/internal/build"
	"github.com/BlueSkyXN/XRAG/internal/server"
)

// The vision chat wire format follows the OpenAI chat/completions content
// parts convention used by WeKnora: one text part plus image_url data URIs.
func TestImageChatSendsContentPartsAndDataURI(t *testing.T) {
	var got map[string]any
	var auth string
	ep := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			t.Fatal(r.URL.Path)
		}
		if e := json.NewDecoder(r.Body).Decode(&got); e != nil {
			t.Fatal(e)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"a red panel with E203"}}],"usage":{"total_tokens":9}}`))
	}))
	defer ep.Close()

	text, usage, e := adapters.ChatImage(ctx, adapters.Endpoint{URL: ep.URL, Model: "vision-x", Key: ""}, "system", "describe",
		[]adapters.ImagePart{{Data: []byte{0x89, 0x50, 0x4e, 0x47}, Mime: "image/png"}})
	must(t, e)
	if text != "a red panel with E203" {
		t.Fatal(text)
	}
	if usage == nil {
		t.Fatal("usage missing")
	}
	if auth != "" {
		t.Fatal("empty key must not send an Authorization header")
	}
	msgs := got["messages"].([]any)
	user := msgs[1].(map[string]any)
	if user["role"] != "user" {
		t.Fatal(user)
	}
	parts := user["content"].([]any)
	if len(parts) != 2 {
		t.Fatal(core.JSON(got))
	}
	if p := parts[0].(map[string]any); p["type"] != "text" || p["text"] != "describe" {
		t.Fatal(p)
	}
	iu := parts[1].(map[string]any)
	if iu["type"] != "image_url" {
		t.Fatal(iu)
	}
	url := iu["image_url"].(map[string]any)["url"].(string)
	want := "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte{0x89, 0x50, 0x4e, 0x47})
	if url != want || iu["image_url"].(map[string]any)["detail"] != "auto" {
		t.Fatal(url)
	}
	if got["model"] != "vision-x" || got["temperature"].(float64) != 0 {
		t.Fatal(core.JSON(got))
	}
}

// Plain text chat keeps the historical two-string messages shape.
func TestImageChatWithoutImagesStaysPlainText(t *testing.T) {
	var got map[string]any
	ep := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer ep.Close()
	_, _, e := adapters.Chat(ctx, adapters.Endpoint{URL: ep.URL, Model: "m"}, "s", "p")
	must(t, e)
	user := got["messages"].([]any)[1].(map[string]any)
	if _, isParts := user["content"].([]any); isParts {
		t.Fatal("text-only chat must not use content parts")
	}
	if user["content"] != "p" {
		t.Fatal(user)
	}
}

func describeSetup(t *testing.T) (productTest, *[]map[string]any) {
	h := productSetup(t)
	calls := &[]map[string]any{}
	ep := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		*calls = append(*calls, body)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"panel: E203 queue full"}}],"usage":{"total_tokens":5}}`))
	}))
	t.Cleanup(ep.Close)
	cfg := `{"embedding":null,"chat":{"url":"` + ep.URL + `","model":"vision-x"}}`
	var c adapters.Config
	must(t, json.Unmarshal([]byte(cfg), &c))
	h.app = server.New(h.s, &build.Builder{Store: h.s, Config: c}, nil)
	ts := httptest.NewServer(h.app)
	t.Cleanup(ts.Close)
	h.url = ts.URL
	return h, calls
}

func TestImageDescribeEndToEnd(t *testing.T) {
	h, calls := describeSetup(t)
	code1, b := h.request(t, "POST", "/v1/describe", map[string]any{
		"kb":     "demo",
		"prompt": "图里显示什么错误码",
		"images": []map[string]any{{"mime": "image/png", "data_base64": base64.StdEncoding.EncodeToString([]byte{1, 2, 3, 4})}},
	}, h.key)
	if code1 != 200 {
		t.Fatal(code1, string(b))
	}
	v := object(t, b)
	if v["description"] != "panel: E203 queue full" || v["images"].(float64) != 1 {
		t.Fatal(core.JSON(v))
	}
	if len(*calls) != 1 {
		t.Fatal("exactly one model call expected")
	}
	parts := (*calls)[0]["messages"].([]any)[1].(map[string]any)["content"].([]any)
	if len(parts) != 2 || parts[1].(map[string]any)["type"] != "image_url" {
		t.Fatal((*calls)[0])
	}
}

func TestImageDescribeRejectsBadInput(t *testing.T) {
	h, _ := describeSetup(t)
	cases := []struct {
		name string
		body map[string]any
		want int
	}{
		{"empty images", map[string]any{"kb": "demo", "prompt": "x", "images": []map[string]any{}}, 400},
		{"missing prompt", map[string]any{"kb": "demo", "images": []map[string]any{{"mime": "image/png", "data_base64": "AAAA"}}}, 400},
		{"bad mime", map[string]any{"kb": "demo", "prompt": "x", "images": []map[string]any{{"mime": "image/bmp", "data_base64": "AAAA"}}}, 400},
		{"bad base64", map[string]any{"kb": "demo", "prompt": "x", "images": []map[string]any{{"mime": "image/png", "data_base64": "!!!"}}}, 400},
		{"unknown field", map[string]any{"kb": "demo", "prompt": "x", "images": []map[string]any{{"mime": "image/png", "data_base64": "AAAA"}}, "extra": 1}, 400},
		{"outside scope", map[string]any{"kb": "other", "prompt": "x", "images": []map[string]any{{"mime": "image/png", "data_base64": "AAAA"}}}, 403},
	}
	for _, c := range cases {
		n, b := h.request(t, "POST", "/v1/describe", c.body, h.key)
		if n != c.want {
			t.Fatalf("%s: got %d want %d: %s", c.name, n, c.want, b)
		}
	}
}

func TestImageDescribeRequiresChatConfig(t *testing.T) {
	h := productSetup(t)
	n, b := h.request(t, "POST", "/v1/describe", map[string]any{
		"kb": "demo", "prompt": "x", "images": []map[string]any{{"mime": "image/png", "data_base64": "AAAA"}},
	}, h.key)
	if n != 422 || !strings.Contains(string(b), "CAPABILITY_UNAVAILABLE") {
		t.Fatal(n, string(b))
	}
}
