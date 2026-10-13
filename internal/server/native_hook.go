package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"github.com/BlueSkyXN/XRAG/core"
	"github.com/BlueSkyXN/XRAG/internal/store"
)

// Native delivery handlers use the configured protocol, never auto-detect a
// weaker authentication scheme from attacker-controlled headers. Payload URLs
// and changed-file lists are deliberately ignored; the registered Source is
// fully re-inventoried by the ordinary build worker.
func (a *App) nativeHook(w http.ResponseWriter, r *http.Request, src store.Source, p store.SourcePolicy, secret string) {
	raw, e := body(r, 2<<20)
	if e != nil {
		a.fail(w, e)
		return
	}
	event, delivery := "", ""
	switch p.HookProvider {
	case "github":
		event = r.Header.Get("X-GitHub-Event")
		delivery = r.Header.Get("X-GitHub-Delivery")
		sig := r.Header.Get("X-Hub-Signature-256")
		if !strings.HasPrefix(sig, "sha256=") || !nativeSignature(raw, secret, strings.TrimPrefix(sig, "sha256=")) {
			a.fail(w, core.Err("UNAUTHENTICATED", "GitHub HMAC-SHA256 signature mismatch", 401))
			return
		}
	case "gitea":
		event = r.Header.Get("X-Gitea-Event")
		delivery = r.Header.Get("X-Gitea-Delivery")
		if !nativeSignature(raw, secret, r.Header.Get("X-Gitea-Signature")) {
			a.fail(w, core.Err("UNAUTHENTICATED", "Gitea HMAC-SHA256 signature mismatch", 401))
			return
		}
	case "gitlab":
		event = r.Header.Get("X-Gitlab-Event")
		delivery = r.Header.Get("Idempotency-Key")
		if delivery == "" {
			delivery = r.Header.Get("X-Gitlab-Webhook-UUID")
		}
		if !hmac.Equal([]byte(r.Header.Get("X-Gitlab-Token")), []byte(secret)) {
			a.fail(w, core.Err("UNAUTHENTICATED", "GitLab secret token mismatch", 401))
			return
		}
	default:
		a.fail(w, core.Err("CAPABILITY_UNAVAILABLE", "unconfigured webhook protocol", 422))
		return
	}
	if !regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,128}$`).MatchString(delivery) {
		a.fail(w, core.Err("INVALID_ARGUMENT", "native webhook requires a valid delivery ID", 400))
		return
	}
	allowed := event == "push" || event == "ping"
	if p.HookProvider == "gitlab" {
		allowed = event == "Push Hook" || event == "Tag Push Hook"
	}
	if !allowed {
		write(w, 202, map[string]any{"ignored": true, "reason": "event_not_subscribed", "effect": "none"})
		return
	}
	var n struct {
		Ref        string `json:"ref"`
		ObjectKind string `json:"object_kind"`
		Repository struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
		Project struct {
			PathWithNamespace string `json:"path_with_namespace"`
		} `json:"project"`
	}
	if json.Unmarshal(raw, &n) != nil {
		a.fail(w, core.Err("INVALID_ARGUMENT", "native webhook requires a JSON object", 400))
		return
	}
	repo := n.Repository.FullName
	if p.HookProvider == "gitlab" {
		repo = n.Project.PathWithNamespace
		if (event == "Push Hook" && n.ObjectKind != "push") || (event == "Tag Push Hook" && n.ObjectKind != "tag_push") {
			a.fail(w, core.Err("INVALID_ARGUMENT", "GitLab event and object_kind disagree", 400))
			return
		}
	}
	if repo != p.HookRepository {
		a.fail(w, core.Err("HOOK_SCOPE_MISMATCH", "event repository does not match configured repository", 403))
		return
	}
	if event == "ping" {
		write(w, 200, map[string]any{"accepted": true, "effect": "none", "provider": p.HookProvider})
		return
	}
	if n.Ref != p.HookRef {
		write(w, 202, map[string]any{"ignored": true, "reason": "ref_not_selected", "effect": "none"})
		return
	}
	j, e := a.Store.Trigger(r.Context(), src.KB, src.ID, "hook_"+p.HookProvider, "hook:"+src.KB+":"+src.ID, "native:"+p.HookProvider+":"+delivery, map[string]any{"event_id": delivery, "body_hash": core.TextHash(string(raw)), "provider": p.HookProvider, "repository": repo, "ref": n.Ref})
	if e != nil {
		a.fail(w, e)
		return
	}
	write(w, 202, map[string]any{"id": j.ID, "state": j.State, "source": src.ID, "provider": p.HookProvider, "effect": "enqueue registered full-source check only"})
}
func nativeSignature(raw []byte, secret, signature string) bool {
	if len(signature) != 64 {
		return false
	}
	received, e := hex.DecodeString(signature)
	if e != nil {
		return false
	}
	m := hmac.New(sha256.New, []byte(secret))
	m.Write(raw)
	return hmac.Equal(received, m.Sum(nil))
}
