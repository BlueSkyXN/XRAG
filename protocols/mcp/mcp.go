// Package mcp implements the explicitly advertised MCP operations.
// HTTP transport and resource-server authorization are separate from core operations.
package mcp

import (
	"context"
	"encoding/json"
	"github.com/BlueSkyXN/XRAG/contracts"
	"github.com/BlueSkyXN/XRAG/core"
	"strings"
)

type Executor func(context.Context, string, []byte) (any, error)
type Server struct {
	Execute    Executor
	AllowWrite bool
	AllowTasks bool
	AllowImage bool
	Semantic   bool
	Graph      bool
	Toolsets   string
	// Assets enables static usage resources, reference reads and retrieval prompts.
	Assets bool
}
type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

func (s Server) Tools() []map[string]any {
	ts := []map[string]any{}
	groups := "," + s.Toolsets + ","
	if s.Toolsets == "" {
		groups = ",core,"
	}
	for _, a := range contracts.All {
		if !strings.Contains(groups, ","+a.Group+",") {
			continue
		}
		if a.Action == "search.semantic" && !s.Semantic {
			continue
		}
		if (strings.HasPrefix(a.Action, "graph.") || a.Action == "+context") && !s.Graph {
			continue
		}
		output := contracts.Output()
		if a.Action == "image.describe" {
			if !s.AllowImage {
				continue
			}
			output = contracts.ImageOutput()
		}
		ts = append(ts, map[string]any{"name": a.Tool, "description": a.Description, "inputSchema": contracts.Input(a), "outputSchema": output, "annotations": map[string]any{"readOnlyHint": true, "openWorldHint": true}})
	}
	if s.AllowWrite && strings.Contains(groups, ",write,") {
		for _, a := range contracts.WriteActions {
			ts = append(ts, map[string]any{"name": a.Tool, "description": a.Description, "inputSchema": contracts.Input(a), "outputSchema": contracts.WriteOutput(a), "annotations": map[string]any{"readOnlyHint": false, "destructiveHint": false, "openWorldHint": false}})
		}
	}
	if s.AllowTasks && strings.Contains(groups, ",research,") {
		for _, a := range contracts.TaskActions {
			ts = append(ts, map[string]any{"name": a.Tool, "description": a.Description, "inputSchema": contracts.Input(a), "outputSchema": contracts.TaskOutput(a), "annotations": map[string]any{"readOnlyHint": a.Action == "task.status", "destructiveHint": false, "openWorldHint": true}})
		}
	}
	return ts
}
func (s Server) Handle(ctx context.Context, b []byte) any {
	r, err := parseRequest(b)
	if err != nil {
		return err
	}
	if r.Method == "" || len(r.ID) == 0 {
		return nil
	}
	out := map[string]any{"jsonrpc": "2.0", "id": r.ID}
	rpcerr := func(code int, msg string) any {
		out["error"] = map[string]any{"code": code, "message": msg}
		return out
	}
	switch r.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		if len(r.Params) > 0 && json.Unmarshal(r.Params, &p) != nil {
			return rpcerr(-32602, "invalid initialization parameters")
		}
		version := negotiate(p.ProtocolVersion)
		caps := map[string]any{"tools": map[string]any{}}
		if s.Assets {
			caps["resources"] = map[string]any{}
			caps["prompts"] = map[string]any{}
		}
		out["result"] = map[string]any{"protocolVersion": version, "serverInfo": map[string]any{"name": "xrag", "version": core.Version}, "capabilities": caps, "instructions": "Use tools for actions. Research returns a durable XRAG job; disconnecting does not cancel it. Resources and prompts contain usage guidance, not extra privileges."}

	case "ping":
		out["result"] = map[string]any{}
	case "tools/list":
		if err := noCursor(r.Params); err != nil {
			return rpcerr(-32602, err.Error())
		}
		out["result"] = map[string]any{"tools": s.Tools()}
	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if json.Unmarshal(r.Params, &p) != nil {
			return rpcerr(-32602, "invalid call parameters")
		}
		a, ok := contracts.Find(p.Name)
		if !ok && s.AllowWrite {
			a, ok = contracts.FindWrite(p.Name)
		}
		if !ok && s.AllowTasks {
			a, ok = contracts.FindTask(p.Name)
		}
		if !ok {
			return rpcerr(-32602, "unknown tool")
		}
		available := false
		for _, t := range s.Tools() {
			if t["name"] == p.Name {
				available = true
			}
		}
		if !available {
			return rpcerr(-32602, "tool unavailable in selected toolsets")
		}
		if len(p.Arguments) == 0 {
			p.Arguments = []byte(`{}`)
		}
		if s.Execute == nil {
			return rpcerr(-32603, "tool executor unavailable")
		}
		v, e := s.Execute(ctx, a.Action, p.Arguments)
		if e != nil {
			v = map[string]any{"error": core.AsError(e)}
			out["result"] = map[string]any{"content": []any{map[string]any{"type": "text", "text": core.JSON(v)}}, "isError": true, "structuredContent": v}
			return out
		}
		out["result"] = map[string]any{"content": []any{map[string]any{"type": "text", "text": core.JSON(v)}}, "structuredContent": v, "isError": false}
	case "resources/list", "resources/templates/list", "resources/read", "prompts/list", "prompts/get":
		if !s.Assets {
			return rpcerr(-32601, "method not advertised")
		}
		value, code, err := s.asset(ctx, r.Method, r.Params)
		if err != nil {
			return rpcerr(code, err.Error())
		}
		out["result"] = value
	default:
		return rpcerr(-32601, "method not supported")
	}
	return out
}
