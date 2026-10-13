package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"github.com/BlueSkyXN/XRAG/adapters"
	"github.com/BlueSkyXN/XRAG/core"
)

// WatchTask writes one JSON object per SSE event. The original server job is not
// cancelled on Ctrl+C, timeout, network loss or stream close.
func WatchTask(ctx context.Context, p Profile, id string, out io.Writer) error {
	if p.Type != "server" || id == "" {
		return core.Err("INVALID_ARGUMENT", "watch requires a server profile and task id", 400)
	}
	if e := adapters.ValidURL(p.URL); e != nil {
		return e
	}
	token, e := adapters.Secret(p.Key)
	if e != nil {
		return e
	}
	req, e := http.NewRequestWithContext(ctx, "GET", strings.TrimRight(p.URL, "/")+"/v1/tasks/events?id="+url.QueryEscape(id), nil)
	if e != nil {
		return e
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "text/event-stream")
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, e := client.Do(req)
	if e != nil {
		return e
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		var v struct {
			Error core.Error `json:"error"`
		}
		_ = json.NewDecoder(io.LimitReader(resp.Body, 65536)).Decode(&v)
		if v.Error.Code != "" {
			v.Error.Status = resp.StatusCode
			return &v.Error
		}
		return fmt.Errorf("task stream HTTP %d", resp.StatusCode)
	}
	if !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		return fmt.Errorf("task stream returned another content type")
	}
	scan := bufio.NewScanner(resp.Body)
	scan.Buffer(make([]byte, 4096), 2<<20)
	event := "message"
	data := ""
	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false)
	emit := func() error {
		if data == "" {
			return nil
		}
		var v any
		if e := json.Unmarshal([]byte(data), &v); e != nil {
			return e
		}
		return enc.Encode(map[string]any{"event": event, "data": v})
	}
	for scan.Scan() {
		line := scan.Text()
		if line == "" {
			if e = emit(); e != nil {
				return e
			}
			if event == "done" {
				return nil
			}
			if event == "error" {
				return fmt.Errorf("task stream ended with an error; see the preceding event")
			}
			event = "message"
			data = ""
			continue
		}
		if strings.HasPrefix(line, "event:") {
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		}
		if strings.HasPrefix(line, "data:") {
			if data != "" {
				data += "\n"
			}
			data += strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " ")
		}
	}
	if e = scan.Err(); e != nil {
		return e
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return fmt.Errorf("task stream ended before a terminal state; reconnect explicitly")
}
