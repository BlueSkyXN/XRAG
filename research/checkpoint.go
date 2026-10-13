package research

import (
	"context"
	"github.com/BlueSkyXN/XRAG/core"
)

// Checkpoint contains only a bounded investigation state, never executable code.
// The host persists it under the task's original owner, scope and deadline.
type Checkpoint struct {
	DeterministicPlanner bool            `json:"deterministic_planner,omitempty"`
	Version              string          `json:"version"`
	Revision             int64           `json:"revision"`
	Phase                string          `json:"phase"`
	NextStep             int             `json:"next_step"`
	Task                 Task            `json:"task"`
	Result               Result          `json:"result"`
	Pending              *Decision       `json:"pending,omitempty"`
	KnownRefs            []string        `json:"known_refs"`
	Repeated             map[string]bool `json:"repeated"`
	Stagnant             int             `json:"stagnant"`
}

// ReplaySafe is intentionally narrow. A Core operation may call a paid model or
// live service. "Read-only" never implies free or deterministic replay.
func ReplaySafe(action string) bool {
	switch action {
	case "files.read", "files.grep", "search.keyword", "kb.discover":
		return true
	}
	return false
}
func (c Checkpoint) CanResume() error {
	if c.Version != "xrag.checkpoint/v1" || c.NextStep < 0 || c.NextStep > c.Task.MaxSteps || c.Result.Scope == "" {
		return core.Err("CHECKPOINT_INVALID", "unsupported or incomplete checkpoint", 409)
	}
	switch c.Phase {
	case "ready", "complete":
		return nil
	case "planned":
		if c.Pending == nil {
			return core.Err("CHECKPOINT_INVALID", "saved decision missing", 409)
		}
		return nil
	case "executing":
		if c.Pending != nil && ReplaySafe(c.Pending.Action) {
			return nil
		}
		return core.Err("RESUME_OUTCOME_UNKNOWN", "tool may have called a model or live backend; start an explicit new task instead of replaying unknown work", 409)
	case "planning":
		if c.DeterministicPlanner {
			return nil
		}
		return core.Err("RESUME_OUTCOME_UNKNOWN", "planner request outcome was not saved; its usage is retained and not replayed automatically", 409)
	}
	return core.Err("CHECKPOINT_INVALID", "unknown checkpoint phase", 409)
}

type CheckpointSave func(context.Context, *Checkpoint) error
