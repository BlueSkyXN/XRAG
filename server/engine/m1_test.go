package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/BlueSkyXN/XRAG/core/types"
)

func TestPublishedFileStaysStableWhenNeighborsChange(t *testing.T) {
	eng, corpus := newServer(t)
	write(t, corpus, "a.md", "alpha body unique-a")
	write(t, corpus, "b.md", "beta body unique-b")
	write(t, corpus, "c.md", "gamma body unique-c")
	kb(t, eng, corpus)
	first := mustBuild(t, eng)
	before := editions(first)
	write(t, corpus, "a.md", "alpha body unique-a changed")
	second := mustBuild(t, eng)
	if item(second, "docs/b.md").Action != "unchanged" || item(second, "docs/c.md").Action != "unchanged" {
		t.Fatalf("neighbors were rebuilt: %+v", second.Files)
	}
	if item(second, "docs/a.md").Action != "published" || item(second, "docs/a.md").After == before["docs/a.md"] {
		t.Fatalf("changed file was not published as a new edition: %+v", item(second, "docs/a.md"))
	}
	got := mustKeyword(t, eng, "unique-b")
	if len(got.Items) != 1 || got.Items[0].Edition != item(second, "docs/b.md").After {
		t.Fatalf("b disappeared or changed edition: %+v", got.Items)
	}
}

func TestOldRefKeepsOldLocation(t *testing.T) {
	eng, corpus := newServer(t)
	alpha := strings.Repeat("甲", 180)
	beta := strings.Repeat("乙", 180) + " unique-beta"
	write(t, corpus, "doc.md", "# Doc\n\n"+alpha+"\n\n## Beta\n\n"+beta+"\n")
	kb(t, eng, corpus)
	mustBuild(t, eng)
	old := mustKeyword(t, eng, "unique-beta")
	if len(old.Items) != 1 {
		t.Fatalf("beta hits: %+v", old.Items)
	}
	write(t, corpus, "doc.md", "# Doc\n\n"+alpha+"\n\nINSERTED\n\n## Beta\n\n"+beta+"\n")
	second := mustBuild(t, eng)
	if item(second, "docs/doc.md").Action != "published" {
		t.Fatal(second.Files)
	}
	again := mustKeyword(t, eng, "unique-beta")
	if again.Items[0].ChunkKey != old.Items[0].ChunkKey {
		t.Fatalf("logical key changed: %s %s", old.Items[0].ChunkKey, again.Items[0].ChunkKey)
	}
	if again.Items[0].ChunkID == old.Items[0].ChunkID {
		t.Fatal("physical chunk id was reused")
	}
	if again.Items[0].LineStart <= old.Items[0].LineStart {
		t.Fatalf("line did not move: old %d new %d", old.Items[0].LineStart, again.Items[0].LineStart)
	}
	read := mustRead(t, eng, old.Items[0].Ref)
	if read.Items[0].Text != old.Items[0].Text || read.Items[0].LineStart != old.Items[0].LineStart {
		t.Fatalf("old ref followed the new file: %+v", read.Items[0])
	}
	if strings.Contains(read.Items[0].Text, "INSERTED") {
		t.Fatal("old ref includes the inserted line")
	}
}

func TestFrontmatterReusesEmbedding(t *testing.T) {
	eng, corpus := newServer(t)
	body := strings.Repeat("正文", 100)
	write(t, corpus, "note.md", "---\nproduct: A\n---\n"+body+"\n")
	kb(t, eng, corpus)
	emb := &countEmbed{}
	opt := Options{Vector: VectorRequired, Embedder: emb}
	if _, err := eng.Build(context.Background(), "demo", opt); err != nil {
		t.Fatal(err)
	}
	if emb.n != 1 {
		t.Fatalf("calls %d", emb.n)
	}
	write(t, corpus, "note.md", "---\nproduct: B\n---\n"+body+"\n")
	if _, err := eng.Build(context.Background(), "demo", opt); err != nil {
		t.Fatal(err)
	}
	if emb.n != 1 {
		t.Fatalf("unchanged model input was embedded again: %d", emb.n)
	}
}

