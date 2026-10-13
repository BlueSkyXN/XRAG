package build

import (
	"context"
	"github.com/BlueSkyXN/XRAG/adapters"
	"github.com/BlueSkyXN/XRAG/core"
)

func (b *Builder) ParseDocument(ctx context.Context, kb, path, text, edition, source string) (core.Document, error) {
	if b.Options.Parser == "commonmark" || (b.Options.Parser == "" && len(b.Config.ParserCommand) > 0) {
		return adapters.ParseDocument(ctx, b.Config.ParserCommand, kb, path, text, edition, source, b.Options.ChunkMax)
	}
	return core.ParseWithLimit(kb, path, text, edition, source, b.Options.ChunkMax)
}
