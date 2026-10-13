package build

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"
	"github.com/BlueSkyXN/XRAG/core"
	"github.com/BlueSkyXN/XRAG/evaluation"
	"github.com/BlueSkyXN/XRAG/internal/store"
)

// Query algorithms are unchanged. Only the view is frozen for all paired runs.
// The shared per-KB job lock excludes builds and knowledge publishes during evaluation.
type frozenRepo struct {
	core.Repository
	View core.View
}

func (r frozenRepo) Snapshot(ctx context.Context, kbs []string) (core.View, error) {
	return r.View, nil
}
func (b *Builder) RunEval(ctx context.Context, j store.Job) (any, error) {
	scoped, scopeErr := b.ForKB(ctx, j.KB)
	if scopeErr != nil {
		return nil, scopeErr
	}
	b = scoped
	ctx = b.MeterContext(ctx, j.KB, j.ID, j.Owner)

	var req evaluation.Request
	if e := json.Unmarshal(j.Payload, &req); e != nil {
		return nil, e
	}
	if e := evaluation.ValidateSet(req.Set); e != nil {
		return nil, e
	}
	if req.Set.KB != j.KB || req.K < 1 || req.K > 50 || len(req.Strategies) == 0 || len(req.Strategies) > 5 {
		return nil, core.Err("INVALID_ARGUMENT", "evaluation request outside limits", 400)
	}
	for _, s := range req.Strategies {
		if !core.Subset([]string{s}, evaluation.Strategies) {
			return nil, core.Err("INVALID_ARGUMENT", "unknown evaluation strategy", 400)
		}
	}
	v, e := b.Store.Snapshot(ctx, []string{j.KB})
	if e != nil {
		return nil, e
	}
	// Stale labels fail explicitly instead of disappearing from the denominator.
	docs := map[string]string{}
	manifest := []string{}
	for _, d := range v.Documents {
		docs[d.Path] = d.Body
		manifest = append(manifest, d.Path+":"+d.Edition+":"+d.FileHash)
	}
	sort.Strings(manifest)
	for _, c := range req.Set.Cases {
		for _, x := range c.Expected {
			body, ok := docs[x.Path]
			if !ok || x.Anchor != "" && !strings.Contains(body, x.Anchor) {
				return nil, core.Err("EVAL_DATASET_INVALID", "expected path/anchor not in published view: "+c.ID+" / "+x.Path, 422)
			}
		}
	}
	report := evaluation.Report{Family: req.Set.Family, Purpose: req.Set.Purpose, SetID: req.Set.ID, SetRevision: req.Set.Revision, KB: j.KB, K: req.K, ViewEpoch: v.Epoch, ManifestHash: core.Hash(manifest), SettingsHash: b.Settings(), Rows: []evaluation.Row{}, Summaries: []evaluation.Summary{}, Pairs: []evaluation.Pair{}, Notes: []string{"Human expectations are supplied, not generated. This is a small diagnostic dataset, not a production quality claim.", "All strategies share the captured file editions. The per-KB job lock blocks XRAG builds/publishes; withdrawals stop the run. External model nondeterminism and independent backend changes cannot be frozen.", "Errors count as zero in positive-case metrics; expected-empty cases have a separate accuracy. Query results are capped at k before scoring; no per-file diversity cutoff.", "Output snippets/trace are bounded. Raw model costs are not inferred from duration."}}
	engine, e := b.EngineFor(ctx, []string{j.KB})
	if e != nil {
		return nil, e
	}
	engine.Repo = frozenRepo{Repository: b.Store, View: v}
	no, yes := false, true
	for _, strategy := range req.Strategies {
		for _, c := range req.Set.Cases {
			if e = b.Store.CheckJob(ctx, j); e != nil {
				return nil, e
			}
			ep, e := b.Store.Epoch(ctx)
			if e != nil {
				return nil, e
			}
			if ep != v.Epoch {
				return nil, core.Err("VIEW_INVALIDATED", "source withdrawn during evaluation", 409)
			}
			action := "+ask"
			q := core.Request{Kbs: []string{j.KB}, Q: c.Question, Limit: req.K, MaxChars: 200000, PerFile: 100, SaveScope: "none", Trace: true, Graph: &no, Rerank: &no, External: &no, RequireComplete: false}
			switch strategy {
			case "keyword":
				action = "search.keyword"
			case "semantic":
				action = "search.semantic"
			case "ask_graph":
				q.Graph = &yes
			case "ask_rerank":
				q.Rerank = &yes
			}
			started := time.Now()
			r, err := engine.Run(ctx, action, q, j.Owner)
			if err == nil && r.Coverage.ExecutionPartial {
				err = core.Err("EVAL_EXECUTION_PARTIAL", "selected strategy did not complete all required routes", 422)
			}
			row := evaluation.Row{CaseID: c.ID, Question: c.Question, ExpectedEmpty: c.ExpectedEmpty, Strategy: strategy, QueryID: r.QueryID, DurationMS: time.Since(started).Milliseconds(), Items: []core.Item{}, Trace: r.Trace, Actual: r.Strategy}
			if err != nil {
				row.Error = core.AsError(err)
				row.Score = evaluation.Score{Matched: []string{}, Missed: []string{}}
			} else {
				row.Score = evaluation.ScoreCase(c, r.Items, req.K)
				for _, it := range r.Items {
					it.Text = ""
					it.Fields = nil
					it.Edges = nil
					it.Refs = nil
					row.Items = append(row.Items, it)
				}
			}
			report.Rows = append(report.Rows, row)
		}
	}
	ep, e := b.Store.Epoch(ctx)
	if e != nil {
		return nil, e
	}
	if ep != v.Epoch {
		return nil, core.Err("VIEW_INVALIDATED", "source withdrawn during evaluation", 409)
	}
	for _, strategy := range req.Strategies {
		sum := evaluation.Summary{Strategy: strategy}
		times := []int64{}
		correct := 0
		for _, r := range report.Rows {
			if r.Strategy != strategy {
				continue
			}
			sum.Cases++
			times = append(times, r.DurationMS)
			if r.Error != nil {
				sum.Errors++
			}
			if r.ExpectedEmpty {
				sum.Negatives++
				if r.Score.EmptyCorrect && r.Error == nil {
					correct++
				}
			} else {
				sum.Positives++
				sum.HitAtK += r.Score.Hit
				sum.RecallAtK += r.Score.Recall
				sum.MRR += r.Score.ReciprocalRank
			}
		}
		if sum.Positives > 0 {
			sum.HitAtK /= float64(sum.Positives)
			sum.RecallAtK /= float64(sum.Positives)
			sum.MRR /= float64(sum.Positives)
		}
		if sum.Negatives > 0 {
			x := float64(correct) / float64(sum.Negatives)
			sum.NegativeAccuracy = &x
		}
		sort.Slice(times, func(i, j int) bool { return times[i] < times[j] })
		if len(times) > 0 {
			sum.P50MS = times[(len(times)-1)/2]
			sum.P95MS = times[(len(times)*95+99)/100-1]
		}
		report.Summaries = append(report.Summaries, sum)
	}
	for i, a := range req.Strategies {
		for _, b := range req.Strategies[i+1:] {
			pair := evaluation.Pair{A: a, B: b, OnlyA: []string{}, OnlyB: []string{}}
			for _, c := range req.Set.Cases {
				if c.ExpectedEmpty {
					continue
				}
				ha, hb := false, false
				for _, r := range report.Rows {
					if r.CaseID == c.ID {
						if r.Strategy == a {
							ha = r.Score.Hit > 0
						}
						if r.Strategy == b {
							hb = r.Score.Hit > 0
						}
					}
				}
				if ha && !hb {
					pair.OnlyA = append(pair.OnlyA, c.ID)
				}
				if hb && !ha {
					pair.OnlyB = append(pair.OnlyB, c.ID)
				}
			}
			report.Pairs = append(report.Pairs, pair)
		}
	}
	return report, nil
}