func TestFailedVectorIsRetried(t *testing.T) {
	eng, corpus := newServer(t)
	write(t, corpus, "note.md", "same body that must be retried")
	kb(t, eng, corpus)
	emb := &failOnce{}
	opt := Options{Vector: VectorRequired, Embedder: emb}
	first, err := eng.Build(context.Background(), "demo", opt)
	if err != nil {
		t.Fatal(err)
	}
	if item(first, "docs/note.md").State != "failed" {
		t.Fatal(first.Files)
	}
	if n := mustKeyword(t, eng, "retried").Total.Value; n != 0 {
		t.Fatalf("failed edition is visible: %d", n)
	}
	second, err := eng.Build(context.Background(), "demo", opt)
	if err != nil {
		t.Fatal(err)
	}
	if item(second, "docs/note.md").Action != "published" || emb.n != 2 {
		t.Fatalf("retry skipped: %+v calls %d", item(second, "docs/note.md"), emb.n)
	}
}

func TestStagingIsNotQueryable(t *testing.T) {
	eng, corpus := newServer(t)
	write(t, corpus, "note.md", "published-body")
	kb(t, eng, corpus)
	mustBuild(t, eng)
	write(t, corpus, "note.md", "secret-new-body")
	staged, err := eng.Build(context.Background(), "demo", Options{HoldPublication: true})
	if err != nil {
		t.Fatal(err)
	}
	if item(staged, "docs/note.md").State != "staging" {
		t.Fatal(staged.Files)
	}
	got := mustKeyword(t, eng, "published-body")
	if len(got.Items) != 1 || strings.Contains(got.Items[0].Text, "secret-new-body") {
		t.Fatalf("query saw unpublished text: %+v", got.Items)
	}
	hidden := mustKeyword(t, eng, "secret-new-body")
	if hidden.Total.Value != 0 {
		t.Fatal(hidden.Items)
	}
	raw, err := os.ReadFile(filepath.Join(corpus, "note.md"))
	if err != nil || !strings.Contains(string(raw), "secret-new-body") {
		t.Fatal("source fixture was not changed")
	}
}

func TestFrozenPagesDoNotResearch(t *testing.T) {
	eng, corpus := newServer(t)
	for i := 0; i < 37; i++ {
		write(t, corpus, filepath.Join("n", filename(i)), "item shared-term "+pad(i))
	}
	kb(t, eng, corpus)
	mustBuild(t, eng)
	first := mustKeyword(t, eng, "shared-term")
	if first.Total.Value != 37 || len(first.Items) != 10 {
		t.Fatalf("total %d page %d", first.Total.Value, len(first.Items))
	}
	os.RemoveAll(corpus)
	var got []types.Item
	for offset := 0; offset < 37; offset += 10 {
		page, err := eng.Page(context.Background(), first.ResultID, Page{Limit: 10, Offset: offset})
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, page.Items...)
	}
	if len(got) != 37 {
		t.Fatalf("paged %d", len(got))
	}
	if got[0].Path != first.Items[0].Path || got[30].Path == "" {
		t.Fatalf("order changed: %#v", []string{got[0].Path, got[30].Path})
	}
}

func TestChineseAndExactIdentifier(t *testing.T) {
	eng, corpus := newServer(t)
	write(t, corpus, "cn.md", "这里记录故障的处理步骤。固定短语在原文里。")
	write(t, corpus, "e203.md", "代码 E203 需要更换部件")
	write(t, corpus, "e2031.md", "代码 E2031 是另一条")
	kb(t, eng, corpus)
	mustBuild(t, eng)
	cn := mustKeyword(t, eng, "故障")
	if len(cn.Items) != 1 || !strings.Contains(cn.Items[0].Text, "固定短语在原文里") {
		t.Fatalf("chinese/phrase: %+v", cn.Items)
	}
	exact := mustKeyword(t, eng, "E203")
	if len(exact.Items) != 1 || exact.Items[0].Path != "docs/e203.md" {
		t.Fatalf("identifier: %+v", exact.Items)
	}
}

func TestGrepCountsLinesOnce(t *testing.T) {
	eng, corpus := newServer(t)
	write(t, corpus, "codes.md", "E203 E203\nE2031\n")
	kb(t, eng, corpus)
	mustBuild(t, eng)
	got, err := eng.Grep(context.Background(), "demo", `E203`)
	if err != nil {
		t.Fatal(err)
	}
	if got.Total.Unit != "matched_lines" || got.Total.Value != 2 {
		t.Fatalf("regex still matches the E2031 line, expected two lines, got %+v", got.Total)
	}
	// The first line contains two hits but one matched line.
	if len(got.Items) < 1 || got.Items[0].LineStart != 1 || strings.Count(got.Items[0].Text, "E203") < 2 {
		t.Fatalf("first line was split: %+v", got.Items)
	}
}

