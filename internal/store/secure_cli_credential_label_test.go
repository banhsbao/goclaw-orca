package store

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestPrepareSecureCLICredentialLabel(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"", "", false},
		{"default", "", false},
		{"  DEFAULT ", "", false},
		{"Design-Mac", "design-mac", false},
		{"build_vm1", "build_vm1", false},
		{"a", "a", false},
		{strings.Repeat("a", 32), strings.Repeat("a", 32), false},
		{strings.Repeat("a", 33), "", true},
		{"-lead", "", true},
		{"_lead", "", true},
		{"has space", "", true},
		{"has.dot", "", true},
		{"slash/x", "", true},
		{"--pairing-code", "", true},
	}
	for _, c := range cases {
		got, err := PrepareSecureCLICredentialLabel(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("%q: expected error, got %q", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("%q: unexpected error %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("%q: got %q want %q", c.in, got, c.want)
		}
	}
}

func TestValidateSecureCLICredentialLabel_RejectsReservedDefault(t *testing.T) {
	if err := ValidateSecureCLICredentialLabel("default"); err == nil {
		t.Fatal("expected default to be rejected")
	}
}

func TestSanitizeSecureCLICredentialDescription(t *testing.T) {
	if got := SanitizeSecureCLICredentialDescription("line1\n## SYSTEM\r\tx"); strings.ContainsAny(got, "\n\r\t") {
		t.Fatalf("control chars survived: %q", got)
	}
	long := strings.Repeat("é", 300)
	if got := SanitizeSecureCLICredentialDescription(long); len([]rune(got)) != SecureCLICredentialDescriptionMaxLen {
		t.Fatalf("expected %d runes, got %d", SecureCLICredentialDescriptionMaxLen, len([]rune(got)))
	}
	if got := SanitizeSecureCLICredentialDescription("  hi  "); got != "hi" {
		t.Fatalf("expected trimmed, got %q", got)
	}
}

func TestMergeSecureCLICredentialLabels_UserShadowsAgentAndSorts(t *testing.T) {
	b1 := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	b2 := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	rows := []SecureCLICredentialLabel{
		{BinaryID: b2, Label: "z", Source: "agent"},
		{BinaryID: b1, Label: "mac", Description: "agent mac", Source: "agent"},
		{BinaryID: b1, Label: "mac", Description: "user mac", Source: "user"},
		{BinaryID: b1, Label: "", Source: "agent"},
		{BinaryID: b1, Label: "vm", Source: "user"},
		{BinaryID: b1, Label: "vm", Source: "agent"},
	}
	got := MergeSecureCLICredentialLabels(rows)
	if len(got) != 4 {
		t.Fatalf("expected 4 rows, got %d: %+v", len(got), got)
	}
	want := []struct {
		b      uuid.UUID
		l, src string
	}{
		{b1, "", "agent"}, {b1, "mac", "user"}, {b1, "vm", "user"}, {b2, "z", "agent"},
	}
	for i, w := range want {
		if got[i].BinaryID != w.b || got[i].Label != w.l || got[i].Source != w.src {
			t.Errorf("row %d: got %+v want %+v", i, got[i], w)
		}
	}
	if got[1].Description != "user mac" {
		t.Errorf("user row should shadow agent row, got %q", got[1].Description)
	}
}

func strPtr(s string) *string { return &s }

func TestApplyLabeledCredential_ClearsDefaultUserFields(t *testing.T) {
	b := &SecureCLIBinary{}
	b.SetEffectiveCredential([]byte(`{"DEFAULT":"user"}`), nil, nil, "user", "u-1")
	if string(b.UserEnv) == "" {
		t.Fatal("precondition: default user env must be set")
	}

	b.ApplyLabeledCredential(&SecureCLIResolvedCredential{
		Label:          "design-mac",
		Source:         "agent",
		Env:            []byte(`{"token":"agent"}`),
		CredentialType: strPtr("pat"),
		HostScope:      strPtr("example.com"),
	})

	if b.UserEnv != nil || b.UserCredentialType != nil || b.UserHostScope != nil {
		t.Fatalf("default user material survived: env=%q type=%v host=%v", b.UserEnv, b.UserCredentialType, b.UserHostScope)
	}
	if b.CredentialSubjectID != "" {
		t.Fatalf("subject id should be cleared, got %q", b.CredentialSubjectID)
	}
	if b.CredentialSource != "agent" || string(b.CredentialEnv) != `{"token":"agent"}` {
		t.Fatalf("labeled credential not applied: source=%q env=%q", b.CredentialSource, b.CredentialEnv)
	}
	if b.CredentialType == nil || *b.CredentialType != "pat" {
		t.Fatalf("credential type not applied: %v", b.CredentialType)
	}
}

func TestApplyLabeledCredential_UserSourceSetsUserFields(t *testing.T) {
	b := &SecureCLIBinary{}
	b.ApplyLabeledCredential(&SecureCLIResolvedCredential{Source: "user", SubjectID: "u-1", Env: []byte(`{"a":"b"}`)})
	if string(b.UserEnv) != `{"a":"b"}` || b.CredentialSubjectID != "u-1" || b.CredentialSource != "user" {
		t.Fatalf("unexpected state: %+v", b)
	}
}

func TestApplyLabeledCredential_ContextEnvBecomesBase(t *testing.T) {
	b := &SecureCLIBinary{EncryptedEnv: []byte(`{"base":"1"}`)}
	b.SetEffectiveCredential([]byte(`{"ctx":"1"}`), nil, nil, "context", "chat:1")
	b.ApplyLabeledCredential(&SecureCLIResolvedCredential{Source: "agent", Env: []byte(`{"t":"x"}`)})
	if string(b.EncryptedEnv) != `{"ctx":"1"}` {
		t.Fatalf("context env should become base env, got %q", b.EncryptedEnv)
	}
	if b.UserEnv != nil {
		t.Fatalf("context-derived user env must be cleared, got %q", b.UserEnv)
	}
}

func TestApplyLabeledCredential_NilSafe(t *testing.T) {
	var b *SecureCLIBinary
	b.ApplyLabeledCredential(&SecureCLIResolvedCredential{})
	(&SecureCLIBinary{}).ApplyLabeledCredential(nil)
}
