package tests

import (
	"testing"
	"time"
	"github.com/BlueSkyXN/XRAG/core"
	"github.com/BlueSkyXN/XRAG/internal/maintenance"
	db "github.com/BlueSkyXN/XRAG/internal/sqlite"
	"github.com/BlueSkyXN/XRAG/internal/store"
)

func ageRetired(t *testing.T, s *store.Store) {
	t.Helper()
	must(t, s.DB.Do(ctx, func(c *db.Conn) error {
		return c.Exec("UPDATE edition_retirement SET retired_at=?", time.Now().Add(-40*24*time.Hour).Unix())
	}))
}
func TestOfflinePrunePreviewAndOwnedReferences(t *testing.T) {
	s, eng := setup(t)
	d := pub(t, s, "original.md", "# Old\nOriginal evidence text.")
	ref := core.ChunkItem(d.Chunks[0], s.Origin).Ref
	k, e := s.SaveKnowledge(ctx, store.Knowledge{KB: "demo", Title: "Evidence", Kind: "case", Body: "Preserve citation", Refs: []string{ref}})
	must(t, e)
	_ = k
	old := pub(t, s, "discard.md", "# Old\nUnreferenced old material.")
	pub(t, s, "original.md", "# New\nChanged original text.")
	pub(t, s, "discard.md", "# New\nCurrent material.")
	ageRetired(t, s)
	p, e := s.PruneHistory(ctx, 30, false, false)
	must(t, e)
	if p.ProtectedEditions != 1 || len(p.EligibleEditions) != 1 || p.RemovedEditions != 0 {
		t.Fatalf("%+v", p)
	}
	_, _, _, e = s.Read(ctx, core.Ref{Origin: s.Origin, KB: "demo", Path: old.Path, Edition: old.Edition, FileHash: old.FileHash})
	must(t, e)
	p, e = s.PruneHistory(ctx, 30, true, false)
	must(t, e)
	if p.RemovedEditions != 1 {
		t.Fatal(p)
	}
	parsedRef, _ := core.DecodeRef(ref)
	_, _, _, e = s.Read(ctx, parsedRef)
	must(t, e)
	oldRef, _ := core.DecodeRef(core.ChunkItem(old.Chunks[0], s.Origin).Ref)
	_, _, _, e = s.Read(ctx, oldRef)
	code(t, e, "REF_GONE")
	r := run(t, eng, "search.keyword", core.Request{Q: "Current", Kbs: []string{"demo"}})
	if len(r.Items) == 0 {
		t.Fatal("active content removed")
	}
}
func TestPruneKeepsFeedbackEdition(t *testing.T) {
	s, _ := setup(t)
	d := pub(t, s, "old.md", "# Kept\nKeep by feedback")
	ref := core.ChunkItem(d.Chunks[0], s.Origin).Ref
	must(t, s.DB.Do(ctx, func(c *db.Conn) error {
		return c.Exec("INSERT INTO feedback VALUES(?,?,?,?,?,?,?)", "fb_keep", "demo", ref, "useful", "", "tester", time.Now().Unix())
	}))
	pub(t, s, "old.md", "# Changed\nNew body")
	ageRetired(t, s)
	p, e := s.PruneHistory(ctx, 30, true, false)
	must(t, e)
	if p.RemovedEditions != 0 || p.ProtectedEditions != 1 {
		t.Fatal(p)
	}
}
func TestPruneRecentRetirementNotBasedOnOriginalCreation(t *testing.T) {
	s, _ := setup(t)
	d := pub(t, s, "a.md", "# Old\nold")
	must(t, s.DB.Do(ctx, func(c *db.Conn) error {
		return c.Exec("UPDATE edition SET created=? WHERE id=?", time.Now().Add(-365*24*time.Hour).Unix(), d.Edition)
	}))
	pub(t, s, "a.md", "# New\nnew")
	p, e := s.PruneHistory(ctx, 30, false, false)
	must(t, e)
	if p.CandidateEditions != 0 {
		t.Fatal("retention used creation time", p)
	}
}
func TestPruneRefusesUnfinishedJobsAndRequiresOfflineLock(t *testing.T) {
	s, _ := setup(t)
	_, e := s.Enqueue(ctx, "demo", "build", "tester", "", map[string]any{})
	must(t, e)
	_, e = s.PruneHistory(ctx, 30, true, false)
	code(t, e, "WORKSPACE_BUSY")
	dir := t.TempDir()
	release, e := maintenance.RuntimeLock(dir, false)
	must(t, e)
	defer release()
	other, e := maintenance.RuntimeLock(dir, true)
	if e == nil {
		other()
		t.Fatal("exclusive maintenance allowed with running process")
	}
	another, e := maintenance.RuntimeLock(dir, false)
	must(t, e)
	another()
}
func TestPruneOptionalModelCacheRemovalAndValidation(t *testing.T) {
	s, _ := setup(t)
	must(t, s.ModelPut(ctx, "cached", map[string]any{"values": []int{1, 2}}))
	p, e := s.PruneHistory(ctx, 30, false, true)
	must(t, e)
	if p.ModelEntries != 1 {
		t.Fatal(p)
	}
	_, ok, e := s.ModelGet(ctx, "cached")
	must(t, e)
	if !ok {
		t.Fatal("preview mutated cache")
	}
	_, e = s.PruneHistory(ctx, 30, true, true)
	must(t, e)
	_, ok, e = s.ModelGet(ctx, "cached")
	must(t, e)
	if ok {
		t.Fatal("explicit removal did not happen")
	}
	_, e = s.PruneHistory(ctx, 0, true, false)
	code(t, e, "INVALID_ARGUMENT")
}
