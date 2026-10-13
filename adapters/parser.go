package adapters

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"time"
	"github.com/BlueSkyXN/XRAG/core"
)

type ParsedBlock struct {
	Start   int      `json:"start"`
	End     int      `json:"end"`
	Heading []string `json:"heading"`
	Atomic  bool     `json:"atomic"`
	Type    string   `json:"type"`
}

// ParseDocument uses a registered parser command, never an arbitrary command from a query.
func ParseDocument(ctx context.Context, command []string, kb, path, text, edition, source string, maxChars int) (core.Document, error) {
	if len(command) == 0 {
		return core.Document{}, core.Err("CAPABILITY_UNAVAILABLE", "register parser command in startup configuration", 422)
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	id := core.ID("parse_")
	cmd := exec.CommandContext(ctx, command[0], command[1:]...)
	cmd.WaitDelay = time.Second
	cmd.Stdin = strings.NewReader(core.JSON(map[string]any{"id": id, "protocol": "xrag.parser/v1", "text": text}) + "\n")
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "LANG=C.UTF-8"}
	out, errout := &limitedBuffer{limit: 24 << 20}, &limitedBuffer{limit: 32768}
	cmd.Stdout = out
	cmd.Stderr = errout
	if e := cmd.Run(); e != nil {
		return core.Document{}, core.Err("PARSER_FAILED", "registered parser failed; check installed requirements", 502)
	}
	var result struct {
		ID     string `json:"id"`
		Error  string `json:"error"`
		Result struct {
			Body     string         `json:"body"`
			Fields   map[string]any `json:"fields"`
			Title    string         `json:"title"`
			Blocks   []ParsedBlock  `json:"blocks"`
			Revision string         `json:"parser_revision"`
		} `json:"result"`
	}
	if json.Unmarshal(out.Bytes(), &result) != nil || result.ID != id || result.Error != "" {
		return core.Document{}, core.Err("PARSER_FAILED", "invalid parser response: "+result.Error, 422)
	}
	if maxChars == 0 {
		maxChars = 1200
	}
	raw := result.Result
	runes := []rune(raw.Body)
	d := core.Document{KB: kb, Path: path, Source: source, Edition: edition, Body: raw.Body, FileHash: core.TextHash(raw.Body), Fields: raw.Fields, Title: raw.Title, Kind: "doc", Chunks: []core.Chunk{}, Supports: []core.Support{}}
	if d.Title == "" {
		d.Title = path
	}
	if d.Fields == nil {
		d.Fields = map[string]any{}
	}
	var blocks []ParsedBlock
	for _, b := range raw.Blocks {
		if b.Start < 0 || b.End-b.Start > 20<<20 || b.End > len(runes) || b.Start >= b.End {
			return d, core.Err("PARSER_FAILED", "invalid original text range", 422)
		}
		if len(blocks) > 0 {
			last := &blocks[len(blocks)-1]
			if last.End > b.Start {
				return d, core.Err("PARSER_FAILED", "overlapping parser blocks", 422)
			}
			if last.End == b.Start && core.JSON(last.Heading) == core.JSON(b.Heading) && b.End-last.Start <= maxChars {
				last.End = b.End
				continue
			}
		}
		blocks = append(blocks, b)
	}
	dup := map[string]int{}
	for _, b := range blocks {
		for lo := b.Start; lo < b.End; {
			hi := b.End
			limit := maxChars
			if b.Atomic {
				limit = maxChars * 4
				if limit > 20000 {
					limit = 20000
				}
			}
			if hi-lo > limit {
				hi = lo + limit
				for i := hi; i > lo+limit/2; i-- {
					if runes[i-1] == '\n' {
						hi = i
						break
					}
				}
			}
			value := string(runes[lo:hi])
			base := core.Hash([]any{kb, path, b.Heading, value})
			n := dup[base]
			dup[base]++
			key := "k" + core.Hash([]any{3, kb, path, b.Heading, value, n})[:32]
			if strings.TrimSpace(value) != "" {
				ls := strings.Count(string(runes[:lo]), "\n") + 1
				le := ls + strings.Count(strings.TrimSuffix(value, "\n"), "\n")
				d.Chunks = append(d.Chunks, core.Chunk{ID: core.UUID([]any{edition, key}), Key: key, KB: kb, Path: path, Edition: edition, FileHash: d.FileHash, Title: d.Title, Heading: b.Heading, Text: value, Start: lo, End: hi, LineStart: ls, LineEnd: le, Fields: d.Fields})
			}
			lo = hi
		}
	}
	return d, nil
}
