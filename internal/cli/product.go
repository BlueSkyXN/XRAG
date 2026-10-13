package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"github.com/BlueSkyXN/XRAG/adapters"
	"github.com/BlueSkyXN/XRAG/core"
)

// SaveOutput refuses to overwrite a user file. Download bytes never use stdout.
func SaveOutput(path string, reader io.Reader) error {
	dest, e := filepath.Abs(path)
	if e != nil {
		return e
	}
	f, e := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return core.Err("VERSION_CONFLICT", "output already exists or cannot be created", 409)
	}
	ok := false
	defer func() {
		f.Close()
		if !ok {
			_ = os.Remove(dest)
		}
	}()
	if _, e = io.Copy(f, reader); e != nil {
		return e
	}
	if e = f.Sync(); e != nil {
		return e
	}
	ok = true
	return nil
}
func Download(ctx context.Context, p Profile, path, dest string) error {
	if e := adapters.ValidURL(p.URL); e != nil {
		return e
	}
	token, e := adapters.Secret(p.Key)
	if e != nil {
		return e
	}
	req, e := http.NewRequestWithContext(ctx, "GET", strings.TrimRight(p.URL, "/")+path, nil)
	if e != nil {
		return e
	}
	req.Header.Set("Authorization", "Bearer "+token)
	res, e := adapters.HTTP.Do(req)
	if e != nil {
		return core.Err("BACKEND_UNAVAILABLE", "download failed", 503)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return core.Err("DOWNLOAD_FAILED", fmt.Sprintf("server returned HTTP %d; no output was saved", res.StatusCode), res.StatusCode)
	}
	return SaveOutput(dest, res.Body)
}
func editProfiles(c Config, path string, args []string, dry, confirm bool) (bool, any, error) {
	if len(args) < 2 || args[0] != "profile" {
		return false, nil, nil
	}
	switch args[1] {
	case "rename", "remove", "use":
	default:
		return false, nil, nil
	}
	if len(args) < 3 {
		return true, nil, core.Err("INVALID_ARGUMENT", "profile rename OLD NEW / remove NAME --confirm / use NAME", 400)
	}
	old := args[2]
	p, ok := c.Profiles[old]
	if !ok {
		return true, nil, core.Err("NOT_FOUND", "profile not found", 404)
	}
	switch args[1] {
	case "rename":
		if len(args) != 4 || strings.TrimSpace(args[3]) == "" {
			return true, nil, core.Err("INVALID_ARGUMENT", "profile rename OLD NEW", 400)
		}
		next := args[3]
		if _, exists := c.Profiles[next]; exists {
			return true, nil, core.Err("VERSION_CONFLICT", "target name exists", 409)
		}
		c.Profiles[next] = p
		delete(c.Profiles, old)
		if c.Default == old {
			c.Default = next
		}
	case "remove":
		if !confirm && !dry {
			return true, nil, core.Err("CONFIRM_REQUIRED", "profile remove requires --confirm; it does not delete server data", 400)
		}
		delete(c.Profiles, old)
		if c.Default == old {
			c.Default = ""
		}
	case "use":
		c.Default = old
	}
	if !dry {
		if e := save(path, c); e != nil {
			return true, nil, e
		}
	}
	return true, map[string]any{"operation": args[1], "profile": old, "profile_id": p.ID, "default_profile": c.Default, "saved": !dry, "network_calls": 0}, nil
}

// Listing profiles never dereferences secrets and masks legacy inline values.
func safeProfiles(c Config) Config {
	var out Config
	_ = json.Unmarshal([]byte(core.JSON(c)), &out)
	redact := func(v string) string {
		if v != "" && !strings.HasPrefix(v, "env:") && !strings.HasPrefix(v, "file:") {
			return "[inline secret omitted]"
		}
		return v
	}
	for name, p := range out.Profiles {
		p.Key = redact(p.Key)
		for _, ep := range []*adapters.Endpoint{p.Qdrant, p.Embedding} {
			if ep != nil {
				ep.Key = redact(ep.Key)
			}
		}
		if p.Neo4j != nil {
			p.Neo4j.Password = redact(p.Neo4j.Password)
		}
		for i := range p.Bindings {
			x := &p.Bindings[i]
			for _, ep := range []*adapters.Endpoint{x.Qdrant, x.Embedding} {
				if ep != nil {
					ep.Key = redact(ep.Key)
				}
			}
			if x.Neo4j != nil {
				x.Neo4j.Password = redact(x.Neo4j.Password)
			}
		}
		out.Profiles[name] = p
	}
	return out
}
