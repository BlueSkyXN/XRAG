package research

import (
	"context"
	"encoding/json"
	"strings"
	"github.com/BlueSkyXN/XRAG/core"
)

// Recipe is a finite read-only operation sequence, not an arbitrary DAG/script.
// $input performs string substitution; $last.ref reads the first result ref.
type Recipe struct {
	Version string       `json:"version"`
	Name    string       `json:"name"`
	Steps   []RecipeStep `json:"steps"`
}
type RecipeStep struct {
	Action    string       `json:"action"`
	Arguments core.Request `json:"arguments"`
}

func (r Recipe) Validate() error {
	if r.Version != "xrag.recipe/v1" || r.Name == "" || len(r.Steps) < 1 || len(r.Steps) > 15 {
		return core.Err("INVALID_RECIPE", "recipe version, name and 1..15 steps required", 400)
	}
	for _, s := range r.Steps {
		if !Allowed(s.Action) {
			return core.Err("ACTION_NOT_ALLOWED", "recipe permits bounded read-only actions", 422)
		}
	}
	return nil
}
func RecipePlanner(recipe Recipe) (Planner, error) {
	if err := recipe.Validate(); err != nil {
		return nil, err
	}
	return PlannerFunc(func(_ context.Context, st State) (Decision, error) {
		i := len(st.Observations)
		if i >= len(recipe.Steps) {
			return Decision{Finish: true, Unresolved: []string{"Fixed recipe completed; inspect observations and evidence. No model-generated investigation conclusion."}}, nil
		}
		step := recipe.Steps[i]
		raw := map[string]any{}
		json.Unmarshal([]byte(core.JSON(step.Arguments)), &raw)
		for key, v := range raw {
			if s, ok := v.(string); ok {
				if s == "$last.ref" {
					if len(st.Observations) == 0 || len(st.Observations[len(st.Observations)-1].Result.Items) == 0 {
						return Decision{}, core.Err("RECIPE_INPUT_MISSING", "previous step returned no first item", 422)
					}
					s = st.Observations[len(st.Observations)-1].Result.Items[0].Ref
				} else {
					s = strings.ReplaceAll(s, "$input", st.Task.Goal)
				}
				raw[key] = s
			}
		}
		var args core.Request
		json.Unmarshal([]byte(core.JSON(raw)), &args)
		return Decision{Action: step.Action, Arguments: args}, nil
	}), nil
}
