package adapters

import (
	"context"
	"encoding/json"
	"github.com/BlueSkyXN/XRAG/core"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Local struct{ Root, Origin, KB, CacheDir string }

func (l *Local) Epoch(ctx context.Context) (int64, error) { return 0, ctx.Err() }
func (l *Local) Snapshot(ctx context.Context, kbs []string) (core.View, error) {
	v := core.View{Origin: l.Origin, Mode: "local", At: time.Now(), Documents: []core.Document{}}
	if !core.Subset(kbs, []string{l.KB}) {
		return v, core.Err("PERMISSION_DENIED", "local profile has a different KB namespace", 403)
	}
	count := 0
	totalBytes := int64(0)
	e := filepath.WalkDir(l.Root, func(p string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if e = ctx.Err(); e != nil {
			return e
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if d.IsDir() {
			if p != l.Root && (strings.HasPrefix(d.Name(), ".") || d.Name() == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(p))
		if ext != ".md" && ext != ".markdown" && ext != ".txt" {
			return nil
		}
		info, e := d.Info()
		if e != nil {
			return e
		}
		count++
		totalBytes += info.Size()
		if count > 20000 || info.Size() > 5<<20 || totalBytes > 50<<20 {
			return core.Err("SOURCE_LIMIT", "local scan exceeds 20000 files, 5 MiB per file, or 50 MiB total", 422)
		}
		b, e := os.ReadFile(p)
		if e != nil {
			return e
		}
		after, e := os.Stat(p)
		if e != nil {
			return e
		}
		if after.Size() != info.Size() || !after.ModTime().Equal(info.ModTime()) {
			return core.Err("SOURCE_CHANGED", "local file changed during scan", 409)
		}
		body, e := core.Normalize(b)
		if e != nil {
			return e
		}
		rel, e := filepath.Rel(l.Root, p)
		if e != nil {
			return e
		}
		rel = filepath.ToSlash(rel)
		ed := "local_" + core.Hash([]string{l.Origin, rel, body})[:32]
		doc, e := core.Parse(l.KB, rel, body, ed, "local")
		if e != nil {
			return e
		}
		v.Documents = append(v.Documents, doc)
		return nil
	})
	return v, e
}
func (l *Local) Read(ctx context.Context, r core.Ref) (core.Document, *core.Chunk, string, error) {
	d := core.Document{}
	if r.Origin != l.Origin {
		return d, nil, "", core.Err("REF_ORIGIN_MISMATCH", "choose the original local profile", 409)
	}
	if _, e := core.CleanPath(r.Path); e != nil {
		return d, nil, "", e
	}
	v, e := l.Snapshot(ctx, []string{r.KB})
	if e != nil {
		return d, nil, "", e
	}
	for _, d := range v.Documents {
		if d.Path != r.Path {
			continue
		}
		if d.FileHash != r.FileHash || d.Edition != r.Edition {
			return d, nil, "", core.Err("SCOPE_CHANGED", "local file changed; start a new query", 409)
		}
		if r.ChunkID == "" {
			return d, nil, "current", nil
		}
		for _, c := range d.Chunks {
			if c.ID == r.ChunkID {
				return d, &c, "current", nil
			}
		}
	}
	return d, nil, "", core.Err("REF_GONE", "local file/chunk no longer exists", 410)
}
func (l *Local) ValidateView(ctx context.Context, old core.View) error {
	now, e := l.Snapshot(ctx, []string{l.KB})
	if e != nil {
		return e
	}
	hashes := map[string]string{}
	for _, d := range now.Documents {
		hashes[d.Path] = d.FileHash
	}
	for _, d := range old.Documents {
		if hashes[d.Path] != d.FileHash {
			return core.Err("SCOPE_CHANGED", "saved local scope has changed", 409)
		}
	}
	return nil
}
func (l *Local) Lexical(ctx context.Context, v core.View, q string) ([]core.Ranked, error) {
	terms := core.Unique(core.Terms(q, true))
	if len(terms) == 0 {
		return nil, core.Err("INVALID_ARGUMENT", "empty query", 400)
	}
	out := []core.Ranked{}
	for _, d := range v.Documents {
		for _, c := range d.Chunks {
			have := map[string]int{}
			for _, t := range core.Terms(c.Title+" "+c.Text, false) {
				have[t]++
			}
			ok := true
			score := 0.
			for _, t := range terms {
				if have[t] == 0 {
					ok = false
					break
				}
				score += float64(have[t])
			}
			if ok {
				out = append(out, core.Ranked{ID: c.ID, Score: score})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score == out[j].Score {
			return out[i].ID < out[j].ID
		}
		return out[i].Score > out[j].Score
	})
	return out, nil
}

// RelaxedLexical 对问句做 OR 召回，以 BM25 排序后保留前 500 个候选。
func (l *Local) RelaxedLexical(ctx context.Context, v core.View, q string) ([]core.Ranked, error) {
	terms := core.Unique(core.Terms(core.StripQuestion(q), true))
	if len(terms) == 0 {
		terms = core.Unique(core.Terms(q, true))
	}
	if len(terms) == 0 {
		return nil, core.Err("INVALID_ARGUMENT", "empty query", 400)
	}
	if len(terms) > 48 {
		terms = terms[:48]
	}
	type candidate struct {
		id          string
		length      int
		frequencies map[string]int
	}
	candidates := []candidate{}
	df := map[string]int{}
	totalLength, count := 0, 0
	for _, d := range v.Documents {
		for _, c := range d.Chunks {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			tokens := core.Terms(c.Title+" "+c.Text, false)
			have := map[string]int{}
			for _, token := range tokens {
				have[token]++
			}
			count++
			totalLength += len(tokens)
			matches := map[string]int{}
			for _, term := range terms {
				if have[term] > 0 {
					matches[term] = have[term]
					df[term]++
				}
			}
			if len(matches) > 0 {
				candidates = append(candidates, candidate{c.ID, len(tokens), matches})
			}
		}
	}
	out := []core.Ranked{}
	if len(candidates) == 0 {
		return out, nil
	}
	avgLength := float64(totalLength) / float64(count)
	const k1, b = 1.2, 0.75
	for _, c := range candidates {
		score := 0.0
		// 按查询顺序累加，避免 map 迭代次序影响浮点分数及并列排序。
		for _, term := range terms {
			tf := float64(c.frequencies[term])
			if tf == 0 {
				continue
			}
			idf := math.Log(1 + (float64(count-df[term])+0.5)/(float64(df[term])+0.5))
			score += idf * tf * (k1 + 1) / (tf + k1*(1-b+b*float64(c.length)/avgLength))
		}
		out = append(out, core.Ranked{ID: c.id, Score: score})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score == out[j].Score {
			return out[i].ID < out[j].ID
		}
		return out[i].Score > out[j].Score
	})
	if len(out) > 500 {
		out = out[:500]
	}
	return out, nil
}
func (l *Local) PutCache(ctx context.Context, id string, c core.Cache) error {
	if e := os.MkdirAll(l.CacheDir, 0700); e != nil {
		return e
	}
	p := filepath.Join(l.CacheDir, core.Hash(id)+".json")
	tmp := p + "." + core.ID("") + ".tmp"
	if e := os.WriteFile(tmp, []byte(core.JSON(c)), 0600); e != nil {
		return e
	}
	return os.Rename(tmp, p)
}
func (l *Local) GetCache(ctx context.Context, id string) (core.Cache, error) {
	c := core.Cache{}
	b, e := os.ReadFile(filepath.Join(l.CacheDir, core.Hash(id)+".json"))
	if e != nil {
		return c, core.Err("RESULT_EXPIRED", "local result not found", 410)
	}
	e = json.Unmarshal(b, &c)
	return c, e
}
