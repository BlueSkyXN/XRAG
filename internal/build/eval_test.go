package build

import (
	"context"
	"testing"

	"github.com/BlueSkyXN/XRAG/core"
	"github.com/BlueSkyXN/XRAG/internal/store"
)

// 隐藏可选接口，以验证内层只实现 Repository 时仍执行严格检索。
type strictFrozenTestRepo struct{ core.Repository }

func TestFrozenRepoRelaxedLexical(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	if err := s.CreateKB(ctx, "demo", "Demo"); err != nil {
		t.Fatal(err)
	}
	publish := func(path string) {
		t.Helper()
		doc, err := core.Parse("demo", path, "日志丢失可能发生在队列满时。", core.ID("ed_"), "manual")
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Stage(ctx, doc); err != nil {
			t.Fatal(err)
		}
		if err := s.Publish(ctx, doc, store.Job{}, "", ""); err != nil {
			t.Fatal(err)
		}
	}
	publish("manual/errors.md")
	view, err := s.Snapshot(ctx, []string{"demo"})
	if err != nil {
		t.Fatal(err)
	}
	// 冻结后新增的匹配文件不能进入本次检索范围。
	publish("manual/later.md")
	for _, tc := range []struct {
		name string
		repo core.Repository
		want int
	}{
		{"delegates to relaxed Store", s, 1},
		{"falls back to strict repository", strictFrozenTestRepo{s}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			engine := core.Engine{Repo: frozenRepo{Repository: tc.repo, View: view}, Mode: "server"}
			query := core.Request{Kbs: []string{"demo"}, Q: "日志丢失怎么排查", Limit: 5}
			ask, err := engine.Run(ctx, "+ask", query, "tester")
			if err != nil {
				t.Fatal(err)
			}
			if ask.Returned != tc.want || (tc.want > 0 && ask.Items[0].Path != "manual/errors.md") {
				t.Fatalf("question recall: %+v", ask)
			}
			keyword, err := engine.Run(ctx, "search.keyword", query, "tester")
			if err != nil || keyword.Returned != 0 {
				t.Fatalf("strict keyword changed: %+v, %v", keyword, err)
			}
			query.Q = "日志丢失"
			short, err := engine.Run(ctx, "+ask", query, "tester")
			if err != nil || short.Returned != 1 || short.Items[0].Path != "manual/errors.md" {
				t.Fatalf("frozen scope or fallback changed: %+v, %v", short, err)
			}
		})
	}
}
