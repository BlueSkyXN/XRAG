// Package research implements bounded, read-only retrieval investigations. It
// uses the same core operations as direct callers; it does not execute business
// writes, arbitrary programs, remote URLs, or recursive agents.
package research

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"github.com/BlueSkyXN/XRAG/core"
)

type Task struct {
	KB         string `json:"kb"`
	Goal       string `json:"goal"`
	Path       string `json:"path,omitempty"`
	MaxSteps   int    `json:"max_steps,omitempty"`
	MaxSeconds int    `json:"max_seconds,omitempty"`
}
type Quantity struct {
	Slot       string `json:"slot"`
	EvidenceID string `json:"evidence_id"`
	Metric     string `json:"metric"`
}
type Finding struct {
	Claim        string     `json:"claim"`
	EvidenceIDs  []string   `json:"evidence_ids"`
	Quantities   []Quantity `json:"quantities,omitempty"`
	EvidenceRefs []string   `json:"evidence_refs,omitempty"`
}
type Evidence struct {
	ID       string        `json:"id"`
	Ref      string        `json:"ref"`
	Kind     string        `json:"kind"`
	Content  any           `json:"content"`
	Metric   *core.Metric  `json:"metric,omitempty"`
	Coverage core.Coverage `json:"coverage"`
}
type Decision struct {
	Action     string       `json:"action,omitempty"`
	Arguments  core.Request `json:"arguments,omitempty"`
	Finish     bool         `json:"finish,omitempty"`
	Findings   []Finding    `json:"findings,omitempty"`
	Unresolved []string     `json:"unresolved,omitempty"`
}
type Observation struct {
	Step   int           `json:"step"`
	Action string        `json:"action"`
	Result core.Response `json:"result"`
}
type State struct {
	Task           Task          `json:"task"`
	Observations   []Observation `json:"observations"`
	Evidence       []Evidence    `json:"evidence"`
	Scope          string        `json:"scope_handle"`
	StepsRemaining int           `json:"steps_remaining"`
}
type Result struct {
	Kind               string        `json:"kind"`
	Goal               string        `json:"goal"`
	KB                 string        `json:"kb"`
	State              string        `json:"state"`
	StopReason         string        `json:"stop_reason"`
	Findings           []Finding     `json:"findings"`
	Evidence           []Evidence    `json:"evidence"`
	Observations       []Observation `json:"observations"`
	Unresolved         []string      `json:"unresolved"`
	Steps              int           `json:"steps"`
	Epoch              int64         `json:"withdrawal_epoch"`
	Scope              string        `json:"scope_handle"`
	ReferencesExpireAt int64         `json:"references_expire_at,omitempty"`
	Started            int64         `json:"started_at"`
	Completed          int64         `json:"completed_at"`
}
type Planner interface {
	Next(context.Context, State) (Decision, error)
}
type PlannerFunc func(context.Context, State) (Decision, error)

func (f PlannerFunc) Next(ctx context.Context, s State) (Decision, error) { return f(ctx, s) }

type Runner struct {
	DeterministicPlanner bool
	Engine               *core.Engine
	Planner              Planner
	Owner                string
	Check                func(context.Context) error
	Record               func(context.Context, Observation) error
	Initial              *Checkpoint
	Save                 CheckpointSave
}

var allowed = map[string]bool{"kb.discover": true, "search.keyword": true, "search.semantic": true, "files.read": true, "files.grep": true, "graph.entity": true, "graph.neighbors": true, "graph.path": true, "graph.chunks": true, "+context": true, "+ask": true}

