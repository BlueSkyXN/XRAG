package tests

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"
	"github.com/BlueSkyXN/XRAG/core"
	"github.com/BlueSkyXN/XRAG/internal/build"
	"github.com/BlueSkyXN/XRAG/internal/housekeeping"
	"github.com/BlueSkyXN/XRAG/internal/maintenance"
	"github.com/BlueSkyXN/XRAG/internal/oplock"
	db "github.com/BlueSkyXN/XRAG/internal/sqlite"
	"github.com/BlueSkyXN/XRAG/internal/store"
)

func TestDemo8OnlineGCRequiresExclusiveOperation(t *testing.T) {
	s, _ := setup(t)
	_, e := s.PruneContentBatch(ctx, 30, 10, "", true)
	code(t, e, "CONTENT_LEASE_REQUIRED")
	c, release, e := s.BeginOperation(ctx)
	must(t, e)
	defer release()
	_, _, e = oplock.Acquire(c, s.Dir, true)
	code(t, e, "LEASE_UPGRADE_DENIED")
}
func TestDemo8OnlineGCWaitsForReadersAndKeepsServerOpen(t *testing.T) {
	s, eng := setup(t)
	old := pub(t, s, "a.md", "old body")
	pub(t, s, "a.md", "new body")
	ageRetired(t, s)
	_, release, e := s.BeginOperation(ctx)
	must(t, e)
	done := make(chan error, 1)
	go func() {
		p, err := (&build.Builder{Store: s}).CollectContent(ctx, 30, 10, true, false)
		if err == nil && p.RemovedEditions != 1 {
			err = fmt.Errorf("removed=%d", p.RemovedEditions)
		}
		done <- err
	}()
	select {
	case e := <-done:
		t.Fatalf("collector passed reader: %v", e)
	case <-time.After(40 * time.Millisecond):
	}
	release()
	select {
	case e := <-done:
		must(t, e)
	case <-time.After(2 * time.Second):
		t.Fatal("collector blocked after reader left")
	}
	_, _, _, e = s.Read(ctx, decode(t, core.ChunkItem(old.Chunks[0], s.Origin).Ref))
	code(t, e, "REF_GONE")
	if run(t, eng, "search.keyword", core.Request{Q: "new"}).Returned != 1 {
		t.Fatal("live server view was lost")
	}
}
func TestDemo8OperationLeaseIsCrossHandleAndCancelable(t *testing.T) {
	s, _ := setup(t)
	other, e := store.Open(s.Dir)
	must(t, e)
	defer other.Close()
	c, release, e := oplock.Acquire(ctx, s.Dir, true)
	must(t, e)
	defer release()
	must(t, s.DB.Do(c, func(conn *db.Conn) error {
		return conn.Exec("INSERT OR REPLACE INTO meta VALUES('test-exclusive','yes')")
	}))
	wait, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
	defer cancel()
	e = other.DB.Do(wait, func(conn *db.Conn) error { return conn.Exec("SELECT 1") })
	if !errors.Is(e, context.DeadlineExceeded) {
		t.Fatal(e)
	}
	release()
	must(t, other.DB.Do(ctx, func(conn *db.Conn) error { return conn.Exec("SELECT 1") }))
}
func TestDemo8OnlineGCHonorsKnowledgeAndQueryReferences(t *testing.T) {
	s, eng := setup(t)
	k := pub(t, s, "knowledge.md", "knowledge evidence")
	pub(t, s, "query.md", "query evidence")
	_, e := s.SaveKnowledge(ctx, store.Knowledge{KB: "demo", Title: "kept", Body: "summary", Refs: []string{core.ChunkItem(k.Chunks[0], s.Origin).Ref}})
	must(t, e)
	run(t, eng, "search.keyword", core.Request{Q: "query"})
	pub(t, s, "knowledge.md", "next knowledge")
	pub(t, s, "query.md", "next query")
	ageRetired(t, s)
	p, e := (&build.Builder{Store: s}).CollectContent(ctx, 30, 10, true, false)
	must(t, e)
	if p.RemovedEditions != 0 || p.ProtectedEditions != 2 {
		t.Fatalf("%+v", p)
	}
}
func TestDemo8OnlineGCCanRunWhileBuildIsOnlyQueued(t *testing.T) {
	s, _ := setup(t)
	pub(t, s, "a.md", "old")
	pub(t, s, "a.md", "new")
	ageRetired(t, s)
	_, e := s.Enqueue(ctx, "demo", "build", "tester", "", map[string]any{"mode": "incremental"})
	must(t, e)
	p, e := (&build.Builder{Store: s}).CollectContent(ctx, 30, 10, true, false)
	must(t, e)
	if p.RemovedEditions != 1 {
		t.Fatal(p)
	}
}
func TestDemo8OnlineGCCursorDoesNotStarveBehindProtectedVersions(t *testing.T) {
	s, _ := setup(t)
	old := []core.Document{}
	for i := 0; i < 4; i++ {
		p := fmt.Sprintf("%d.md", i)
		old = append(old, pub(t, s, p, "old"))
		pub(t, s, p, "new")
	}
	ageRetired(t, s)
	// First candidate is always protected. A persistent cursor must pass it.
	var first string
	must(t, s.DB.Do(ctx, func(c *db.Conn) error {
		rs, e := c.Query("SELECT id FROM edition WHERE state='retired' ORDER BY id LIMIT 1")
		if e == nil {
			first = db.String(rs[0], "id")
		}
		return e
	}))
	for _, d := range old {
		if d.Edition == first {
			_, e := s.SaveKnowledge(ctx, store.Knowledge{KB: "demo", Title: "keep", Body: "kept", Refs: []string{core.ChunkItem(d.Chunks[0], s.Origin).Ref}})
			must(t, e)
		}
	}
	count := 0
	for i := 0; i < 5; i++ {
		p, e := (&build.Builder{Store: s}).CollectContent(ctx, 30, 1, true, false)
		must(t, e)
		count += p.RemovedEditions
	}
	if count != 3 {
		t.Fatalf("removed %d, cursor starved", count)
	}
}
func TestDemo8OnlineGCRemoteRequiresExplicitOptIn(t *testing.T) {
	s, b, g, _, _ := setupGC7(t)
	pub(t, s, "old.md", "new")
	ageRetired(t, s)
	_, e := b.CollectContent(ctx, 30, 10, true, false)
	code(t, e, "EXTERNAL_CLEANUP_REQUIRED")
	if len(g.requests) > 0 {
		t.Fatal("unapproved remote call")
	}
	p, e := b.CollectContent(ctx, 30, 10, true, true)
	must(t, e)
	if p.RemovedEditions != 1 {
		t.Fatal(p)
	}
}
func TestDemo8OnlineGCPreviewDoesNotAdvanceOrCallRemote(t *testing.T) {
	s, b, g, _, _ := setupGC7(t)
	pub(t, s, "old.md", "new")
	ageRetired(t, s)
	p, e := b.CollectContent(ctx, 30, 1, false, true)
	must(t, e)
	if !p.DryRun || p.RemovedEditions != 0 || len(g.requests) != 0 {
		t.Fatal(p)
	}
	next, e := s.ContentCursor(ctx)
	must(t, e)
	if next != "" {
		t.Fatal(next)
	}
}
func TestDemo8MaintenanceContentQueuedAndExecuted(t *testing.T) {
	s, _ := setup(t)
	pub(t, s, "a.md", "old")
	pub(t, s, "a.md", "new")
	ageRetired(t, s)
	set, e := s.MaintenanceSetting(ctx)
	must(t, e)
	set.Policy.ContentDays = 30
	set.Policy.BatchSize = 10
	_, e = s.SetMaintenance(ctx, set.Revision, set.Policy)
	must(t, e)
	j, e := s.QueueMaintenance(ctx, "tester", false, false, true)
	must(t, e)
	if !j.Content {
		t.Fatal(j)
	}
	must(t, (&housekeeping.Controller{Store: s}).Tick(ctx))
	runs, e := s.MaintenanceRuns(ctx)
	must(t, e)
	if len(runs) != 1 || runs[0].State != "succeeded" {
		t.Fatal(runs)
	}
}
func TestDemo8RestoredCopyStopsContentPlan(t *testing.T) {
	s, _ := setup(t)
	set, e := s.MaintenanceSetting(ctx)
	must(t, e)
	set.Policy.Enabled = true
	set.Policy.ContentEvery = 60
	set.Policy.ContentDays = 30
	_, e = s.SetMaintenance(ctx, set.Revision, set.Policy)
	must(t, e)
	file := filepath.Join(t.TempDir(), "backup.sqlite")
	must(t, s.Backup(ctx, file))
	dest := filepath.Join(t.TempDir(), "restored")
	_, e = maintenance.Restore(ctx, file, dest)
	must(t, e)
	restored, e := store.Open(dest)
	must(t, e)
	defer restored.Close()
	v, e := restored.MaintenanceSetting(ctx)
	must(t, e)
	if v.Policy.Enabled || v.NextContent != 0 {
		t.Fatal(v)
	}
}
