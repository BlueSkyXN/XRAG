// Package automation turns explicitly enabled checks into the same build jobs
// as manual requests. It never builds or calls a model itself.
package automation

import (
	"context"
	"sync"
	"time"
	"github.com/BlueSkyXN/XRAG/core"
	"github.com/BlueSkyXN/XRAG/internal/build"
	"github.com/BlueSkyXN/XRAG/internal/source"
	"github.com/BlueSkyXN/XRAG/internal/watch"
)

type observation struct {
	CronMinute      int64
	Monitor         watch.Monitor
	MonitorRoot     string
	Next            time.Time
	Fingerprint     string
	Since           time.Time
	LastJob         string
	LastFingerprint string
}
type Controller struct {
	Builder *build.Builder
	seen    map[string]*observation
}

func (c *Controller) Tick(ctx context.Context, now time.Time) error {
	if c.seen == nil {
		c.seen = map[string]*observation{}
	}
	kbs, e := c.Builder.Store.KBs(ctx)
	if e != nil {
		return e
	}
	present := map[string]bool{}
	defer func() {
		for key, o := range c.seen {
			if !present[key] {
				if o.Monitor != nil {
					_ = o.Monitor.Close()
				}
				delete(c.seen, key)
			}
		}
	}()
	for _, kb := range kbs {
		sources, e := c.Builder.Store.Sources(ctx, kb["id"].(string))
		if e != nil {
			return e
		}
		for _, src := range sources {
			present[src.KB+":"+src.ID] = true
			if src.Options.Disabled {
				if o := c.seen[src.KB+":"+src.ID]; o != nil && o.Monitor != nil {
					_ = o.Monitor.Close()
					o.Monitor = nil
				}
				continue
			}
			policy, e := c.Builder.Store.Policy(ctx, src.KB, src.ID)
			if e != nil {
				return e
			}
			key := src.KB + ":" + src.ID
			interval := policy.EverySeconds
			reason := "schedule"
			o := c.seen[key]
			if o == nil {
				o = &observation{}
				c.seen[key] = o
			}
			event := false
			if policy.NativeWatch && watch.Available() {
				if o.Monitor == nil || o.MonitorRoot != src.Root {
					if o.Monitor != nil {
						_ = o.Monitor.Close()
					}
					mon, err := watch.New(src.Root)
					if err == nil {
						o.Monitor = mon
						o.MonitorRoot = src.Root
					} else {
						_ = c.Builder.Store.ObserveSource(ctx, src, "", "", err)
					}
				}
				if o.Monitor != nil {
					dirty, err := o.Monitor.Changed()
					event = dirty
					if dirty || err != nil {
						_ = o.Monitor.Close()
						o.Monitor = nil
					}
				}
			} else if o.Monitor != nil {
				_ = o.Monitor.Close()
				o.Monitor = nil
			}
			cronDue := false
			if policy.Schedule != "" {
				schedule, err := core.ParseCron(policy.Schedule, policy.Timezone)
				if err != nil {
					return err
				}
				minute := now.Unix() / 60
				if schedule.Matches(now) && o.CronMinute != minute {
					cronDue = true
					o.CronMinute = minute
				}
			}
			if event {
				reason = "watch_event"
			} else if cronDue {
				reason = "cron"
			}
			if policy.WatchSeconds > 0 && (interval == 0 || policy.WatchSeconds < interval) {
				interval = policy.WatchSeconds
				reason = "watch_poll"
			}
			if !event && !cronDue && (interval == 0 || now.Before(o.Next)) {
				continue
			}
			if interval > 0 {
				o.Next = now.Add(time.Duration(interval) * time.Second)
			}
			scan, err := source.Open(ctx, c.Builder.Store.Dir, src)
			if err != nil {
				_ = c.Builder.Store.ObserveSource(ctx, src, "", "", err)
				continue
			}
			scoped, err := c.Builder.ForKB(ctx, src.KB)
			if err != nil {
				return err
			}
			fp := core.Hash([]string{scan.Fingerprint, scoped.Settings()})
			if e = c.Builder.Store.ObserveSource(ctx, src, scan.Revision, fp, nil); e != nil {
				return e
			}
			state, e := c.Builder.Store.SourceState(ctx, src.KB, src.ID)
			if e != nil {
				return e
			}
			if state.Indexed == fp {
				o.Fingerprint = ""
				o.LastJob = ""
				continue
			}
			if o.Fingerprint != fp {
				o.Fingerprint = fp
				o.Since = now
			}
			if (policy.WatchSeconds > 0 || policy.NativeWatch) && now.Sub(o.Since) < time.Duration(policy.DebounceSeconds)*time.Second {
				o.Next = now.Add(time.Duration(policy.DebounceSeconds) * time.Second)
				continue
			}
			if o.LastFingerprint == fp && o.LastJob != "" {
				j, err := c.Builder.Store.Job(ctx, o.LastJob)
				if err == nil && (j.State == "queued" || j.State == "running") {
					continue
				}
			}
			j, e := c.Builder.Store.Trigger(ctx, src.KB, src.ID, reason, "automation", "", nil)
			if e != nil {
				return e
			}
			o.LastFingerprint = fp
			o.LastJob = j.ID
		}
	}
	return nil
}
func (c *Controller) Loop(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()
	defer func() {
		for _, o := range c.seen {
			if o.Monitor != nil {
				_ = o.Monitor.Close()
			}
		}
	}()
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			if e := c.Tick(ctx, now); e != nil && ctx.Err() == nil && c.Builder.Logger != nil {
				c.Builder.Logger.Printf("automation: %v", e)
			}
		}
	}
}
