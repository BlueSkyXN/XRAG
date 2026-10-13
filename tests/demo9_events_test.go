package tests

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"github.com/BlueSkyXN/XRAG/core"
	"github.com/BlueSkyXN/XRAG/internal/cli"
	"github.com/BlueSkyXN/XRAG/internal/oplock"
	"github.com/BlueSkyXN/XRAG/protocols/mcp"
)

func openTaskStream9(t *testing.T, base, key, id string) (*http.Response, *bufio.Reader) {
	t.Helper()
	r, e := http.NewRequest("GET", base+"/v1/tasks/events?id="+id, nil)
	must(t, e)
	r.Header.Set("Authorization", "Bearer "+key)
	r.Header.Set("Accept", "text/event-stream")
	client := &http.Client{Timeout: 4 * time.Second}
	p, e := client.Do(r)
	must(t, e)
	if p.StatusCode != 200 {
		b, _ := io.ReadAll(p.Body)
		p.Body.Close()
		t.Fatal(p.StatusCode, string(b))
	}
	return p, bufio.NewReader(p.Body)
}
func readEvent9(t *testing.T, r *bufio.Reader) (string, map[string]any) {
	t.Helper()
	event := ""
	data := ""
	for {
		line, e := r.ReadString('\n')
		must(t, e)
		line = strings.TrimSuffix(line, "\n")
		if line == "" && data != "" {
			v := map[string]any{}
			must(t, json.Unmarshal([]byte(data), &v))
			return event, v
		}
		if strings.HasPrefix(line, "event: ") {
			event = strings.TrimPrefix(line, "event: ")
		}
		if strings.HasPrefix(line, "data: ") {
			data += strings.TrimPrefix(line, "data: ")
		}
	}
}
func TestDemo9TaskEventsStreamQueuedToTerminal(t *testing.T) {
	s, _, h, key := mcpApp9(t)
	p, e := s.Authenticate(ctx, key)
	must(t, e)
	j, e := s.Enqueue(ctx, "demo", "recipe", p.ID, "", map[string]any{})
	must(t, e)
	resp, r := openTaskStream9(t, h.URL, key, j.ID)
	defer resp.Body.Close()
	event, v := readEvent9(t, r)
	if event != "task" || v["job"].(map[string]any)["state"] != "queued" {
		t.Fatal(event, v)
	}
	claimed, ok, e := s.Claim(ctx)
	must(t, e)
	if !ok {
		t.Fatal("no claim")
	}
	must(t, s.Finish(ctx, claimed, map[string]any{"ok": true}, nil))
	event, v = readEvent9(t, r)
	if event != "task" || v["job"].(map[string]any)["state"] != "succeeded" {
		t.Fatal(event, v)
	}
	event, v = readEvent9(t, r)
	if event != "done" || v["id"] != j.ID {
		t.Fatal(event, v)
	}
}
func TestDemo9TaskEventsDisconnectDoesNotCancelDurableJob(t *testing.T) {
	s, _, h, key := mcpApp9(t)
	p, e := s.Authenticate(ctx, key)
	must(t, e)
	j, e := s.Enqueue(ctx, "demo", "recipe", p.ID, "", map[string]any{})
	must(t, e)
	resp, r := openTaskStream9(t, h.URL, key, j.ID)
	readEvent9(t, r)
	resp.Body.Close()
	got, e := s.Job(ctx, j.ID)
	must(t, e)
	if got.Cancel || got.State != "queued" {
		t.Fatal(got)
	}
}
func TestDemo9TaskEventsDoNotKeepContentLease(t *testing.T) {
	s, _, h, key := mcpApp9(t)
	p, e := s.Authenticate(ctx, key)
	must(t, e)
	j, e := s.Enqueue(ctx, "demo", "recipe", p.ID, "", map[string]any{})
	must(t, e)
	resp, r := openTaskStream9(t, h.URL, key, j.ID)
	defer resp.Body.Close()
	readEvent9(t, r)
	c, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	_, release, e := oplock.Acquire(c, s.Dir, true)
	must(t, e)
	release()
}
func TestDemo9TaskEventsRecheckRevokedIdentity(t *testing.T) {
	s, _, h, _ := mcpApp9(t)
	k, key, e := s.CreateManagedKey(ctx, "reader", "reader", []string{"demo"}, 0)
	must(t, e)
	p, e := s.Authenticate(ctx, key)
	must(t, e)
	j, e := s.Enqueue(ctx, "demo", "recipe", p.ID, "", map[string]any{})
	must(t, e)
	resp, r := openTaskStream9(t, h.URL, key, j.ID)
	defer resp.Body.Close()
	readEvent9(t, r)
	must(t, s.DisableKey(ctx, k.ID))
	event, v := readEvent9(t, r)
	if event != "error" || v["error"].(map[string]any)["code"] != "UNAUTHENTICATED" {
		t.Fatal(event, v)
	}
}
func TestDemo9TaskEventsCannotReadOtherOwner(t *testing.T) {
	s, _, h, _ := mcpApp9(t)
	_, key, e := s.CreateManagedKey(ctx, "reader", "reader", []string{"demo"}, 0)
	must(t, e)
	j, e := s.Enqueue(ctx, "demo", "recipe", "someone-else", "", map[string]any{})
	must(t, e)
	r, _ := http.NewRequest("GET", h.URL+"/v1/tasks/events?id="+j.ID, nil)
	r.Header.Set("Authorization", "Bearer "+key)
	r.Header.Set("Accept", "text/event-stream")
	resp, e := http.DefaultClient.Do(r)
	must(t, e)
	defer resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Fatal(resp.StatusCode)
	}
}
func TestDemo9TaskEventsWithdrawalSuppressesResultBody(t *testing.T) {
	s, _, h, key := mcpApp9(t)
	pub(t, s, "a.md", "# private\nsecret")
	p, e := s.Authenticate(ctx, key)
	must(t, e)
	j, e := s.Enqueue(ctx, "demo", "research", p.ID, "", map[string]any{})
	must(t, e)
	j, _, e = s.Claim(ctx)
	must(t, e)
	ep, e := s.Epoch(ctx)
	must(t, e)
	must(t, s.Finish(ctx, j, map[string]any{"withdrawal_epoch": ep, "findings": []string{"forbidden-secret"}}, nil))
	must(t, s.Withdraw(ctx, "demo", "a.md"))
	resp, r := openTaskStream9(t, h.URL, key, j.ID)
	defer resp.Body.Close()
	_, v := readEvent9(t, r)
	if strings.Contains(core.JSON(v), "forbidden-secret") || !strings.Contains(core.JSON(v), "VIEW_INVALIDATED") {
		t.Fatal(v)
	}
}
func TestDemo9TaskEventsRequireExplicitAccept(t *testing.T) {
	_, _, h, key := mcpApp9(t)
	r, _ := http.NewRequest("GET", h.URL+"/v1/tasks/events?id=x", nil)
	r.Header.Set("Authorization", "Bearer "+key)
	resp, e := http.DefaultClient.Do(r)
	must(t, e)
	resp.Body.Close()
	if resp.StatusCode != 406 {
		t.Fatal(resp.StatusCode)
	}
}
func TestDemo9CLIWatchOutputsActualTaskEvents(t *testing.T) {
	s, _, h, key := mcpApp9(t)
	p, e := s.Authenticate(ctx, key)
	must(t, e)
	j, e := s.Enqueue(ctx, "demo", "recipe", p.ID, "", map[string]any{})
	must(t, e)
	j, _, e = s.Claim(ctx)
	must(t, e)
	must(t, s.Finish(ctx, j, map[string]any{"result": "real job"}, nil))
	t.Setenv("XRAG_WATCH_KEY", key)
	var out bytes.Buffer
	must(t, cli.WatchTask(ctx, cli.Profile{Type: "server", URL: h.URL, Key: "env:XRAG_WATCH_KEY"}, j.ID, &out))
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], "real job") || !strings.Contains(lines[1], "done") {
		t.Fatal(out.String())
	}
}
func TestDemo9CLIWatchCancellationLeavesTaskQueued(t *testing.T) {
	s, _, h, key := mcpApp9(t)
	p, e := s.Authenticate(ctx, key)
	must(t, e)
	j, e := s.Enqueue(ctx, "demo", "recipe", p.ID, "", map[string]any{})
	must(t, e)
	t.Setenv("XRAG_WATCH_KEY", key)
	c, cancel := context.WithTimeout(ctx, 80*time.Millisecond)
	defer cancel()
	e = cli.WatchTask(c, cli.Profile{Type: "server", URL: h.URL, Key: "env:XRAG_WATCH_KEY"}, j.ID, io.Discard)
	if e == nil {
		t.Fatal("wait never canceled")
	}
	got, e := s.Job(ctx, j.ID)
	must(t, e)
	if got.Cancel {
		t.Fatal("watch canceled durable job")
	}
}
func TestDemo9MCPTransportRecoversExecutorPanic(t *testing.T) {
	tr := mcpTransportTest9(t, func(context.Context, string, []byte) (any, error) { panic("never leak secret stack") })
	defer tr.Close()
	status, _, b := postMCP9(t, tr.URL, "k", rpc9("tools/call", map[string]any{"name": "search_keyword", "arguments": map[string]any{"q": "x"}}), "text/event-stream")
	if status != 200 || strings.Contains(string(b), "secret stack") || !strings.Contains(string(b), "-32603") {
		t.Fatal(status, string(b))
	}
}

func mcpTransportTest9(t *testing.T, execute mcp.Executor) *httptest.Server {
	t.Helper()
	tr := &mcp.HTTPTransport{}
	sv := mcp.Server{Toolsets: "core", Execute: execute}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { tr.Serve(w, r, sv, "tester") }))
}
