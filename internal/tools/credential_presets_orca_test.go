package tools

import (
	"encoding/json"
	"slices"
	"testing"
)

func TestOrcaPresetContract(t *testing.T) {
	preset := GetPreset("orca")
	if preset == nil {
		t.Fatal("orca preset is missing")
	}
	if preset.BinaryName != "orca" {
		t.Fatalf("BinaryName = %q, want orca", preset.BinaryName)
	}
	if preset.AdapterName != "" {
		t.Fatalf("AdapterName = %q, want passthrough so sandboxed exec keeps working", preset.AdapterName)
	}
	if got := requiredCredentialEnvVars("orca"); !slices.Contains(got, "ORCA_PAIRING_CODE") {
		t.Fatalf("requiredCredentialEnvVars(orca) = %v, missing ORCA_PAIRING_CODE", got)
	}
}

func TestOrcaPresetDenyArgs(t *testing.T) {
	preset := GetPreset("orca")
	if preset == nil {
		t.Fatal("orca preset is missing")
	}
	deny, err := json.Marshal(preset.DenyArgs)
	if err != nil {
		t.Fatal(err)
	}

	blocked := [][]string{
		{"--json", "computer", "click"},
		{"--pairing-code=orca://pair?code=x", "status"},
		{"status", "--pairing-code", "orca://pair?code=x"},
		{"worktree", "list", "--environment", "other"},
		{"serve", "--port", "6768"},
		{"open"},
		{"environment", "add", "--name", "x", "--pairing-code", "y"},
		{"account", "list"},
		{"computer", "list-apps"},
		{"diagnostics", "memory"},
		{"skills", "install", "--skill", "orca-cli"},
		{"orchestration", "reset"},
	}
	for _, args := range blocked {
		if got := matchesBinaryDeny(args, deny); got == "" {
			t.Errorf("matchesBinaryDeny(%v) allowed, want blocked", args)
		}
	}

	allowed := [][]string{
		{"status", "--json"},
		{"worktree", "list", "--json"},
		{"worktree", "create", "--name", "fix-login", "--no-parent", "--agent", "claude", "--prompt", "fix it", "--json"},
		{"agent", "launch", "--agent", "codex", "--worktree", "id:r::/p", "--prompt", "go", "--json"},
		{"terminal", "send", "--terminal", "t1", "--text", "open the file", "--enter", "--json"},
		{"skills", "get", "orca-cli"},
		{"orchestration", "task-list", "--json"},
	}
	for _, args := range allowed {
		if got := matchesBinaryDeny(args, deny); got != "" {
			t.Errorf("matchesBinaryDeny(%v) blocked by %q, want allowed", args, got)
		}
	}
}
