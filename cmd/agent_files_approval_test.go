package cmd

import (
	"strings"
	"testing"
)

func TestPatchApprovalBindsToExactContent(t *testing.T) {
	const secret = "test-secret"
	hash := proposalHash("tech-lead", "AGENTS.md", "- old rule", "- new rule")
	code, err := approvalCodeFor(hash, secret)
	if err != nil {
		t.Fatalf("approvalCodeFor: %v", err)
	}
	if len(code) != agentFilesApprovalCodeLen {
		t.Fatalf("code length = %d", len(code))
	}
	if err := checkPatchApproval(hash, code, secret, false); err != nil {
		t.Fatalf("valid approval rejected: %v", err)
	}

	changed := proposalHash("tech-lead", "AGENTS.md", "- old rule", "- different rule")
	if err := checkPatchApproval(changed, code, secret, false); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("approval reused for different content: %v", err)
	}
	otherAgent := proposalHash("orchestrator", "AGENTS.md", "- old rule", "- new rule")
	if otherAgent == hash {
		t.Fatal("proposal hash ignores agent")
	}
}

func TestPatchApprovalRequiredOnlyWhenEnabled(t *testing.T) {
	hash := proposalHash("tech-lead", "AGENTS.md", "a", "b")
	if err := checkPatchApproval(hash, "", "", false); err != nil {
		t.Fatalf("approvals disabled but patch blocked: %v", err)
	}
	if err := checkPatchApproval(hash, "", "secret", true); err != nil {
		t.Fatalf("dry run blocked: %v", err)
	}
	if err := checkPatchApproval(hash, "", "secret", false); err == nil || !strings.Contains(err.Error(), "needs founder approval") {
		t.Fatalf("missing approval accepted: %v", err)
	}
	if err := checkPatchApproval(hash, "0000000000000000", "secret", false); err == nil {
		t.Fatal("forged approval accepted")
	}
}

func TestApprovalCodeRejectsBadInput(t *testing.T) {
	if _, err := approvalCodeFor("abc", "secret"); err == nil {
		t.Fatal("short hash accepted")
	}
	if _, err := approvalCodeFor(strings.Repeat("a", 16), ""); err == nil {
		t.Fatal("approval issued without secret")
	}
}