func Allowed(action string) bool { return allowed[action] }
func Normalize(t Task) (Task, error) {
	t.Goal = strings.TrimSpace(t.Goal)
	if !core.ValidKB(t.KB) || t.Goal == "" || len([]rune(t.Goal)) > 2000 {
		return t, core.Err("INVALID_ARGUMENT", "kb and goal (1..2000 characters) required", 400)
	}
	if t.MaxSteps == 0 {
		t.MaxSteps = 8
	}
	if t.MaxSeconds == 0 {
		t.MaxSeconds = 120
	}
	if t.MaxSteps < 1 || t.MaxSteps > 16 || t.MaxSeconds < 1 || t.MaxSeconds > 600 {
		return t, core.Err("INVALID_ARGUMENT", "max_steps 1..16; max_seconds 1..600", 400)
	}
	if t.Path != "" {
		p, e := core.CleanPath(strings.TrimSuffix(t.Path, "/"))
		if e != nil {
			return t, e
		}
		t.Path = p
	}
	return t, nil
}
func (r Runner) Run(ctx context.Context, task Task) (out Result, err error) {
	task, err = Normalize(task)
	if err != nil {
		return out, err
	}
	out = Result{Kind: "research", Goal: task.Goal, KB: task.KB, State: "partial", Findings: []Finding{}, Evidence: []Evidence{}, Observations: []Observation{}, Unresolved: []string{}, Started: time.Now().Unix()}
	defer func() {
		out.Completed = time.Now().Unix()
		if err != nil {
			out.State = "failed"
			out.StopReason = core.AsError(err).Code
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				out.StopReason = "interrupted"
			}
			if core.AsError(err).Code == "VIEW_INVALIDATED" {
				out.Evidence = nil
				out.Observations = nil
				out.Findings = nil
			}
		}
	}()
	if r.Engine == nil || r.Planner == nil {
		return out, core.Err("CAPABILITY_UNAVAILABLE", "retrieval engine and planner required", 422)
	}
	deadline := time.Now().Add(time.Duration(task.MaxSeconds) * time.Second)
	if r.Initial != nil {
		deadline = time.Unix(r.Initial.Result.Started+int64(task.MaxSeconds), 0)
	}
	ctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	epoch, e := r.Engine.Repo.Epoch(ctx)
	if e != nil {
		return out, e
	}
	if r.Initial != nil {
		if err = r.Initial.CanResume(); err != nil {
			return out, err
		}
		if core.Hash(r.Initial.Task) != core.Hash(task) {
			return out, core.Err("CHECKPOINT_INVALID", "task does not match the saved checkpoint", 409)
		}
		if r.Initial.Result.Epoch != epoch {
			return out, core.Err("VIEW_INVALIDATED", "source withdrawal invalidates the saved scope", 409)
		}
		out = r.Initial.Result
		if _, err = r.Engine.Run(ctx, "handles.describe", core.Request{Kbs: []string{task.KB}, ID: out.Scope}, r.Owner); err != nil {
			return out, err
		}
	}
	out.Epoch = epoch
	check := func() error {
		if e := ctx.Err(); e != nil {
			return e
		}
		if r.Check != nil {
			if e := r.Check(ctx); e != nil {
				return e
			}
		}
		now, e := r.Engine.Repo.Epoch(ctx)
		if e != nil {
			return e
		}
		if now != epoch {
			return core.Err("VIEW_INVALIDATED", "a source was withdrawn during investigation; start a new task", 409)
		}
		return nil
	}
	if err = check(); err != nil {
		return out, err
	}
	root := core.Response{}
	if r.Initial == nil {
		filters := []core.Filter{}
		if task.Path != "" {
			filters = append(filters, core.Filter{Field: "path", Op: "prefix", Value: task.Path})
		}
		root, err = r.Engine.Run(ctx, "files.tree", core.Request{Kbs: []string{task.KB}, Filters: filters, Limit: 1, MaxChars: 1000, SaveScope: "required"}, r.Owner)
		if err != nil {
			return out, err
		}
		if root.Handle == "" {
			return out, core.Err("HANDLE_TOO_LARGE", "cannot fix the complete investigation scope", 422)
		}
		out.Scope = root.Handle
	}
	known := map[string]bool{}
	evidenceByRef := map[string]int{}
	registerRefs := func(resp core.Response) {
		if resp.SummaryRef != "" {
			known[resp.SummaryRef] = true
		}
		for _, it := range resp.Items {
			if it.Ref != "" {
				known[it.Ref] = true
			}
			for _, ref := range it.Refs {
				known[ref] = true
			}
		}
	}
	addEvidence := func(ref, kind string, content any, metric *core.Metric, cov core.Coverage) {
		if ref == "" {
			return
		}
		if _, ok := evidenceByRef[ref]; ok {
			return
		}
		id := fmt.Sprintf("e%d", len(out.Evidence)+1)
		evidenceByRef[ref] = len(out.Evidence)
		out.Evidence = append(out.Evidence, Evidence{ID: id, Ref: ref, Kind: kind, Content: content, Metric: metric, Coverage: cov})
		known[ref] = true
	}
	// Metadata listing is not silently promoted to task findings. The planner must
	// explicitly run a query and select evidence before making a claim.
	registerRefs(root)
	repeats := map[string]bool{}
	stagnant := 0
	startStep := 0
	checkpointRevision := int64(0)
	phase := "ready"
	var pending *Decision
	if r.Initial != nil {
		startStep = r.Initial.NextStep
		checkpointRevision = r.Initial.Revision
		phase = r.Initial.Phase
		pending = r.Initial.Pending
		for _, ref := range r.Initial.KnownRefs {
			known[ref] = true
		}
		for i, ev := range out.Evidence {
			evidenceByRef[ev.Ref] = i
			known[ev.Ref] = true
		}
		for k, v := range r.Initial.Repeated {
			repeats[k] = v
		}
		stagnant = r.Initial.Stagnant
		if phase == "complete" {
			return out, nil
		}
	}
	save := func(next int, p string, d *Decision) error {
		if r.Save == nil {
			return nil
		}
		refs := make([]string, 0, len(known))
		for ref := range known {
			refs = append(refs, ref)
		}
		sort.Strings(refs)
		cp := Checkpoint{DeterministicPlanner: r.DeterministicPlanner, Version: "xrag.checkpoint/v1", Revision: checkpointRevision, Phase: p, NextStep: next, Task: task, Result: out, Pending: d, KnownRefs: refs, Repeated: repeats, Stagnant: stagnant}
		if e := r.Save(ctx, &cp); e != nil {
			return e
		}
		checkpointRevision = cp.Revision
		return nil
	}
	if r.Initial == nil {
		if err = save(0, "ready", nil); err != nil {
			return out, err
		}
	}
	for step := startStep; step < task.MaxSteps; step++ {

		if err = check(); err != nil {
			return out, err
		}
		state := State{Task: task, Observations: out.Observations, Evidence: out.Evidence, Scope: out.Scope, StepsRemaining: task.MaxSteps - step}
		if len(core.JSON(state)) > 180000 {
			return out, core.Err("CONTEXT_LIMIT", "investigation context budget reached", 422)
		}
		var d Decision
		replayExecution := step == startStep && phase == "executing"
		if step == startStep && (phase == "planned" || phase == "executing") && pending != nil {
			d = *pending
		} else {
			if err = save(step, "planning", nil); err != nil {
				return out, err
			}
			d, err = r.Planner.Next(ctx, state)
			if err != nil {
				return out, err
			}
		}
		out.Steps = step + 1
		if !replayExecution {
			if err = save(step, "planned", &d); err != nil {
				return out, err
			}
		}
		if err = check(); err != nil {
			return out, err
		}
		if d.Finish {
			if d.Action != "" {
				return out, core.Err("MODEL_RESPONSE_INVALID", "finish and tool action are mutually exclusive", 502)
			}
			if len(d.Findings) > 20 || len(d.Unresolved) > 20 {
				return out, core.Err("MODEL_RESPONSE_INVALID", "too many findings", 502)
			}
			fs, e := RenderFindings(d.Findings, out.Evidence)
			if e != nil {
				return out, e
			}
			if len(fs) == 0 && len(d.Unresolved) == 0 {
				return out, core.Err("MODEL_RESPONSE_INVALID", "finish requires findings or unresolved explanation", 502)
			}
			out.Findings = fs
			out.Unresolved = d.Unresolved
			out.State = "succeeded"
			out.StopReason = "completed"
			if len(out.Unresolved) > 0 {
				out.State = "partial"
				out.StopReason = "completed_with_gaps"
			}
			out.Completed = time.Now().Unix()
			if err = save(step+1, "complete", nil); err != nil {
				return out, err
			}
			return out, nil
		}
		if !Allowed(d.Action) {
			return out, core.Err("ACTION_NOT_ALLOWED", "investigation permits retrieval actions only", 422)
		}
		if len(d.Findings) > 0 {
			return out, core.Err("MODEL_RESPONSE_INVALID", "findings belong to finish only", 502)
		}
		q := d.Arguments
		if len(q.Kbs) > 0 && !core.Subset(q.Kbs, []string{task.KB}) {
			return out, core.Err("SCOPE_VIOLATION", "cannot query another KB", 403)
		}
		if q.Within != "" && q.Within != out.Scope {
			return out, core.Err("SCOPE_VIOLATION", "cannot replace the fixed scope", 403)
		}
		if core.Bool(q.External) || q.Cursor != "" || q.ResultID != "" || q.SaveScope != "" && q.SaveScope != "none" {
			return out, core.Err("ACTION_NOT_ALLOWED", "external expansion, additional scopes and cursors are not allowed in this bounded runner", 422)
		}
		q.Kbs = []string{task.KB}
		q.Within = out.Scope
		q.SaveScope = "none"
		q.Trace = true
		off := false
		q.External = &off
		if q.Limit <= 0 {
			q.Limit = 5
		}
		if q.Limit > 10 {
			q.Limit = 10
		}
		if q.MaxChars <= 0 || q.MaxChars > 5000 {
			q.MaxChars = 5000
		}
		if q.Length <= 0 || q.Length > 5000 {
			q.Length = 5000
		}
		if d.Action == "files.read" {
			if q.Ref == "" || !known[q.Ref] {
				return out, core.Err("EVIDENCE_UNKNOWN", "read only references returned by this investigation", 422)
			}
			q.Within = ""
			q.Path = "" // fixed immutable refs were obtained within the scope
		}
		key := core.Hash([]any{d.Action, q})
		if !replayExecution {
			if repeats[key] {
				stagnant++
			} else {
				stagnant = 0
				repeats[key] = true
			}
		}
		if stagnant >= 2 {
			out.StopReason = "no_new_action"
			out.Unresolved = []string{"Repeated identical retrieval without new action."}
			return out, nil
		}
		d.Arguments = q
		if err = save(step, "executing", &d); err != nil {
			return out, err
		}
		resp, e := r.Engine.Run(ctx, d.Action, q, r.Owner)
		if e != nil {
			return out, e
		}
		if err = check(); err != nil {
			return out, err
		}
		resp.Trace = nil
		if len(core.JSON(resp)) > 35000 {
			return out, core.Err("RESULT_LIMIT", "single tool result exceeds investigation context budget", 422)
		}
		registerRefs(resp)
		if (d.Action == "search.keyword" || d.Action == "files.grep") && resp.SummaryRef != "" && resp.Total.Value != nil {
			m := resp.Total
			addEvidence(resp.SummaryRef, "aggregate", map[string]any{"metric": m, "action": d.Action, "query": q, "coverage": resp.Coverage}, &m, resp.Coverage)
		}
		if d.Action == "files.read" {
			switch v := resp.Data.(type) {
			case core.ReadData:
				addEvidence(q.Ref, v.Kind, v, nil, resp.Coverage)
			default:
				addEvidence(q.Ref, "structured_result", v, nil, resp.Coverage)
			}
		}
		for _, it := range resp.Items {
			if it.Kind == "path" || it.Kind == "relation" {
				addEvidence(it.Ref, it.Kind, it, nil, resp.Coverage)
			}
		}
		ob := Observation{step + 1, d.Action, resp}
		out.Observations = append(out.Observations, ob)
		if r.Record != nil {
			if err = r.Record(ctx, ob); err != nil {
				return out, err
			}
		}
		if err = save(step+1, "ready", nil); err != nil {
			return out, err
		}
	}
	out.StopReason = "step_budget"
	out.Unresolved = []string{"Maximum planning/tool steps reached; findings are not automatically invented from remaining candidates."}
	return out, nil
}
func RenderFindings(findings []Finding, evidence []Evidence) ([]Finding, error) {
	by := map[string]Evidence{}
	for _, e := range evidence {
		by[e.ID] = e
	}
	out := []Finding{}
	for _, f := range findings {
		if strings.TrimSpace(f.Claim) == "" || len([]rune(f.Claim)) > 4000 || len(f.EvidenceIDs) == 0 || len(f.EvidenceIDs) > 30 {
			return nil, core.Err("MODEL_RESPONSE_INVALID", "each finding requires a bounded claim and registered evidence", 502)
		}
		refs := []string{}
		nonAggregate := false
		ids := map[string]bool{}
		for _, id := range f.EvidenceIDs {
			e, ok := by[id]
			if !ok {
				return nil, core.Err("EVIDENCE_UNKNOWN", "finding cites unregistered evidence", 502)
			}
			refs = append(refs, e.Ref)
			if e.Kind != "aggregate" {
				nonAggregate = true
			}
			ids[id] = true
		}
		if !nonAggregate && len(f.Quantities) == 0 {
			return nil, core.Err("QUANTITY_UNSUPPORTED", "aggregate-only finding must render its bound metric", 502)
		}
		if len(f.Quantities) > 20 {
			return nil, core.Err("MODEL_RESPONSE_INVALID", "too many quantity bindings", 502)
		}
		slots := map[string]bool{}
		for _, q := range f.Quantities {
			e, ok := by[q.EvidenceID]
			m := e.Metric
			if !ok || !ids[q.EvidenceID] || q.Metric != "total" || q.Slot == "" || strings.ContainsAny(q.Slot, "{} ") || slots[q.Slot] || m == nil || m.Value == nil || m.Accuracy != "exact" || !m.CalculationComplete || e.Coverage.ExecutionPartial {
				return nil, core.Err("QUANTITY_UNSUPPORTED", "quantities must bind exact complete total metrics in cited evidence", 502)
			}
			slot := "{" + q.Slot + "}"
			if !strings.Contains(f.Claim, slot) {
				return nil, core.Err("QUANTITY_UNSUPPORTED", "quantity slot is absent from claim", 502)
			}
			f.Claim = strings.ReplaceAll(f.Claim, slot, fmt.Sprintf("%d %s", *m.Value, m.Unit))
			slots[q.Slot] = true
		}
		f.EvidenceRefs = core.Unique(refs)
		out = append(out, f)
	}
	return out, nil
}
