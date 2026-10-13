package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"time"
	"github.com/BlueSkyXN/XRAG/adapters"
	"github.com/BlueSkyXN/XRAG/core"
)

// The default hook is XRAG v1. Native Git providers are selected explicitly in
// the source policy and dispatched before the custom timestamp/signature path.
func (a *App) hook(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) != 4 || r.Method != "POST" {
		a.fail(w, core.Err("NOT_FOUND", "hook not found", 404))
		return
	}
	kb, id := parts[2], parts[3]
	if !core.ValidKB(kb) || !core.ValidKB(id) {
		a.fail(w, core.Err("INVALID_ARGUMENT", "invalid hook source", 400))
		return
	}
	src, e := a.Store.Source(r.Context(), kb, id)
	if e != nil {
		a.fail(w, e)
		return
	}
	if src.Options.Disabled {
		a.fail(w, core.Err("SOURCE_DISABLED", "source is disabled", 409))
		return
	}
	policy, e := a.Store.Policy(r.Context(), kb, id)
	if e != nil {
		a.fail(w, e)
		return
	}
	if policy.HookSecretRef == "" {
		a.fail(w, core.Err("CAPABILITY_UNAVAILABLE", "hook is not enabled for this source", 422))
		return
	}
	secret, e := adapters.Secret(policy.HookSecretRef)
	if e != nil || len(secret) < 32 {
		a.fail(w, core.Err("CAPABILITY_UNAVAILABLE", "hook secret must resolve to at least 32 characters on Server", 422))
		return
	}
	if policy.HookProvider != "" && policy.HookProvider != "custom" {
		a.nativeHook(w, r, src, policy, secret)
		return
	}
	ts := r.Header.Get("X-XRAG-Timestamp")
	event := r.Header.Get("X-XRAG-Event")
	sec, e := strconv.ParseInt(ts, 10, 64)
	now := time.Now().Unix()
	if e != nil || sec < now-300 || sec > now+300 || len(event) == 0 || len(event) > 128 || strings.ContainsAny(event, "\r\n") {
		a.fail(w, core.Err("UNAUTHENTICATED", "hook timestamp or event id invalid", 401))
		return
	}
	raw, e := body(r, 64<<10)
	if e != nil {
		a.fail(w, e)
		return
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts + "\n" + event + "\n"))
	mac.Write(raw)
	received, e := hex.DecodeString(strings.TrimPrefix(r.Header.Get("X-XRAG-Signature"), "sha256="))
	if e != nil || !hmac.Equal(received, mac.Sum(nil)) {
		a.fail(w, core.Err("UNAUTHENTICATED", "hook signature mismatch", 401))
		return
	}
	var notice struct {
		Reason string `json:"reason,omitempty"`
	}
	if e = decode(raw, &notice); e != nil {
		a.fail(w, e)
		return
	}
	j, e := a.Store.Trigger(r.Context(), kb, id, "hook", "hook:"+kb+":"+id, "hook:"+event, map[string]any{"event_id": event, "body_hash": core.TextHash(string(raw))})
	if e != nil {
		a.fail(w, e)
		return
	}
	write(w, 202, map[string]any{"id": j.ID, "state": j.State, "source": id, "effect": "enqueue full-source check only; no source content accepted"})
}
