package contracts

// Tasks are opt-in; they do not change the default retrieval tool set.
var TaskActions = []Action{
	{Action: "research.submit", Path: "/v1/research", Tool: "research_submit", Group: "research", Input: "research.submit.json", Description: "Submit a bounded read-only investigation. Uses configured Chat and incurs model usage. Returns a job, not an immediate answer."},
	{Action: "recipe.run", Path: "/v1/recipes/run", Tool: "recipe_run", Group: "research", Input: "recipe.run.json", Description: "Run an explicit finite retrieval recipe. Returns saved observations and evidence; does not invent a conclusion or write knowledge."},
	{Action: "task.status", Path: "/v1/tasks/status", Tool: "research_status", Group: "research", Input: "task.id.json", Description: "Read your task status and its saved findings, evidence and stopping reason."},
	{Action: "task.cancel", Path: "/v1/tasks/cancel", Tool: "research_cancel", Group: "research", Input: "task.id.json", Description: "Request cancellation. Previously issued provider calls cannot be undone."},
	{Action: "task.resume", Path: "/v1/tasks/resume", Tool: "research_resume", Group: "research", Input: "task.id.json", Description: "Explicitly resume an interrupted task from a usable checkpoint. Preserves scope, original deadline and budget; refuses unknown model outcomes."},
}

func FindTask(s string) (Action, bool) {
	for _, a := range TaskActions {
		if a.Action == s || a.Tool == s || a.Path == s {
			return a, true
		}
	}
	return Action{}, false
}
func TaskOutput(a Action) map[string]any {
	p := map[string]any{"id": map[string]any{"type": "string"}, "state": map[string]any{"type": "string"}, "result": map[string]any{}}
	required := []string{"id", "state"}
	if a.Action == "task.cancel" {
		p["cancel_requested"] = map[string]any{"type": "boolean"}
		required = []string{"id", "cancel_requested"}
	}
	return map[string]any{"type": "object", "required": required, "properties": p}
}
