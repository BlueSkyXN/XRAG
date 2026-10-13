// Package housekeeping runs explicitly configured maintenance in Worker hosts.
// API-only deployments enqueue requests but do not create backups or rotate data.
package housekeeping

import (
	"context"
	"log"
	"sync"
	"time"
	"github.com/BlueSkyXN/XRAG/internal/build"
	"github.com/BlueSkyXN/XRAG/internal/store"
)

type Controller struct {
	Store  *store.Store
	Logger *log.Logger
}

func (c *Controller) Tick(ctx context.Context) error {
	if err := c.Store.ScheduleMaintenance(ctx, time.Now()); err != nil {
		return err
	}
	run, ok, err := c.Store.ClaimMaintenance(ctx)
	if err != nil || !ok {
		return err
	}
	// Lease lasts longer than this bounded run. Never restart an uncertain backup
	// automatically within the same slot; the next explicit/scheduled run is new.
	task, cancel := context.WithTimeout(ctx, 4*time.Minute)
	defer cancel()
	result := map[string]any{"content_versions_removed": false, "external_backends_backed_up": false}
	if run.Backup {
		var b store.BackupInfo
		b, err = c.Store.CreateBackup(task)
		if err == nil {
			err = c.Store.MarkAutomaticBackup(task, b.ID, run.ID)
			result["backup"] = b
		}
		if err == nil {
			result["removed_backups"], err = c.Store.RotateAutomaticBackups(task, run.Policy.BackupKeep)
		}
	}
	if err == nil && run.Records {
		var report store.RecordsReport
		report, err = c.Store.RotateRecords(task, run.Policy.RecordsDays, run.Policy.BatchSize, true)
		result["records"] = report
		if err == nil && report.More {
			err = c.Store.ContinueRecords(task, run)
		}
	}
	if err == nil && run.Content {
		var report store.PruneReport
		report, err = (&build.Builder{Store: c.Store}).CollectContent(task, run.Policy.ContentDays, min(run.Policy.BatchSize, 100), true, run.Policy.ContentExternal)
		result["content"] = report
		result["content_versions_removed"] = report.RemovedEditions > 0
	}
	finish, fCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer fCancel()
	if e := c.Store.FinishMaintenance(finish, run, result, err); e != nil {
		return e
	}
	return err
}
func (c *Controller) Loop(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		if e := c.Tick(ctx); e != nil && ctx.Err() == nil && c.Logger != nil {
			c.Logger.Printf("maintenance: %v", e)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