func TestSecondBuildWaits(t *testing.T) {
	eng, corpus := newServer(t)
	write(t, corpus, "note.md", "block me")
	kb(t, eng, corpus)
	emb := &gateEmbed{started: make(chan struct{}), release: make(chan struct{})}
	var wg sync.WaitGroup
	wg.Add(1)
	var first error
	go func() {
		defer wg.Done()
		_, first = eng.Build(context.Background(), "demo", Options{Vector: VectorRequired, Embedder: emb})
	}()
	<-emb.started
	_, err := eng.Build(context.Background(), "demo", Options{Vector: VectorRequired, Embedder: emb})
	f := types.As(err)
	if f == nil || f.Code != "BUILD_IN_PROGRESS" {
		t.Fatalf("second build: %v", err)
	}
	close(emb.release)
	wg.Wait()
	if first != nil {
		t.Fatal(first)
	}
}

type countEmbed struct{ n int }

func (c *countEmbed) Profile() types.VectorProfile {
	return types.VectorProfile{ID: "p1", ModelID: "m", Dimension: 3, Revision: "1"}
}
func (c *countEmbed) Embed(context.Context, string) ([]float32, error) {
	c.n++
	return []float32{1, 2, 3}, nil
}

type failOnce struct{ n int }

func (f *failOnce) Profile() types.VectorProfile {
	return types.VectorProfile{ID: "p1", ModelID: "m", Dimension: 1, Revision: "1"}
}
func (f *failOnce) Embed(context.Context, string) ([]float32, error) {
	f.n++
	if f.n == 1 {
		return nil, types.Fail("MODEL_FAILED", "temporary", 502)
	}
	return []float32{1}, nil
}

type gateEmbed struct {
	once    sync.Once
	started chan struct{}
	release chan struct{}
}

func (g *gateEmbed) Profile() types.VectorProfile {
	return types.VectorProfile{ID: "p1", ModelID: "m", Dimension: 1, Revision: "1"}
}
func (g *gateEmbed) Embed(context.Context, string) ([]float32, error) {
	g.once.Do(func() { close(g.started) })
	<-g.release
	return []float32{1}, nil
}

func newServer(t *testing.T) (*Engine, string) {
	t.Helper()
	root := t.TempDir()
	eng, err := Open(filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { eng.Close() })
	corpus := filepath.Join(root, "corpus")
	if err := os.MkdirAll(corpus, 0o755); err != nil {
		t.Fatal(err)
	}
	return eng, corpus
}

func kb(t *testing.T, eng *Engine, corpus string) {
	t.Helper()
	if err := eng.CreateKB(context.Background(), "demo", "Demo"); err != nil {
		t.Fatal(err)
	}
	if err := eng.AddPathSource(context.Background(), "demo", "docs", "docs", corpus); err != nil {
		t.Fatal(err)
	}
}

func mustBuild(t *testing.T, eng *Engine) Summary {
	t.Helper()
	got, err := eng.Build(context.Background(), "demo", Options{})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func mustKeyword(t *testing.T, eng *Engine, q string) types.Response {
	t.Helper()
	got, err := eng.Keyword(context.Background(), "demo", q, Page{})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func mustRead(t *testing.T, eng *Engine, ref string) types.Response {
	t.Helper()
	got, err := eng.Read(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func write(t *testing.T, root, name, body string) {
	t.Helper()
	p := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func editions(s Summary) map[string]string {
	out := map[string]string{}
	for _, f := range s.Files {
		out[f.Path] = f.After
	}
	return out
}

func item(s Summary, path string) FileResult {
	for _, f := range s.Files {
		if f.Path == path {
			return f
		}
	}
	return FileResult{}
}

func filename(i int) string { return pad(i) + ".md" }

func pad(i int) string {
	if i < 10 {
		return "0" + string(rune('0'+i))
	}
	return string(rune('0'+i/10)) + string(rune('0'+i%10))
}
