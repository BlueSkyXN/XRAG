package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"github.com/BlueSkyXN/XRAG/adapters"
	"github.com/BlueSkyXN/XRAG/contracts"
	"github.com/BlueSkyXN/XRAG/core"
	"github.com/BlueSkyXN/XRAG/protocols/mcp"
)

type DirectBinding struct {
	KB         string              `json:"kb"`
	Collection string              `json:"physical_collection,omitempty"`
	Qdrant     *adapters.Endpoint  `json:"qdrant,omitempty"`
	Neo4j      *adapters.NeoConfig `json:"neo4j,omitempty"`
	Embedding  *adapters.Endpoint  `json:"embedding,omitempty"`
	Origin     string              `json:"origin_id,omitempty"`
}
type Profile struct {
	Bindings   []DirectBinding     `json:"bindings,omitempty"`
	ID         string              `json:"profile_id"`
	Type       string              `json:"type"`
	URL        string              `json:"url,omitempty"`
	Key        string              `json:"key,omitempty"`
	KBs        []string            `json:"default_kbs"`
	Root       string              `json:"root,omitempty"`
	Origin     string              `json:"origin_id,omitempty"`
	Collection string              `json:"physical_collection,omitempty"`
	Qdrant     *adapters.Endpoint  `json:"qdrant,omitempty"`
	Neo4j      *adapters.NeoConfig `json:"neo4j,omitempty"`
	Embedding  *adapters.Endpoint  `json:"embedding,omitempty"`
}
type Config struct {
	Default  string             `json:"default_profile"`
	Profiles map[string]Profile `json:"profiles"`
}

