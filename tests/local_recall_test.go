package tests

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/BlueSkyXN/XRAG/adapters"
	"github.com/BlueSkyXN/XRAG/core"
)

func TestLocalTwentyThousandFileBoundary(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 20000; i++ {
		must(t, os.WriteFile(filepath.Join(root, fmt.Sprintf("%05d.md", i)), nil, 0600))
	}
	repo := &adapters.Local{Root: root, Origin: "local-limit", KB: "demo", CacheDir: t.TempDir()}
	view, err := repo.Snapshot(ctx, []string{"demo"})
	must(t, err)
	if len(view.Documents) != 20000 {
		t.Fatal(len(view.Documents))
	}
	must(t, os.WriteFile(filepath.Join(root, "20000.md"), nil, 0600))
	_, err = repo.Snapshot(ctx, []string{"demo"})
	code(t, err, "SOURCE_LIMIT")
}

func TestLocalFileByteLimitRemains(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "large.md")
	must(t, os.WriteFile(p, nil, 0600))
	must(t, os.Truncate(p, (5<<20)+1))
	repo := &adapters.Local{Root: root, Origin: "local-limit", KB: "demo", CacheDir: t.TempDir()}
	_, err := repo.Snapshot(ctx, []string{"demo"})
	code(t, err, "SOURCE_LIMIT")
}

func TestLocalQuestionRecallAndKeywordBaseline(t *testing.T) {
	var suite struct {
		Cases []struct {
			ID, Question  string
			ExpectedEmpty bool `json:"expected_empty"`
			Expected      []struct{ Path string }
		}
	}
	data, err := os.ReadFile("../examples/eval-questions.json")
	must(t, err)
	must(t, json.Unmarshal(data, &suite))
	var baseline map[string]core.Response
	data, err = os.ReadFile("testdata/local-keyword-before.json")
	must(t, err)
	must(t, json.Unmarshal(data, &baseline))
	repo := &adapters.Local{Root: "../examples/docs", Origin: "local-eval", KB: "demo", CacheDir: t.TempDir()}
	engine := &core.Engine{Repo: repo, Mode: "local"}
	totals := map[string]int{}
	for _, c := range suite.Cases {
		t.Run(c.ID, func(t *testing.T) {
			q := core.Request{Q: c.Question, Limit: 5}
			ask := run(t, engine, "+ask", q)
			hit := c.ExpectedEmpty && len(ask.Items) == 0
			for _, want := range c.Expected {
				for _, item := range ask.Items {
					if item.Path == strings.TrimPrefix(want.Path, "manual/") {
						hit = true
					}
				}
			}
			paths := []string{}
			for _, item := range ask.Items {
				paths = append(paths, item.Path)
			}
			t.Logf("%s | %s | hit=%v | %v", c.ID, c.Question, hit, paths)
			if !hit {
				t.Errorf("file hit@5 failed: %v", paths)
			}
			group := "question"
			if strings.HasPrefix(c.ID, "short") {
				group = "short"
			}
			if c.ExpectedEmpty {
				group = "empty"
			}
			if hit {
				totals[group]++
			}
			keyword := run(t, engine, "search.keyword", q)
			keyword.QueryID = ""
			keyword.ResultID = ""
			keyword.Handle = ""
			keyword.NextCursor = ""
			keyword.SummaryRef = ""
			// 排除每次请求生成的结果句柄，保留条目顺序、分数、原文引用、计数及策略。
			if core.JSON(keyword) != core.JSON(baseline[c.ID]) {
				t.Errorf("keyword changed for %s\nGOT %s\nWANT %s", c.ID, core.JSON(keyword), core.JSON(baseline[c.ID]))
			}
		})
	}
	if !reflect.DeepEqual(totals, map[string]int{"question": 14, "short": 4, "empty": 2}) {
		t.Fatalf("totals: %v", totals)
	}
	t.Logf("totals: %v; keyword 20/20 unchanged", totals)
}

func TestLocalRelaxedLimitsAndBM25(t *testing.T) {
	repo := &adapters.Local{}
	chunks := []core.Chunk{}
	for i := 504; i >= 0; i-- {
		chunks = append(chunks, core.Chunk{ID: fmt.Sprintf("chunk-%04d", i), Text: "shared common"})
	}
	chunks = append(chunks, core.Chunk{ID: "rare", Text: "shared raretarget"})
	view := core.View{Documents: []core.Document{{Chunks: chunks}}}
	ranked, err := repo.RelaxedLexical(ctx, view, "shared raretarget 怎么")
	must(t, err)
	if len(ranked) != 500 || ranked[0].ID != "rare" {
		t.Fatalf("BM25/candidate cap: %v", ranked)
	}
	for i := 1; i < len(ranked); i++ {
		a, b := ranked[i-1], ranked[i]
		if a.Score < b.Score || (a.Score == b.Score && a.ID > b.ID) {
			t.Fatal("score/ID order")
		}
	}
	strict, err := repo.Lexical(ctx, view, "shared")
	must(t, err)
	if len(strict) != 506 {
		t.Fatal("keyword candidates truncated")
	}
	terms := []string{}
	for i := 0; i < 49; i++ {
		terms = append(terms, fmt.Sprintf("term%02d", i))
	}
	view.Documents[0].Chunks = []core.Chunk{{ID: "included", Text: "term47"}, {ID: "excluded", Text: "term48"}}
	ranked, err = repo.RelaxedLexical(ctx, view, strings.Join(terms, " "))
	must(t, err)
	if len(ranked) != 1 || ranked[0].ID != "included" {
		t.Fatalf("48 term truncation: %v", ranked)
	}
	view.Documents[0].Chunks = []core.Chunk{{ID: "fallback", Text: "如何"}}
	ranked, err = repo.RelaxedLexical(ctx, view, "如何")
	must(t, err)
	if len(ranked) != 1 {
		t.Fatal("empty stripping fallback")
	}
	_, err = repo.RelaxedLexical(ctx, view, "?!")
	code(t, err, "INVALID_ARGUMENT")
}
