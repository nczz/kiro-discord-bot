package acp

import "testing"

func TestAgentModelAndModeHelpers(t *testing.T) {
	a := &Agent{sessResult: &SessionNewResult{
		Models: &ModelState{
			CurrentModelID:  "auto",
			AvailableModels: []ModelEntry{{ModelID: "auto"}, {ModelID: "claude-sonnet-4.6"}},
		},
		Modes: &ModeState{
			CurrentModeID:  "confident",
			AvailableModes: []ModeEntry{{ID: "confident"}, {ID: "kiro_default"}},
		},
	}}

	if !a.HasModel("auto") || a.HasModel("missing") {
		t.Fatalf("model helper mismatch")
	}
	if !a.HasMode("kiro_default") || a.HasMode("default") {
		t.Fatalf("mode helper mismatch")
	}

	a.setCurrentModelID("claude-sonnet-4.6")
	if got := a.CurrentModelID(); got != "claude-sonnet-4.6" {
		t.Fatalf("current model = %q", got)
	}
	a.setCurrentModeID("kiro_default")
	if got := a.CurrentModeID(); got != "kiro_default" {
		t.Fatalf("current mode = %q", got)
	}
}
