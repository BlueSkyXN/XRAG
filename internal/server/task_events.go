package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
	"github.com/BlueSkyXN/XRAG/core"
	"github.com/BlueSkyXN/XRAG/internal/store"
)

// The event stream sends current task snapshots, not an alleged replay log.
// Reconnecting always receives the current state. It never holds a content
// lease while sleeping and never cancels the durable job when disconnected.
func (a *App) taskEvents(w http.ResponseWriter, r *http.Request, p store.Principal) bool {
	if r.URL.Path != "/v1/tasks/events" {
		return false
	}
	if r.Method != "GET" {
		w.Header().Set("Allow", "GET")
		a.fail(w, core.Err("METHOD_NOT_ALLOWED", "use GET", 405))
		return true
	}
	if !strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
		a.fail(w, core.Err("INVALID_ARGUMENT", "Accept text/event-stream required", 406))
		return true
	}
	id := r.URL.Query().Get("id")
	if id == "" || len(id) > 100 {
		a.fail(w, core.Err("INVALID_ARGUMENT", "one task id required", 400))
		return true
	}
	read := func() (store.Job, error) {
		current, e := a.principal(r)
		if e != nil {
			return store.Job{}, e
		}
		if current.ID != p.ID {
			return store.Job{}, core.Err("UNAUTHENTICATED", "identity changed", 401)
		}
		j, e := a.Store.Job(r.Context(), id)
		if e != nil {
			return j, e
		}
		if _, e = a.Store.Scope(r.Context(), current, []string{j.KB}); e != nil {
			return j, e
		}
		if j.Owner != p.ID && !store.RoleAtLeast(current, "maintainer") {
			return j, core.Err("PERMISSION_DENIED", "task belongs to another caller", 403)
		}
		return a.jobVisible(j), nil
	}
	j, e := read()
	if e != nil {
		a.fail(w, e)
		return true
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(200)
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Time{})
	send := func(event string, v any) bool {
		b, e := json.Marshal(v)
		if e != nil {
			return false
		}
		if _, e = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b); e != nil {
			return false
		}
		return rc.Flush() == nil
	}
	last := ""
	poll := time.NewTicker(500 * time.Millisecond)
	defer poll.Stop()
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	lifetime := time.NewTimer(10 * time.Minute)
	defer lifetime.Stop()
	for {
		snapshot := map[string]any{"job": j, "stream_semantics": "current_snapshot", "checkpoint_resume": "explicit_only"}
		hash := core.Hash(snapshot)
		if hash != last {
			if !send("task", snapshot) {
				return true
			}
			last = hash
		}
		if j.State != "queued" && j.State != "running" {
			send("done", map[string]any{"id": j.ID, "state": j.State})
			return true
		}
		select {
		case <-r.Context().Done():
			return true
		case <-lifetime.C:
			send("reconnect", map[string]any{"id": id})
			return true
		case <-heartbeat.C:
			if _, e = fmt.Fprint(w, ": keepalive\n\n"); e != nil || rc.Flush() != nil {
				return true
			}
		case <-poll.C:
			j, e = read()
			if e != nil {
				send("error", map[string]any{"error": core.AsError(e)})
				return true
			}
		}
	}
}