func reorder(args []string) ([]string, []string) {
	flags, rest := []string{}, []string{}
	bools := map[string]bool{"--wait": true, "--trace": true, "--graph": true, "--rerank": true, "--dry-run": true, "--confirm": true}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "--") {
			flags = append(flags, a)
			if !strings.Contains(a, "=") && !bools[a] && i+1 < len(args) {
				i++
				flags = append(flags, args[i])
			}
		} else {
			rest = append(rest, a)
		}
	}
	return flags, rest
}
func Load(path string) (Config, error) {
	c := Config{Profiles: map[string]Profile{}}
	b, e := os.ReadFile(path)
	if os.IsNotExist(e) {
		return c, nil
	}
	if e != nil {
		return c, e
	}
	e = json.Unmarshal(b, &c)
	if c.Profiles == nil {
		c.Profiles = map[string]Profile{}
	}
	return c, e
}
func save(path string, c Config) error {
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return e
	}
	b, _ := json.MarshalIndent(c, "", "  ")
	tmp := path + ".tmp"
	if e := os.WriteFile(tmp, b, 0600); e != nil {
		return e
	}
	return os.Rename(tmp, path)
}
func print(out io.Writer, v any) error {
	e := json.NewEncoder(out)
	e.SetIndent("", "  ")
	e.SetEscapeHTML(false)
	return e.Encode(v)
}
func Remote(ctx context.Context, p Profile, method, path string, body any, key string) (any, error) {
	if e := adapters.ValidURL(p.URL); e != nil {
		return nil, e
	}
	token, e := adapters.Secret(p.Key)
	if e != nil {
		return nil, e
	}
	var b io.Reader
	if body != nil {
		b = strings.NewReader(core.JSON(body))
	}
	req, e := http.NewRequestWithContext(ctx, method, strings.TrimRight(p.URL, "/")+path, b)
	if e != nil {
		return nil, e
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	resp, e := adapters.HTTP.Do(req)
	if e != nil {
		return nil, core.Err("BACKEND_UNAVAILABLE", "XRAG server request failed", 503)
	}
	defer resp.Body.Close()
	data, e := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if e != nil {
		return nil, e
	}
	var result any
	if e = json.Unmarshal(data, &result); e != nil {
		return nil, core.Err("BACKEND_UNAVAILABLE", "invalid server response", 503)
	}
	if resp.StatusCode >= 400 {
		var wrap struct {
			Error core.Error `json:"error"`
		}
		if json.Unmarshal(data, &wrap) == nil && wrap.Error.Code != "" {
			wrap.Error.Status = resp.StatusCode
			return nil, &wrap.Error
		}
		return nil, core.Err("BACKEND_UNAVAILABLE", fmt.Sprintf("server HTTP %d", resp.StatusCode), resp.StatusCode)
	}
	return result, nil
}
func LocalEngine(p Profile, cacheDir string) (*core.Engine, error) {
	if p.Type == "local" {
		root, e := filepath.Abs(p.Root)
		if e != nil {
			return nil, e
		}
		if p.Origin == "" || len(p.KBs) != 1 {
			return nil, core.Err("INVALID_ARGUMENT", "local profile needs fixed origin_id and one default_kbs entry", 400)
		}
		r := &adapters.Local{Root: root, Origin: p.Origin, KB: p.KBs[0], CacheDir: cacheDir}
		return &core.Engine{Repo: r, Mode: "local"}, nil
	}
	if p.Type == "direct" {
		bindings := p.Bindings
		if len(bindings) == 0 {
			if len(p.KBs) != 1 {
				return nil, core.Err("INVALID_ARGUMENT", "multi-KB direct requires explicit bindings", 400)
			}
			bindings = []DirectBinding{{KB: p.KBs[0], Collection: p.Collection, Qdrant: p.Qdrant, Neo4j: p.Neo4j, Embedding: p.Embedding, Origin: p.Origin}}
		}
		group := &adapters.DirectGroup{Readers: map[string]*adapters.Direct{}, CacheDir: cacheDir}
		hasSemantic, hasGraph := false, false
		for _, bind := range bindings {
			if !core.ValidKB(bind.KB) || group.Readers[bind.KB] != nil {
				return nil, core.Err("INVALID_ARGUMENT", "invalid/duplicate direct KB binding", 400)
			}
			if bind.Qdrant == nil && bind.Neo4j == nil {
				return nil, core.Err("INVALID_ARGUMENT", "each direct binding needs Qdrant or Neo4j", 400)
			}
			d := &adapters.Direct{KB: bind.KB, Collection: bind.Collection, Origin: bind.Origin, CacheDir: cacheDir}
			if bind.Qdrant != nil {
				if bind.Collection == "" || !core.ValidKB(bind.Collection) {
					return nil, core.Err("INVALID_ARGUMENT", "physical_collection is required", 400)
				}
				if e := adapters.ValidURL(bind.Qdrant.URL); e != nil {
					return nil, e
				}
				d.Q = &adapters.Qdrant{Endpoint: *bind.Qdrant, Origin: bind.Origin}
				if bind.Embedding != nil {
					d.Q.Embed = &adapters.Embedder{Config: *bind.Embedding}
					hasSemantic = true
				}
			}
			if bind.Neo4j != nil {
				if e := adapters.ValidURL(bind.Neo4j.URL); e != nil {
					return nil, e
				}
				d.Neo = &adapters.Neo4j{Config: *bind.Neo4j, Origin: bind.Origin}
				hasGraph = true
			}
			group.Readers[bind.KB] = d
		}
		engine := &core.Engine{Repo: group, Mode: "direct"}
		if hasSemantic {
			engine.Semantic = group
		}
		if hasGraph {
			engine.Graph = group
		}
		return engine, nil
	}
	return nil, core.Err("INVALID_ARGUMENT", "unknown profile type", 400)
}
func Main(args []string, out, errout io.Writer) error {
	home, _ := os.UserHomeDir()
	f := flag.NewFlagSet("xrag", flag.ContinueOnError)
	f.SetOutput(errout)
	config := f.String("config", filepath.Join(home, ".xrag", "client.json"), "client config")
	profile := f.String("profile", "", "profile name")
	url := f.String("server", "", "XRAG URL")
	keyfile := f.String("key-file", "", "key file path")
	local := f.String("local", "", "read-only local directory")
	kb := f.String("kb", "", "comma separated KBs")
	data := f.String("data", "", "JSON object or @file")
	recipeFile := f.String("recipe", "", "recipe JSON/YAML file; local input conversion only")
	ref := f.String("ref", "", "source ref")
	within := f.String("within", "", "scope handle")
	path := f.String("path", "", "relative path")
	root := f.String("root", "", "source root on server")
	mount := f.String("mount", "", "source mount")
	sourceKind := f.String("kind", "path", "source kind: path|git|s3|weknora")
	gitRef := f.String("git-ref", "HEAD", "git branch/tag/commit to read")
	buildMode := f.String("mode", "incremental", "build mode: incremental|full|vectors_only|graph_only")
	name := f.String("name", "", "entity/title/name")
	id := f.String("id", "", "object id")
	direction := f.String("direction", "both", "graph traversal direction: in|out|both")
	from := f.String("from", "", "path starting entity")
	to := f.String("to", "", "path ending entity")
	limit := f.Int("limit", 10, "result count")
	maxchars := f.Int("max-chars", 8000, "returned text budget")
	length := f.Int("length", 8000, "read chars")
	offset := f.Int("offset", 0, "read offset")
	hops := f.Int("hops", 2, "graph depth")
	cursor := f.String("cursor", "", "result cursor")
	resultID := f.String("result", "", "result id")
	saveScope := f.String("save-scope", "auto", "auto|required|none")
	wait := f.Bool("wait", false, "wait for server job")
	trace := f.Bool("trace", false, "actual trace")
	graph := f.Bool("graph", false, "include graph in ask")
	rerank := f.Bool("rerank", false, "enable reranker")
	dry := f.Bool("dry-run", false, "explain without execution")
	format := f.String("format", "json", "json|table")
	output := f.String("out", "", "write JSON or downloaded file to a NEW path")
	toolsets := f.String("toolsets", "core,graph,files,utility", "MCP groups")
	timeout := f.Duration("timeout", 120*time.Second, "client wall time")
	rev := f.String("revision", "", "knowledge draft revision")
	role := f.String("role", "reader", "key role")
	confirm := f.Bool("confirm", false, "confirm withdrawal")
	flags, rest := reorder(args)
	if e := f.Parse(flags); e != nil {
		return e
	}
	if len(rest) == 0 || rest[0] == "help" {
		fmt.Fprintln(out, "XRAG 0.8.0-exp.1 — Server builds, Client reads\n\n  xrag profile add team --server http://127.0.0.1:8765 --key-file .xrag/admin.key --kb demo\n  xrag search keyword E203 --profile team\n  xrag +ask 日志 --profile team --graph\n  xrag files tree --local ./examples/docs --kb demo\n  xrag build run demo --profile team --wait\n  xrag mcp --profile team\n  xrag source add --kind git --root /repo --git-ref main --id project --mount project --profile team\n  xrag source policy --id project --data @examples/source-policy.json --profile team\n  xrag eval import --data @examples/eval-demo.json --profile team\n  xrag eval run SET_ID --wait --profile team\n  xrag trace list --profile team\n  xrag jobs details JOB_ID --profile team\n\nProduct operations:\n  xrag overview --profile team\n  xrag settings show --profile team\n  xrag keys list --profile team\n  xrag feedback list --profile team\n  xrag backup create --confirm --profile team\n  xrag backup download BACKUP_ID --out snapshot.sqlite --profile team\n  xrag kb export demo --out published.zip --profile team\n  xrag maintenance preview --profile team\n  xrag profile rename OLD NEW\n\nNew lifecycle operations:\n  xrag wiki regenerate ITEM_ID --data @examples/wiki-regenerate.json --wait --profile team\n  xrag knowledge revisions ITEM_ID --profile team\n  xrag sync-target add --data @examples/sync-target-path.json --confirm --profile team\n  xrag knowledge sync ITEM_ID --data @examples/knowledge-sync.json --confirm --wait --profile team\n  xrag source add --data @examples/source-s3.json --profile team\n\nCurrent KB operations:\n  xrag kb settings demo --profile team --data @examples/kb-settings.json\n  xrag build run demo --mode vectors_only --wait --profile team\n  xrag build run demo --mode graph_only --wait --profile team\n  xrag usage show --profile team\n  xrag search external 日志 --profile team\n  xrag source edit SOURCE_ID --data @examples/source-edit.json --profile team\n\nMaintenance and evaluation workflows:\n  xrag maintenance policy --profile team\n  xrag maintenance policy --profile team --data @examples/maintenance-policy.json\n  xrag maintenance run --profile team --data @examples/maintenance-run.json --confirm\n  xrag feedback test-case FEEDBACK_ID --profile team --data @examples/feedback-test-case.json\n  xrag eval drafts --profile team\n  xrag eval approve DRAFT_ID --profile team --data @examples/eval-approval.json\n  xrag eval from-drafts --profile team --data @examples/eval-from-drafts.json\n  xrag trace diagnose QUERY_ID --profile team --data @examples/query-diagnose.json\n\nInvestigation:\n  xrag research run --profile team --data @examples/research-task.json --wait\n  xrag recipe run --profile team --data @examples/recipe-run.json --wait\n  xrag research resume JOB_ID --profile team --wait\n  xrag jobs watch JOB_ID --profile team --timeout 10m\n\nUse --data @request.json for all action arguments. Use --dry-run to inspect without a request.")
		return nil
	}
	if rest[0] == "version" {
		fmt.Fprintln(out, core.Version)
		return nil
	}
	c, e := Load(*config)
	if e != nil {
		return e
	}
	if handled, v, err := editProfiles(c, *config, rest, *dry, *confirm); handled {
		if err != nil {
			return err
		}
		return print(out, v)
	}
	if len(rest) >= 2 && rest[0] == "profile" {
		if rest[1] == "list" {
			return print(out, safeProfiles(c))
		}
		if rest[1] == "add" && len(rest) == 3 {
			if _, ok := c.Profiles[rest[2]]; ok {
				return core.Err("VERSION_CONFLICT", "profile exists; edit a copy explicitly instead of rebinding old references", 409)
			}
			p := Profile{ID: core.ID("p_"), Type: "server", URL: *url, KBs: []string{"demo"}}
			if *kb != "" {
				p.KBs = strings.Split(*kb, ",")
			}
			if *local != "" {
				p.Type = "local"
				p.Root = absPath(*local)
				p.Origin = core.ID("local_")
			} else if e = adapters.ValidURL(p.URL); e != nil {
				return e
			}
			if *keyfile != "" {
				p.Key = "file:" + absPath(*keyfile)
			} else if os.Getenv("XRAG_API_KEY") != "" {
				p.Key = "env:XRAG_API_KEY"
			}
			c.Profiles[rest[2]] = p
			if c.Default == "" {
				c.Default = rest[2]
			}
			if *dry {
				return print(out, map[string]any{"would_save": p})
			}
			if e = save(*config, c); e != nil {
				return e
			}
			return print(out, map[string]any{"saved": rest[2], "config": *config, "network_calls": 0})
		}
		return core.Err("INVALID_ARGUMENT", "profile add NAME or profile list", 400)
	}
	if rest[0] == "schema" {
		return print(out, contracts.All)
	}
	selected := *profile
	if selected == "" {
		selected = c.Default
	}
	p, found := c.Profiles[selected]
	if *profile != "" && !found {
		return core.Err("NOT_FOUND", "named profile not found", 404)
	}
	if !found {
		p = Profile{ID: "adhoc", Type: "server", URL: "http://127.0.0.1:8765", KBs: []string{"demo"}}
	}
	if *url != "" && *local != "" {
		return core.Err("INVALID_ARGUMENT", "choose --server OR --local", 400)
	}
	if *url != "" {
		p.Type = "server"
		p.URL = *url
	}
	if *local != "" {
		p.Type = "local"
		p.Root = absPath(*local)
		p.Origin = "local_" + core.Hash(p.Root)[:24]
		p.ID = p.Origin
		p.KBs = []string{"demo"}
	}
	if *kb != "" {
		p.KBs = strings.Split(*kb, ",")
	}
	if len(p.KBs) == 0 && p.Type == "direct" {
		for _, b := range p.Bindings {
			p.KBs = append(p.KBs, b.KB)
		}
	}
	if len(p.KBs) == 0 {
		p.KBs = []string{"demo"}
	}
	if *keyfile != "" {
		p.Key = "file:" + absPath(*keyfile)
	} else if p.Key == "" && os.Getenv("XRAG_API_KEY") != "" {
		p.Key = "env:XRAG_API_KEY"
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	cacheDir := filepath.Join(filepath.Dir(*config), "results", core.Hash([]any{p.ID, p.Type, p.URL, p.Collection})[:24])
	var engine *core.Engine
	execAction := func(ctx context.Context, action string, b []byte) (any, error) {
		if wa, ok := contracts.FindWrite(action); ok {
			if p.Type != "server" {
				return nil, core.Err("MODE_UNSUPPORTED", "write tools require a Server profile", 422)
			}
			var payload map[string]any
			if json.Unmarshal(b, &payload) != nil {
				return nil, core.Err("INVALID_ARGUMENT", "invalid JSON", 400)
			}
			if _, ok := payload["kb"]; !ok && len(p.KBs) == 1 {
				payload["kb"] = p.KBs[0]
			}
			return Remote(ctx, p, "POST", wa.Path, payload, "")
		}
		if task, ok := contracts.FindTask(action); ok {
			if p.Type != "server" {
				return nil, core.Err("MODE_UNSUPPORTED", "tasks execute on Server", 422)
			}
			var payload map[string]any
			if err := json.Unmarshal(b, &payload); err != nil {
				return nil, err
			}
			if action == "research.submit" || action == "recipe.run" {
				if _, ok := payload["kb"]; !ok && len(p.KBs) == 1 {
					payload["kb"] = p.KBs[0]
				}
			}
			return Remote(ctx, p, "POST", task.Path, payload, "")
		}
		a, ok := contracts.Find(action)
		if !ok {
			return nil, core.Err("MODE_UNSUPPORTED", "unknown action", 422)
		}
		req, e := contracts.Decode(a, b)
		if e != nil {
			return nil, e
		}
		if req.Kbs == nil {
			req.Kbs = p.KBs
		}
		if p.Type == "server" {
			var payload map[string]any
			_ = json.Unmarshal(b, &payload)
			if _, ok := payload["kbs"]; !ok {
				payload["kbs"] = p.KBs
			}
			return Remote(ctx, p, "POST", a.Path, payload, "")
		}
		if engine == nil {
			engine, e = LocalEngine(p, cacheDir)
			if e != nil {
				return nil, e
			}
		}
		if p.Type == "direct" && (action == "files.tree" || action == "files.grep") {
			return nil, core.Err("MODE_UNSUPPORTED", "direct Demo has chunks only; this operation needs the Server", 422)
		}
		return engine.Run(ctx, action, req, p.ID)
	}
	if rest[0] == "mcp" {
		if *dry {
			return print(out, map[string]any{"mode": p.Type, "toolsets": *toolsets})
		}
		m := mcp.Server{Assets: true, AllowTasks: p.Type == "server", AllowWrite: p.Type == "server", Execute: execAction, Semantic: p.Embedding != nil, Graph: p.Type == "server" || p.Neo4j != nil, Toolsets: *toolsets}
		if p.Type != "server" {
			engine, e = LocalEngine(p, cacheDir)
			if e != nil {
				return e
			}
			m.Semantic = engine.Semantic != nil
			m.Graph = engine.Graph != nil
		}
		if p.Type == "server" {
			v, e := Remote(ctx, p, "GET", "/v1/system", nil, "")
			if e != nil {
				return e
			}
			m.Semantic = v.(map[string]any)["embedding"] == true
		}
		return m.Stdio(context.Background(), os.Stdin, out)
	}
	opts := map[string]any{}
	if *data != "" {
		b := []byte(*data)
		if strings.HasPrefix(*data, "@") {
			b, e = os.ReadFile((*data)[1:])
			if e != nil {
				return e
			}
		}
		if e = json.Unmarshal(b, &opts); e != nil {
			return e
		}
	}
	visited := map[string]bool{}
	f.Visit(func(fl *flag.Flag) { visited[fl.Name] = true })
	if visited["kb"] {
		opts["kbs"] = p.KBs
	}
	command := rest[0]
	used := 1
	if len(rest) >= 2 {
		if _, ok := contracts.Find(rest[0] + " " + rest[1]); ok {
			command = rest[0] + " " + rest[1]
			used = 2
		}
	}
	if a, ok := contracts.Find(command); ok {
		args := rest[used:]
		set := func(k string, v any, flag string) {
			if visited[flag] {
				opts[k] = v
			}
		}
		set("limit", *limit, "limit")
		set("max_chars", *maxchars, "max-chars")
		set("length", *length, "length")
		set("offset", *offset, "offset")
		set("trace", *trace, "trace")
		set("graph", *graph, "graph")
		set("rerank", *rerank, "rerank")
		set("save_scope", *saveScope, "save-scope")
		set("within", *within, "within")
		set("path", *path, "path")
		set("ref", *ref, "ref")
		set("name", *name, "name")
		set("id", *id, "id")
		set("direction", *direction, "direction")
		set("from_id", *from, "from")
		set("to_id", *to, "to")
		if a.Action == "graph.path" {
			set("max_hops", *hops, "hops")
		} else {
			set("hops", *hops, "hops")
		}
		set("cursor", *cursor, "cursor")
		set("result_id", *resultID, "result")
		if len(args) > 0 {
			switch a.Action {
			case "search.keyword", "search.semantic", "search.external", "+ask":
				opts["q"] = strings.Join(args, " ")
			case "files.grep":
				opts["pattern"] = strings.Join(args, " ")
			case "graph.entity", "graph.neighbors", "graph.chunks", "+context":
				opts["name"] = strings.Join(args, " ")
			case "handles.describe":
				opts["id"] = args[0]
			case "files.read":
				opts["ref"] = args[0]
			}
		}
		if *dry {
			return print(out, map[string]any{"action": a.Action, "mode": p.Type, "request": opts, "will_execute": false, "endpoint": p.URL, "local_root": p.Root})
		}
		v, e := execAction(ctx, a.Action, []byte(core.JSON(opts)))
		if e != nil {
			return e
		}
		if *format == "table" {
			return table(out, v)
		}
		return print(out, v)
	}
	if p.Type != "server" {
		return core.Err("MODE_UNSUPPORTED", "management is only performed by Server", 422)
	}
	method, route := "POST", ""
	binaryDownload := false
	payload := any(opts)
	cmd := strings.Join(rest[:min(2, len(rest))], " ")
	target := p.KBs[0]
	if cmd != "keys create" {
		delete(opts, "kbs")
	}
	switch cmd {
	case "jobs watch", "research watch":
		if len(rest) < 3 {
			return core.Err("INVALID_ARGUMENT", "task ID required", 400)
		}
		if *dry {
			return print(out, map[string]any{"mode": "server", "operation": "task.events", "id": rest[2], "will_execute": false})
		}
		return WatchTask(ctx, p, rest[2], out)
	case "research resume", "recipe resume":
		if len(rest) < 3 {
			return core.Err("INVALID_ARGUMENT", "task ID required", 400)
		}
		route = "/v1/tasks/resume"
		method = "POST"
		opts["id"] = rest[2]
	case "research run", "recipe run":
		if *recipeFile != "" {
			rr, err := LoadRecipe(ctx, *recipeFile)
			if err != nil {
				return err
			}
			opts["recipe"] = rr
		}
		if cmd == "research run" {
			route = "/v1/research"
		} else {
			route = "/v1/recipes/run"
		}
		opts["kb"] = target
		payload = opts
	case "kb show", "kb rename", "kb remove":
		if len(rest) != 3 {
			return fmt.Errorf("%s KB_ID", cmd)
		}
		route = "/v1/kbs/" + rest[2]
		method = "GET"
		if cmd == "kb rename" {
			method = "PATCH"
			payload = map[string]any{"name": *name}
		}
		if cmd == "kb remove" {
			if !*confirm {
				return fmt.Errorf("kb remove requires --confirm")
			}
			method = "DELETE"
			payload = map[string]any{"confirm": true}
		}
	case "kb settings":
		if len(rest) > 2 {
			target = rest[2]
		}
		route = "/v1/kbs/" + target + "/settings"
		method = "GET"
		if *data != "" {
			method = "PUT"
			payload = opts
		}
	case "usage show":
		route = "/v1/kbs/" + target + "/usage"
		method = "GET"
	case "source edit", "source remove", "source show":
		sid := *id
		if len(rest) > 2 {
			sid = rest[2]
		}
		if sid == "" {
			return fmt.Errorf("source id required")
		}
		route = "/v1/kbs/" + target + "/sources/" + sid
		method = "GET"
		if cmd == "source edit" {
			method = "PATCH"
		}
		if cmd == "source remove" {
			if !*confirm {
				return fmt.Errorf("source remove requires --confirm")
			}
			method = "DELETE"
			opts["confirm"] = true
		}
	case "overview":
		method = "GET"
		route = "/v1/overview"
	case "settings show":
		method = "GET"
		route = "/v1/settings/providers"
	case "settings save":
		if *data == "" {
			return fmt.Errorf("settings save --data @settings.json (expected_revision and config)")
		}
		method = "PUT"
		route = "/v1/settings/providers"
	case "keys list":
		method = "GET"
		route = "/v1/keys"
	case "keys remove":
		if len(rest) != 3 || !*confirm {
			return fmt.Errorf("keys remove KEY_ID --confirm")
		}
		method = "DELETE"
		route = "/v1/keys/" + rest[2]
		payload = map[string]any{"confirm": true}
	case "keys rename":
		if len(rest) != 3 || *name == "" {
			return fmt.Errorf("keys rename KEY_ID --name NAME")
		}
		method = "PATCH"
		route = "/v1/keys/" + rest[2]
		payload = map[string]any{"name": *name}
	case "feedback list":
		method = "GET"
		route = "/v1/feedback?kb=" + target + "&limit=" + fmt.Sprint(*limit) + "&offset=" + fmt.Sprint(*offset)
	case "feedback resolve":
		if len(rest) != 3 || *data == "" {
			return fmt.Errorf("feedback resolve ID --data @resolution.json")
		}
		method = "PATCH"
		route = "/v1/feedback/" + rest[2]
	case "backup list":
		method = "GET"
		route = "/v1/backups"
	case "backup create":
		if !*confirm && !*dry {
			return fmt.Errorf("backup create --confirm (database snapshot, not source folders)")
		}
		route = "/v1/backups"
		payload = map[string]any{"confirm": *confirm}
	case "backup remove":
		if len(rest) != 3 || !*confirm {
			return fmt.Errorf("backup remove ID --confirm")
		}
		method = "DELETE"
		route = "/v1/backups/" + rest[2]
		payload = map[string]any{"confirm": true}
	case "backup download":
		if len(rest) != 3 || *output == "" {
			return fmt.Errorf("backup download ID --out NEW_FILE.sqlite")
		}
		method = "GET"
		route = "/v1/backups/" + rest[2] + "/download"
		binaryDownload = true
	case "kb export":
		if len(rest) >= 3 {
			target = rest[2]
		}
		if *output == "" {
			return fmt.Errorf("kb export KB --out NEW_FILE.zip")
		}
		method = "GET"
		route = "/v1/kbs/" + target + "/export"
		binaryDownload = true
	case "maintenance policy":
		method = "GET"
		route = "/v1/maintenance/policy"
		if *data != "" {
			method = "PUT"
		}
	case "maintenance runs":
		method = "GET"
		route = "/v1/maintenance/runs"
	case "maintenance content":
		method = "GET"
		route = "/v1/maintenance/content"
	case "maintenance records":
		method = "GET"
		route = "/v1/maintenance/records"
	case "maintenance run":
		if !*confirm && !*dry {
			return fmt.Errorf("maintenance run --data @request.json --confirm")
		}
		route = "/v1/maintenance/runs"
		opts["confirm"] = *confirm
	case "feedback test-case":
		if len(rest) != 3 || *data == "" {
			return fmt.Errorf("feedback test-case ID --data '{\"question\":\"...\"}'")
		}
		route = "/v1/feedback/" + rest[2] + "/test-case"
	case "eval drafts":
		method = "GET"
		route = "/v1/eval/drafts?kb=" + target
	case "eval approve":
		if len(rest) != 3 || *data == "" {
			return fmt.Errorf("eval approve DRAFT_ID --data @approval.json")
		}
		route = "/v1/eval/drafts/" + rest[2] + "/approve"
	case "eval from-drafts":
		if *data == "" {
			return fmt.Errorf("eval from-drafts --data @dataset.json")
		}
		route = "/v1/eval/sets/from-drafts"
		if opts["kb"] == nil {
			opts["kb"] = target
		}
	case "eval export":
		if len(rest) != 3 {
			return fmt.Errorf("eval export SET_ID [--out FILE.json]")
		}
		method = "GET"
		route = "/v1/eval/sets/" + rest[2]
	case "trace diagnose":
		if len(rest) != 3 || *data == "" {
			return fmt.Errorf("trace diagnose QUERY_ID --data @target.json")
		}
		route = "/v1/queries/" + rest[2] + "/diagnose"
		if opts["kb"] == nil {
			opts["kb"] = target
		}
	case "maintenance preview":
		method = "GET"
		route = "/v1/maintenance/cache"
	case "maintenance clean":
		if !*confirm && !*dry {
			return fmt.Errorf("maintenance clean --confirm")
		}
		route = "/v1/maintenance/cache"
		payload = map[string]any{"confirm": *confirm}
	case "status":
		method = "GET"
		route = "/v1/system"
		payload = nil
	case "kb list":
		method = "GET"
		route = "/v1/kbs"
	case "kb create":
		if len(rest) < 3 {
			return fmt.Errorf("kb create ID")
		}
		route = "/v1/kbs"
		payload = map[string]any{"id": rest[2], "name": *name}
	case "build run", "build estimate":
		if len(rest) >= 3 {
			target = rest[2]
		}
		route = "/v1/kbs/" + target + "/builds"
		payload = map[string]any{"mode": *buildMode}
		if cmd == "build estimate" {
			route = "/v1/kbs/" + target + "/build-estimates"
			payload = map[string]any{}
		}
	case "source add":
		route = "/v1/kbs/" + target + "/sources"
		if *data != "" {
			payload = opts
		} else {
			payload = map[string]any{"id": *id, "kind": *sourceKind, "root": *root, "mount": *mount}
		}
		if *sourceKind == "git" && *data == "" {
			payload.(map[string]any)["git_ref"] = *gitRef
		}
	case "source list":
		method = "GET"
		route = "/v1/kbs/" + target + "/sources"
	case "source policy":
		if *id == "" {
			return fmt.Errorf("source policy --id SOURCE --data @policy.json")
		}
		route = "/v1/kbs/" + target + "/sources/" + *id + "/policy"
		method = "GET"
		payload = nil
		if *data != "" {
			method = "PUT"
			payload = opts
		}
	case "eval report":
		if len(rest) < 3 {
			return fmt.Errorf("eval report JOB_ID")
		}
		method = "GET"
		route = "/v1/jobs/" + rest[2]
	case "eval import":
		if *data == "" {
			return fmt.Errorf("eval import --data @dataset.json")
		}
		route = "/v1/eval/sets"
		opts["kb"] = target
		delete(opts, "kbs")
	case "eval list":
		method = "GET"
		route = "/v1/eval/sets"
	case "eval run":
		route = "/v1/eval/runs"
		if len(rest) > 2 {
			opts["set_id"] = rest[2]
		}
		delete(opts, "kbs")
	case "trace list":
		method = "GET"
		route = "/v1/queries"
	case "trace show":
		if len(rest) < 3 {
			return fmt.Errorf("trace show QUERY_ID")
		}
		method = "GET"
		route = "/v1/queries/" + rest[2]
	case "source upload":
		if len(rest) < 3 {
			return fmt.Errorf("source upload FILE")
		}
		b, e := os.ReadFile(rest[2])
		if e != nil {
			return e
		}
		filePath := *path
		if filePath == "" {
			filePath = filepath.Base(rest[2])
		}
		route = "/v1/kbs/" + target + "/upload"
		payload = map[string]any{"path": filePath, "body": string(b)}
	case "knowledge list":
		method = "GET"
		route = "/v1/kbs/" + target + "/knowledge"
	case "knowledge create":
		route = "/v1/kbs/" + target + "/knowledge"
	case "knowledge publish":
		if len(rest) >= 3 {
			*id = rest[2]
		}
		route = "/v1/knowledge/" + *id + "/publish"
		payload = map[string]any{"revision": *rev}
	case "wiki regenerate":
		if len(rest) != 3 {
			return fmt.Errorf("wiki regenerate ITEM_ID --data '{\"expected_revision\":\"...\"}' --wait")
		}
		route = "/v1/knowledge/" + rest[2] + "/regenerate"
	case "knowledge revisions":
		if len(rest) != 3 {
			return fmt.Errorf("knowledge revisions ITEM_ID")
		}
		method = "GET"
		route = "/v1/knowledge/" + rest[2] + "/revisions"
	case "knowledge sync":
		if len(rest) != 3 || !*confirm {
			return fmt.Errorf("knowledge sync ITEM_ID --data @sync.json --confirm --wait")
		}
		route = "/v1/knowledge/" + rest[2] + "/sync"
		opts["confirm"] = true
	case "knowledge sync-status":
		if len(rest) != 3 {
			return fmt.Errorf("knowledge sync-status ITEM_ID")
		}
		method = "GET"
		route = "/v1/knowledge/" + rest[2] + "/sync"
	case "knowledge archive":
		if len(rest) != 3 || !*confirm {
			return fmt.Errorf("knowledge archive ITEM_ID --data '{\"revision\":\"...\"}' --confirm")
		}
		route = "/v1/knowledge/" + rest[2] + "/archive"
		opts["confirm"] = true
	case "sync-target list":
		method = "GET"
		route = "/v1/kbs/" + target + "/sync-targets"
	case "sync-target add":
		if *data == "" || !*confirm {
			return fmt.Errorf("sync-target add --data @target.json --confirm")
		}
		route = "/v1/kbs/" + target + "/sync-targets"
		opts["confirm"] = true
	case "wiki generate":
		route = "/v1/kbs/" + target + "/wiki"
		if len(rest) > 2 {
			opts["topic"] = strings.Join(rest[2:], " ")
		}
		payload = opts
	case "jobs list":
		method = "GET"
		route = "/v1/jobs"
	case "jobs show", "jobs cancel", "jobs details":
		if len(rest) < 3 {
			return fmt.Errorf("jobs show/cancel ID")
		}
		route = "/v1/jobs/" + rest[2]
		method = "GET"
		if cmd == "jobs details" {
			route += "/details"
		}
		if cmd == "jobs cancel" {
			method = "POST"
			route += "/cancel"
		}
	case "keys create":
		route = "/v1/keys"
		if _, ok := opts["name"]; !ok || visited["name"] {
			opts["name"] = *name
		}
		if _, ok := opts["role"]; !ok || visited["role"] {
			opts["role"] = *role
		}
		if _, ok := opts["kbs"]; !ok {
			opts["kbs"] = p.KBs
		}
		payload = opts
	case "files withdraw":
		route = "/v1/kbs/" + target + "/withdraw"
		payload = map[string]any{"path": *path, "confirm": *confirm}
	case "feedback":
		route = "/v1/feedback"
	default:
		return core.Err("INVALID_ARGUMENT", "unknown command; use xrag help", 400)
	}
	if *dry {
		return print(out, map[string]any{"method": method, "route": route, "payload": payload, "will_execute": false})
	}
	if binaryDownload {
		if e := Download(ctx, p, route, *output); e != nil {
			return e
		}
		return print(out, map[string]any{"saved": *output, "route": route})
	}
	key := ""
	if method == "POST" || method == "PUT" {
		key = core.ID("client_")
	}
	v, e := Remote(ctx, p, method, route, payload, key)
	if e != nil {
		return e
	}
	if *wait {
		if m, ok := v.(map[string]any); ok && (m["state"] == "queued" || m["state"] == "running") {
			jid, _ := m["id"].(string)
			for {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(300 * time.Millisecond):
				}
				v, e = Remote(ctx, p, "GET", "/v1/jobs/"+jid, nil, "")
				if e != nil {
					return e
				}
				m = v.(map[string]any)
				state, _ := m["state"].(string)
				if state != "queued" && state != "running" {
					if state != "succeeded" && state != "partial" {
						_ = print(out, v)
						return core.Err("JOB_FAILED", "server job ended: "+state, 500)
					}
					break
				}
			}
		}
	}
	if *output != "" {
		var buf bytes.Buffer
		if e = print(&buf, v); e != nil {
			return e
		}
		if e = SaveOutput(*output, &buf); e != nil {
			return e
		}
		return print(out, map[string]any{"saved": *output})
	}
	return print(out, v)
}
func absPath(p string) string {
	a, e := filepath.Abs(p)
	if e != nil {
		return p
	}
	return a
}
func table(out io.Writer, v any) error {
	b, _ := json.Marshal(v)
	var r core.Response
	if e := json.NewDecoder(bytes.NewReader(b)).Decode(&r); e != nil {
		return e
	}
	total := "unknown"
	if r.Total.Value != nil {
		total = fmt.Sprint(*r.Total.Value)
	}
	fmt.Fprintf(out, "%s / %s | returned %d | total %s %s (%s)\n", r.Action, r.Mode, r.Returned, total, r.Total.Unit, r.Total.Accuracy)
	for i, it := range r.Items {
		fmt.Fprintf(out, "%d. %s %s\n   %s\n", i+1, it.Path, it.Name, it.Ref)
	}
	return nil
}
