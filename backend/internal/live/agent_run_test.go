package live

import "testing"

func TestAgentRunViewOmitsInstructionAndParsesCheckpoint(t *testing.T) {
	summary := []byte(`{"purpose":"agent_workflow","checkpoint":{"phase":"tool_completed","instruction":"secret prompt","steps":[{"tool":"list_team_tasks"},{"tool":"web_search"}]}}`)
	view := agentRunView("ABCD", "ai-pool", "qwen3:14b", "running", summary, nil, nil)
	if view["purpose"] != "agent_workflow" || view["checkpointPhase"] != "tool_completed" {
		t.Fatalf("unexpected view: %#v", view)
	}
	steps, ok := view["steps"].([]string)
	if !ok || len(steps) != 2 || steps[0] != "list_team_tasks" || steps[1] != "web_search" {
		t.Fatalf("steps not parsed: %#v", view["steps"])
	}
	if _, leaked := view["instruction"]; leaked {
		t.Fatal("run view must not expose the instruction")
	}
	for _, value := range view {
		if text, ok := value.(string); ok && text == "secret prompt" {
			t.Fatal("run view leaked the instruction")
		}
	}
}

func TestCheckpointMessagesBoundsToolPayload(t *testing.T) {
	long := make([]byte, 40*1024)
	for i := range long {
		long[i] = 'x'
	}
	out := checkpointMessages([]aiMessage{{Role: "tool", Content: string(long)}})
	if len(out) != 1 || len(out[0].Content) > 16*1024 {
		t.Fatalf("checkpoint message not bounded: len=%d", len(out[0].Content))
	}
}
