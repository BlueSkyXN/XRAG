package tests

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/BlueSkyXN/XRAG/core"
)

// 仅暴露原 Repository 接口，验证 local/direct 等未实现可选接口时的退路。
type strictLexicalRepo struct{ core.Repository }

func TestAskQuestionRecallAndStrictKeyword(t *testing.T) {
	s, engine := setup(t)
	body, err := os.ReadFile("../examples/docs/02-errors.md")
	must(t, err)
	pub(t, s, "manual/02-errors.md", string(body))
	query := core.Request{Q: "日志丢失怎么排查", Limit: 5, Trace: true}
	ask := run(t, engine, "+ask", query)
	if ask.Returned == 0 || ask.Items[0].Path != "manual/02-errors.md" || ask.Total.Accuracy != "unknown" {
		t.Fatalf("question recall: %+v", ask)
	}
	keyword := run(t, engine, "search.keyword", query)
	if keyword.Returned != 0 || keyword.Total.Accuracy != "exact" || *keyword.Total.Value != 0 {
		t.Fatalf("strict keyword changed: %+v", keyword)
	}
	for _, mode := range []string{"local", "direct"} {
		fallback := &core.Engine{Repo: strictLexicalRepo{s}, Mode: mode}
		if got := run(t, fallback, "+ask", query); got.Returned != 0 {
			t.Fatalf("%s fallback changed: %+v", mode, got)
		}
	}
}

func TestRelaxedLexicalCandidateWindowAndBM25(t *testing.T) {
	s, _ := setup(t)
	var body strings.Builder
	for i := 0; i < 505; i++ {
		fmt.Fprintf(&body, "## 条目 %d\nE203 shared\n\n", i)
	}
	pub(t, s, "many.md", body.String())
	pub(t, s, "rare.md", "# 稀有\nE203 raretarget\n")
	view, err := s.Snapshot(ctx, []string{"demo"})
	must(t, err)
	ranked, err := s.RelaxedLexical(ctx, view, "E203 raretarget 怎么")
	must(t, err)
	if len(ranked) != 500 {
		t.Fatalf("candidate count = %d, want 500", len(ranked))
	}
	var topPath string
	for _, doc := range view.Documents {
		for _, chunk := range doc.Chunks {
			if chunk.ID == ranked[0].ID {
				topPath = doc.Path
			}
		}
	}
	if topPath != "rare.md" {
		t.Fatalf("BM25 top path = %q", topPath)
	}
	for i := 1; i < len(ranked); i++ {
		prev, next := ranked[i-1], ranked[i]
		if prev.Score < next.Score || (prev.Score == next.Score && prev.ID > next.ID) {
			t.Fatal("candidates must be ordered by BM25 and chunk ID")
		}
	}
	strict, err := s.Lexical(ctx, view, "E203")
	must(t, err)
	if len(strict) != 506 {
		t.Fatalf("strict candidate count = %d, want 506", len(strict))
	}
}
