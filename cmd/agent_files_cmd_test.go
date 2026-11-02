package cmd

import (
	"strings"
	"testing"
)

func TestApplyAgentFilePatchReplacesSingleOccurrence(t *testing.T) {
	content := "## Rules\n- Rule A.\n- Rule B.\n"
	got, err := applyAgentFilePatch(content, "- Rule B.\n", "- Rule B.\n- Rule C.\n")
	if err != nil {
		t.Fatalf("applyAgentFilePatch: %v", err)
	}
	if got != "## Rules\n- Rule A.\n- Rule B.\n- Rule C.\n" {
		t.Fatalf("got %q", got)
	}
}

func TestApplyAgentFilePatchRejectsUnsafeInput(t *testing.T) {
	content := "- same\n- same\n- unique\n"
	cases := map[string]struct {
		old, new, want string
	}{
		"missing":   {"- absent", "- x", "not found"},
		"ambiguous": {"- same", "- x", "occurs 2 times"},
		"empty":     {"  ", "- x", "must not be empty"},
		"identical": {"- unique", "- unique", "identical"},
		"too large": {"- unique", strings.Repeat("x", agentFilesMaxPatchSize+1), "max"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := applyAgentFilePatch(content, tc.old, tc.new)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want containing %q", err, tc.want)
			}
		})
	}
}

func TestCheckAgentFilePatchAllowed(t *testing.T) {
	if err := checkAgentFilePatchAllowed("tech-lead", "AGENTS.md", "ops-retro, amee-orchestrator"); err != nil {
		t.Fatalf("tech-lead AGENTS.md rejected: %v", err)
	}
	if err := checkAgentFilePatchAllowed("ops-retro", "AGENTS.md", "ops-retro, amee-orchestrator"); err == nil {
		t.Fatal("protected agent was allowed")
	}
	for _, file := range []string{"SOUL.md", "IDENTITY.md", "USER_PREDEFINED.md", "../AGENTS.md"} {
		if err := checkAgentFilePatchAllowed("tech-lead", file, ""); err == nil {
			t.Fatalf("file %s was allowed", file)
		}
	}
}
