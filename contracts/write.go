package contracts

// Write tools are separate from the read-only query registry and are opt-in.
var WriteActions = []Action{
	{Action: "knowledge.submit", Path: "/v1/knowledge-submit", Tool: "knowledge_submit", Group: "write", Input: "knowledge.submit.json", Description: "Submit an evidence-backed knowledge draft. Does not publish or run a build."},
	{Action: "feedback.submit", Path: "/v1/feedback-submit", Tool: "feedback_submit", Group: "write", Input: "feedback.submit.json", Description: "Record feedback on a result. Does not modify ranking or source contents."},
}

func FindWrite(s string) (Action, bool) {
	for _, a := range WriteActions {
		if s == a.Action || s == a.Tool || s == a.Path {
			return a, true
		}
	}
	return Action{}, false
}
func WriteOutput(a Action) map[string]any {
	if a.Action == "feedback.submit" {
		return map[string]any{"type": "object", "properties": map[string]any{"saved": map[string]any{"type": "boolean"}}, "required": []string{"saved"}}
	}
	return map[string]any{"type": "object", "properties": map[string]any{"id": map[string]any{"type": "string"}, "draft_revision": map[string]any{"type": "string"}, "state": map[string]any{"type": "string"}}, "required": []string{"id", "draft_revision", "state"}}
}
