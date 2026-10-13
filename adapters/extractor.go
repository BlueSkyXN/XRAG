package adapters

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
	"github.com/BlueSkyXN/XRAG/core"
)

type limitedBuffer struct {
	bytes.Buffer
	limit int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > b.limit {
		return 0, fmt.Errorf("plugin output exceeded limit")
	}
	return b.Buffer.Write(p)
}

// Extract is a real subprocess call. The plugin only returns suggestions; the host writes stores.
func Extract(ctx context.Context, command []string, revision string, c core.Chunk) ([]core.Support, error) {
	if len(command) == 0 {
		return nil, core.Err("CAPABILITY_UNAVAILABLE", "extractor command is empty", 422)
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	id := core.ID("ex_")
	body := map[string]any{"protocol": "xrag.extract/v1", "id": id, "chunk_id": c.ID, "text": c.Text, "revision": revision}
	cmd := exec.CommandContext(ctx, command[0], command[1:]...)
	cmd.Stdin = strings.NewReader(core.JSON(body) + "\n")
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "LANG=C.UTF-8"}
	out := &limitedBuffer{limit: 1 << 20}
	stderr := &limitedBuffer{limit: 32768}
	cmd.Stdout = out
	cmd.Stderr = stderr
	if e := cmd.Run(); e != nil {
		return nil, core.Err("EXTRACTOR_FAILED", "extractor process failed", 502)
	}
	var result struct {
		ID     string `json:"id"`
		Result struct {
			Relations []struct {
				From     string `json:"from"`
				To       string `json:"to"`
				Rel      string `json:"rel"`
				Evidence string `json:"evidence"`
			} `json:"relations"`
		} `json:"result"`
		Error any `json:"error"`
	}
	if json.Unmarshal(out.Bytes(), &result) != nil || result.ID != id || result.Error != nil {
		return nil, core.Err("EXTRACTOR_FAILED", "invalid plugin response", 502)
	}
	ss := []core.Support{}
	seen := map[string]bool{}
	for _, r := range result.Result.Relations {
		if strings.TrimSpace(r.From) == "" || strings.TrimSpace(r.To) == "" || strings.TrimSpace(r.Rel) == "" || strings.TrimSpace(r.Evidence) == "" || !strings.Contains(c.Text, r.Evidence) {
			return nil, core.Err("EXTRACTOR_FAILED", "relation evidence must identify nonempty source text", 502)
		}
		key := "sp_" + core.Hash([]any{c.ID, revision, r.From, r.Rel, r.To})[:32]
		if !seen[key] {
			ss = append(ss, core.Support{ID: key, KB: c.KB, Path: c.Path, Edition: c.Edition, ChunkID: c.ID, From: r.From, To: r.To, Relation: r.Rel, Evidence: r.Evidence, Origin: "plugin"})
			seen[key] = true
		}
	}
	return ss, nil
}
