package build

import (
	"context"
	"github.com/BlueSkyXN/XRAG/core"
)

func (b *Builder) EngineFor(ctx context.Context, kbs []string) (*core.Engine, error) {
	e := &core.Engine{Repo: b.Store, Mode: "server", Graph: scopedGraph{b}}
	for _, kb := range kbs {
		bb, err := b.ForKB(ctx, kb)
		if err != nil {
			return nil, err
		}
		if bb.Config.Embedding != nil {
			e.Semantic = scopedSemantic{b}
		}
		if bb.Config.Rerank != nil {
			e.Rerank = scopedRerank{b}
		}
		for _, p := range bb.Config.External {
			if !p.Disabled {
				e.External = scopedExternal{b}
			}
		}
	}
	return e, nil
}
func (b *Builder) Capabilities(ctx context.Context, kb string) (map[string]any, error) {
	bb, e := b.ForKB(ctx, kb)
	if e != nil {
		return nil, e
	}
	c := bb.Config
	graph := "SQLite explicit/support graph"
	if c.Neo4j != nil {
		graph = "neo4j-query-api"
	}
	ext := false
	for _, p := range c.External {
		ext = ext || !p.Disabled
	}
	return map[string]any{"embedding": c.Embedding != nil, "qdrant": c.Qdrant != nil, "wiki": c.Chat != nil, "rerank": c.Rerank != nil, "external": ext, "graph_backend": graph, "graph_mode": c.Graph.Mode, "parser": bb.Options.Parser, "settings_revision": bb.Options.Revision}, nil
}
